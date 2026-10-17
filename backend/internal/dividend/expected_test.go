package dividend_test

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// TestExpected covers the expected-dividends walk of one instrument
// (ADR 0009). Each want row is "date quantity per-share
// gross".
func TestExpected(t *testing.T) {
	d := decimal.RequireFromString
	id := int64(1)
	tx := func(typ, date, qty string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: typ, Date: date, Quantity: d(qty), Price: d("1"), Amount: d(qty), Currency: "USD"}
	}
	paid := func(date string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: transaction.TypeDividend, Date: date, Amount: d("2.50"), Currency: "USD"}
	}
	divs := func(amount string, dates ...string) []dividend.Dividend {
		var out []dividend.Dividend
		for _, date := range dates {
			out = append(out, dividend.Dividend{InstrumentID: id, Date: date, Amount: d(amount), Currency: "USD"})
		}
		return out
	}

	for _, c := range []struct {
		name      string
		rows      []transaction.Transaction
		divs      []dividend.Dividend
		dismissed map[dividend.DismissalKey]bool
		today     string
		want      []string
	}{
		{
			name: "a buy on the ex-date does not count",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-03-01", "10"), tx(transaction.TypeBuy, "2024-05-10", "5")},
			divs: divs("1.2345", "2024-05-10"),
			want: []string{"2024-05-10 10 1.2345 12.35"},
		},
		{
			name: "not held before the first buy or after a full sell",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-02-01", "10"), tx(transaction.TypeSell, "2024-04-01", "10")},
			divs: divs("0.5", "2024-01-15", "2024-03-01", "2024-05-01"),
			want: []string{"2024-03-01 10 0.5 5.00"},
		},
		{
			// O: paid on its ex-date, which only that ex-date gives.
			name: "a recorded dividend on an ex-date hides that one only",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-05", "10"), paid("2024-02-28")},
			divs: divs("0.27", "2024-01-30", "2024-02-28", "2024-03-28"),
			want: []string{"2024-01-30 10 0.27 2.70", "2024-03-28 10 0.27 2.70"},
		},
		{
			// MAIN: paid 6 days before the next ex-date, it hides the
			// previous one, not the nearest.
			name: "a recorded dividend before the next ex-date hides the one before it",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-05", "10"), paid("2024-02-22")},
			divs: divs("0.25", "2024-01-30", "2024-02-28"),
			want: []string{"2024-02-28 10 0.25 2.50"},
		},
		{
			name:  "an ex-date after today is skipped",
			rows:  []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-05", "10")},
			divs:  divs("0.5", "2024-05-10", "2024-07-01"),
			today: "2024-06-30",
			want:  []string{"2024-05-10 10 0.5 5.00"},
		},
		{
			name: "a dismissed ex-date is skipped, per instrument",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-05", "10")},
			divs: divs("0.5", "2024-05-10", "2024-08-10"),
			dismissed: map[dividend.DismissalKey]bool{
				{InstrumentID: id, Date: "2024-05-10"}: true,
				{InstrumentID: 2, Date: "2024-08-10"}:  true,
			},
			want: []string{"2024-08-10 10 0.5 5.00"},
		},
		{
			name: "a sub-cent gross is dropped",
			rows: []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-02", "0.001528463")},
			divs: divs("1.8724", "2024-03-27"),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			today := c.today
			if today == "" {
				today = "2024-12-31"
			}
			got, err := dividend.Expected(c.rows, c.divs, c.dismissed, today)
			if err != nil {
				t.Fatalf("Expected: %v", err)
			}
			var lines []string
			for _, s := range got {
				if s.InstrumentID != id || s.Currency != "USD" {
					t.Errorf("row %+v: want instrument %d, USD", s, id)
				}
				lines = append(lines, s.Date+" "+s.Quantity.String()+" "+s.PerShare.String()+" "+s.Gross.StringFixed(2))
			}
			if !reflect.DeepEqual(lines, c.want) {
				t.Errorf("got  %q\nwant %q", lines, c.want)
			}
		})
	}
}

// TestUpcoming gives one instrument a past and a future ex-date: the future
// one is only in Upcoming, the past one only in Expected, and Upcoming counts
// the quantity held now, not before the ex-date.
func TestUpcoming(t *testing.T) {
	d := decimal.RequireFromString
	id := int64(1)
	tx := func(typ, date, qty string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: typ, Date: date, Quantity: d(qty), Price: d("1"), Amount: d(qty), Currency: "TRY"}
	}
	div := func(date string) dividend.Dividend {
		return dividend.Dividend{InstrumentID: id, Date: date, Amount: d("0.3333"), Currency: "TRY"}
	}
	rows := []transaction.Transaction{tx(transaction.TypeBuy, "2024-01-05", "10"), tx(transaction.TypeBuy, "2024-06-01", "5"), tx(transaction.TypeBuy, "2024-08-01", "100")}
	divs := []dividend.Dividend{div("2024-05-10"), div("2024-07-15")}
	const today = "2024-06-30"

	past, err := dividend.Expected(rows, divs, nil, today)
	if err != nil {
		t.Fatalf("Expected: %v", err)
	}
	if len(past) != 1 || past[0].Date != "2024-05-10" {
		t.Errorf("Expected: got %+v, want only 2024-05-10", past)
	}
	got := dividend.Upcoming(rows, divs, today)
	if len(got) != 1 || got[0].Date != "2024-07-15" || !got[0].Quantity.Equal(d("15")) || got[0].Gross.StringFixed(2) != "5.00" {
		t.Errorf("Upcoming: got %+v, want only 2024-07-15 for 15 shares, gross 5.00", got)
	}

	sold := []transaction.Transaction{rows[0], rows[1], tx(transaction.TypeSell, "2024-06-30", "15")}
	if got := dividend.Upcoming(sold, divs, today); len(got) != 0 {
		t.Errorf("Upcoming after selling everything: got %+v, want none", got)
	}
}
