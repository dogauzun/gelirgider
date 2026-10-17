// Package csv parses a transactions CSV (a gelirgider export or a broker's
// spreadsheet) into a statement.Statement. It is pure: records in, result
// out. The caller splits the file into records, so this package never sees a
// separator, a BOM or a quote.
//
// Parsing is partial by design (ADR 0006): a row that cannot be read becomes a
// statement.ParseError and the next row is parsed anyway. The returned errors
// are "not a csv statement", when the header has no date or type column, and
// "no rows", when no data row follows it: such a file has no period, so there
// is nothing to preview or record. A ParseError.Message is a fixed
// string and never quotes a cell.
//
// A CSV prints no period, so the Statement's is left empty: the parse handler
// derives it from the rows' dates, which needs the clock.
package csv

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

func init() {
	statement.Parsers["csv"] = Parse
}

// errNotCSV is the "not this broker's statement" case of ADR 0006.
var errNotCSV = errors.New("not a csv statement")

// errNoRows is a header with nothing under it.
var errNoRows = errors.New("no rows")

// headerAliases maps a lower-cased header cell to its field.
var headerAliases = map[string]string{
	"date": "date", "tarih": "date",
	"type": "type", "tür": "type", "işlem": "type", "islem": "type",
	"symbol": "symbol", "sembol": "symbol", "kod": "symbol",
	"quantity": "quantity", "adet": "quantity", "miktar": "quantity",
	"price": "price", "fiyat": "price",
	"amount": "amount", "tutar": "amount",
	"fee": "fee", "komisyon": "fee",
	"currency": "currency", "para birimi": "currency", "döviz": "currency",
	"note": "note", "not": "note", "açıklama": "note",
	"market": "market", "piyasa": "market", "borsa": "market",
}

// typeAliases maps a lower-cased type cell to its row kind.
var typeAliases = map[string]string{
	"buy": transaction.TypeBuy, "alış": transaction.TypeBuy, "alis": transaction.TypeBuy, "al": transaction.TypeBuy,
	"sell": transaction.TypeSell, "satış": transaction.TypeSell, "satis": transaction.TypeSell, "sat": transaction.TypeSell,
	"dividend": transaction.TypeDividend, "temettü": transaction.TypeDividend, "temettu": transaction.TypeDividend,
	"interest": "interest", "faiz": "interest", "nema": "interest",
	"deposit": transaction.TypeDeposit, "yatırma": transaction.TypeDeposit, "para yatırma": transaction.TypeDeposit, "yatirma": transaction.TypeDeposit,
	"withdrawal": transaction.TypeWithdrawal, "çekme": transaction.TypeWithdrawal, "para çekme": transaction.TypeWithdrawal, "cekme": transaction.TypeWithdrawal,
	"fee": transaction.TypeFee, "ücret": transaction.TypeFee, "masraf": transaction.TypeFee,
}

// marketAliases maps a lower-cased market cell to its market code: the codes
// the export writes and the market names the app shows, in tr and en.
var marketAliases = map[string]string{
	"bist": "BIST", "borsa istanbul": "BIST",
	"us": "US", "abd": "US",
	"tefas":  "TEFAS",
	"crypto": "CRYPTO", "kripto": "CRYPTO",
	"gold": "GOLD", "altın": "GOLD", "altin": "GOLD",
}

// lookup finds a cell in an alias map. Turkish lower-casing reads "İşlem" and
// "ALIŞ" right but turns an English "PRICE" into "prıce", so the plain
// lower-casing is tried as well.
func lookup(m map[string]string, cell string) (string, bool) {
	s := strings.TrimSpace(cell)
	if v, ok := m[strings.ToLowerSpecial(unicode.TurkishCase, s)]; ok {
		return v, true
	}
	v, ok := m[strings.ToLower(s)]
	return v, ok
}

// Parse turns CSV records into a Statement. rows[0] is the header.
func Parse(rows [][]string) (statement.Statement, error) {
	if len(rows) == 0 {
		return statement.Statement{}, errNotCSV
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		if f, ok := lookup(headerAliases, h); ok {
			if _, dup := col[f]; !dup {
				col[f] = i
			}
		}
	}
	if _, ok := col["date"]; !ok {
		return statement.Statement{}, errNotCSV
	}
	if _, ok := col["type"]; !ok {
		return statement.Statement{}, errNotCSV
	}

	st := statement.Statement{Broker: "csv"}
	currencies := map[string]int{}
	n := 0
	for _, row := range rows[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		n++
		parseRow(&st, col, row, currencies)
	}
	if n == 0 {
		return statement.Statement{}, errNoRows
	}
	st.Currency = majority(currencies)
	return st, nil
}

func parseRow(st *statement.Statement, col map[string]int, row []string, currencies map[string]int) {
	cell := func(field string) string {
		i, ok := col[field]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	date, err := parseDate(cell("date"))
	if err != nil {
		st.BadDate("CSV", row)
		return
	}
	kind, ok := lookup(typeAliases, cell("type"))
	if !ok {
		st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "unknown_type", Value: cell("type"), Message: "unknown type", Date: date})
		return
	}

	trade := kind == transaction.TypeBuy || kind == transaction.TypeSell
	nums := map[string]decimal.Decimal{}
	longAmount := false
	for _, f := range []string{"quantity", "price", "amount", "fee"} {
		if cell(f) == "" {
			continue
		}
		n, err := parseNumber(cell(f))
		// A trade amount past the digit cap is read on, and kept below only
		// if it is quantity × price, like the transaction API:
		// the export writes that product as stored.
		if err == errTooLong && f == "amount" && trade {
			longAmount, err = true, nil
		}
		if err != nil {
			if err == errAmbiguous {
				st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "ambiguous_number", Value: f, Message: "ambiguous thousands separator", Date: date})
				return
			}
			st.BadNumber("CSV", row, f, date)
			return
		}
		nums[f] = n
	}
	missing := func(fields ...string) bool {
		for _, f := range fields {
			if _, ok := nums[f]; !ok {
				st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "missing_field", Value: f, Message: "missing field", Date: date})
				return true
			}
		}
		return false
	}

	// An empty currency cell stays empty on a trade or a dividend: the parse
	// handler takes the currency of the instrument the row resolves to.
	// A cash row has no instrument, so it is TRY.
	currency := strings.ToUpper(cell("currency"))
	sym := symbol(cell("symbol"))
	market, known := marketHint(cell("market"))

	switch {
	case !known:
		st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "unknown_market", Value: cell("market"), Message: "unknown market", Date: date})
		return
	case trade:
		if sym == "" {
			st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "missing_field", Value: "symbol", Message: "missing field", Date: date})
			return
		}
		if missing("quantity", "price") {
			return
		}
		amount, ok := nums["amount"]
		if !ok {
			amount = nums["quantity"].Mul(nums["price"])
		}
		if longAmount && !amount.Equal(nums["quantity"].Mul(nums["price"])) {
			st.BadNumber("CSV", row, "amount", date)
			return
		}
		st.Trades = append(st.Trades, statement.Trade{
			Date: date, Symbol: sym, Side: kind, Quantity: nums["quantity"], Price: nums["price"],
			Amount: amount, Fee: nums["fee"], Currency: currency, Market: market,
		})
	case kind == transaction.TypeDividend && sym != "":
		if missing("amount") {
			return
		}
		st.Dividends = append(st.Dividends, statement.Dividend{
			Date: date, Symbol: sym, Gross: nums["amount"], Tax: nums["fee"], Currency: currency, Market: market,
		})
	default:
		if missing("amount") {
			return
		}
		// A fee row's amount is the fee; the commit refuses a second fee on
		// top of it, so the row is flagged here instead of failing there.
		if kind == transaction.TypeFee && !nums["fee"].IsZero() {
			st.Errors = append(st.Errors, statement.ParseError{Section: "CSV", Row: row, Code: "fee_on_fee", Message: "a fee row cannot carry a fee", Date: date})
			return
		}
		if kind == "interest" {
			kind = transaction.TypeDividend
		}
		if currency == "" {
			currency = "TRY"
		}
		st.Cash = append(st.Cash, statement.CashMovement{
			Date: date, Kind: kind, Amount: nums["amount"], Fee: nums["fee"], Currency: currency,
		})
	}
	// A row without a currency does not vote: its instrument decides it.
	if currency != "" {
		currencies[currency]++
	}
}

// marketHint reads a market cell through marketAliases. An empty cell is no
// hint; any other value must name a market, or the row would silently resolve
// to nothing, so ok is false for it.
func marketHint(cell string) (string, bool) {
	if cell == "" {
		return "", true
	}
	return lookup(marketAliases, cell)
}

// majority is the most frequent row currency; a tie, or no rows, is TRY.
func majority(counts map[string]int) string {
	best, bestN, tie := "TRY", 0, false
	for c, n := range counts {
		switch {
		case n > bestN:
			best, bestN, tie = c, n, false
		case n == bestN:
			tie = true
		}
	}
	if tie {
		return "TRY"
	}
	return best
}

var dateLayouts = []string{time.DateOnly, "02.01.2006", "02/01/2006"}

// parseDate reads a market date as YYYY-MM-DD; there is no time zone to
// convert.
func parseDate(cell string) (string, error) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, cell); err == nil {
			return t.Format(time.DateOnly), nil
		}
	}
	return "", errors.New("unreadable date")
}

var (
	errAmbiguous = errors.New("ambiguous thousands separator")
	// errTooLong is a plain number past DecimalPattern's digit cap but
	// within longShape. parseNumber returns it with the value.
	errTooLong = errors.New("too many digits")
	// longShape bounds quantity × price of two numbers within the cap: at
	// most 15+15 integer and 9+9 fraction digits.
	longShape = regexp.MustCompile(`^\d{1,30}(\.\d{1,18})?$`)
	// ambiguousShape is one kind of separator in thousands shape: "1.000",
	// "1,000" or "1.000.000" may be grouping or a decimal point, so it is not
	// read at all, the same rule as the transaction form.
	ambiguousShape = regexp.MustCompile(`^[1-9]\d{0,2}((\.\d{3})+|(,\d{3})+)$`)
)

// parseNumber reads "1234.56", "1234,56" and "1.234,56": a comma after the
// last dot is the decimal point and the dots are thousands; a comma before
// the last dot ("1,234.56") is a thousands separator. With both, the
// thousands groups must be three digits ("1.2.3,4" is unreadable). The
// result must have the shape the transaction API accepts: no sign, no
// exponent, capped digits.
// Unchecked, "1e2000000000" panics on Mul and "1e3000000" prints 3 MB. A
// number in longShape but past the cap is returned with errTooLong.
func parseNumber(cell string) (decimal.Decimal, error) {
	if ambiguousShape.MatchString(cell) {
		return decimal.Decimal{}, errAmbiguous
	}
	s := cell
	if c := strings.LastIndex(s, ","); c >= 0 {
		sep, mark := ",", "."
		if c > strings.LastIndex(s, ".") {
			sep, mark = ".", ","
		}
		// With both separators the thousands groups must be three digits,
		// or "1.2.3,4" would be read as 123.4.
		if strings.Contains(s, ".") && !statement.Grouped(s, sep, mark) {
			return decimal.Decimal{}, errors.New("unreadable number")
		}
		s = strings.ReplaceAll(s, sep, "")
		s = strings.ReplaceAll(s, ",", ".")
	}
	if !transaction.DecimalPattern.MatchString(s) {
		if longShape.MatchString(s) {
			d, err := decimal.NewFromString(s)
			if err == nil {
				err = errTooLong
			}
			return d, err
		}
		return decimal.Decimal{}, errors.New("unreadable number")
	}
	return decimal.NewFromString(s)
}

// symbol upper-cases a ticker and drops the BIST bulletin suffix like Midas.
// Plain upper-casing, not Turkish: tickers are ASCII, and Turkish casing
// would turn a lower-case "sise" into "SİSE".
func symbol(cell string) string {
	return bist.TrimBoardSuffix(strings.ToUpper(cell))
}
