package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// pointReturns is each point's twr or benchmark_twr, "null" for a nil one.
func pointReturns(points []historyEntry, field func(historyEntry) *string) []string {
	out := make([]string, len(points))
	for i, p := range points {
		out[i] = "null"
		if v := field(p); v != nil {
			out[i] = *v
		}
	}
	return out
}

func pointTWR(e historyEntry) *string          { return e.TWR }
func pointBenchmarkTWR(e historyEntry) *string { return e.BenchmarkTWR }

// TestPortfolioHistory seeds two buys (day 1 and day 3) and three prices
// (day 1, day 2, day 4), then checks the computed series for days 1-5 by
// hand:
//
//	day 1: buy 10 @ 5 (cost basis 50), price 5    -> cash -50, value 10*5=50,  total 0,   cost basis 50
//	day 2: (no new tx),                 price 6    -> cash -50, value 10*6=60,  total 10,  cost basis 50
//	day 3: buy 10 @ 7 (cost basis +70), no price   -> cash -120, value carries day 2's price 6: 20*6=120, total 0, cost basis 120
//	day 4: (no new tx),                 price 9    -> cash -120, value 20*9=180, total 60, cost basis 120
//	day 5: (no new tx, no new price)               -> carries day 4's price 9: same as day 4, total 60, cost basis 120
func TestPortfolioHistory(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	buys := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-03", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("7"), Amount: decimal.RequireFromString("70"), Fee: decimal.Zero, Currency: "TRY"},
	}
	for _, tx := range buys {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	for _, p := range []struct{ date, close string }{
		{"2024-01-01", "5"},
		{"2024-01-02", "6"},
		{"2024-01-04", "9"},
	} {
		if err := prices.Upsert(context.Background(), instrumentID, p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert(%s): %v", p.date, err)
		}
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-05", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Currency != "TRY" {
		t.Errorf("currency = %q, want TRY", got.Currency)
	}

	want := []historyEntry{
		{Date: "2024-01-01", TotalValue: "0.00", Cash: "-50.00", CostBasis: "50.00"},
		{Date: "2024-01-02", TotalValue: "10.00", Cash: "-50.00", CostBasis: "50.00"},
		{Date: "2024-01-03", TotalValue: "0.00", Cash: "-120.00", CostBasis: "120.00"},
		{Date: "2024-01-04", TotalValue: "60.00", Cash: "-120.00", CostBasis: "120.00"},
		{Date: "2024-01-05", TotalValue: "60.00", Cash: "-120.00", CostBasis: "120.00"},
	}

	if len(got.Points) != len(want) {
		t.Fatalf("len(points) = %d, want %d; got %+v", len(got.Points), len(want), got.Points)
	}
	for i := range want {
		if got.Points[i] != want[i] {
			t.Errorf("entry[%d] = %+v, want %+v", i, got.Points[i], want[i])
		}
	}
}

// TestPortfolioHistoryFXConversion checks that a non-TRY
// position converts at that day's fx rate, carried forward across days with
// no row, per ADR 0003's "missing day carries the last known rate forward"
// rule (same as prices).
//
// A TRY position (10 @ 5, price never changes so it always nets to zero:
// same pattern as day 1 of TestPortfolioHistory) and a USD position (10 @ 2)
// are bought on day 1. The USD price rises to 3 on day 2 and stays there, so
// the USD position carries a flat 10 USD unrealized gain from day 2 on --
// isolating the fx rate as the only thing that moves total_value from day 2
// to day 3. fx rows: USD = 40 on day 1, USD = 50 on day 3 (day 2 has no row
// and must carry day 1's rate forward).
//
//	day 1: USD unrealized gain 0            -> total_value 0
//	day 2: USD unrealized gain 10 * 40 = 400 -> rate carried forward from day 1
//	day 3: USD unrealized gain 10 * 50 = 500 -> rate updates to day 3's row
func TestPortfolioHistoryFXConversion(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	tryRes, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TRYST', 'BIST', 'TRY Stock', 'TRY')`)
	if err != nil {
		t.Fatalf("insert TRY instrument: %v", err)
	}
	tryInstrumentID, err := tryRes.LastInsertId()
	if err != nil {
		t.Fatalf("TRY instrument id: %v", err)
	}

	usdRes, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDST', 'US', 'USD Stock', 'USD')`)
	if err != nil {
		t.Fatalf("insert USD instrument: %v", err)
	}
	usdInstrumentID, err := usdRes.LastInsertId()
	if err != nil {
		t.Fatalf("USD instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	buys := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &tryInstrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &usdInstrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("2"), Amount: decimal.RequireFromString("20"), Fee: decimal.Zero, Currency: "USD"},
	}
	for _, tx := range buys {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), tryInstrumentID, "2024-01-01", "5", "test"); err != nil {
		t.Fatalf("prices.Upsert TRY: %v", err)
	}
	if err := prices.Upsert(context.Background(), usdInstrumentID, "2024-01-01", "2", "test"); err != nil {
		t.Fatalf("prices.Upsert USD day1: %v", err)
	}
	if err := prices.Upsert(context.Background(), usdInstrumentID, "2024-01-02", "3", "test"); err != nil {
		t.Fatalf("prices.Upsert USD day2: %v", err)
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-01", "40", "manual"); err != nil {
		t.Fatalf("fx.Upsert day1: %v", err)
	}
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "50", "manual"); err != nil {
		t.Fatalf("fx.Upsert day3: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Currency != "TRY" {
		t.Errorf("currency = %q, want TRY", got.Currency)
	}

	want := []historyEntry{
		{Date: "2024-01-01", TotalValue: "0.00", Cash: "-850.00", CostBasis: "850.00"},
		{Date: "2024-01-02", TotalValue: "400.00", Cash: "-850.00", CostBasis: "850.00"},
		{Date: "2024-01-03", TotalValue: "500.00", Cash: "-1050.00", CostBasis: "1050.00"},
	}
	if len(got.Points) != len(want) {
		t.Fatalf("len(points) = %d, want %d; got %+v", len(got.Points), len(want), got.Points)
	}
	for i := range want {
		if got.Points[i] != want[i] {
			t.Errorf("entry[%d] = %+v, want %+v", i, got.Points[i], want[i])
		}
	}
}

// TestPortfolioHistoryNotOwned covers that a portfolio belonging to another
// user 404s instead of leaking its history.
func TestPortfolioHistoryNotOwned(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	owner, err := users.Create(context.Background(), "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create owner: %v", err)
	}
	intruder, err := users.Create(context.Background(), "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create intruder: %v", err)
	}
	intruderSession, err := sessions.CreateSession(context.Background(), intruder.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), owner.ID, "Owner's portfolio")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/history", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: intruderSession.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestPortfolioHistoryInvalidRange covers the 400 cases: from after to, and
// a range wider than 3660 days.
func TestPortfolioHistoryInvalidRange(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	for _, q := range []string{
		"from=2024-01-05&to=2024-01-01",
		"from=2010-01-01&to=2024-01-01",
		// to is clamped to today, which leaves from after it.
		"from=2099-01-01&to=2099-01-05",
	} {
		url := fmt.Sprintf("/api/portfolios/%d/history?%s", pf.ID, q)
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want %d; body: %s", q, rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}
}

// TestPortfolioHistoryDefaultFromClamped covers a transaction far
// older than maxHistoryDays, which used to make the auto-defaulted `from` (the
// earliest transaction date) trip the same "range exceeds 3660 days" check
// that guards an explicit `from`, so "Tümü" ("all") failed silently for any
// portfolio with a transaction more than ~10 years old. With no `from` param
// the handler must clamp the default to `to - maxHistoryDays` and return 200
// instead of 400.
func TestPortfolioHistoryDefaultFromClamped(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	tx := transaction.Transaction{
		PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy,
		Date: "2005-06-15", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"),
		Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY",
	}
	if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	const to = "2026-09-14"
	url := fmt.Sprintf("/api/portfolios/%d/history?to=%s", pf.ID, to)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if want := maxHistoryDays + 1; len(got.Points) != want {
		t.Fatalf("len(points) = %d, want %d", len(got.Points), want)
	}
	if got.Points[len(got.Points)-1].Date != to {
		t.Errorf("last point date = %q, want %q", got.Points[len(got.Points)-1].Date, to)
	}
}

// TestPortfolioHistoryMissingRates checks that the response names what the
// day loop left out when it drops holdings it cannot convert, which draws a
// flat-then-cliff chart. One USD buy on day 1 and the
// first USD fx row on day 3: days 1-2 have no rate, so the range reports
// USD once, and a range starting on day 3 reports nothing. The stock's
// first price lands on day 2, still without a rate, so its first-price jump
// converts to zero and adds nothing to net_flow.
func TestPortfolioHistoryMissingRates(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	usdRes, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDST', 'US', 'USD Stock', 'USD')`)
	if err != nil {
		t.Fatalf("insert USD instrument: %v", err)
	}
	usdInstrumentID, err := usdRes.LastInsertId()
	if err != nil {
		t.Fatalf("USD instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	buy := transaction.Transaction{
		PortfolioID: pf.ID, InstrumentID: &usdInstrumentID, Type: transaction.TypeBuy,
		Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("2"),
		Amount: decimal.RequireFromString("20"), Fee: decimal.Zero, Currency: "USD",
	}
	if _, err := transactions.Create(context.Background(), usr.ID, buy); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), usdInstrumentID, "2024-01-02", "3", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "40", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	get := func(from string) historyResponse {
		t.Helper()
		url := fmt.Sprintf("/api/portfolios/%d/history?from=%s&to=2024-01-03", pf.ID, from)
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got historyResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}

	// Days 1 and 2 convert nothing, so the chart's first two points sit at
	// zero and jump on day 3 -- USD is named once, not once per day.
	uncovered := get("2024-01-01")
	if want := []string{"USD"}; !slices.Equal(uncovered.MissingRates, want) {
		t.Errorf("missing_rates = %v, want %v", uncovered.MissingRates, want)
	}
	// Cash, not total_value: the buy nets value against cash, so the cliff
	// only shows on a line that isn't self-cancelling. Day 1 drops the -20
	// USD cash entirely; day 3 converts it at 40.
	if uncovered.Points[0].Cash != "0.00" {
		t.Errorf("day 1 cash = %q, want 0.00 (the excluded-holding cliff)", uncovered.Points[0].Cash)
	}
	if uncovered.NetFlow != "0.00" {
		t.Errorf("net_flow = %q, want 0.00 (no rate for the day-2 first-price jump)", uncovered.NetFlow)
	}

	covered := get("2024-01-03")
	if len(covered.MissingRates) != 0 {
		t.Errorf("missing_rates = %v, want empty", covered.MissingRates)
	}
	if covered.Points[0].Cash != "-800.00" {
		t.Errorf("day 3 cash = %q, want -800.00 (USD cash converted at 40)", covered.Points[0].Cash)
	}
}

// TestPortfolioHistoryNetFlow checks that net_flow is the
// external cash that arrived after the first point, so the chart header can
// take it out of last - first. A deposit dated on `from` is already inside
// the first point's total and must not be counted again.
//
//	day 1: deposit 100            -> total 100, net flow 0 (already in the first point)
//	day 2: deposit 1000           -> total 1100, net flow 1000
//	day 3: withdrawal 300         -> total 800, net flow 700
//
// The value changed by 700 over the range and all of it was cash moving in
// and out: last - first - net_flow = 0.
func TestPortfolioHistoryNetFlow(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "flow@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("100"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeWithdrawal, Date: "2024-01-03", Amount: decimal.RequireFromString("300"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.NetFlow != "700.00" {
		t.Errorf("net_flow = %q, want 700.00", got.NetFlow)
	}
	if len(got.Points) != 3 {
		t.Fatalf("len(points) = %d, want 3; got %+v", len(got.Points), got.Points)
	}
	if first, last := got.Points[0].TotalValue, got.Points[2].TotalValue; first != "100.00" || last != "800.00" {
		t.Errorf("total_value first = %q, last = %q; want 100.00 and 800.00", first, last)
	}
}

// TestPortfolioHistoryNetFlowCarriesUnratedCurrency checks that a USD
// deposit on a day with no USD fx row converts to nothing, but its cash is
// not lost -- it enters total_value on the first day that has a rate. The
// flow has to land on that same day, or the header's last - first -
// net_flow reports the deposit as a gain.
//
//	day 1: deposit 100 TRY            -> total 100,   net flow 0 (first point)
//	day 2: deposit 1000 USD, no rate  -> total 100,   net flow 0 (carried)
//	day 3: USD rate 40                -> total 40100, net flow 40000
func TestPortfolioHistoryNetFlowCarriesUnratedCurrency(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "carry@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("100"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "USD"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "40", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.NetFlow != "40000.00" {
		t.Errorf("net_flow = %q, want 40000.00 (the carried USD deposit at day 3's rate)", got.NetFlow)
	}
	if len(got.Points) != 3 {
		t.Fatalf("len(points) = %d, want 3; got %+v", len(got.Points), got.Points)
	}
	if first, last := got.Points[0].TotalValue, got.Points[2].TotalValue; first != "100.00" || last != "40100.00" {
		t.Fatalf("total_value first = %q, last = %q; want 100.00 and 40100.00", first, last)
	}
	// The whole range's move was money coming in, so the header's delta is
	// zero once net_flow is taken out.
	first := decimal.RequireFromString(got.Points[0].TotalValue)
	last := decimal.RequireFromString(got.Points[2].TotalValue)
	if delta := last.Sub(first).Sub(decimal.RequireFromString(got.NetFlow)); !delta.IsZero() {
		t.Errorf("last - first - net_flow = %s, want 0", delta)
	}
}

// TestPortfolioHistoryMissingPrice checks that a holding whose first
// price row comes after its buy date counts at cost until then, and those
// points carry price_unknown. The jump to market on the first price day is
// not the range's gain, so it lands in net_flow.
//
//	day 1: deposit 50, buy 10 @ 5, no price -> total 50 (at cost), price unknown
//	day 2: no price                         -> total 50, price unknown
//	day 3: first price 8                    -> total 80, jump 30 into net flow, weight 1/3
//	day 4: carries 8                        -> total 80
func TestPortfolioHistoryMissingPrice(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('LATE', 'BIST', 'Late A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "late@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := price.NewRepo(conn).Upsert(context.Background(), instrumentID, "2024-01-03", "8", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-04", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := []historyEntry{
		{Date: "2024-01-01", TotalValue: "50.00", Cash: "0.00", CostBasis: "50.00", PriceUnknown: true},
		{Date: "2024-01-02", TotalValue: "50.00", Cash: "0.00", CostBasis: "50.00", PriceUnknown: true},
		{Date: "2024-01-03", TotalValue: "80.00", Cash: "0.00", CostBasis: "50.00"},
		{Date: "2024-01-04", TotalValue: "80.00", Cash: "0.00", CostBasis: "50.00"},
	}
	// The first-price jump is a flow, so the daily twr stays flat too.
	if twr := pointReturns(got.Points, pointTWR); !slices.Equal(twr, []string{"0.000000", "0.000000", "0.000000", "0.000000"}) {
		t.Errorf("twr = %v, want all 0.000000", twr)
	}
	for i := range got.Points {
		got.Points[i].TWR = nil
	}
	if !slices.Equal(got.Points, want) {
		t.Errorf("points = %+v, want %+v", got.Points, want)
	}
	if !slices.Equal(got.MissingPrices, []string{"LATE"}) {
		t.Errorf("missing_prices = %v, want [LATE]", got.MissingPrices)
	}
	if got.NetFlow != "30.00" {
		t.Errorf("net_flow = %q, want 30.00 (the first-price jump)", got.NetFlow)
	}
	// values [50, 50, 80, 80], flows [0, 0, 30, 0]: the jump is a flow, so
	// every return is 0. TWR would read 0.600000 if it were not.
	if returns, _ := json.Marshal(got.Returns); string(returns) != `{"simple":"0.000000","twr":"0.000000","mwr":"0.000000"}` {
		t.Errorf("returns = %s, want all 0.000000", returns)
	}
}

// TestPortfolioHistoryNetFlowReconcilesRoundedPoints checks that when
// only a first-price jump moves the total, net_flow equals the change in
// the rounded total_value, even when the jump alone rounds another way.
//
//	day 1: deposit 100, buy A 1 @ 10 (price 10.004), buy B 1 @ 10 (no price)
//	       -> total 100.004 -> "100.00"
//	day 2: B's first price 10.002 -> jump 0.002, total 100.006 -> "100.01"
//
// Rounding the jump on its own gives 0.00, and the header reads +0,01.
func TestPortfolioHistoryNetFlowReconcilesRoundedPoints(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	var ids []int64
	for _, symbol := range []string{"AAA", "BBB"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, symbol, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		ids = append(ids, id)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "round@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	txs := []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("100"), Fee: decimal.Zero, Currency: "TRY"},
	}
	for i := range ids {
		txs = append(txs, transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &ids[i], Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("1"), Price: decimal.RequireFromString("10"), Amount: decimal.RequireFromString("10"), Fee: decimal.Zero, Currency: "TRY"})
	}
	for _, tx := range txs {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), ids[0], "2024-01-01", "10.004", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := prices.Upsert(context.Background(), ids[1], "2024-01-02", "10.002", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-02", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Points) != 2 {
		t.Fatalf("len(points) = %d, want 2; got %+v", len(got.Points), got.Points)
	}
	if first, last := got.Points[0].TotalValue, got.Points[1].TotalValue; first != "100.00" || last != "100.01" {
		t.Fatalf("total_value first = %q, last = %q; want 100.00 and 100.01", first, last)
	}
	if got.NetFlow != "0.01" {
		t.Errorf("net_flow = %q, want 0.01 (last - first total_value)", got.NetFlow)
	}
}

// TestPortfolioHistoryReturns checks that the sweep feeds each point's
// total and the day's external flow into periodReturns (ADR 0011, row 2).
//
//	day 1: deposit 1000, buy 10 @ 100, price 100 -> total 1000
//	day 2: price 80, deposit 1000               -> total 1800, flow 1000
//	day 3: price 71                             -> total 1710
func TestPortfolioHistoryReturns(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('RET', 'BIST', 'Ret A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "returns@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for date, close := range map[string]string{"2024-01-01": "100", "2024-01-02": "80", "2024-01-03": "71"} {
		if err := prices.Upsert(context.Background(), instrumentID, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.NetFlow != "1000.00" {
		t.Errorf("net_flow = %q, want 1000.00", got.NetFlow)
	}
	if returns, _ := json.Marshal(got.Returns); string(returns) != `{"simple":"-0.145000","twr":"-0.240000","mwr":"-0.190000"}` {
		t.Errorf("returns = %s, want simple -0.145000, twr -0.240000, mwr -0.190000; points %+v", returns, got.Points)
	}
	// The day-2 deposit moves the value from 1000 to 1800, but the
	// day's twr is (1800 - 1000) / 1000 - 1, not +80%. The last point ends
	// on the range's twr.
	twr := pointReturns(got.Points, pointTWR)
	if !slices.Equal(twr, []string{"0.000000", "-0.200000", "-0.240000"}) {
		t.Errorf("twr = %v, want [0.000000 -0.200000 -0.240000]", twr)
	}
	if got.Returns.TWR == nil || twr[len(twr)-1] != *got.Returns.TWR {
		t.Errorf("last twr = %s, want returns.twr", twr[len(twr)-1])
	}
	if b := pointReturns(got.Points, pointBenchmarkTWR); !slices.Equal(b, []string{"null", "null", "null"}) {
		t.Errorf("benchmark_twr = %v without a benchmark, want null on every point", b)
	}
}

// TestPortfolioHistoryReturnsNegativeCash buys with no deposit:
// cash is -1000 every day and values are only the P/L, [10, 100, 200], so
// every method would read V_last / V_0 - 1 = 1900%. All three returns are
// null, and so are the USD benchmark's, whose shadow starts at the same 10.
func TestPortfolioHistoryReturnsNegativeCash(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('NEG', 'BIST', 'Neg A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "negative@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	buy := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(conn).Create(context.Background(), usr.ID, buy); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
	prices := price.NewRepo(conn)
	for date, close := range map[string]string{"2024-01-01": "101", "2024-01-02": "110", "2024-01-03": "120"} {
		if err := prices.Upsert(context.Background(), instrumentID, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := fx.NewRepo(conn).Upsert(context.Background(), "USD", "2024-01-01", "10", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03&benchmark=USD", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	const null = `{"simple":null,"twr":null,"mwr":null}`
	if returns, _ := json.Marshal(got.Returns); string(returns) != null {
		t.Errorf("returns = %s, want all null; points %+v", returns, got.Points)
	}
	if b, _ := json.Marshal(got.Benchmark); string(b) != `{"code":"USD","returns":`+null+`,"estimated_from":null}` {
		t.Errorf("benchmark = %s, want all returns null", b)
	}
	// Null on every point, never on some.
	allNull := []string{"null", "null", "null"}
	if twr := pointReturns(got.Points, pointTWR); !slices.Equal(twr, allNull) {
		t.Errorf("twr = %v, want null on every point", twr)
	}
	if b := pointReturns(got.Points, pointBenchmarkTWR); !slices.Equal(b, allNull) {
		t.Errorf("benchmark_twr = %v, want null on every point", b)
	}
}

// TestPortfolioHistoryReturnsSubCentCash deposits the 1235.18 shown and buys
// all-in at a 6-decimal price costing 1235.1842835: cash is
// -0.0042835, which every point reports as 0.00, so it is not a negative
// cash base and the returns are the +5.4% of 1235.18 -> 1301.88.
func TestPortfolioHistoryReturnsSubCentCash(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('IPJ', 'TEFAS', 'Fund', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "subcent@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	txs := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("1235.18"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("1000.5"), Price: decimal.RequireFromString("1.234567"), Amount: decimal.RequireFromString("1235.1842835"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := txs.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for date, close := range map[string]string{"2024-01-01": "1.234567", "2024-01-03": "1.301234"} {
		if err := prices.Upsert(context.Background(), instrumentID, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Returns.Simple == nil || *got.Returns.Simple != "0.054000" {
		b, _ := json.Marshal(got.Returns)
		t.Errorf("returns = %s, want simple 0.054000; points %+v", b, got.Points)
	}
}

// TestPortfolioHistorySeedsOldPrice holds an instrument whose last prices
// are years before the range and has none inside it (a delisted symbol).
// The fetched 8 is older than the user's manual 9, so the manual one is
// the last price and every point values the 10 shares at 90, not at cost
// and not as price_unknown.
func TestPortfolioHistorySeedsOldPrice(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('GONE', 'BIST', 'Gone A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "gone@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2020-01-01", Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2020-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := price.NewRepo(conn).Upsert(context.Background(), instrumentID, "2020-06-01", "8", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := price.NewManualRepo(conn).Upsert(context.Background(), usr.ID, instrumentID, "2021-01-01", "9"); err != nil {
		t.Fatalf("manualPrices.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-03", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := []historyEntry{
		{Date: "2024-01-01", TotalValue: "90.00", Cash: "0.00", CostBasis: "50.00"},
		{Date: "2024-01-02", TotalValue: "90.00", Cash: "0.00", CostBasis: "50.00"},
		{Date: "2024-01-03", TotalValue: "90.00", Cash: "0.00", CostBasis: "50.00"},
	}
	if twr := pointReturns(got.Points, pointTWR); !slices.Equal(twr, []string{"0.000000", "0.000000", "0.000000"}) {
		t.Errorf("twr = %v, want all 0.000000", twr)
	}
	for i := range got.Points {
		got.Points[i].TWR = nil
	}
	if !slices.Equal(got.Points, want) {
		t.Errorf("points = %+v, want %+v", got.Points, want)
	}
	if len(got.MissingPrices) != 0 {
		t.Errorf("missing_prices = %v, want none", got.MissingPrices)
	}
	if got.NetFlow != "0.00" {
		t.Errorf("net_flow = %q, want 0.00", got.NetFlow)
	}
}

// TestPortfolioHistoryExplicitFromClamped covers an explicit
// `from` a year before the only deposit, which used to draw zeros up to it and
// count the deposit as a mid-range flow, inflating the range return. The
// series must start on the deposit day, with the deposit inside the first
// point instead of in net_flow.
func TestPortfolioHistoryExplicitFromClamped(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "clamp@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	deposit := transaction.Transaction{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-03-01", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(conn).Create(context.Background(), usr.ID, deposit); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2023-06-01&to=2024-06-01", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if first := got.Points[0]; first.Date != "2024-03-01" || first.TotalValue != "1000.00" {
		t.Errorf("first point = %+v, want 2024-03-01 with total 1000.00", first)
	}
	if got.NetFlow != "0.00" {
		t.Errorf("net_flow = %q, want 0.00", got.NetFlow)
	}
}

// TestPortfolioHistoryFutureToClamped covers a `to` after today, which
// used to be taken as is, so with no `from` the series ran up to 2099 and
// the default window (`to` - maxHistoryDays) dropped the real history.
// `to` is clamped to today in Europe/Istanbul instead.
func TestPortfolioHistoryFutureToClamped(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "future@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	today := price.Today()
	todayDate, err := time.Parse(time.DateOnly, today)
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}
	first := todayDate.AddDate(0, 0, -30).Format(time.DateOnly)
	deposit := transaction.Transaction{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: first, Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(conn).Create(context.Background(), usr.ID, deposit); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?to=2099-01-01", pf.ID)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got.Points) != 31 {
		t.Fatalf("len(points) = %d, want 31", len(got.Points))
	}
	if d := got.Points[0].Date; d != first {
		t.Errorf("first point date = %q, want %q", d, first)
	}
	if d := got.Points[len(got.Points)-1].Date; d != today {
		t.Errorf("last point date = %q, want %q", d, today)
	}
}

// TestSplitRowsKeepValueFlat checks that transaction.Repo.List hands the
// summary and the value history one split row per split of an instrument the
// portfolio holds. Alice buys 10 @ 100 on day 1; the unadjusted close drops
// from 100 to 50 on day 3, the ex-date of a 2-for-1 split. Her position must
// read 20 @ avg 50 worth 1000, and history must stay flat across the ex-date
// instead of stepping down by 500. Bob holds only another instrument: no
// split row reaches his list.
//
// A second alice portfolio also buys 2 @ 50 on the ex-date itself: the split
// must apply before it (22 shares), not after (24), in the history sweep too,
// which applies rows in List's order.
func TestSplitRowsKeepValueFlat(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	var ids []int64
	for _, sym := range []string{"SPLT", "OTHR"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, sym, sym)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		ids = append(ids, id)
	}
	splitID, otherID := ids[0], ids[1]

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	portfolios := portfolio.NewRepo(conn)
	transactions := transaction.NewRepo(conn)
	prices := price.NewRepo(conn)

	type account struct {
		user, pf int64
		session  string
	}
	newAccount := func(email string, instrumentID int64) account {
		usr, err := users.Create(context.Background(), email, "hash")
		if err != nil {
			t.Fatalf("users.Create: %v", err)
		}
		session, err := sessions.CreateSession(context.Background(), usr.ID)
		if err != nil {
			t.Fatalf("sessions.CreateSession: %v", err)
		}
		pf, err := portfolios.Create(ctx, usr.ID, "Test")
		if err != nil {
			t.Fatalf("portfolios.Create: %v", err)
		}
		if _, err := transactions.Create(ctx, usr.ID, transaction.Transaction{
			PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01",
			Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"),
			Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY",
		}); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
		return account{usr.ID, pf.ID, session.ID}
	}
	alice := newAccount("alice@example.com", splitID)
	bob := newAccount("bob@example.com", otherID)

	for _, p := range []struct {
		id          int64
		date, close string
	}{
		{splitID, "2024-01-01", "100"},
		{splitID, "2024-01-03", "50"},
		{otherID, "2024-01-01", "100"},
	} {
		if err := prices.Upsert(ctx, p.id, p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := split.NewRepo(conn).Upsert(ctx, splitID, "2024-01-03", decimal.RequireFromString("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	get := func(a account, path string, out any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/%s", a.pf, path), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: a.session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; body: %s", path, rec.Code, rec.Body.String())
		}
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	var sum summaryResponse
	get(alice, "summary", &sum)
	if len(sum.Positions) != 1 {
		t.Fatalf("alice positions = %+v, want 1", sum.Positions)
	}
	if p := sum.Positions[0]; p.Quantity != "20" || p.AvgCost != "50" || p.MarketValue != "1000.00" {
		t.Errorf("alice position qty/avg/value = %s/%s/%s, want 20/50/1000.00", p.Quantity, p.AvgCost, p.MarketValue)
	}

	// Market value 1000 every day, so total (value + cash) is 0 every day.
	// Without the split row day 3 would read 10*50 - 1000 = -500.
	var hist historyResponse
	get(alice, "history?from=2024-01-01&to=2024-01-03", &hist)
	if len(hist.Points) != 3 {
		t.Fatalf("alice history = %+v, want 3 points", hist.Points)
	}
	for _, p := range hist.Points {
		if p.TotalValue != "0.00" || p.Cash != "-1000.00" {
			t.Errorf("alice %s total/cash = %s/%s, want 0.00/-1000.00", p.Date, p.TotalValue, p.Cash)
		}
	}

	// Split before the ex-date buy: 22*50 - 1100 = 0. After it: 24*50 - 1100 = 100.
	pf2, err := portfolios.Create(ctx, alice.user, "Ex-date")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf2.ID, InstrumentID: &splitID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf2.ID, InstrumentID: &splitID, Type: transaction.TypeBuy, Date: "2024-01-03", Quantity: decimal.RequireFromString("2"), Price: decimal.RequireFromString("50"), Amount: decimal.RequireFromString("100"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, alice.user, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	get(account{alice.user, pf2.ID, alice.session}, "history?from=2024-01-03&to=2024-01-03", &hist)
	if len(hist.Points) != 1 || hist.Points[0].TotalValue != "0.00" {
		t.Errorf("ex-date buy history = %+v, want total 0.00 (split applied first)", hist.Points)
	}

	// A bonus buy at price 0 two days after the ex-date books the split for
	// its portfolio and is held from the ex-date, when the price is
	// scaled: 20*50 - 1000 = 0 every day. Held only from its own date, days 3
	// and 4 would read 10*50 - 1000 = -500.
	pf3, err := portfolios.Create(ctx, alice.user, "Bonus")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf3.ID, InstrumentID: &splitID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf3.ID, InstrumentID: &splitID, Type: transaction.TypeBuy, Date: "2024-01-05", Quantity: decimal.RequireFromString("10"), Price: decimal.Zero, Amount: decimal.Zero, Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, alice.user, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	get(account{alice.user, pf3.ID, alice.session}, "history?from=2024-01-01&to=2024-01-05", &hist)
	if len(hist.Points) != 5 {
		t.Fatalf("bonus history = %+v, want 5 points", hist.Points)
	}
	for _, p := range hist.Points {
		if p.TotalValue != "0.00" {
			t.Errorf("bonus %s total = %s, want 0.00", p.Date, p.TotalValue)
		}
	}

	get(bob, "summary", &sum)
	if len(sum.Positions) != 1 || sum.Positions[0].Quantity != "10" {
		t.Errorf("bob positions = %+v, want one of quantity 10", sum.Positions)
	}
	txs, err := transactions.List(ctx, bob.user, bob.pf)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(txs) != 1 || txs[0].Type != transaction.TypeBuy {
		t.Errorf("bob List = %+v, want his one buy", txs)
	}
}

// historyBenchmarkFixture is TestPortfolioHistoryBenchmark's shared state:
// one user and portfolio, read by every subtest and changed by some, so the
// subtests run in the order the test lists them.
type historyBenchmarkFixture struct {
	handler      http.Handler
	session      string
	usr          user.User
	pf           portfolio.Portfolio
	users        *user.Repo
	transactions *transaction.Repo
	benchmarks   *benchmark.Repo
}

const benchmarkUnavailable = `"returns":{"simple":null,"twr":null,"mwr":null},"estimated_from":null}`

var benchmarkAllNull = []string{"null", "null", "null"}

func (f *historyBenchmarkFixture) get(t *testing.T, query string, wantStatus int) map[string]json.RawMessage {
	t.Helper()
	url := fmt.Sprintf("/api/portfolios/%d/history?%s", f.pf.ID, query)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: f.session})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// benchmarkValues is each point's benchmark_value, skipping points without one.
func benchmarkValues(t *testing.T, got map[string]json.RawMessage) []string {
	t.Helper()
	var points []map[string]any
	if err := json.Unmarshal(got["points"], &points); err != nil {
		t.Fatalf("decode points: %v", err)
	}
	var out []string
	for _, p := range points {
		if v, ok := p["benchmark_value"]; ok {
			out = append(out, v.(string))
		}
	}
	return out
}

// benchmarkTWR is each point's benchmark_twr.
func benchmarkTWR(t *testing.T, got map[string]json.RawMessage) []string {
	t.Helper()
	var points []historyEntry
	if err := json.Unmarshal(got["points"], &points); err != nil {
		t.Fatalf("decode points: %v", err)
	}
	return pointReturns(points, pointBenchmarkTWR)
}

// decodeBenchmark decodes the response's benchmark object.
func decodeBenchmark(t *testing.T, got map[string]json.RawMessage) benchmarkResponse {
	t.Helper()
	var b benchmarkResponse
	if err := json.Unmarshal(got["benchmark"], &b); err != nil {
		t.Fatalf("decode benchmark: %v", err)
	}
	return b
}

func (f *historyBenchmarkFixture) upsertBenchmark(t *testing.T, code string, levels map[string]string) {
	t.Helper()
	for date, level := range levels {
		if err := f.benchmarks.UpsertMany(context.Background(), []benchmark.Row{{Code: code, Date: date, Value: level, Source: "test"}}); err != nil {
			t.Fatalf("benchmarks.UpsertMany: %v", err)
		}
	}
}

// TestPortfolioHistoryBenchmark reports in TRY with USD at 10, 8, 7.6 and
// 1000 TRY deposited on day 1 and on day 2, no trades: values [1000, 2000,
// 2000], flows [0, 1000, 0]. The USD shadow buys 100 units on day 1 and 125
// on day 2, so S = [1000, 1800, 1710], the series of ADR 0012 row 1: twr
// 7.6/10 - 1 = -0.24, simple (1710 - 1000 - 1000) / 2000 = -0.145, mwr
// (1 - 0.1)^2 - 1 = -0.19, since 1000·0.9^2 + 1000·0.9 = 1710. XU100 and
// TUFE take their levels from benchmark_values, seeded in their subtests.
func TestPortfolioHistoryBenchmark(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	f := &historyBenchmarkFixture{
		handler:      New(logger, conn, false, settings.NewRepo(conn, nil), nil),
		users:        user.NewRepo(conn),
		transactions: transaction.NewRepo(conn),
		benchmarks:   benchmark.NewRepo(conn),
	}
	if f.usr, err = f.users.Create(context.Background(), "benchmark@example.com", "hash"); err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), f.usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	f.session = session.ID
	if f.pf, err = portfolio.NewRepo(conn).Create(context.Background(), f.usr.ID, "Test"); err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	for _, date := range []string{"2024-01-01", "2024-01-02"} {
		tx := transaction.Transaction{PortfolioID: f.pf.ID, Type: transaction.TypeDeposit, Date: date, Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := f.transactions.Create(context.Background(), f.usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	fxRates := fx.NewRepo(conn)
	for date, rate := range map[string]string{"2024-01-01": "10", "2024-01-02": "8", "2024-01-03": "7.6"} {
		if err := fxRates.Upsert(context.Background(), "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	// Order matters: the XU100 subtests seed benchmark_values after "XU100
	// with no rows", and "no level on the first day" adds a deposit.
	t.Run("USD", f.testUSD)
	t.Run("absent", f.testAbsent)
	t.Run("XU100 with no rows", f.testXU100NoRows)
	t.Run("XU100 from benchmark_values", f.testXU100)
	t.Run("XU100 in USD", f.testXU100InUSD)
	t.Run("TUFE", f.testTUFE)
	t.Run("inflation", f.testInflation)
	t.Run("GOLD from benchmark_values", f.testGOLD)
	t.Run("unknown code", func(t *testing.T) {
		f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=SILVER", http.StatusBadRequest)
	})
	t.Run("no level on the first day", f.testNoLevelOnFirstDay)
}

func (f *historyBenchmarkFixture) testUSD(t *testing.T) {
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=USD", http.StatusOK)
	if values := benchmarkValues(t, got); !slices.Equal(values, []string{"1000.00", "1800.00", "1710.00"}) {
		t.Errorf("benchmark_value = %v, want [1000.00 1800.00 1710.00]", values)
	}
	if b := string(got["benchmark"]); b != `{"code":"USD","returns":{"simple":"-0.145000","twr":"-0.240000","mwr":"-0.190000"},"estimated_from":null}` {
		t.Errorf("benchmark = %s", b)
	}
	if r := string(got["returns"]); r != `{"simple":"0.000000","twr":"0.000000","mwr":"0.000000"}` {
		t.Errorf("returns = %s, want the portfolio's own zeros", r)
	}
	// The shadow's own daily twr, ending on its returns.twr.
	if b := benchmarkTWR(t, got); !slices.Equal(b, []string{"0.000000", "-0.200000", "-0.240000"}) {
		t.Errorf("benchmark_twr = %v, want [0.000000 -0.200000 -0.240000]", b)
	}
}

func (f *historyBenchmarkFixture) testAbsent(t *testing.T) {
	got := f.get(t, "from=2024-01-01&to=2024-01-03", http.StatusOK)
	if b := string(got["benchmark"]); b != "null" {
		t.Errorf("benchmark = %s, want null", b)
	}
	if values := benchmarkValues(t, got); len(values) != 0 {
		t.Errorf("benchmark_value = %v, want none", values)
	}
	if b := benchmarkTWR(t, got); !slices.Equal(b, benchmarkAllNull) {
		t.Errorf("benchmark_twr = %v, want null on every point", b)
	}
}

func (f *historyBenchmarkFixture) testXU100NoRows(t *testing.T) {
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=XU100", http.StatusOK)
	if b := string(got["benchmark"]); b != `{"code":"XU100",`+benchmarkUnavailable {
		t.Errorf("benchmark = %s", b)
	}
	if values := benchmarkValues(t, got); len(values) != 0 {
		t.Errorf("benchmark_value = %v, want none", values)
	}
	// The shadow has no value: null on every point.
	if b := benchmarkTWR(t, got); !slices.Equal(b, benchmarkAllNull) {
		t.Errorf("benchmark_twr = %v, want null on every point", b)
	}
}

func (f *historyBenchmarkFixture) testXU100(t *testing.T) {
	// Closes 10000 and 8000, none on day 3, so day 2 carries: the
	// shadow buys 0.1 units, then 0.125, and its twr is 8000/10000 - 1.
	f.upsertBenchmark(t, benchmark.CodeXU100, map[string]string{"2024-01-01": "10000", "2024-01-02": "8000"})
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=XU100", http.StatusOK)
	if values := benchmarkValues(t, got); !slices.Equal(values, []string{"1000.00", "1800.00", "1800.00"}) {
		t.Errorf("benchmark_value = %v, want [1000.00 1800.00 1800.00]", values)
	}
	if b := decodeBenchmark(t, got); b.Returns.TWR == nil || *b.Returns.TWR != "-0.200000" {
		t.Errorf("benchmark = %s, want twr -0.200000", got["benchmark"])
	}
	if bt := benchmarkTWR(t, got); !slices.Equal(bt, []string{"0.000000", "-0.200000", "-0.200000"}) {
		t.Errorf("benchmark_twr = %v, want [0.000000 -0.200000 -0.200000]", bt)
	}
}

func (f *historyBenchmarkFixture) testXU100InUSD(t *testing.T) {
	// In USD the values are [100, 250, 263.16] with a 125 flow on day 2,
	// and XU100 is [1000, 1000, 1052.63]: units 0.1, then 0.225.
	if err := f.users.UpdateReportingCurrency(context.Background(), f.usr.ID, "USD"); err != nil {
		t.Fatalf("users.UpdateReportingCurrency: %v", err)
	}
	defer func() {
		if err := f.users.UpdateReportingCurrency(context.Background(), f.usr.ID, "TRY"); err != nil {
			t.Fatalf("users.UpdateReportingCurrency: %v", err)
		}
	}()
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=XU100", http.StatusOK)
	if values := benchmarkValues(t, got); !slices.Equal(values, []string{"100.00", "225.00", "236.84"}) {
		t.Errorf("benchmark_value = %v, want [100.00 225.00 236.84]", values)
	}
}

func (f *historyBenchmarkFixture) testTUFE(t *testing.T) {
	// December 100 and January 110 spread January's 10% over its 31
	// days (ADR 0012), so day 2 is 1000 * 1.1^(1/31) + 1000.
	f.upsertBenchmark(t, benchmark.CodeTUFE, map[string]string{"2023-12-01": "100", "2024-01-01": "110"})
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=TUFE", http.StatusOK)
	if values := benchmarkValues(t, got); len(values) != 3 || values[1] != "2003.08" {
		t.Errorf("benchmark_value = %v, want day 2 2003.08", values)
	}
	if b := decodeBenchmark(t, got); b.EstimatedFrom != nil {
		t.Errorf("estimated_from = %q inside the last stored month, want null", *b.EstimatedFrom)
	}

	got = f.get(t, "from=2024-01-01&to=2024-02-02&benchmark=TUFE", http.StatusOK)
	if b := decodeBenchmark(t, got); b.EstimatedFrom == nil || *b.EstimatedFrom != "2024-02-01" {
		t.Errorf("benchmark = %s, want estimated_from 2024-02-01", got["benchmark"])
	}
}

// testInflation runs after testTUFE seeded December 100 and January 110:
// day 1 is 100 * 1.1^(1/31) and day 3 is 100 * 1.1^(3/31), so over the
// range the CPI change is 1.1^(2/31) - 1, whatever benchmark is requested.
func (f *historyBenchmarkFixture) testInflation(t *testing.T) {
	want := fmt.Sprintf(`"%.6f"`, math.Pow(1.1, 2.0/31)-1)
	for _, query := range []string{"from=2024-01-01&to=2024-01-03", "from=2024-01-01&to=2024-01-03&benchmark=USD"} {
		got := f.get(t, query, http.StatusOK)
		if inf := string(got["inflation"]); inf != want {
			t.Errorf("%s: inflation = %s, want %s", query, inf, want)
		}
		if est := string(got["inflation_estimated_from"]); est != "null" {
			t.Errorf("%s: inflation_estimated_from = %s inside the last stored month, want null", query, est)
		}
	}

	got := f.get(t, "from=2024-01-01&to=2024-02-02", http.StatusOK)
	if est := string(got["inflation_estimated_from"]); est != `"2024-02-01"` {
		t.Errorf("inflation_estimated_from = %s, want \"2024-02-01\"", est)
	}

	if err := f.users.UpdateReportingCurrency(context.Background(), f.usr.ID, "USD"); err != nil {
		t.Fatalf("users.UpdateReportingCurrency: %v", err)
	}
	defer func() {
		if err := f.users.UpdateReportingCurrency(context.Background(), f.usr.ID, "TRY"); err != nil {
			t.Fatalf("users.UpdateReportingCurrency: %v", err)
		}
	}()
	got = f.get(t, "from=2024-01-01&to=2024-01-03", http.StatusOK)
	if inf := string(got["inflation"]); inf != "null" {
		t.Errorf("USD reporting: inflation = %s, want null", inf)
	}
	if est := string(got["inflation_estimated_from"]); est != "null" {
		t.Errorf("USD reporting: inflation_estimated_from = %s, want null", est)
	}
}

func (f *historyBenchmarkFixture) testGOLD(t *testing.T) {
	// TRY per gram 2000 and 2500, none on day 3, so day 2 carries: the
	// shadow buys 0.5 grams, then 0.4, and its twr is 2500/2000 - 1.
	f.upsertBenchmark(t, benchmark.CodeGOLD, map[string]string{"2024-01-01": "2000", "2024-01-02": "2500"})
	got := f.get(t, "from=2024-01-01&to=2024-01-03&benchmark=GOLD", http.StatusOK)
	if values := benchmarkValues(t, got); !slices.Equal(values, []string{"1000.00", "2250.00", "2250.00"}) {
		t.Errorf("benchmark_value = %v, want [1000.00 2250.00 2250.00]", values)
	}
	if b := decodeBenchmark(t, got); b.Returns.TWR == nil || *b.Returns.TWR != "0.250000" {
		t.Errorf("benchmark = %s, want twr 0.250000", got["benchmark"])
	}
}

func (f *historyBenchmarkFixture) testNoLevelOnFirstDay(t *testing.T) {
	// Moving the range start before the first USD rate needs the first
	// deposit on that day too, else from clamps to it.
	early := transaction.Transaction{PortfolioID: f.pf.ID, Type: transaction.TypeDeposit, Date: "2023-12-31", Amount: decimal.RequireFromString("1"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := f.transactions.Create(context.Background(), f.usr.ID, early); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
	for _, code := range []string{"USD", "XU100", "TUFE", "GOLD"} {
		got := f.get(t, "from=2023-12-31&to=2024-01-03&benchmark="+code, http.StatusOK)
		if b := string(got["benchmark"]); b != `{"code":"`+code+`",`+benchmarkUnavailable {
			t.Errorf("benchmark = %s", b)
		}
		if values := benchmarkValues(t, got); len(values) != 0 {
			t.Errorf("%s benchmark_value = %v, want none", code, values)
		}
		if b := benchmarkTWR(t, got); !slices.Equal(b, []string{"null", "null", "null", "null"}) {
			t.Errorf("%s benchmark_twr = %v, want null on every point", code, b)
		}
	}
}

// TestPortfolioHistoryInstrumentBenchmark checks ?benchmark=instrument:<id>.
// Fund B closes 10, 8 and 7.6, the USD example's prices, and
// the portfolio is worth 1000 on day one and gets 1000 more on day 2, so
// the shadow is the USD example's [1000, 1800, 1710] and its twr is B's own
// 7.6/10 - 1.
func TestPortfolioHistoryInstrumentBenchmark(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	ids := map[string]int64{}
	for _, sym := range []string{"AAA", "BBB", "CCC", "LATE", "OTHER"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', ?, 'TRY')`, sym, sym)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[sym], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	users := user.NewRepo(conn)
	usr, err := users.Create(context.Background(), "instrument-benchmark@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(ctx, usr.ID, "Funds")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	second, err := portfolios.Create(ctx, usr.ID, "Second")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	d := decimal.RequireFromString
	instr := func(sym string) *int64 { id := ids[sym]; return &id }
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: d("1000"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: instr("AAA"), Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: instr("BBB"), Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: instr("LATE"), Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("1"), Price: d("10"), Amount: d("10"), Currency: "TRY"},
		{PortfolioID: second.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("100"), Currency: "TRY"},
		{PortfolioID: second.ID, InstrumentID: instr("OTHER"), Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("1"), Price: d("10"), Amount: d("10"), Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, p := range []struct {
		sym, date, close string
	}{
		{"AAA", "2024-01-01", "10"}, {"AAA", "2024-01-02", "11"}, {"AAA", "2024-01-03", "12"},
		{"BBB", "2024-01-01", "10"}, {"BBB", "2024-01-02", "8"}, {"BBB", "2024-01-03", "7.6"},
		{"CCC", "2024-01-01", "10"},
		{"LATE", "2024-01-02", "10"},
		{"OTHER", "2024-01-01", "10"}, {"OTHER", "2024-01-03", "12"},
	} {
		if err := prices.Upsert(ctx, ids[p.sym], p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	for date, rate := range map[string]string{"2024-01-01": "10", "2024-01-02": "8", "2024-01-03": "7.6"} {
		if err := fx.NewRepo(conn).Upsert(ctx, "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	get := func(t *testing.T, scope, benchmark string, wantStatus int) historyResponse {
		t.Helper()
		url := "/api/portfolios/" + scope + "/history?from=2024-01-01&to=2024-01-03&benchmark=" + benchmark
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != wantStatus {
			t.Fatalf("%s status = %d, want %d; body: %s", benchmark, rec.Code, wantStatus, rec.Body.String())
		}
		var got historyResponse
		if wantStatus == http.StatusOK {
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return got
	}
	shadow := func(got historyResponse) []string {
		var out []string
		for _, p := range got.Points {
			out = append(out, p.BenchmarkValue)
		}
		return out
	}
	str := func(p *string) string {
		if p == nil {
			return "null"
		}
		return *p
	}
	pfID := fmt.Sprint(pf.ID)
	bCode := fmt.Sprintf("instrument:%d", ids["BBB"])

	t.Run("held fund", func(t *testing.T) {
		got := get(t, pfID, bCode, http.StatusOK)
		if values := shadow(got); !slices.Equal(values, []string{"1000.00", "1800.00", "1710.00"}) {
			t.Errorf("benchmark_value = %v, want [1000.00 1800.00 1710.00]", values)
		}
		b := got.Benchmark
		if b == nil || b.Code != bCode || b.EstimatedFrom != nil {
			t.Fatalf("benchmark = %+v, want code %s and no estimated_from", b, bCode)
		}
		if r := b.Returns; str(r.TWR) != "-0.240000" || str(r.Simple) != "-0.145000" || str(r.MWR) != "-0.190000" {
			t.Errorf("returns = %s %s %s, want -0.145000 -0.240000 -0.190000", str(r.Simple), str(r.TWR), str(r.MWR))
		}
	})
	t.Run("rejected", func(t *testing.T) {
		for _, code := range []string{
			fmt.Sprintf("instrument:%d", ids["CCC"]),   // never traded
			fmt.Sprintf("instrument:%d", ids["OTHER"]), // traded in another portfolio only
			"instrument:abc", "instrument:0", "instrument:", fmt.Sprintf("instrument:+%d", ids["BBB"]),
		} {
			get(t, pfID, code, http.StatusBadRequest)
		}
	})
	t.Run("first close after from", func(t *testing.T) {
		got := get(t, pfID, fmt.Sprintf("instrument:%d", ids["LATE"]), http.StatusOK)
		if r := got.Benchmark.Returns; r.Simple != nil || r.TWR != nil || r.MWR != nil {
			t.Errorf("returns = %+v, want all null", r)
		}
		if values := shadow(got); !slices.Equal(values, []string{"", "", ""}) {
			t.Errorf("benchmark_value = %v, want none", values)
		}
	})
	t.Run("all scope", func(t *testing.T) {
		got := get(t, "all", fmt.Sprintf("instrument:%d", ids["OTHER"]), http.StatusOK)
		if twr := str(got.Benchmark.Returns.TWR); twr != "0.200000" {
			t.Errorf("twr = %s, want 0.200000", twr)
		}
	})
	t.Run("in USD", func(t *testing.T) {
		// B's TRY closes over the USD rates are 1 every day: a flat
		// benchmark, and the shadow is V_0 = 100 USD plus 1000/8 on day 2.
		if err := users.UpdateReportingCurrency(context.Background(), usr.ID, "USD"); err != nil {
			t.Fatalf("users.UpdateReportingCurrency: %v", err)
		}
		defer func() {
			if err := users.UpdateReportingCurrency(context.Background(), usr.ID, "TRY"); err != nil {
				t.Fatalf("users.UpdateReportingCurrency: %v", err)
			}
		}()
		got := get(t, pfID, bCode, http.StatusOK)
		if values := shadow(got); !slices.Equal(values, []string{"100.00", "225.00", "225.00"}) {
			t.Errorf("benchmark_value = %v, want [100.00 225.00 225.00]", values)
		}
		if twr := str(got.Benchmark.Returns.TWR); twr != "0.000000" {
			t.Errorf("twr = %s, want 0.000000", twr)
		}
	})
}

// TestPortfolioHistoryInstrumentBenchmarkSplit checks that a split of the
// benchmark instrument is not a loss: the unadjusted close halves on the
// ex-date of a 2-for-1 split (100, 100, 50, 55), and the benchmark series
// must stay flat across it, then follow the 50 -> 55 move.
func TestPortfolioHistoryInstrumentBenchmarkSplit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('SPLT', 'BIST', 'SPLT', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	usr, err := user.NewRepo(conn).Create(ctx, "benchmark-split@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	for date, close := range map[string]string{"2024-01-01": "100", "2024-01-02": "100", "2024-01-03": "50", "2024-01-04": "55"} {
		if err := price.NewRepo(conn).Upsert(ctx, id, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := split.NewRepo(conn).Upsert(ctx, id, "2024-01-03", d("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	url := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-01&to=2024-01-04&benchmark=instrument:%d", pf.ID, id)
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	New(logger, conn, false, settings.NewRepo(conn, nil), nil).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var got historyResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var values []string
	for _, p := range got.Points {
		values = append(values, p.BenchmarkValue)
	}
	if want := []string{"1000.00", "1000.00", "1000.00", "1100.00"}; !slices.Equal(values, want) {
		t.Errorf("benchmark_value = %v, want %v", values, want)
	}
}

// TestPortfolioHistoryAllScope checks the `all` scope against the
// per-portfolio series. A buys X on day 1 and sells it all on day
// 2; B's day-2 buy of X is saved first, so a lower id: one shared State in
// id order would buy B's shares into A's lot and sell A's at the mixed
// average cost, leaving day 2's cost basis at 250 instead of 300.
// Both hold Y, split 2:1 on day 3: each portfolio's List carries its own
// synthetic split row, which one shared State would apply twice.
func TestPortfolioHistoryAllScope(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	var xID, yID int64
	for sym, id := range map[string]*int64{"XXX": &xID, "YYY": &yID} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, sym, sym)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if *id, err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "all@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	portfolios := portfolio.NewRepo(conn)
	a, err := portfolios.Create(ctx, usr.ID, "A")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	b, err := portfolios.Create(ctx, usr.ID, "B")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: a.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Currency: "TRY"},
		{PortfolioID: b.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Currency: "TRY"},
		{PortfolioID: a.ID, InstrumentID: &xID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{PortfolioID: a.ID, InstrumentID: &yID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("5"), Price: d("10"), Amount: d("50"), Currency: "TRY"},
		{PortfolioID: b.ID, InstrumentID: &yID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("5"), Price: d("10"), Amount: d("50"), Currency: "TRY"},
		{PortfolioID: b.ID, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: d("500"), Currency: "TRY"},
		{PortfolioID: b.ID, InstrumentID: &xID, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("10"), Price: d("20"), Amount: d("200"), Currency: "TRY"},
		{PortfolioID: a.ID, InstrumentID: &xID, Type: transaction.TypeSell, Date: "2024-01-02", Quantity: d("10"), Price: d("25"), Amount: d("250"), Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, p := range []struct {
		id          int64
		date, close string
	}{{xID, "2024-01-01", "10"}, {xID, "2024-01-02", "25"}, {yID, "2024-01-01", "10"}, {yID, "2024-01-03", "6"}} {
		if err := prices.Upsert(ctx, p.id, p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := split.NewRepo(conn).Upsert(ctx, yID, "2024-01-03", d("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}
	if err := fx.NewRepo(conn).Upsert(ctx, "USD", "2024-01-01", "30", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	get := func(id string) historyResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/history?from=2024-01-01&to=2024-01-03&benchmark=USD", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; body: %s", id, rec.Code, rec.Body.String())
		}
		var got historyResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode %s: %v", id, err)
		}
		if len(got.Points) != 3 {
			t.Fatalf("%s points = %+v, want 3", id, got.Points)
		}
		return got
	}
	all, pa, pb := get("all"), get(fmt.Sprint(a.ID)), get(fmt.Sprint(b.ID))

	for i, p := range all.Points {
		sum := func(field func(historyEntry) string) string {
			return d(field(pa.Points[i])).Add(d(field(pb.Points[i]))).StringFixed(2)
		}
		if want := sum(func(e historyEntry) string { return e.TotalValue }); p.TotalValue != want {
			t.Errorf("%s total_value = %s, want %s", p.Date, p.TotalValue, want)
		}
		if want := sum(func(e historyEntry) string { return e.CostBasis }); p.CostBasis != want {
			t.Errorf("%s cost_basis = %s, want %s", p.Date, p.CostBasis, want)
		}
		if p.BenchmarkValue == "" {
			t.Errorf("%s benchmark_value missing", p.Date)
		}
	}
	// Day 2: B's X at 200 plus Y at 50 in each portfolio; A's X is closed.
	// Day 3: cash 1100 + 1250, plus 10 X at 25 and 20 Y at 6 (40 if the
	// split were applied twice).
	if got := all.Points[1].CostBasis; got != "300.00" {
		t.Errorf("day 2 cost_basis = %s, want 300.00", got)
	}
	if got := all.Points[2].TotalValue; got != "2720.00" {
		t.Errorf("day 3 total_value = %s, want 2720.00", got)
	}
	if all.Returns.TWR == nil {
		t.Errorf("returns = %+v, want a twr", all.Returns)
	}
	if all.Benchmark == nil || all.Benchmark.Returns.TWR == nil {
		t.Errorf("benchmark = %+v, want USD with a twr", all.Benchmark)
	}
}
