package dividend_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// TestForecast covers the twelve-month forecast of one instrument with today
// 2026-10-18. Each want row is "period date planned quantity per-share
// gross"; date is "-" for a projected row.
func TestForecast(t *testing.T) {
	d := decimal.RequireFromString
	id := int64(1)
	tx := func(typ, date, qty string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: typ, Date: date, Quantity: d(qty), Price: d("1"), Amount: d(qty), Currency: "TRY"}
	}
	paid := func(date, amount string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: transaction.TypeDividend, Date: date, Amount: d(amount), Currency: "TRY"}
	}
	planned := dividend.Dividend{InstrumentID: id, Date: "2026-11-20", Amount: d("0.3"), Currency: "TRY"}

	for _, c := range []struct {
		name string
		rows []transaction.Transaction
		divs []dividend.Dividend
		want []string
	}{
		{
			name: "per share of the quantity then times the quantity now",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2026-01-05", "10"), paid("2026-03-10", "25.00"), tx(transaction.TypeBuy, "2026-05-05", "10")},
			want: []string{"2027-03 - false 20 2.5 50.00"},
		},
		{
			name: "a row of the current month a year ago projects nothing, the next month projects",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2025-01-05", "10"), paid("2025-10-05", "25.00"), paid("2025-11-05", "25.00")},
			want: []string{"2026-11 - false 10 2.5 25.00"},
		},
		{
			name: "a row with no holding at its date is skipped",
			rows: []transaction.Transaction{paid("2026-02-10", "25.00"), tx(transaction.TypeBuy, "2026-03-01", "10")},
		},
		{
			name: "a position sold down to zero projects nothing",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2026-01-05", "10"), paid("2026-03-10", "25.00"), tx(transaction.TypeSell, "2026-06-01", "10")},
		},
		{
			name: "a split after the dividend scales the per-share down and keeps the gross",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2026-10-02", "90"), paid("2026-10-12", "450.00"), tx(transaction.TypeSplit, "2026-10-14", "2")},
			want: []string{"2027-10 - false 180 2.5 450.00"},
		},
		{
			name: "a split before the dividend leaves the per-share as it is",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2026-10-02", "90"), tx(transaction.TypeSplit, "2026-10-05", "2"), paid("2026-10-12", "450.00")},
			want: []string{"2027-10 - false 180 2.5 450.00"},
		},
		{
			name: "a planned ex-date replaces the projected row of its period",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2025-01-05", "20"), paid("2025-11-05", "10.00"), paid("2026-01-05", "4.00")},
			divs: []dividend.Dividend{planned},
			want: []string{"2026-11 2026-11-20 true 20 0.3 6.00", "2027-01 - false 20 0.2 4.00"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, r := range dividend.Forecast(c.rows, c.divs, "2026-10-18") {
				date := "-"
				if r.Date != "" {
					date = r.Date
				}
				got = append(got, fmt.Sprintf("%s %s %t %s %s %s", r.Period, date, r.Planned, r.Quantity, r.PerShare, r.Gross.StringFixed(2)))
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}

	periods := dividend.ForecastPeriods("2026-10-18")
	if len(periods) != 12 || periods[0] != "2026-11" || periods[11] != "2027-10" {
		t.Errorf("ForecastPeriods = %v, want 12 from 2026-11 to 2027-10", periods)
	}
}
