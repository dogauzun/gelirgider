package dividend

import (
	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// Suggestion is one expected dividend (ADR 0009): Quantity held before the
// ex-date Date times the gross PerShare, Gross rounded to 2 decimals.
type Suggestion struct {
	InstrumentID int64
	Date         string
	Quantity     decimal.Decimal
	PerShare     decimal.Decimal
	Gross        decimal.Decimal
	Currency     string
}

// Expected returns the expected dividends of one instrument: the ex-dates of
// divs on or before today on which rows held it, not matched by a recorded
// dividend transaction, not in dismissed and with a gross above 0. rows are
// the instrument's transactions in transaction.SortForApply order and divs
// its dividends date-ascending, as transaction.Repo.List and Repo.ListFor
// return them. The result is date-ascending.
func Expected(rows []transaction.Transaction, divs []Dividend, dismissed map[DismissalKey]bool, today string) ([]Suggestion, error) {
	// Dates of the dividend transactions already recorded, ascending.
	var recorded []string
	for _, t := range rows {
		if t.Type == transaction.TypeDividend {
			recorded = append(recorded, t.Date)
		}
	}

	// One pass: dividends and rows are both date-ascending, so each row is
	// applied once, before the first ex-date after it. Collect the held
	// ex-dates first: recorded dividends are matched against all of them
	// before any is suggested.
	var candidates []Suggestion
	var exDates []string
	state := holdings.NewState(nil)
	next := 0
	for _, d := range divs {
		if d.Date > today {
			break
		}
		for next < len(rows) && rows[next].Date < d.Date {
			state.Apply(rows[next])
			next++
		}
		if qty := state.Quantity(d.InstrumentID); qty.IsPositive() {
			candidates = append(candidates, Suggestion{InstrumentID: d.InstrumentID, Date: d.Date, Quantity: qty, PerShare: d.Amount, Currency: d.Currency})
			exDates = append(exDates, d.Date)
		}
	}
	// A recorded dividend hides the ex-date it matches; it covers
	// hand-entered dividends and Midas imports, which carry the payment date.
	matched, err := MatchRecorded(recorded, exDates)
	if err != nil {
		return nil, err
	}
	hidden := map[string]bool{}
	for _, ex := range matched {
		hidden[ex] = true
	}
	var out []Suggestion
	for _, c := range candidates {
		if hidden[c.Date] || dismissed[DismissalKey{InstrumentID: c.InstrumentID, Date: c.Date}] {
			continue
		}
		c.Gross = c.Quantity.Mul(c.PerShare).Round(2)
		// A fractional holding can earn under a cent: a 0.00 row cannot be
		// saved as a transaction, so it is not expected.
		if !c.Gross.IsPositive() {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
