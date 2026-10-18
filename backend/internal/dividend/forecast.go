package dividend

import (
	"cmp"
	"slices"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// ForecastRow is one expected dividend of the next twelve months. A projected
// row (Planned false) repeats a recorded dividend of the last twelve months a
// year later; a planned row is a stored ex-date. Date is the planned ex-date,
// empty for a projected row.
type ForecastRow struct {
	InstrumentID int64
	Period       string // YYYY-MM
	Date         string
	Planned      bool
	Quantity     decimal.Decimal
	PerShare     decimal.Decimal
	Gross        decimal.Decimal
	Currency     string
}

// periodAfter returns the YYYY-MM period n months after the month of a
// YYYY-MM-DD date. It steps from the first day of the month so AddDate never
// overflows a short month.
func periodAfter(date string, n int) string {
	t, err := time.Parse(time.DateOnly, date[:7]+"-01")
	if err != nil {
		panic("dividend: bad date " + date) // dates are validated at the boundary
	}
	return t.AddDate(0, n, 0).Format("2006-01")
}

// ForecastPeriods returns the twelve YYYY-MM periods starting with the month
// after today's.
func ForecastPeriods(today string) []string {
	out := make([]string, 12)
	for i := range out {
		out[i] = periodAfter(today, i+1)
	}
	return out
}

// Forecast returns the forecast rows of one instrument (see ForecastRow):
// every dividend transaction dated from the first day of the month eleven
// months before today's through today projects its per-share amount (its
// amount over the quantity held at the start of its date; a row with no
// holding then is skipped, divided by the ratio of every split of the
// instrument dated after it, a bonus buy booked in place of a split scaling
// it the same way) times the quantity held now, in its month plus
// twelve. The planned rows of Upcoming whose period is in ForecastPeriods
// replace the projected rows of the same period. rows and divs are ordered as
// for Expected; the result is sorted by period, then date.
func Forecast(rows []transaction.Transaction, divs []Dividend, today string) []ForecastRow {
	from := periodAfter(today, -11) + "-01"
	// A projected row waits for the quantity held now, known only after the
	// walk has passed every row dated on or before today.
	var out []ForecastRow
	state := holdings.NewState(nil)
	// A split after a projected row's date scales its per-share amount down
	// by the ratio, so the gross stays the recorded amount (ADR 0008). A
	// bonus buy booked in place of a split scales it by the growth it adds.
	apply := func(t transaction.Transaction) {
		booked := t.Type == transaction.TypeBuy && t.BookedSplit() && t.InstrumentID != nil
		var before decimal.Decimal
		if booked {
			before = state.Quantity(*t.InstrumentID)
		}
		state.Apply(t)
		var ratio decimal.Decimal
		switch {
		case t.Type == transaction.TypeSplit:
			ratio = t.Quantity
		case booked && before.IsPositive():
			ratio = state.Quantity(*t.InstrumentID).Div(before)
		default:
			return
		}
		for i := range out {
			if out[i].InstrumentID == *t.InstrumentID {
				out[i].PerShare = out[i].PerShare.Div(ratio)
			}
		}
	}
	next := 0
	for _, t := range rows {
		if t.Type != transaction.TypeDividend || t.InstrumentID == nil || t.Date < from || t.Date > today {
			continue
		}
		for next < len(rows) && rows[next].Date < t.Date {
			apply(rows[next])
			next++
		}
		if qty := state.Quantity(*t.InstrumentID); qty.IsPositive() {
			out = append(out, ForecastRow{InstrumentID: *t.InstrumentID, Period: periodAfter(t.Date, 12), PerShare: t.Amount.Div(qty), Currency: t.Currency})
		}
	}
	for ; next < len(rows) && rows[next].Date <= today; next++ {
		apply(rows[next])
	}
	kept := out[:0]
	for _, r := range out {
		r.Quantity = state.Quantity(r.InstrumentID)
		if r.Quantity.IsPositive() {
			r.Gross = r.PerShare.Mul(r.Quantity).Round(2)
			kept = append(kept, r)
		}
	}
	out = kept

	periods := ForecastPeriods(today)
	replaced := map[string]bool{}
	var planned []ForecastRow
	for _, s := range Upcoming(rows, divs, today) {
		period := s.Date[:7]
		if !slices.Contains(periods, period) {
			continue
		}
		replaced[period] = true
		planned = append(planned, ForecastRow{InstrumentID: s.InstrumentID, Period: period, Date: s.Date, Planned: true, Quantity: s.Quantity, PerShare: s.PerShare, Gross: s.Gross, Currency: s.Currency})
	}
	out = slices.DeleteFunc(out, func(r ForecastRow) bool { return replaced[r.Period] })
	out = append(out, planned...)
	slices.SortStableFunc(out, func(a, b ForecastRow) int {
		return cmp.Or(cmp.Compare(a.Period, b.Period), cmp.Compare(a.Date, b.Date))
	})
	return out
}
