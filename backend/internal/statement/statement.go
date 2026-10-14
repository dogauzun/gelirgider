// Package statement imports broker account statements. Statement is the
// broker-neutral result of parsing one: parsers (one package per broker)
// turn extracted text rows into a Statement, and the types in this file
// know nothing about users, portfolios, HTTP or the database. The HTTP
// handlers preview and commit a parsed statement into a portfolio, and Repo
// stores one row per imported file (see imports.go).
package statement

import (
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// Statement is one parsed broker statement. Parsers are partial by design: a
// row that cannot be read becomes a ParseError and parsing continues, so the
// caller gets everything that did parse plus the list of rows to fix by hand.
// Only "this is not a statement of this broker at all", or a CSV with no row
// under its header, is a returned error.
//
// All dates are YYYY-MM-DD in Europe/Istanbul. All money is decimal.
type Statement struct {
	Broker      string // "midas" | "csv" | "garanti" | "garanti_yatirim"
	PeriodStart string
	PeriodEnd   string
	Currency    string // account currency, "USD" or "TRY"
	// Market is optional: the market of Holdings and of the portfolio
	// positions the statement covers, for a statement whose currency does
	// not tell it (a TRY fund statement holds TEFAS funds, not BIST shares).
	// "" means marketForCurrency[Currency]; Midas and CSV leave it empty.
	Market string
	// CashBalance is the account's cash at period end as the statement
	// prints it, in Currency; nil when it printed none or it was unreadable.
	// Like Holdings it is for reconciliation only.
	CashBalance *decimal.Decimal
	Trades      []Trade
	Cash        []CashMovement
	Dividends   []Dividend
	Holdings    []Holding    // period-end snapshot, for reconciliation only
	Skipped     []Skipped    // rows the parser recognised but deliberately did not map
	Errors      []ParseError // rows or sections the parser could not read
}

// Trade is one executed buy or sell.
type Trade struct {
	Date     string
	Symbol   string
	Side     string // "buy" | "sell"
	Quantity decimal.Decimal
	Price    decimal.Decimal
	Amount   decimal.Decimal
	Fee      decimal.Decimal
	Currency string
	// Market is an optional market hint ("BIST", "US", "TEFAS", ...), ""
	// when the parser has none.
	Market string
}

// CashMovement is money entering or leaving the account.
type CashMovement struct {
	Date        string
	Kind        string // "deposit" | "withdrawal" | "dividend" (interest, no instrument) | "fee"
	Amount      decimal.Decimal
	Fee         decimal.Decimal // zero when the parser has none
	Currency    string
	Description string
}

// Dividend is one dividend payment, gross with its withholding tax.
type Dividend struct {
	Date     string
	Symbol   string
	Gross    decimal.Decimal
	Tax      decimal.Decimal
	Currency string
	// Market is an optional market hint ("BIST", "US", "TEFAS", ...), ""
	// when the parser has none.
	Market string
}

// Holding is one position in the period-end snapshot.
type Holding struct {
	Symbol   string
	Quantity decimal.Decimal
	AvgCost  decimal.Decimal
	Currency string
	// Price is the period-end unit price the statement prints or implies,
	// zero when it gives none. The preview prices an opening position with
	// it when no stored price exists.
	Price decimal.Decimal
	// Name is the fund name printed next to the code, "" when the statement
	// prints none. A commit remembers it, so a later statement's movement
	// named only by it resolves.
	Name string
}

// Skipped is a row the parser understood but chose not to map.
type Skipped struct {
	Section string
	Reason  string
	Row     []string
}

// ParseError is a row or section the parser could not read. The rest of the
// statement is still returned. Code is stable and the UI translates it, with
// Value filling its one parameter (a cell count or an unknown type); Message
// is the English text for logs and for a UI that does not know the code.
//
// Value means one thing per code: for bad_number, ambiguous_number and
// missing_field it is the field key (quantity, price, fee, amount, avg_cost,
// gross, tax, net); for cell_count the expected cell count; for the unknown_*
// codes the cell that was not recognised; bad_date carries none. The raw cell
// of an unreadable number or date is in Row.
type ParseError struct {
	Section string
	Row     []string
	Code    string
	Value   string
	Message string
	Date    string // YYYY-MM-DD when readable, else ""
}

// BadNumber records that the number cell for field could not be read. date is
// the row's date when it was readable, else "".
func (st *Statement) BadNumber(section string, row []string, field, date string) {
	st.Errors = append(st.Errors, ParseError{Section: section, Row: row, Code: "bad_number", Value: field, Message: "unreadable " + field, Date: date})
}

// BadDate records that the row's date cell could not be read.
func (st *Statement) BadDate(section string, row []string) {
	st.Errors = append(st.Errors, ParseError{Section: section, Row: row, Code: "bad_date", Message: "unreadable date"})
}

// CellCount records a row that does not have the want cells its section needs.
func (st *Statement) CellCount(section string, row []string, want int) {
	st.Errors = append(st.Errors, ParseError{Section: section, Row: row, Code: "cell_count", Value: strconv.Itoa(want), Message: "expected " + strconv.Itoa(want) + " cells"})
}

// Parsers maps a broker key to its parser. Each broker package registers
// itself here so callers pick a parser by name without importing every broker.
var Parsers = map[string]func([][]string) (Statement, error){}

// Detect runs the rows of a PDF through every parser but csv's, in key
// order, and returns the key and statement of the first that accepts them.
// A parser's only error is "not a statement of this broker", so
// the first nil error is the broker.
func Detect(rows [][]string) (string, Statement, error) {
	for _, key := range slices.Sorted(maps.Keys(Parsers)) {
		if key == "csv" {
			continue
		}
		if st, err := Parsers[key](rows); err == nil {
			return key, st, nil
		}
	}
	return "", Statement{}, errors.New("not a supported statement")
}

// Grouped reports whether number cell s has valid thousands groups: the part
// before the decimal mark is free of sep or in 3-digit groups ("1.234.567,8"),
// and the part after it has no sep. The PDF parsers strip sep before reading
// the digits, so without this "1.2.3,4" would read as 123.4 (the CSV parser
// guards the same way). The first group must not start with 0, or
// "0.234,5" would read as 234.5. A leading sign is ignored.
func Grouped(s, sep, mark string) bool {
	whole, fraction, _ := strings.Cut(strings.TrimLeft(s, "+-"), mark)
	if strings.Contains(fraction, sep) {
		return false
	}
	groups := strings.Split(whole, sep)
	if len(groups) == 1 {
		return true
	}
	if len(groups[0]) < 1 || len(groups[0]) > 3 || groups[0][0] == '0' {
		return false
	}
	for _, g := range groups[1:] {
		if len(g) != 3 {
			return false
		}
	}
	return true
}

// ParseNumber reads a PDF number cell with thousands separator sep and
// decimal mark mark, surrounding whitespace and an optional sign: US
// "-3,382.40", "+12,615.20" or Turkish "-3.382,40", "1.250,000", the
// thousands in 3-digit groups (see Grouped). The digits must have the
// transaction API's shape, so an exponent or an overlong number is
// unreadable rather than a panic later.
func ParseNumber(cell, sep, mark string) (decimal.Decimal, error) {
	s := strings.TrimSpace(cell)
	if !Grouped(s, sep, mark) {
		return decimal.Decimal{}, errors.New("unreadable number")
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, sep, ""), mark, ".")
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	if !transaction.DecimalPattern.MatchString(s) {
		return decimal.Decimal{}, errors.New("unreadable number")
	}
	n, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, err
	}
	if negative {
		n = n.Neg()
	}
	return n, nil
}

var (
	// iban is a Turkish IBAN, TR, two check digits and 22 digits, spaced in
	// groups or not, or another country's unspaced. The fixed length keeps
	// an amount after a spaced IBAN, "TR00 … 1234 1000 TL", out of it.
	iban = regexp.MustCompile(`\bTR\d{2}(?: ?\d){22}\b|\b[A-Z]{2}\d{12,32}\b`)
	// account is a bank account number such as "123456-100" or
	// "1234-6000001". The leading cell keeps a number with a decimal part,
	// "95.9090909-1234567-102", from being read as one from its fraction.
	account = regexp.MustCompile(`(^|[^\d.])\d{4,}(?:-\d+)+`)
	// isoDate is a date or month the account pattern also matches, as a CSV
	// row's raw cells carry: it is never masked.
	isoDate = regexp.MustCompile(`^\d{4}-\d{2}(?:-\d{2})?$`)
)

// MaskIdentifiers masks the IBANs and account numbers in a statement's
// description, so the preview never shows them in full: an IBAN
// becomes "TR** **** 1234", an account number "***6-100", both keeping their
// last four digits. Every row of one statement names the same account, so
// the rest of the description is enough to recognise the row.
func MaskIdentifiers(s string) string {
	s = iban.ReplaceAllStringFunc(s, func(m string) string {
		return m[:2] + "** **** " + lastDigits(strings.ReplaceAll(m, " ", ""))
	})
	return account.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.IndexAny(m, "0123456789")
		if isoDate.MatchString(m[i:]) {
			return m
		}
		return m[:i] + "***" + lastDigits(m[i:])
	})
}

// lastDigits returns the tail of s that holds its last four digits.
func lastDigits(s string) string {
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] >= '0' && s[i] <= '9' {
			if n++; n == 4 {
				return s[i:]
			}
		}
	}
	return s
}
