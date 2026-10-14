// Package midas parses the Midas "HESAP EKSTRESİ" monthly account statement
// into a statement.Statement. It is pure: rows in, result out, no database,
// no HTTP, no user, no clock.
//
// Parsing is partial by design (ADR 0006): a row that cannot be read becomes
// a statement.ParseError and the next row is parsed anyway. The only returned
// error is "no period row found: not a Midas statement", which means the
// document is not a Midas statement at all.
//
// Customer data: the customer block (name, TCKN, address) is never read. A
// ParseError.Message is a fixed string and never quotes a cell, so it is safe
// to log. The raw cells in ParseError.Row and Skipped.Row, and the İşlem
// Açıklaması text in CashMovement.Description, are not: the description
// carries the counterparty bank and IBAN of a deposit. ADR 0006 keeps them
// deliberately, because the user has to recognise the row to fix it in the
// import preview, and they are the user's own data. Callers show them to that
// user and nowhere else: never in a log line, and never persisted — the
// transaction note is the generic "Midas <period>" string. The preview masks
// the IBAN to its last four digits (statement.MaskIdentifiers, GLG-1196).
package midas

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/shopspring/decimal"
)

func init() {
	// The broker package registers itself: statement cannot import midas
	// because midas imports statement for the result types.
	statement.Parsers["midas"] = Parse
}

// Section titles. A section starts at the row whose first cell begins with
// its title ("PORTFÖY ÖZETİ (") and runs until the next section title.
const (
	secHoldings  = "PORTFÖY ÖZETİ"
	secTrades    = "YATIRIM İŞLEMLERİ"
	secCash      = "HESAP İŞLEMLERİ"
	secDividends = "TEMETTÜ İŞLEMLERİ"
	secTransfers = "HİSSE TRANSFERLERİ"
)

var sectionTitles = []string{secHoldings, secTrades, secCash, secDividends, secTransfers}

// noisePrefixes match rows that carry no table data: the company header that
// repeats on every page, the customer block, footnotes and the empty-section
// notice. They are ignored wherever they appear.
var noisePrefixes = []string{
	"Midas Menkul Değerler",
	"Mersis",
	"Ticari Sicil",
	"Adres",
	"www.midasmenkul.com",
	"Müşteri Adı Soyadı",
	"TCKN",
	"Hesap Açılış Tarihi",
	"Nakit Bakiye",
	"Toplam Portföy Değeri",
	"İşlem Hacmi",
	"Kayıt bulunmamaktadır",
}

// columnHeaders are the first cell of a section's column header row. Header
// rows are optional per section (the TRY sample's HESAP İŞLEMLERİ has none),
// so they are recognised by text and never by position.
var columnHeaders = map[string]bool{
	"Sermaye Piyasası Aracı": true,
	"Tarih":                  true,
	"Talep Tarihi":           true,
	"Ödeme Tarihi":           true,
}

const executed = "Gerçekleşti"

// Parse turns the text rows of a Midas statement PDF into a Statement.
func Parse(rows [][]string) (statement.Statement, error) {
	st := statement.Statement{Broker: "midas"}
	section := ""
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		first := row[0]
		// The period row is part of the header block that repeats on every
		// page, so it is always consumed and only the first one is recorded.
		if start, end, ok := period(row); ok {
			if st.PeriodStart == "" {
				st.PeriodStart, st.PeriodEnd = start, end
			}
			continue
		}
		if st.Currency == "" && strings.HasPrefix(first, "Nakit Bakiye") {
			if amount, currency, err := parseHeaderMoney(row[len(row)-1]); err == nil {
				st.Currency, st.CashBalance = currency, &amount
			}
			continue
		}
		if title, ok := sectionTitle(first); ok {
			section = title
			continue
		}
		if strings.HasPrefix(first, "*") {
			// A footnote closes its section: the trailing disclaimer block
			// runs on for several rows that are not marked in any other way.
			section = ""
			continue
		}
		if section == "" || isNoise(first) || columnHeaders[first] || isPageNumber(row) {
			continue
		}
		switch section {
		case secHoldings:
			parseHolding(&st, row)
		case secTrades:
			parseTrade(&st, row)
		case secCash:
			parseCash(&st, row)
		case secDividends:
			parseDividend(&st, row)
		case secTransfers:
			st.Skipped = append(st.Skipped, statement.Skipped{Section: secTransfers, Reason: "stock transfer", Row: row})
		}
	}
	if st.PeriodStart == "" {
		return statement.Statement{}, fmt.Errorf("no period row found: not a Midas statement")
	}
	return st, nil
}

// period reads "01/12/25|-|31/12/25|HESAP EKSTRESİ". The customer block that
// follows it is never read.
func period(row []string) (string, string, bool) {
	if len(row) < 4 || !strings.HasSuffix(strings.TrimSpace(row[len(row)-1]), "HESAP EKSTRESİ") {
		return "", "", false
	}
	start, err := parseDate(row[0])
	if err != nil {
		return "", "", false
	}
	end, err := parseDate(row[2])
	if err != nil {
		return "", "", false
	}
	return start, end, true
}

func sectionTitle(cell string) (string, bool) {
	for _, title := range sectionTitles {
		if strings.HasPrefix(cell, title) {
			return title, true
		}
	}
	return "", false
}

func isNoise(cell string) bool {
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(cell, prefix) {
			return true
		}
	}
	return false
}

// isPageNumber matches the "1|/|1" footer of every page.
func isPageNumber(row []string) bool {
	return len(row) == 3 && row[1] == "/"
}

// PORTFÖY ÖZETİ: Sermaye Piyasası Aracı|Adet|Hisse Başı Ortalama Maliyet|Kar / Zarar|Toplam Değeri
func parseHolding(st *statement.Statement, row []string) {
	if len(row) != 5 {
		st.Errors = append(st.Errors, statement.ParseError{Section: secHoldings, Row: row, Code: "cell_count", Value: "5", Message: "expected 5 cells"})
		return
	}
	quantity, err := parseNumber(row[1])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secHoldings, Row: row, Code: "unreadable_quantity", Message: "unreadable quantity"})
		return
	}
	avgCost, currency, err := parseMoney(row[2])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secHoldings, Row: row, Code: "unreadable_avg_cost", Message: "unreadable average cost"})
		return
	}
	hold := statement.Holding{
		Symbol:   symbol(row[0]),
		Quantity: quantity,
		AvgCost:  avgCost,
		Currency: currency,
	}
	// The price is Toplam Değeri / Adet, for an opening position the
	// portfolio has no stored price for (GLG-737, GLG-816); an unreadable
	// value leaves it zero, as reconciliation needs only the quantity.
	if value, _, err := parseMoney(row[4]); err == nil && quantity.IsPositive() {
		hold.Price = value.DivRound(quantity, 6)
	}
	st.Holdings = append(st.Holdings, hold)
}

// YATIRIM İŞLEMLERİ: Tarih|İşlem Türü|Sembol|İşlem Tipi|İşlem Durumu|Para
// Birimi|Emir Adedi|Emir Tutarı|Gerçekleşen Adet|Ortalama İşlem Fiyatı|İşlem
// Ücreti|İşlem Tutarı. Both Emir columns are ignored: one of them is "-"
// depending on whether the order was placed by amount or by quantity.
func parseTrade(st *statement.Statement, row []string) {
	if len(row) != 12 {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "cell_count", Value: "12", Message: "expected 12 cells"})
		return
	}
	if row[4] != executed {
		st.Skipped = append(st.Skipped, statement.Skipped{Section: secTrades, Reason: "not executed", Row: row})
		return
	}
	date, err := parseDate(row[0])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unreadable_date", Message: "unreadable date"})
		return
	}
	var side string
	switch row[3] {
	case "Alış":
		side = "buy"
	case "Satış":
		side = "sell"
	default:
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unknown_trade_type", Value: row[3], Message: "unknown trade type " + row[3], Date: date})
		return
	}
	quantity, err := parseNumber(row[8])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unreadable_quantity", Message: "unreadable quantity", Date: date})
		return
	}
	price, err := parseNumber(row[9])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unreadable_price", Message: "unreadable price", Date: date})
		return
	}
	fee, err := parseNumber(row[10])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unreadable_fee", Message: "unreadable fee", Date: date})
		return
	}
	amount, err := parseNumber(row[11])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secTrades, Row: row, Code: "unreadable_amount", Message: "unreadable amount", Date: date})
		return
	}
	st.Trades = append(st.Trades, statement.Trade{
		Date:     date,
		Symbol:   symbol(row[2]),
		Side:     side,
		Quantity: quantity,
		Price:    price,
		Amount:   amount,
		Fee:      fee,
		Currency: row[5],
	})
}

// HESAP İŞLEMLERİ: Talep Tarihi|İşlem Tarihi|İşlem Tipi|İşlem Açıklaması|İşlem
// Durumu|Tutar (YP).
func parseCash(st *statement.Statement, row []string) {
	if len(row) != 6 {
		st.Errors = append(st.Errors, statement.ParseError{Section: secCash, Row: row, Code: "cell_count", Value: "6", Message: "expected 6 cells"})
		return
	}
	var kind string
	switch row[2] {
	case "Diğer Gelir":
		// "Aylık Nema Geliri", Midas's monthly interest on idle TRY cash. It
		// is income, not a contribution, so it imports as a dividend with no
		// instrument (GLG-359, GLG-533).
		kind = "dividend"
	case "Para Çekme":
		kind = "withdrawal"
	case "Diğer Gider":
		// A tax or expense debit, so far only "Stopaj", the withholding on
		// the monthly interest. It imports as a fee (GLG-1177).
		kind = "fee"
	case "Döviz Alış", "Döviz Satış":
		kind = "fx"
	case "Para Yatırma", "Ücretsiz İşlem":
		// Ücretsiz İşlem is Midas's zero-commission credit, paid into the cash
		// account like a deposit (GLG-1187).
		kind = "deposit"
	default:
		date, _ := parseDate(row[1])
		st.Errors = append(st.Errors, statement.ParseError{Section: secCash, Row: row, Code: "unknown_cash_type", Value: row[2], Message: "unknown cash type " + row[2], Date: date})
		return
	}
	if row[4] != executed {
		st.Skipped = append(st.Skipped, statement.Skipped{Section: secCash, Reason: "not executed", Row: row})
		return
	}
	date, err := parseDate(row[1])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secCash, Row: row, Code: "unreadable_date", Message: "unreadable date"})
		return
	}
	amount, currency, err := parseMoney(row[5])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secCash, Row: row, Code: "unreadable_amount", Message: "unreadable amount", Date: date})
		return
	}
	if kind == "fee" {
		// Printed negative; a fee's amount is positive, as in the Garanti
		// parsers.
		amount = amount.Abs()
	}
	if kind == "fx" {
		// Midas prints a conversion on both the TRY and the USD statement,
		// each with its own exact amount, so each books only its own leg,
		// like a transfer between two of the user's accounts (GLG-363).
		// Döviz Satış sells dollars, Döviz Alış buys them.
		switch {
		case currency != "USD" && currency != "TRY":
			st.Skipped = append(st.Skipped, statement.Skipped{Section: secCash, Reason: "fx conversion", Row: row})
			return
		case (row[2] == "Döviz Satış") == (currency == "USD"):
			kind = "withdrawal"
		default:
			kind = "deposit"
		}
	}
	st.Cash = append(st.Cash, statement.CashMovement{
		Date:        date,
		Kind:        kind,
		Amount:      amount,
		Currency:    currency,
		Description: row[3],
	})
}

// TEMETTÜ İŞLEMLERİ: Ödeme Tarihi|Sermaye Piyasası Aracı|Brüt Temettü
// Tutarı|Stopaj|Net Temettü Tutarı.
func parseDividend(st *statement.Statement, row []string) {
	if len(row) != 5 {
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "cell_count", Value: "5", Message: "expected 5 cells"})
		return
	}
	date, err := parseDate(row[0])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "unreadable_date", Message: "unreadable date"})
		return
	}
	gross, currency, err := parseMoney(row[2])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "unreadable_gross", Message: "unreadable gross amount", Date: date})
		return
	}
	tax, _, err := parseMoney(row[3])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "unreadable_tax", Message: "unreadable withholding tax", Date: date})
		return
	}
	net, _, err := parseMoney(row[4])
	if err != nil {
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "unreadable_net", Message: "unreadable net amount", Date: date})
		return
	}
	if !gross.Sub(tax).Equal(net) {
		// The row does not say gross - tax = net, so the columns shifted.
		st.Errors = append(st.Errors, statement.ParseError{Section: secDividends, Row: row, Code: "net_mismatch", Message: "gross minus tax does not equal net", Date: date})
		return
	}
	st.Dividends = append(st.Dividends, statement.Dividend{
		Date:     date,
		Symbol:   symbol(row[1]),
		Gross:    gross,
		Tax:      tax,
		Currency: currency,
	})
}

// symbol takes the ticker out of "OPT25.F - Osmanlı Portföy BIST..." or of a
// bare "VTI", and drops the BIST bulletin suffix so it matches
// instruments.symbol.
func symbol(cell string) string {
	s := cell
	if i := strings.Index(s, "-"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	for _, suffix := range []string{".E", ".F", ".S1"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// parseMoney reads "8.237,46 USD" into an amount and its currency. A cell
// without a currency token yields an empty currency.
func parseMoney(cell string) (decimal.Decimal, string, error) {
	fields := strings.Fields(cell)
	if len(fields) == 0 {
		return decimal.Decimal{}, "", fmt.Errorf("empty amount")
	}
	amount, err := parseNumber(fields[0])
	if err != nil {
		return decimal.Decimal{}, "", err
	}
	currency := ""
	if len(fields) > 1 {
		currency = fields[len(fields)-1]
	}
	return amount, currency, nil
}

// parseHeaderMoney reads a header block amount such as Nakit Bakiye's
// "0.74 USD". The header prints plain decimals ("27.46", "88702.860000"), so
// "1.500 USD" is one and a half, not the tables' Turkish thousand (GLG-498);
// only a comma switches to the Turkish format (GLG-419).
func parseHeaderMoney(cell string) (decimal.Decimal, string, error) {
	amount, currency, err := parseMoney(cell)
	if err != nil || strings.Contains(cell, ",") {
		return amount, currency, err
	}
	amount, err = decimal.NewFromString(strings.Fields(cell)[0])
	return amount, currency, err
}

// thousandsOnly matches a comma-less Turkish number made of 3-digit groups,
// "1.000", "+1.000" or "-1.234.567". The first group never starts with 0, so
// "0.640" stays a plain decimal.
var thousandsOnly = regexp.MustCompile(`^[+-]?[1-9]\d{0,2}(\.\d{3})+$`)

// parseNumber accepts both formats the statement mixes: Turkish "8.237,46"
// and "1.000" in the tables, and plain "88702.860000" in the header block.
// With a comma, the dots must be 3-digit thousands groups ("1.2.3,4" is
// unreadable, GLG-908). A header amount goes through parseHeaderMoney
// instead, because a plain decimal with a 3-digit fraction ("1.500") reads
// as a thousand here.
// The result must have the transaction API's shape, plus the sign Kar /
// Zarar prints: unchecked, "1e2000000000" panics on Mul and "1e3000000"
// prints 3 MB (GLG-629, like the CSV parser's GLG-615).
func parseNumber(cell string) (decimal.Decimal, error) {
	if s := strings.TrimSpace(cell); strings.Contains(s, ",") || thousandsOnly.MatchString(s) {
		return statement.ParseNumber(s, ".", ",")
	}
	// No comma here, so "," as the separator leaves a plain "88702.86".
	return statement.ParseNumber(cell, ",", ".")
}

// parseDate reads the DD/MM/YY date at the start of a cell ("26/09/25
// 18:52:07") as the YYYY-MM-DD market date it is; Midas prints Europe/Istanbul
// calendar dates, so there is nothing to convert.
func parseDate(cell string) (string, error) {
	fields := strings.Fields(cell)
	if len(fields) == 0 {
		return "", fmt.Errorf("empty date")
	}
	t, err := time.Parse("02/01/06", fields[0])
	if err != nil {
		return "", fmt.Errorf("unreadable date")
	}
	return t.Format(time.DateOnly), nil
}
