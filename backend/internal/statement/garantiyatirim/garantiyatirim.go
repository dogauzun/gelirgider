// Package garantiyatirim parses the monthly Garanti BBVA Yatırım brokerage
// statement of a BIST share account into a statement.Statement. It is pure:
// rows in, result out, no database, no HTTP, no user, no clock.
//
// pdftext.Rows returns one row per page with hundreds of cells for this PDF,
// so the rows are flattened into one cell stream and records are matched by
// cell patterns, never by row or page boundaries. Every page repeats the
// broker header, the statement date and the customer's name and address; a
// page title ("Hesap Durumu", "Menkul Kıymet Hareket Listesi", "Hesap
// Ekstresi") switches the section.
//
// Parsing is partial by design (ADR 0006): a record that cannot be read
// becomes a statement.ParseError. Its Value only ever carries a field key,
// a ledger description or a date cell, never the repeated customer cells;
// an IPO refund's description starts with the customer's name, so it is
// replaced by a fixed one.
// The only returned error is "not a garanti_yatirim statement".
package garantiyatirim

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

const broker = "garanti_yatirim"

func init() {
	statement.Parsers[broker] = Parse
}

// Page titles. Each one switches the section until the next title.
const (
	secStatus = "Hesap Durumu"
	secMoves  = "Menkul Kıymet Hareket Listesi"
	secLedger = "Hesap Ekstresi"
)

// Section names in Skipped and ParseError.
const (
	holdingsSection = "holdings"
	ledgerSection   = "ledger"
	tradesSection   = "trades"
)

// codeBadNumber is the ParseError code of a number cell that is not valid.
const codeBadNumber = "bad_number"

// kindVirman is the trade kind of a share transfer in.
const kindVirman = "HS-Virman Giriş"

// numberFormat is how a statement prints its number cells. Most statements
// use US format ("1,234.56", quantity "150.000"); an on-request copy uses
// Turkish format ("1.234,56", quantity "150,000") in its cells while the
// prices inside a ledger description stay US.
type numberFormat struct {
	// number is any cell that looks like a number in this format. Whether
	// it is a readable one is parseNumber's job.
	number *regexp.Regexp
	// signed is the ledger's İşlem amount, always printed with a sign; it
	// tells a ledger record from the repeated page header that also starts
	// with a date.
	signed          *regexp.Regexp
	thousands, mark string
}

var (
	usFormat = numberFormat{
		number:    regexp.MustCompile(`^[+-]?[\d,]*\.?\d+$`),
		signed:    regexp.MustCompile(`^[+-][\d,]*\.?\d+$`),
		thousands: ",", mark: ".",
	}
	trFormat = numberFormat{
		number:    regexp.MustCompile(`^[+-]?[\d.]*,?\d+$`),
		signed:    regexp.MustCompile(`^[+-][\d.]*,?\d+$`),
		thousands: ".", mark: ",",
	}
	// usCell and trCell are a number with a decimal part, unambiguous in
	// one format; they decide the statement's format.
	usCell = regexp.MustCompile(`^[+-]?\d{1,3}(,\d{3})*\.\d{2,3}$`)
	trCell = regexp.MustCompile(`^[+-]?\d{1,3}(\.\d{3})*,\d{2,3}$`)

	dateShape = regexp.MustCompile(`^\d{2}/\d{2}/\d{4}$`)
	assetType = regexp.MustCompile(`^[A-Z]{2}$`)
	symbol    = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

	openingRow = regexp.MustCompile(`^Devir .* Cari TRY Bakiye Hareketleri$`)
	// tradeRow is a buy (HA) or a sell (HS): trade date, side, price,
	// quantity, symbol. Statements from 2018 print a capital letter before
	// the reference digits.
	tradeRow = regexp.MustCompile(`^(\d{2}/\d{2}/\d{4}) Ref = [A-Z]?\d+ (HA|HS) ([\d.]+)x([\d.]+) (\S+)$`)
	// commissionRow ends in "Komisyon" before May 2024 and in "Komisyon
	// Tutarı ve BSMV" after; Matrah is the traded amount it is charged on.
	commissionRow = regexp.MustCompile(`^Matrah ([\d.]+) Oran [\d.]+ Komisyon( Tutarı ve BSMV)?$`)
	// transferRow moves cash between the bank and the brokerage account:
	// a deposit when positive, a withdrawal when negative. 2018 statements
	// move a dividend to the bank with "Temettü Transferi işlemi".
	transferRow = regexp.MustCompile(`^(.* Nolu Hesabın Banka (Takas|MKK Masraf Transfer) İşlemi|Diğer Bakiye Transferi-.+|Temettü Transferi işlemi - \d+-\d+ No'lu Hesap)$`)
	// takasRow is the transferRow that settles the day's trades.
	takasRow = regexp.MustCompile(`^.* Nolu Hesabın Banka Takas İşlemi$`)
	// feeRow is an MKK, account-keeping or order-cancel charge: a fee when
	// negative, a refund (deposit) when positive.
	// dividendRow is a cash dividend paid on a share (.E) or an ETF (.F):
	// the payment when it carries no suffix, its transfer to the bank
	// account when it ends in "-<digits>-<digits>".
	dividendRow = regexp.MustCompile(`^([A-Z0-9]+)\.[EF] Temettü [\d.]+ Adet Temettü Oranı : [\d.]+(-\d+-\d+)?$`)
	feeRow      = regexp.MustCompile(`^(\p{Lu}+ \d{4} +MKK'ya Ödenen Ücret- .+|\d{4} +\d+\. Dönem-Hesap bakım ücreti tahsilatı|Borsa Emir İptal (Ücreti ve Vergiler|Ücreti ve Vergi-\d+|BSMV))$`)
	// ipoRow is the money for an IPO request of a quantity coming in from
	// the bank (positive) and blocked for the request (negative); the
	// allotment is the Hareket Listesi's buy, priced from this row.
	ipoRow = regexp.MustCompile(`^\d+ ([\d.,]+) adet ([A-Z][A-Z0-9]*) Halka Arz Tahsil$`)
	// refundRow is the part of an IPO request not allotted coming back
	// (positive) and returning to the bank (negative). Its description
	// starts with the customer's name, so it is never quoted.
	refundRow = regexp.MustCompile(`^.+ \d+ ([A-Z][A-Z0-9]*) için iade$`)
	// fractionRow is a fractional share cashed out: a sell of the fraction.
	fractionRow = regexp.MustCompile(`^([\d.]+) adet ([A-Z][A-Z0-9]*) bedeli kurum küsurat hesabından virman$`)
)

// Parse turns the text rows of a Garanti BBVA Yatırım statement PDF into a
// Statement.
func Parse(rows [][]string) (statement.Statement, error) {
	var cells []string
	website, status, us, tr := false, false, false, false
	for _, row := range rows {
		for _, cell := range row {
			c := strings.TrimSpace(cell)
			website = website || isWebsite(c)
			status = status || c == secStatus
			us = us || usCell.MatchString(c)
			tr = tr || trCell.MatchString(c)
			cells = append(cells, c)
		}
	}
	if !website || !status {
		return statement.Statement{}, errors.New("not a garanti_yatirim statement")
	}
	p := parser{cells: cells, f: usFormat, st: statement.Statement{Broker: broker, Currency: "TRY"}}
	if tr && !us {
		p.f = trFormat
	}

	var moves [][]string
	section := ""
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		switch c {
		case secStatus, secMoves, secLedger:
			section, p.inHoldings = c, false
			continue
		}
		if c == "Dönemi" && p.at(i+1) == ":" {
			if start, end, ok := period(p.at(i + 2)); ok && p.st.PeriodStart == "" {
				p.st.PeriodStart, p.st.PeriodEnd = start, end
			}
			i += 2
			continue
		}
		switch section {
		case secStatus:
			i += p.statusCell(i)
		case secMoves:
			if n := moveLen(cells, i, p.f); n > 0 {
				moves = append(moves, cells[i:i+n])
				i += n - 1
			}
		case secLedger:
			i += p.ledgerCell(i)
		}
	}
	st := p.st
	if st.PeriodStart == "" && p.statementDate != "" {
		st.PeriodStart, st.PeriodEnd = p.statementDate[:8]+"01", p.statementDate
	}
	// The trade list is read once the ledger is, which prices an IPO
	// allotment and books a fractional share's cash-out.
	requests, cashOuts := ipoRequests(p.ledger, p.f), fractionCashOuts(p.ledger, p.f)
	for _, rec := range moves {
		parseMove(&st, rec, p.f, requests, cashOuts)
	}
	parseLedger(&st, p.ledger, p.f)
	return st, nil
}

// parser is Parse's state while it walks the cells in format f: the
// statement read so far, the Hesap Durumu date, whether the cells are in
// the holdings table, and the ledger records for parseLedger.
type parser struct {
	cells         []string
	f             numberFormat
	st            statement.Statement
	statementDate string
	inHoldings    bool
	ledger        [][]string
}

// at is cells[i], or "" past the end.
func (p *parser) at(i int) string {
	if i < len(p.cells) {
		return p.cells[i]
	}
	return ""
}

// statusCell reads the Hesap Durumu cell at i and returns how many cells
// after it the record took.
func (p *parser) statusCell(i int) int {
	c, f := p.cells[i], p.f
	switch {
	case c == "Tarih" && p.at(i+1) == ":" && dateShape.MatchString(p.at(i+2)):
		if d, err := parseDate(p.at(i + 2)); err == nil && p.statementDate == "" {
			p.statementDate = d
		}
		return 2
	case c == "Cari Bakiye" && p.at(i+1) == "Mevcudu" && p.at(i+2) == "Rezervi (T + 1)" && f.number.MatchString(p.at(i+3)):
		if n, err := parseNumber(p.at(i+3), f); err != nil {
			p.st.BadNumber(holdingsSection, p.cells[i:i+4], "amount", "")
		} else if p.st.CashBalance == nil {
			p.st.CashBalance = &n
		}
		return 3
	case c == "MEVCUT MENKUL KIYMETLER":
		p.inHoldings = true
	case c == "Hisse toplamı :":
		p.inHoldings = false
	case p.inHoldings && holdingLen(p.cells, i, f) > 0:
		n := holdingLen(p.cells, i, f)
		parseHolding(&p.st, p.cells[i:i+n], f)
		return n - 1
	}
	return 0
}

// ledgerCell reads the Hesap Ekstresi cell at i and returns how many cells
// after it the record took.
func (p *parser) ledgerCell(i int) int {
	c, f := p.cells[i], p.f
	if !dateShape.MatchString(c) {
		return 0
	}
	text := func(s string) bool { return s != "" && !dateShape.MatchString(s) && !f.number.MatchString(s) }
	switch {
	case f.signed.MatchString(p.at(i+2)) && f.number.MatchString(p.at(i+3)):
		p.ledger = append(p.ledger, p.cells[i:i+4])
		return 3
	case text(p.at(i+1)) && text(p.at(i+2)) && f.signed.MatchString(p.at(i+3)) && f.number.MatchString(p.at(i+4)):
		// A long description wraps into a second cell.
		p.ledger = append(p.ledger, []string{c, p.at(i+1) + " " + p.at(i+2), p.at(i + 3), p.at(i + 4)})
		return 4
	case i > 0 && isWebsite(p.cells[i-1]):
		// The page header's date; customer cells follow it.
	default:
		// Never drop a ledger row silently, never quote the cells after
		// the date: they may be the customer block.
		date, _ := parseDate(c)
		p.st.Errors = append(p.st.Errors, statement.ParseError{Section: ledgerSection, Row: []string{c}, Code: "bad_row", Value: c, Message: "incomplete ledger row", Date: date})
	}
	return 0
}

// isWebsite is the broker's website cell every page repeats: the Garanti
// BBVA Yatırım one, or the Garanti Yatırım one of statements before the 2019
// rebrand.
func isWebsite(c string) bool {
	return c == "www.garantibbvayatirim.com.tr" || c == "www.garantiyatirim.com.tr"
}

// holdingLen is the cell count of the holding record at i, or 0: type,
// symbol, name, quantity, market value, reserved quantity. Some statements
// (April 2024) print no name cell. The legal-text word cells on
// page 1 never match.
func holdingLen(cells []string, i int, f numberFormat) int {
	if i+2 >= len(cells) || !assetType.MatchString(cells[i]) {
		return 0
	}
	n := 6
	if f.number.MatchString(cells[i+2]) {
		n = 5
	}
	if i+n > len(cells) {
		return 0
	}
	for _, c := range cells[i+n-3 : i+n] {
		if !f.number.MatchString(c) {
			return 0
		}
	}
	return n
}

func parseHolding(st *statement.Statement, rec []string, f numberFormat) {
	if rec[0] != "HS" {
		// A fixed reason the client maps to a translated text; the
		// asset code stays in the raw cells.
		st.Skipped = append(st.Skipped, statement.Skipped{Section: holdingsSection, Reason: "unsupported asset type", Row: rec})
		return
	}
	q := rec[len(rec)-3]
	quantity, err := parseNumber(q, f)
	if err != nil {
		st.BadNumber(holdingsSection, rec, "quantity", "")
		return
	}
	hold := statement.Holding{Symbol: rec[1], Quantity: quantity, Currency: "TRY"}
	// The price is market value / quantity, for an opening position the
	// portfolio has no stored price for; an unreadable value
	// leaves it zero.
	if value, err := parseNumber(rec[len(rec)-2], f); err == nil && quantity.IsPositive() {
		hold.Price = value.DivRound(quantity, 6)
	}
	st.Holdings = append(st.Holdings, hold)
}

// moveLen is the cell count of the Hareket Listesi record at i, or 0:
// symbol, name, settlement date, signed quantity, type, price. Some
// statements print no name cell, like their holdings. The page
// header's date is followed by customer cells, never by a number, so it
// never matches.
func moveLen(cells []string, i int, f numberFormat) int {
	if i+5 > len(cells) || !symbol.MatchString(cells[i]) {
		return 0
	}
	n := 5
	if !dateShape.MatchString(cells[i+1]) {
		n = 6
		if i+n > len(cells) || f.number.MatchString(cells[i+1]) || !dateShape.MatchString(cells[i+2]) {
			return 0
		}
	}
	quantity, kind, price := cells[i+n-3], cells[i+n-2], cells[i+n-1]
	if !f.number.MatchString(quantity) || kind == "" || f.number.MatchString(kind) || dateShape.MatchString(kind) || !f.number.MatchString(price) {
		return 0
	}
	return n
}

// ipoRequest is an IPO request read from the ledger: the quantity asked
// for, the money paid for it and the part of it refunded.
type ipoRequest struct {
	requested, paid, refund decimal.Decimal
}

// ipoRequests maps each symbol with a Halka Arz Tahsil ledger row to its
// request. Each amount is its first row's, unsigned: the money in and its
// blocking print the same amount, as do the refund and its return to the
// bank. An unreadable number stays zero; parseLedger reports its row.
func ipoRequests(ledger [][]string, f numberFormat) map[string]ipoRequest {
	requests, refunds := map[string]ipoRequest{}, map[string]decimal.Decimal{}
	for _, rec := range ledger {
		amount, _ := parseNumber(rec[2], f)
		if m := ipoRow.FindStringSubmatch(rec[1]); m != nil {
			if _, ok := requests[m[2]]; !ok {
				requested, _ := parseNumber(m[1], usFormat)
				requests[m[2]] = ipoRequest{requested: requested, paid: amount.Abs()}
			}
		} else if m := refundRow.FindStringSubmatch(rec[1]); m != nil && refunds[m[1]].IsZero() {
			refunds[m[1]] = amount.Abs()
		}
	}
	for sym, r := range requests {
		r.refund = refunds[sym]
		requests[sym] = r
	}
	return requests
}

// fractionCashOuts counts the fractional share cash-outs parseLedger books as
// sells, keyed by fractionKey.
func fractionCashOuts(ledger [][]string, f numberFormat) map[string]int {
	cashOuts := map[string]int{}
	for _, rec := range ledger {
		m := fractionRow.FindStringSubmatch(rec[1])
		if m == nil {
			continue
		}
		amount, err := parseNumber(rec[2], f)
		quantity, qerr := parseNumber(m[1], usFormat)
		if err == nil && qerr == nil && amount.IsPositive() && quantity.IsPositive() {
			cashOuts[fractionKey(m[2], quantity)]++
		}
	}
	return cashOuts
}

func fractionKey(symbol string, quantity decimal.Decimal) string {
	return symbol + " " + quantity.String()
}

// parseMove books the Hareket Listesi records the cash ledger does not
// show: a bonus issue (Bedelsiz) as a buy at price 0 and a priced transfer
// in (HS-Virman Giriş) as a buy at its price. HS Alış and HS Satış
// are the ledger's trades, which carry the commission, so they are not read
// here. A transfer in at price 0 has no known cost (unknown_cost) and any
// other type is unknown: both are left to the user.
//
// A transfer out (HS-Virman Çıkış) of the symbol and quantity of a ledger
// fractional share cash-out is that cash-out, booked by the ledger's sell, so
// it is skipped; each cash-out matches one transfer out. Any other
// transfer out is to another institution: unknown, left to the user.
//
// A transfer in of a symbol with an IPO request in the ledger is the
// allotment. Its printed price may repeat another row's, so the
// price is the request's payment / requested quantity, and allotted quantity
// x price must be the payment less the refund within a kuruş, else it is a
// bad_number left to the user. The amount is that net payment, so cash
// matches the account.
func parseMove(st *statement.Statement, rec []string, f numberFormat, requests map[string]ipoRequest, cashOuts map[string]int) {
	n := len(rec)
	kind := rec[n-2]
	if kind == "HS Alış" || kind == "HS Satış" {
		return
	}
	date, err := parseDate(rec[n-4])
	if err != nil {
		st.BadDate(tradesSection, rec)
		return
	}
	if kind == "HS-Virman Çıkış" {
		if q, err := parseNumber(rec[n-3], f); err == nil && cashOuts[fractionKey(rec[0], q.Abs())] > 0 {
			cashOuts[fractionKey(rec[0], q.Abs())]--
			st.Skipped = append(st.Skipped, statement.Skipped{Section: tradesSection, Reason: "fractional share cash-out, booked from the ledger", Row: rec})
			return
		}
	}
	if kind != "Bedelsiz" && kind != kindVirman {
		st.Errors = append(st.Errors, statement.ParseError{Section: tradesSection, Row: rec, Code: "unknown_row", Value: kind, Message: "unknown trade list row", Date: date})
		return
	}
	quantity, err := parseNumber(rec[n-3], f)
	if err != nil {
		st.BadNumber(tradesSection, rec, "quantity", date)
		return
	}
	if !quantity.IsPositive() {
		st.Errors = append(st.Errors, statement.ParseError{Section: tradesSection, Row: rec, Code: codeBadNumber, Value: "quantity", Message: "quantity is not positive", Date: date})
		return
	}
	if r, ok := requests[rec[0]]; ok && kind == kindVirman {
		price, paid := decimal.Zero, r.paid.Sub(r.refund)
		if r.requested.IsPositive() {
			price = r.paid.DivRound(r.requested, 9)
		}
		if !price.IsPositive() || quantity.Mul(price).Sub(paid).Abs().GreaterThan(cent) {
			st.Errors = append(st.Errors, statement.ParseError{Section: tradesSection, Row: rec, Code: codeBadNumber, Value: "quantity", Message: "IPO allotment does not match the ledger's payment and refund", Date: date})
			return
		}
		st.Trades = append(st.Trades, statement.Trade{
			Date: date, Symbol: rec[0], Side: transaction.TypeBuy, Quantity: quantity,
			Price: price, Amount: paid, Currency: "TRY",
		})
		return
	}
	price := decimal.Zero
	if kind == kindVirman {
		if price, err = parseNumber(rec[n-1], f); err != nil {
			st.BadNumber(tradesSection, rec, "price", date)
			return
		}
		if !price.IsPositive() {
			st.Errors = append(st.Errors, statement.ParseError{Section: tradesSection, Row: rec, Code: "unknown_cost", Value: kind, Message: "transfer in with unknown cost", Date: date})
			return
		}
	}
	st.Trades = append(st.Trades, statement.Trade{
		Date: date, Symbol: rec[0], Side: transaction.TypeBuy, Quantity: quantity,
		Price: price, Amount: quantity.Mul(price), Currency: "TRY",
	})
}

// trade is a parsed trade with its ledger value date, which keys the
// commission split.
type trade struct {
	index     int
	valueDate string
}

// parseLedger reads the Hesap Ekstresi records (value date, description,
// signed amount, running balance) once the period is known, because a trade
// date outside the period falls back to the value date. The amount is in
// the statement's format f, the description's price and quantity always US.
func parseLedger(st *statement.Statement, ledger [][]string, f numberFormat) {
	// A Komisyon is split over the trades of its value date, wherever they
	// sit in the ledger, or else is a fee in ledger order.
	tradeDates := tradeDateSet(ledger)
	var trades []trade
	var commissions []commission
	for _, rec := range ledger {
		desc, dateCell, amountCell := rec[1], rec[0], rec[2]
		refund := refundRow.FindStringSubmatch(desc)
		if refund != nil {
			// Keep the customer's name out of every output.
			desc, rec = "IPO refund "+refund[1], []string{dateCell, amountCell}
		}
		valueDate, err := parseDate(dateCell)
		if err != nil {
			st.BadDate(ledgerSection, rec)
			continue
		}
		amount, err := parseNumber(amountCell, f)
		if err != nil {
			st.BadNumber(ledgerSection, rec, "amount", valueDate)
			continue
		}
		div := dividendRow.FindStringSubmatch(desc)
		fraction := fractionRow.FindStringSubmatch(desc)
		switch {
		case ipoRow.MatchString(desc) && amount.IsNegative():
			st.Skipped = append(st.Skipped, statement.Skipped{Section: ledgerSection, Reason: "IPO payment, booked by the allotment", Row: rec})
		case refund != nil && amount.IsPositive():
			st.Skipped = append(st.Skipped, statement.Skipped{Section: ledgerSection, Reason: "IPO refund", Row: rec})
		case ipoRow.MatchString(desc) && amount.IsPositive():
			st.Cash = append(st.Cash, statement.CashMovement{
				Date: valueDate, Kind: transaction.TypeDeposit, Amount: amount, Currency: "TRY", Description: desc,
			})
		case refund != nil && amount.IsNegative():
			st.Cash = append(st.Cash, statement.CashMovement{
				Date: valueDate, Kind: transaction.TypeWithdrawal, Amount: amount.Abs(), Currency: "TRY", Description: desc,
			})
		case fraction != nil && amount.IsPositive():
			parseFraction(st, rec, fraction, amount, valueDate)
		case openingRow.MatchString(desc):
			st.Skipped = append(st.Skipped, statement.Skipped{Section: ledgerSection, Reason: "opening balance", Row: rec})
		case tradeRow.MatchString(desc):
			if t, ok := parseTradeRow(st, rec, tradeRow.FindStringSubmatch(desc), amount, valueDate); ok {
				trades = append(trades, t)
			}
		case commissionRow.MatchString(desc) && tradeDates[rec[0]]:
			// An unreadable Matrah stays zero and splits over every trade.
			matrah, _ := parseNumber(commissionRow.FindStringSubmatch(desc)[1], usFormat)
			commissions = append(commissions, commission{valueDate, matrah, amount.Abs()})
		case commissionRow.MatchString(desc):
			st.Cash = append(st.Cash, statement.CashMovement{
				Date: valueDate, Kind: transaction.TypeFee, Amount: amount.Abs(), Currency: "TRY", Description: desc,
			})
		case div != nil && div[2] == "" && amount.IsPositive():
			// The printed amount is what reached the account, which looks
			// like the payment after withholding. It is booked as Gross with
			// Tax 0 so cash matches the account; Tax 0 does not mean the
			// dividend was untaxed, and no gross or tax is derived from a
			// withholding rate (ADR 0018).
			st.Dividends = append(st.Dividends, statement.Dividend{
				Date: valueDate, Symbol: div[1], Gross: amount, Tax: decimal.Zero, Currency: "TRY",
			})
		case div != nil && div[2] != "", transferRow.MatchString(desc), feeRow.MatchString(desc):
			bookTransfer(st, desc, amount, valueDate)
		default:
			st.Errors = append(st.Errors, statement.ParseError{Section: ledgerSection, Row: rec, Code: "unknown_row", Value: desc, Message: "unknown ledger row", Date: valueDate})
		}
	}
	bookCommissions(st, trades, commissions)
	moveTakasDeposits(st, trades)
}

// tradeDateSet returns the value dates of the ledger's trade records.
func tradeDateSet(ledger [][]string) map[string]bool {
	dates := map[string]bool{}
	for _, rec := range ledger {
		if tradeRow.MatchString(rec[1]) {
			dates[rec[0]] = true
		}
	}
	return dates
}

// parseFraction books the sale of a fractional share of the ledger record
// rec, m being its description's fractionRow match, into st.Trades, or
// records a ParseError.
func parseFraction(st *statement.Statement, rec, m []string, amount decimal.Decimal, valueDate string) {
	quantity, err := parseNumber(m[1], usFormat)
	if err != nil || !quantity.IsPositive() {
		st.BadNumber(ledgerSection, rec, "quantity", valueDate)
		return
	}
	// The amount is booked as printed; the price is rounded to the
	// transaction API's 9 fraction digits.
	st.Trades = append(st.Trades, statement.Trade{
		Date: valueDate, Symbol: m[2], Side: transaction.TypeSell, Quantity: quantity,
		Price: amount.DivRound(quantity, 9), Amount: amount, Currency: "TRY",
	})
}

// bookTransfer books a transfer, fee or dividend-adjustment ledger record
// into st.Cash: a deposit when the amount is positive, else a fee or a
// withdrawal.
func bookTransfer(st *statement.Statement, desc string, amount decimal.Decimal, valueDate string) {
	kind := transaction.TypeDeposit
	switch {
	case amount.IsNegative() && feeRow.MatchString(desc):
		kind = transaction.TypeFee
	case amount.IsNegative():
		kind = transaction.TypeWithdrawal
	}
	st.Cash = append(st.Cash, statement.CashMovement{
		Date: valueDate, Kind: kind, Amount: amount.Abs(), Currency: "TRY", Description: desc,
	})
}

// parseTradeRow books the buy or sell of the ledger record rec, m being its
// description's tradeRow match, into st.Trades and returns it, or records
// a ParseError and returns false.
func parseTradeRow(st *statement.Statement, rec, m []string, amount decimal.Decimal, valueDate string) (trade, bool) {
	price, err := parseNumber(m[3], usFormat)
	if err != nil {
		st.BadNumber(ledgerSection, rec, "price", valueDate)
		return trade{}, false
	}
	quantity, err := parseNumber(m[4], usFormat)
	if err != nil {
		st.BadNumber(ledgerSection, rec, "quantity", valueDate)
		return trade{}, false
	}
	// An amount that is not price x quantity was misread; never book it.
	if amount.Abs().Sub(price.Mul(quantity)).Abs().GreaterThan(cent) {
		st.Errors = append(st.Errors, statement.ParseError{Section: ledgerSection, Row: rec, Code: codeBadNumber, Value: "amount", Message: "amount is not price x quantity", Date: valueDate})
		return trade{}, false
	}
	date := valueDate
	if d, err := parseDate(m[1]); err == nil && d >= st.PeriodStart && d <= st.PeriodEnd {
		date = d
	}
	side := transaction.TypeBuy
	if m[2] == "HS" {
		side = transaction.TypeSell
	}
	t := trade{index: len(st.Trades), valueDate: valueDate}
	st.Trades = append(st.Trades, statement.Trade{
		Date:     date,
		Symbol:   m[5],
		Side:     side,
		Quantity: quantity,
		Price:    price,
		Amount:   amount.Abs(),
		Currency: "TRY",
	})
	return t, true
}

// commission is a Komisyon ledger record on a day with trades: its value
// date, the Matrah it names and the amount charged.
type commission struct {
	valueDate      string
	matrah, amount decimal.Decimal
}

// bookCommissions splits each commission over the trades of its value date
// it picks, or books it as a fee when that day has no readable trade.
func bookCommissions(st *statement.Statement, trades []trade, commissions []commission) {
	// The trades are grouped once, so each Komisyon costs a lookup, not a
	// scan of every trade, and each group is split once.
	groups := map[string]*group{}
	for _, t := range trades {
		for _, key := range []string{t.valueDate + " " + st.Trades[t.index].Side, t.valueDate} {
			g := groups[key]
			if g == nil {
				g = &group{}
				groups[key] = g
			}
			g.trades = append(g.trades, t.index)
			g.total = g.total.Add(st.Trades[t.index].Amount)
		}
	}
	var booked []*group
	for _, c := range commissions {
		g := pick(groups, c.valueDate, c.matrah)
		if g == nil {
			// Every trade of that day was unreadable: keep the commission as a fee.
			st.Cash = append(st.Cash, statement.CashMovement{Date: c.valueDate, Kind: transaction.TypeFee, Amount: c.amount, Currency: "TRY"})
			continue
		}
		if !g.booked {
			g.booked = true
			booked = append(booked, g)
		}
		g.fee = g.fee.Add(c.amount)
	}
	for _, g := range booked {
		split(st, g)
	}
}

// moveTakasDeposits redates each Banka Takas deposit in st.Cash. Such a
// deposit pays for the buys of its value date, which are dated on their
// trade date: it moves to the earliest of them, so cash does not go
// negative until the value date. Withdrawals and sells keep their
// dates.
func moveTakasDeposits(st *statement.Statement, trades []trade) {
	firstBuy := map[string]string{}
	for _, t := range trades {
		d := st.Trades[t.index].Date
		if st.Trades[t.index].Side == transaction.TypeBuy && (firstBuy[t.valueDate] == "" || d < firstBuy[t.valueDate]) {
			firstBuy[t.valueDate] = d
		}
	}
	for i, c := range st.Cash {
		if d := firstBuy[c.Date]; c.Kind == transaction.TypeDeposit && takasRow.MatchString(c.Description) && d != "" && d < c.Date {
			st.Cash[i].Date = d
		}
	}
}

// group is the trades of one value date and side, or of the whole value
// date: their indices in st.Trades, their summed Amount and the Komisyon
// booked to them. taken marks a side that already got a Komisyon.
type group struct {
	trades        []int
	total, fee    decimal.Decimal
	taken, booked bool
}

// pick finds the trades a Komisyon of valueDate goes to, nil when that day
// has none. A day with buys and sells has one Komisyon per side: it goes to
// the buys or the sells whose amounts sum to its Matrah, else to every trade
// of the day. Of the sides within a kuruş of the Matrah, an untaken one
// wins, then the closest, so buys and sells of equal totals get one each and
// near-equal totals do not swap their fees.
func pick(groups map[string]*group, valueDate string, matrah decimal.Decimal) *group {
	var best *group
	bestDiff := decimal.Zero
	for _, side := range []string{transaction.TypeBuy, transaction.TypeSell} {
		g := groups[valueDate+" "+side]
		if g == nil {
			continue
		}
		diff := g.total.Sub(matrah).Abs()
		if diff.GreaterThan(cent) {
			continue
		}
		was := best != nil && best.taken
		if best == nil || was && !g.taken || was == g.taken && diff.LessThan(bestDiff) {
			best, bestDiff = g, diff
		}
	}
	if best != nil {
		best.taken = true
		return best
	}
	return groups[valueDate]
}

// split spreads the Komisyon booked to g over its trades pro rata by Amount,
// rounded to 2 decimals, the remainder on the last one. Komisyon rows that
// went to the same trades are split as their sum.
func split(st *statement.Statement, g *group) {
	left := g.fee
	for n, idx := range g.trades {
		share := left
		if n < len(g.trades)-1 {
			share = decimal.Zero
			if g.total.IsPositive() {
				share = g.fee.Mul(st.Trades[idx].Amount).Div(g.total).Round(2)
			}
		}
		left = left.Sub(share)
		st.Trades[idx].Fee = st.Trades[idx].Fee.Add(share)
	}
}

// period reads "01/03/2024 - 31/03/2024".
func period(cell string) (string, string, bool) {
	from, to, ok := strings.Cut(cell, " - ")
	if !ok {
		return "", "", false
	}
	start, err := parseDate(from)
	if err != nil {
		return "", "", false
	}
	end, err := parseDate(to)
	if err != nil {
		return "", "", false
	}
	return start, end, true
}

// parseDate reads DD/MM/YYYY as the YYYY-MM-DD Europe/Istanbul market date
// it already is.
func parseDate(cell string) (string, error) {
	t, err := time.Parse("02/01/2006", strings.TrimSpace(cell))
	if err != nil {
		return "", errors.New("unreadable date")
	}
	return t.Format(time.DateOnly), nil
}

var cent = decimal.New(1, -2)

// parseNumber reads a number cell in format f (see statement.ParseNumber).
func parseNumber(cell string, f numberFormat) (decimal.Decimal, error) {
	return statement.ParseNumber(cell, f.thousands, f.mark)
}
