package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// cashFlowRows inserts rows of pf for the cash flow tests.
func (e *periodsEnv) cashFlowRows(t *testing.T, userID int64, rows ...transaction.Transaction) {
	t.Helper()
	if _, err := transaction.NewRepo(e.conn).CreateAll(context.Background(), userID, rows); err != nil {
		t.Fatalf("transactions.CreateAll: %v", err)
	}
}

// monthsFrom counts the calendar months from first (YYYY-MM) to today's.
func monthsFrom(t *testing.T, first string) int {
	t.Helper()
	f, err := time.Parse("2006-01", first)
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.DateOnly, price.Today())
	if err != nil {
		t.Fatal(err)
	}
	return (now.Year()-f.Year())*12 + int(now.Month()-f.Month()) + 1
}

func TestCashFlows(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "cf@example.com")
	pf := e.portfolio(t, userID)
	id := e.instrument(t, "THYAO")
	e.cashFlowRows(t, userID,
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2026-07-10", Amount: dec("1000"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2026-09-05", Amount: dec("500"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeWithdrawal, Date: "2026-09-20", Amount: dec("200"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: pf, InstrumentID: &id, Type: transaction.TypeDividend, Date: "2026-09-25", Amount: dec("100"), Fee: dec("10"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDividend, Date: "2026-08-01", Amount: dec("50"), Currency: "TRY"},
	)
	var got cashFlowsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/cashflows", pf), http.StatusOK, &got)

	if want := monthsFrom(t, "2026-07"); len(got.Months) != want {
		t.Fatalf("months = %d, want %d (2026-07 to %s)", len(got.Months), want, price.Today()[:7])
	}
	for i, m := range got.Months {
		want := time.Date(2026, 7+time.Month(i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		if m.Period != want {
			t.Fatalf("months[%d].period = %q, want %q", i, m.Period, want)
		}
	}
	zero := "0.00"
	if want := (cashFlowBucket{Period: "2026-08", Deposits: zero, Withdrawals: zero, Net: zero, Dividends: zero, Interest: "50.00", Fees: zero}); got.Months[1] != want {
		t.Errorf("2026-08 = %+v, want %+v", got.Months[1], want)
	}
	if want := (cashFlowBucket{Period: "2026-09", Deposits: "500.00", Withdrawals: "200.00", Net: "300.00", Dividends: "90.00", Interest: zero, Fees: zero}); got.Months[2] != want {
		t.Errorf("2026-09 = %+v, want %+v", got.Months[2], want)
	}
	if got.Totals.Period != "" || got.Totals.Deposits != "1500.00" || got.Totals.Net != "1300.00" || got.Totals.Dividends != "90.00" {
		t.Errorf("totals = %+v, want deposits 1500.00, net 1300.00, dividends 90.00", got.Totals)
	}
	if got.Currency != "TRY" || len(got.MissingRates) != 0 {
		t.Errorf("currency %q missing_rates %v, want TRY and none", got.Currency, got.MissingRates)
	}
}

func TestCashFlowsAllScope(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "all@example.com")
	_, otherSession := e.user(t, "other@example.com")
	p1, p2 := e.portfolio(t, userID), e.portfolio(t, userID)
	e.cash(t, userID, p1, "2026-07-10", "1000")
	e.cash(t, userID, p2, "2026-07-20", "250")

	var got cashFlowsResponse
	e.get(t, session, "/api/portfolios/all/cashflows", http.StatusOK, &got)
	if got.Months[0].Period != "2026-07" || got.Months[0].Deposits != "1250.00" || got.Totals.Deposits != "1250.00" {
		t.Errorf("all = %+v totals %+v, want 2026-07 deposits 1250.00", got.Months[0], got.Totals)
	}

	var other cashFlowsResponse
	e.get(t, otherSession, "/api/portfolios/all/cashflows", http.StatusOK, &other)
	if asJSON(t, other.Months) != "[]" || other.Totals.Deposits != "0.00" || asJSON(t, other.MissingRates) != "[]" {
		t.Errorf("other user's all = %s, want empty months and zero totals", asJSON(t, other))
	}
	e.get(t, otherSession, fmt.Sprintf("/api/portfolios/%d/cashflows", p1), http.StatusNotFound, nil)
}

func TestCashFlowsMissingRate(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "usd@example.com")
	pf := e.portfolio(t, userID)
	e.cashFlowRows(t, userID, transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2026-07-10", Amount: dec("1000"), Currency: "USD"})
	path := fmt.Sprintf("/api/portfolios/%d/cashflows", pf)

	var got cashFlowsResponse
	e.get(t, session, path, http.StatusOK, &got)
	if got.Totals.Deposits != "0.00" || got.Months[0].Deposits != "0.00" || fmt.Sprint(got.MissingRates) != "[USD]" {
		t.Errorf("without a rate: month %+v totals %+v missing %v, want zeros and [USD]", got.Months[0], got.Totals, got.MissingRates)
	}

	if err := fx.NewRepo(e.conn).Upsert(context.Background(), "USD", "2026-07-01", "40", "manual"); err != nil {
		t.Fatal(err)
	}
	e.get(t, session, path, http.StatusOK, &got)
	if got.Months[0].Deposits != "40000.00" || len(got.MissingRates) != 0 {
		t.Errorf("with a rate: deposits %q missing %v, want 40000.00 and none", got.Months[0].Deposits, got.MissingRates)
	}
}
