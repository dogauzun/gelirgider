// Package garanti parses the monthly Garanti BBVA "YATIRIM EKSTRESİ", the
// fund balances and fund buys and sells of the bank's investment account,
// into a statement.Statement. It is pure: rows in, result out, no database,
// no HTTP, no user, no clock.
//
// pdftext.Rows returns one row per page with hundreds of cells for this PDF,
// so the rows are flattened into one cell stream and records are matched by
// cell patterns, never by row or page boundaries. A section title switches
// the state; the legal text every page repeats is dropped.
//
// Movements are netted per (date, fund, price) and the money they move is
// booked as one deposit or withdrawal per day, so portfolio cash ends each
// day where it started.
//
// Parsing is partial by design (ADR 0006): a record that cannot be read
// becomes a statement.ParseError. Its Value and Row only ever carry cells
// from a section, never the customer cells above the first section title.
// The only returned error is "not a garanti fund statement".
package garanti

import (
	"errors"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/shopspring/decimal"
)

const broker = "garanti"

func init() {
	statement.Parsers[broker] = Parse
}

// Section titles. Each one switches the section until the next title.
const (
	secAccount  = "YATIRIM İŞLEMLERİNDE KULLANILAN HESAP BAKİYELERİ"
	secBalances = "YATIRIM FONU BAKİYELERİ"
	secMoves    = "YATIRIM FON HAREKETLERİ"
)

// Section names in ParseError.
const (
	holdingsSection  = "holdings"
	movementsSection = "movements"
)

// boilerplate starts the 5 legal-text cells every page carries, with its
// "Sayfa n / m" among them.
const boilerplate = "Ekstrenizde yalnızca "

var (
	headerRow   = regexp.MustCompile(`^(OCAK|ŞUBAT|MART|NİSAN|MAYIS|HAZİRAN|TEMMUZ|AĞUSTOS|EYLÜL|EKİM|KASIM|ARALIK) (\d{4}) YATIRIM EKSTRESİ$`)
	months      = []string{"OCAK", "ŞUBAT", "MART", "NİSAN", "MAYIS", "HAZİRAN", "TEMMUZ", "AĞUSTOS", "EYLÜL", "EKİM", "KASIM", "ARALIK"}
	accountNo   = regexp.MustCompile(`^\d{4}-\d+$`)
	fundCode    = regexp.MustCompile(`^[A-Z0-9]{3}$`)
	dateShape   = regexp.MustCompile(`^\d{2}\.\d{2}\.\d{4}$`)
	timeShape   = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)
	numberShape = regexp.MustCompile(`^(-\s*)?[\d.]*\d(,\d+)?$`)
	currencies  = map[string]string{"TL": "TRY", "USD": "USD", "EUR": "EUR"}
	// balanceHeader are the column header cells of the balances table.
	balanceHeader = map[string]bool{"Hesap": true, "Fon Adı": true, "Fon": true, "Kodu": true, "Para": true, "Birimi": true, "Adet": true, "Fiyat": true, "Tutar(YP)": true, "Tutar(TL)": true}
)

// funds indexes the balance records' normalized names, so code matches a
// movement's name in time linear in its length, not in the records
// (GLG-1270). names is sorted and distinct; codes[i] is the code of
// names[i], "" when its records carry two codes; next[i] is the first index
// after i whose code differs from codes[i].
type funds struct {
	names, codes []string
	next         []int
}

// movement is one readable ALIŞ or SATIŞ, its units and Tutar signed.
type movement struct {
	date, name    string
	units, price  decimal.Decimal
	stopaj, gross decimal.Decimal
}

// Parse turns the text rows of a Garanti BBVA fund statement PDF into a
// Statement.
func Parse(rows [][]string) (statement.Statement, error) {
	var cells []string
	for _, row := range rows {
		for _, cell := range row {
			cells = append(cells, strings.TrimSpace(cell))
		}
	}
	st := statement.Statement{Broker: broker, Currency: "TRY", Market: "TEFAS"}
	for _, c := range cells {
		if m := headerRow.FindStringSubmatch(c); m != nil {
			year, _ := strconv.Atoi(m[2])
			month := time.Month(slices.Index(months, m[1]) + 1)
			st.PeriodStart = time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
			st.PeriodEnd = time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
			break
		}
	}
	if st.PeriodStart == "" {
		return statement.Statement{}, errors.New("not a garanti fund statement")
	}
	// Drop the legal text, so a record split by a page break still reads.
	var kept []string
	for i := 0; i < len(cells); i++ {
		if strings.HasPrefix(cells[i], boilerplate) {
			i += 4
			continue
		}
		kept = append(kept, cells[i])
	}
	cells = kept
	at := func(i int) string {
		if i < len(cells) {
			return cells[i]
		}
		return ""
	}
	isTitle := func(c string) bool { return c == secAccount || c == secBalances || c == secMoves }
	isAnchor := func(i int) bool { return dateShape.MatchString(at(i)) && timeShape.MatchString(at(i+1)) }

	fundCodes := map[string]string{} // a balance record's name to its code
	holding := map[string]int{}      // a code to its index in st.Holdings
	var moves []movement
	var pending []string // the balance record's cells read so far
	section := ""
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		if isTitle(c) {
			section, pending = c, nil
			continue
		}
		switch section {
		case secBalances:
			switch {
			case c == "TOPLAM":
				section = ""
			case fundCode.MatchString(c) && currencies[at(i+1)] != "":
				i = parseBalance(&st, cells, i, pending, fundCodes, holding) - 1
				pending = nil
			case balanceHeader[c] || accountNo.MatchString(c):
			default:
				pending = append(pending, c)
			}
		case secMoves:
			if !isAnchor(i) {
				continue
			}
			j := i + 2
			var lead, nums []string
			for j < len(cells) && len(lead) < 4 && !numberShape.MatchString(cells[j]) && !isAnchor(j) && !isTitle(cells[j]) {
				lead = append(lead, cells[j])
				j++
			}
			for j < len(cells) && len(nums) < 4 && numberShape.MatchString(cells[j]) && !isAnchor(j) {
				nums = append(nums, cells[j])
				j++
			}
			if m, ok := parseMovement(&st, cells[i:j], lead, nums); ok {
				moves = append(moves, m)
			}
			i = j - 1
		}
	}
	net(&st, moves, index(fundCodes))
	return st, nil
}

// parseBalance reads the balance record whose code is cells[i] and whose
// currency is cells[i+1], names being its name cells read before the code,
// into st, fundCodes and holding. It returns the index of the first cell
// after the record.
func parseBalance(st *statement.Statement, cells []string, i int, names []string, fundCodes map[string]string, holding map[string]int) int {
	c := cells[i]
	j := i + 2
	for j < len(cells) && numberShape.MatchString(cells[j]) {
		j++
	}
	rec := append(append([]string{}, names...), cells[i:j]...)
	if len(names) < 1 || len(names) > 2 || j < i+5 {
		st.Errors = append(st.Errors, statement.ParseError{Section: holdingsSection, Row: rec, Code: "bad_row", Message: "unreadable fund balance"})
		return j
	}
	units, err := parseNumber(cells[i+2])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: holdingsSection, Row: rec, Code: "bad_number", Value: "quantity", Message: "unreadable quantity"})
		return j
	}
	// The Fiyat column; an unreadable one only costs the opening position
	// its statement price (GLG-737).
	price, _ := parseNumber(cells[i+3])
	name := normalize(strings.Join(names, " "))
	// A name listed with two codes matches neither.
	if prev, ok := fundCodes[name]; ok && prev != c {
		fundCodes[name] = ""
	} else {
		fundCodes[name] = c
	}
	// A fund held in two accounts is listed once per account: the snapshot
	// holds it once, with the units summed.
	if k, ok := holding[c]; ok {
		st.Holdings[k].Quantity = st.Holdings[k].Quantity.Add(units)
	} else {
		holding[c] = len(st.Holdings)
		st.Holdings = append(st.Holdings, statement.Holding{Symbol: c, Quantity: units, Currency: currencies[cells[i+1]], Price: price, Name: name})
	}
	return j
}

// index sorts the balance records' names, mapped to their code, into funds.
func index(fundCodes map[string]string) funds {
	f := funds{names: slices.Sorted(maps.Keys(fundCodes))}
	f.codes = make([]string, len(f.names))
	f.next = make([]int, len(f.names))
	for i, name := range f.names {
		f.codes[i] = fundCodes[name]
	}
	for i := len(f.names) - 1; i >= 0; i-- {
		f.next[i] = i + 1
		if i+1 < len(f.names) && f.codes[i+1] == f.codes[i] {
			f.next[i] = f.next[i+1]
		}
	}
	return f
}

// parseMovement reads one record: date, time, 1-2 name cells, the type, an
// optional detail cell, then Adet, Fiyat, Stopaj, Tutar.
func parseMovement(st *statement.Statement, rec, lead, nums []string) (movement, bool) {
	date := ""
	if t, err := time.Parse("02.01.2006", rec[0]); err == nil {
		date = t.Format(time.DateOnly)
	}
	if len(nums) != 4 || len(lead) < 2 {
		st.Errors = append(st.Errors, statement.ParseError{Section: movementsSection, Row: rec, Code: "bad_row", Message: "incomplete fund movement", Date: date})
		return movement{}, false
	}
	k := 1
	for k < len(lead) && lead[k] != "ALIŞ" && lead[k] != "SATIŞ" {
		k++
	}
	if k == len(lead) {
		// The type is the last cell, or the one before a NEMA detail.
		kind := lead[len(lead)-1]
		if kind == "NEMA" && len(lead) > 2 {
			kind = lead[len(lead)-2]
		}
		st.Errors = append(st.Errors, statement.ParseError{Section: movementsSection, Row: rec, Code: "unknown_type", Value: kind, Message: "unknown fund movement type " + kind, Date: date})
		return movement{}, false
	}
	if k > 2 || len(lead)-k-1 > 1 {
		st.Errors = append(st.Errors, statement.ParseError{Section: movementsSection, Row: rec, Code: "bad_row", Message: "unreadable fund movement", Date: date})
		return movement{}, false
	}
	if date == "" || date < st.PeriodStart || date > st.PeriodEnd {
		st.Errors = append(st.Errors, statement.ParseError{Section: movementsSection, Row: rec, Code: "bad_date", Value: rec[0], Message: "date outside the statement period"})
		return movement{}, false
	}
	// A negative stopaj is a withholding refund, one cell "-   9,87" that
	// numberShape lets through. A trade's fee is never
	// negative, so the trade books none and the day's cash leaves the refund
	// out.
	f := [4]string(nums)
	stopaj, refund := strings.CutPrefix(f[2], "-")
	f[2] = strings.TrimSpace(stopaj)
	var n [4]decimal.Decimal
	for x, field := range []string{"quantity", "price", "fee", "amount"} {
		v, err := parseNumber(f[x])
		if err != nil {
			st.Errors = append(st.Errors, statement.ParseError{Section: movementsSection, Row: rec, Code: "bad_number", Value: field, Message: "unreadable " + field, Date: date})
			return movement{}, false
		}
		n[x] = v
	}
	m := movement{date: date, name: normalize(strings.Join(lead[:k], " ")), units: n[0], price: n[1], stopaj: n[2], gross: n[3]}
	if refund {
		m.stopaj = decimal.Zero
	}
	if lead[k] == "SATIŞ" {
		m.units, m.gross = m.units.Neg(), m.gross.Neg()
	}
	return m, true
}

// net groups the movements by (date, fund, price) into one trade, or one fee
// when the units net to zero, and books each day's cash effect as one
// deposit or withdrawal.
func net(st *statement.Statement, moves []movement, funds funds) {
	type group struct {
		date, symbol         string
		price                decimal.Decimal
		units, gross, stopaj decimal.Decimal
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, m := range moves {
		symbol := funds.code(m.name)
		key := m.date + "\x00" + symbol + "\x00" + m.price.String()
		g := byKey[key]
		if g == nil {
			g = &group{date: m.date, symbol: symbol, price: m.price}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.units = g.units.Add(m.units)
		g.gross = g.gross.Add(m.gross)
		g.stopaj = g.stopaj.Add(m.stopaj)
	}
	var dates []string
	effect := map[string]decimal.Decimal{}
	fees := map[string][]statement.CashMovement{}
	for _, g := range groups {
		if _, ok := effect[g.date]; !ok {
			dates = append(dates, g.date)
		}
		e := effect[g.date]
		switch {
		case !g.units.IsZero():
			// A buy pays the gross out of cash, a sell brings it in.
			side, cash := "sell", g.gross.Abs()
			if g.units.IsPositive() {
				side, cash = "buy", cash.Neg()
			}
			e = e.Add(cash).Sub(g.stopaj)
			st.Trades = append(st.Trades, statement.Trade{Date: g.date, Symbol: g.symbol, Side: side, Quantity: g.units.Abs(), Price: g.price, Amount: g.gross.Abs(), Fee: g.stopaj, Currency: "TRY", Market: "TEFAS"})
		case g.stopaj.IsPositive():
			e = e.Sub(g.stopaj)
			fees[g.date] = append(fees[g.date], statement.CashMovement{Date: g.date, Kind: "fee", Amount: g.stopaj, Currency: "TRY"})
		}
		effect[g.date] = e
	}
	for _, date := range dates {
		st.Cash = append(st.Cash, fees[date]...)
		e := effect[date]
		switch {
		case e.IsNegative():
			st.Cash = append(st.Cash, statement.CashMovement{Date: date, Kind: "deposit", Amount: e.Neg(), Currency: "TRY"})
		case e.IsPositive():
			st.Cash = append(st.Cash, statement.CashMovement{Date: date, Kind: "withdrawal", Amount: e, Currency: "TRY"})
		}
	}
}

// code maps a movement's fund name to the code of the one fund whose
// balance record names equal it or are a prefix of it, or it of that name
// (movement names are cut around 30 characters); a fund held in two
// accounts has two such records with one code. Otherwise the name itself is
// the symbol, which the preview shows as an unknown instrument.
//
// It walks name a byte at a time, keeping [lo, hi) the names that start
// with name[:d]: names[lo] equals name[:d] when it is d bytes long.
func (f funds) code(name string) string {
	match := ""
	take := func(c string) bool {
		if c == "" || match != "" && match != c {
			return false
		}
		match = c
		return true
	}
	lo, hi := 0, len(f.names)
	for d := 0; lo < hi; d++ {
		if len(f.names[lo]) == d {
			if !take(f.codes[lo]) {
				return name
			}
			lo++
		}
		if d == len(name) {
			// [lo, hi) are the longer names that start with name.
			if lo < hi && (f.next[lo] < hi || !take(f.codes[lo])) {
				return name
			}
			break
		}
		b := name[d]
		l := lo + sort.Search(hi-lo, func(k int) bool { return f.names[lo+k][d] >= b })
		hi = l + sort.Search(hi-l, func(k int) bool { return f.names[l+k][d] > b })
		lo = l
	}
	if match == "" {
		return name
	}
	return match
}

// normalize upper-cases a fund name the Turkish way and collapses its
// whitespace.
func normalize(name string) string {
	return strings.Join(strings.Fields(strings.ToUpperSpecial(unicode.TurkishCase, name)), " ")
}

// parseNumber reads the statement's Turkish format: "." thousands, ","
// decimal ("89.997,72", "33.070", "2,721431"), see statement.ParseNumber.
// The statement's numbers carry no sign (ADR 0018), so a signed cell is
// unreadable (GLG-1304); the stopaj refund's sign is cut before the call.
func parseNumber(cell string) (decimal.Decimal, error) {
	if strings.ContainsAny(cell, "+-") {
		return decimal.Decimal{}, errors.New("unreadable number")
	}
	return statement.ParseNumber(cell, ".", ",")
}
