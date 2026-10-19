package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// seedYears seeds the portfolio of TestYears: 1000 deposited and 10 XXX
// bought at 50 on 2024-11-15, closes of 50, 60, 66 and 72, and a deposit of
// 500 in June 2025. XXX is the first instrument created.
func seedYears(t *testing.T, e *periodsEnv, userID int64) (pf int64) {
	t.Helper()
	pf = e.portfolio(t, userID)
	x := e.instrument(t, "XXX")
	e.cash(t, userID, pf, "2024-11-15", "1000")
	e.buy(t, userID, lot{portfolioID: pf, instrumentID: x, date: "2024-11-15", qty: "10", px: "50"})
	e.cash(t, userID, pf, "2025-06-15", "500")
	prices := price.NewRepo(e.conn)
	for date, closePrice := range map[string]string{"2024-11-15": "50", "2024-12-31": "60", "2025-06-30": "66", "2025-12-31": "72"} {
		if err := prices.Upsert(context.Background(), x, date, closePrice, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	return pf
}

func TestYears(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "years@example.com")
	pf := seedYears(t, e, userID)
	_, other := e.user(t, "other@example.com")
	empty := e.portfolio(t, userID)

	var got yearsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years", pf), http.StatusOK, &got)
	today := todayIstanbul(t)
	if want := atoi(t, today[:4]) - 2024 + 1; len(got.Years) != want {
		t.Fatalf("years = %s, want %d rows, 2024 to %s", asJSON(t, got.Years), want, today[:4])
	}
	if got.Currency != "TRY" || len(got.MissingRates) != 0 || got.MissingRates == nil {
		t.Errorf("currency = %q, missing_rates = %v, want TRY and []", got.Currency, got.MissingRates)
	}
	y24, y25, last := got.Years[0], got.Years[1], got.Years[len(got.Years)-1]

	zero := str("0.00")
	if g, w := asJSON(t, y24.attributionBuckets), asJSON(t, attributionBuckets{str("100.00"), zero, zero, zero}); g != w {
		t.Errorf("2024 buckets = %s, want %s", g, w)
	}
	if y24.Year != "2024" || y24.From != "2024-11-15" || y24.To != "2024-12-31" || !y24.Partial ||
		y24.Opening != "1000.00" || y24.Closing != "1100.00" || y24.NetFlow != "0.00" || y24.Gain != "100.00" {
		t.Errorf("2024 = %s", asJSON(t, y24))
	}
	if y24.Returns.TWR == nil || *y24.Returns.TWR != "0.100000" {
		t.Errorf("2024 twr = %v, want 0.100000", y24.Returns.TWR)
	}

	if g, w := asJSON(t, y25.attributionBuckets), asJSON(t, attributionBuckets{str("120.00"), zero, zero, zero}); g != w {
		t.Errorf("2025 buckets = %s, want %s", g, w)
	}
	if y25.Year != "2025" || y25.From != "2025-01-01" || y25.To != "2025-12-31" || y25.Partial ||
		y25.Opening != "1100.00" || y25.Closing != "1720.00" || y25.NetFlow != "500.00" || y25.Gain != "120.00" {
		t.Errorf("2025 = %s", asJSON(t, y25))
	}
	if y25.Returns.TWR == nil || *y25.Returns.TWR != "0.075000" || y25.Returns.Simple == nil || *y25.Returns.Simple != "0.075000" {
		t.Errorf("2025 returns = %s, want twr and simple 0.075000", asJSON(t, y25.Returns))
	}

	if last.Year != today[:4] || last.Opening != "1720.00" || last.Closing != "1720.00" || last.Gain != "0.00" || !last.Partial {
		t.Errorf("last = %s, want %s opening and closing 1720.00, gain 0.00", asJSON(t, last), today[:4])
	}
	for _, y := range got.Years {
		if y.Benchmark != nil {
			t.Errorf("%s: benchmark = %s without the parameter, want null", y.Year, asJSON(t, y.Benchmark))
		}
	}

	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years?benchmark=XYZ", pf), http.StatusBadRequest, nil)
	e.get(t, other, fmt.Sprintf("/api/portfolios/%d/years", pf), http.StatusNotFound, nil)

	var none yearsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years", empty), http.StatusOK, &none)
	if g, w := asJSON(t, none), `{"currency":"TRY","years":[],"missing_rates":[],"inflation_estimated_from":null}`; g != w {
		t.Errorf("empty portfolio = %s, want %s", g, w)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestYearsMatchPeriods holds every row to the periods route's year bucket.
func TestYearsMatchPeriods(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "years-periods@example.com")
	pf := seedYears(t, e, userID)
	rates := fx.NewRepo(e.conn)
	for date, rate := range map[string]string{"2024-11-01": "34", "2025-07-01": "40"} {
		if err := rates.Upsert(context.Background(), "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	var years yearsResponse
	var periods periodsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years?benchmark=USD", pf), http.StatusOK, &years)
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods?benchmark=USD", pf), http.StatusOK, &periods)
	if len(years.Years) != len(periods.Years) {
		t.Fatalf("%d rows, want the periods route's %d", len(years.Years), len(periods.Years))
	}
	for i, y := range years.Years {
		p := periods.Years[i]
		if y.Year != p.Period {
			t.Fatalf("row %d is %s, want %s", i, y.Year, p.Period)
		}
		got := []any{y.From, y.To, y.Partial, y.Returns, y.Benchmark}
		want := []any{p.From, p.To, p.Partial, p.Returns, p.Benchmark}
		if g, w := asJSON(t, got), asJSON(t, want); g != w {
			t.Errorf("%s = %s, want the periods route's %s", y.Year, g, w)
		}
	}
	if years.Years[1].Benchmark == nil {
		t.Errorf("2025 benchmark is null, want the USD benchmark")
	}
}

func TestYearsInflation(t *testing.T) {
	e := newPeriodsEnv(t)
	ctx := context.Background()
	userID, session := e.user(t, "years-inflation@example.com")
	pf := seedYears(t, e, userID)
	var rows []benchmark.Row
	level := 100.0
	for k := 0; k < 13; k++ {
		month := fmt.Sprintf("%d-%02d-01", 2024+(11+k)/12, (11+k)%12+1)
		rows = append(rows, benchmark.Row{Code: benchmark.CodeTUFE, Date: month, Value: strconv.FormatFloat(level, 'f', -1, 64), Source: "test"})
		level *= 1.02
	}
	if err := benchmark.NewRepo(e.conn).UpsertMany(ctx, rows); err != nil {
		t.Fatalf("benchmarks.UpsertMany: %v", err)
	}

	var got yearsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years", pf), http.StatusOK, &got)
	if got.Years[0].Inflation != nil {
		t.Errorf("2024 inflation = %s, want null: TÜFE starts in December", *got.Years[0].Inflation)
	}
	y25 := got.Years[1]
	var h historyResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/history?from=%s&to=%s", pf, y25.From, y25.To), http.StatusOK, &h)
	if y25.Inflation == nil || h.Inflation == nil || *y25.Inflation != *h.Inflation {
		t.Fatalf("2025 inflation = %v, want the history's %v", y25.Inflation, h.Inflation)
	}
	want := math.Pow(1.02, 12-1.0/31) - 1
	if v, _ := strconv.ParseFloat(*y25.Inflation, 64); math.Abs(v-want) > 1e-3 {
		t.Errorf("2025 inflation = %s, want about %.4f", *y25.Inflation, want)
	}
	if got.InflationEstimatedFrom == nil || *got.InflationEstimatedFrom != "2026-01-01" {
		t.Errorf("inflation_estimated_from = %v, want 2026-01-01", got.InflationEstimatedFrom)
	}

	if err := user.NewRepo(e.conn).UpdateReportingCurrency(ctx, userID, "USD"); err != nil {
		t.Fatalf("UpdateReportingCurrency: %v", err)
	}
	var usd yearsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years", pf), http.StatusOK, &usd)
	for _, y := range usd.Years {
		if y.Inflation != nil {
			t.Errorf("%s: inflation = %s in USD, want null", y.Year, *y.Inflation)
		}
	}
	if usd.InflationEstimatedFrom != nil {
		t.Errorf("inflation_estimated_from = %s in USD, want null", *usd.InflationEstimatedFrom)
	}
}

func TestYearsAllScope(t *testing.T) {
	e := newPeriodsEnv(t)
	idA, sessionA := e.user(t, "years-a@example.com")
	idB, sessionB := e.user(t, "years-b@example.com")
	e.cash(t, idA, e.portfolio(t, idA), "2025-03-01", "1000")
	e.cash(t, idA, e.portfolio(t, idA), "2025-03-01", "1000")
	e.cash(t, idB, e.portfolio(t, idB), "2025-03-01", "300")

	for session, want := range map[string]string{sessionA: "2000.00", sessionB: "300.00"} {
		var got yearsResponse
		e.get(t, session, "/api/portfolios/all/years", http.StatusOK, &got)
		if len(got.Years) == 0 || got.Years[0].Year != "2025" || got.Years[0].Opening != want {
			t.Errorf("all scope = %s, want a 2025 row with opening %s", asJSON(t, got.Years), want)
		}
	}
}

// TestYearsNetFlowMatchesHistory holds each row's net_flow to the history
// route's for the same span. A converted flow that is not whole cents makes
// the rounded-total step differ from the flow, and the step is read on the
// flow's own sweep day, not on the day index within the year.
func TestYearsNetFlowMatchesHistory(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "years-netflow@example.com")
	pf := e.portfolio(t, userID)
	if err := fx.NewRepo(e.conn).Upsert(context.Background(), "USD", "2024-11-01", "34.1234", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}
	e.cash(t, userID, pf, "2024-11-15", "1000")
	e.cashFlowRows(t, userID,
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2025-03-01", Amount: dec("10"), Fee: dec("0"), Currency: "USD"},
		transaction.Transaction{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2025-06-01", Amount: dec("10"), Fee: dec("0"), Currency: "USD"},
	)

	var got yearsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/years", pf), http.StatusOK, &got)
	y25 := got.Years[1]
	if y25.Opening != "1000.00" || y25.Closing != "1682.47" || y25.NetFlow != "682.47" || y25.Gain != "0.00" {
		t.Errorf("2025 = %s, want opening 1000.00, closing 1682.47, net_flow 682.47, gain 0.00", asJSON(t, y25))
	}
	for _, y := range got.Years {
		var h historyResponse
		e.get(t, session, fmt.Sprintf("/api/portfolios/%d/history?from=%s&to=%s", pf, y.From, y.To), http.StatusOK, &h)
		if y.NetFlow != h.NetFlow {
			t.Errorf("%s: net_flow = %s, want the history's %s", y.Year, y.NetFlow, h.NetFlow)
		}
	}
}
