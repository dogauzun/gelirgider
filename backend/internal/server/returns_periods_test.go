package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// periodsEnv is one database with the server on it, for the periods tests.
type periodsEnv struct {
	conn       *sql.DB
	handler    http.Handler
	portfolios int // names portfolios P1, P2, ...
}

func newPeriodsEnv(t *testing.T) *periodsEnv {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return &periodsEnv{conn: conn, handler: New(logger, conn, false, settings.NewRepo(conn, nil), nil)}
}

// user creates a user and returns its id and a session cookie value.
func (e *periodsEnv) user(t *testing.T, email string) (int64, string) {
	t.Helper()
	usr, err := user.NewRepo(e.conn).Create(context.Background(), email, "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(e.conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return usr.ID, session.ID
}

func (e *periodsEnv) portfolio(t *testing.T, userID int64) int64 {
	t.Helper()
	e.portfolios++
	pf, err := portfolio.NewRepo(e.conn).Create(context.Background(), userID, fmt.Sprint("P", e.portfolios))
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	return pf.ID
}

func (e *periodsEnv) instrument(t *testing.T, symbol string) int64 {
	t.Helper()
	res, err := e.conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, symbol, symbol)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	return id
}

// cash records a deposit (amount > 0) or a withdrawal (amount < 0) in TRY.
func (e *periodsEnv) cash(t *testing.T, userID, portfolioID int64, date, amount string) {
	t.Helper()
	d := decimal.RequireFromString(amount)
	tx := transaction.Transaction{PortfolioID: portfolioID, Type: transaction.TypeDeposit, Date: date, Amount: d, Fee: decimal.Zero, Currency: "TRY"}
	if d.IsNegative() {
		tx.Type, tx.Amount = transaction.TypeWithdrawal, d.Neg()
	}
	if _, err := transaction.NewRepo(e.conn).Create(context.Background(), userID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
}

// lot is one TRY buy for periodsEnv.buy, its fields named so two ids cannot
// swap silently.
type lot struct {
	portfolioID, instrumentID int64
	date, qty, px             string
}

func (e *periodsEnv) buy(t *testing.T, userID int64, l lot) {
	t.Helper()
	q, p := decimal.RequireFromString(l.qty), decimal.RequireFromString(l.px)
	tx := transaction.Transaction{PortfolioID: l.portfolioID, InstrumentID: &l.instrumentID, Type: transaction.TypeBuy, Date: l.date, Quantity: q, Price: p, Amount: q.Mul(p), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(e.conn).Create(context.Background(), userID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
}

func (e *periodsEnv) get(t *testing.T, session, path string, wantStatus int, out any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	if rec.Code != wantStatus {
		t.Fatalf("GET %s: status = %d, want %d; body: %s", path, rec.Code, wantStatus, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// presetRange is the range the frontend's calendar presets send for a
// bucket's period: YYYY-01-01..YYYY-12-31 for a year (resolveRange in
// frontend/src/lib/ranges.ts), the first to the last day for a month.
func presetRange(period string) (string, string) {
	if len(period) == 4 {
		return period + "-01-01", period + "-12-31"
	}
	first, _ := time.Parse("2006-01", period)
	return first.Format(time.DateOnly), first.AddDate(0, 1, -1).Format(time.DateOnly)
}

// checkBucketsMatchHistory holds every bucket to the history endpoint for
// the same preset range: the same dates, the same returns, the same
// benchmark returns, and a null benchmark exactly where the history has no
// shadow series.
func checkBucketsMatchHistory(t *testing.T, e *periodsEnv, session, scope, bench string, got periodsResponse) {
	t.Helper()
	for _, buckets := range [][]periodBucket{got.Months, got.Years} {
		for _, b := range buckets {
			from, to := presetRange(b.Period)
			query := fmt.Sprintf("from=%s&to=%s", from, to)
			if bench != "" {
				query += "&benchmark=" + bench
			}
			var h historyResponse
			e.get(t, session, fmt.Sprintf("/api/portfolios/%s/history?%s", scope, query), http.StatusOK, &h)
			if first, last := h.Points[0].Date, h.Points[len(h.Points)-1].Date; b.From != first || b.To != last {
				t.Errorf("%s: from..to = %s..%s, want the history's %s..%s", b.Period, b.From, b.To, first, last)
			}
			if g, w := asJSON(t, b.Returns), asJSON(t, h.Returns); g != w {
				t.Errorf("%s: returns = %s, want the history's %s", b.Period, g, w)
			}
			switch {
			case bench == "":
				if b.Benchmark != nil {
					t.Errorf("%s: benchmark = %s without the parameter, want null", b.Period, asJSON(t, b.Benchmark))
				}
			case b.Benchmark == nil:
				if h.Points[0].BenchmarkValue != "" {
					t.Errorf("%s: benchmark null, but the history has a shadow series", b.Period)
				}
			default:
				if g, w := asJSON(t, b.Benchmark), asJSON(t, h.Benchmark.Returns); g != w {
					t.Errorf("%s: benchmark = %s, want the history's %s", b.Period, g, w)
				}
			}
		}
	}
}

// checkBucketShape checks buckets are oldest first with no gaps, and that
// exactly the first and the last are partial.
func checkBucketShape(t *testing.T, buckets []periodBucket) {
	t.Helper()
	for i, b := range buckets {
		if want := i == 0 || i == len(buckets)-1; b.Partial != want {
			t.Errorf("%s: partial = %v, want %v", b.Period, b.Partial, want)
		}
		if i == 0 {
			continue
		}
		prev, _ := time.Parse(time.DateOnly, buckets[i-1].To)
		if next := prev.AddDate(0, 0, 1).Format(time.DateOnly); b.From != next {
			t.Errorf("%s: from = %s, want %s, the day after %s", b.Period, b.From, next, buckets[i-1].Period)
		}
	}
}

func todayIstanbul(t *testing.T) string {
	t.Helper()
	return time.Now().In(price.Istanbul).Format(time.DateOnly)
}

// TestPeriodReturnsMatchHistory seeds a portfolio from November 2024 with
// a mid-year deposit in 2025, a withdrawal, and a holding bought in March
// 2025 that has no price until 10 April, whose first-price jump is a flow
// only in a range that opens while it is unpriced (April). Every month's
// and year's bucket must equal the history endpoint for that period's
// preset range, with no benchmark, with USD and with XU100, whose levels
// start on 10 March 2025.
func TestPeriodReturnsMatchHistory(t *testing.T) {
	e := newPeriodsEnv(t)
	ctx := context.Background()
	userID, session := e.user(t, "periods@example.com")
	pf := e.portfolio(t, userID)
	x, y := e.instrument(t, "XXX"), e.instrument(t, "YYY")

	e.cash(t, userID, pf, "2024-11-15", "1000")
	e.buy(t, userID, lot{portfolioID: pf, instrumentID: x, date: "2024-11-15", qty: "10", px: "50"})
	e.buy(t, userID, lot{portfolioID: pf, instrumentID: y, date: "2025-03-20", qty: "5", px: "40"})
	e.cash(t, userID, pf, "2025-06-15", "2000")
	e.buy(t, userID, lot{portfolioID: pf, instrumentID: x, date: "2025-06-16", qty: "20", px: "61"})
	e.cash(t, userID, pf, "2025-09-10", "-300")

	prices := price.NewRepo(e.conn)
	for _, p := range []struct {
		id          int64
		date, close string
	}{
		{x, "2024-11-15", "50"}, {x, "2024-12-10", "55"}, {x, "2025-01-01", "52"},
		{x, "2025-02-14", "60"}, {x, "2025-05-20", "58"}, {x, "2025-06-15", "61"},
		{x, "2025-08-01", "70"}, {x, "2025-12-31", "72"}, {x, "2026-03-02", "75"},
		{y, "2025-04-10", "45"}, {y, "2025-07-01", "47"},
	} {
		if err := prices.Upsert(ctx, p.id, p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	rates := fx.NewRepo(e.conn)
	for date, rate := range map[string]string{"2024-11-01": "34", "2025-01-01": "35.5", "2025-06-01": "38", "2025-10-01": "41", "2026-01-01": "43"} {
		if err := rates.Upsert(ctx, "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}
	benchmarks := benchmark.NewRepo(e.conn)
	for date, level := range map[string]string{"2025-03-10": "10000", "2025-05-05": "11000", "2025-09-01": "12500", "2026-02-01": "13000"} {
		if err := benchmarks.UpsertMany(ctx, []benchmark.Row{{Code: benchmark.CodeXU100, Date: date, Value: level, Source: "test"}}); err != nil {
			t.Fatalf("benchmarks.UpsertMany: %v", err)
		}
	}

	for _, bench := range []string{"", "USD", "XU100"} {
		t.Run("benchmark="+bench, func(t *testing.T) {
			path := fmt.Sprintf("/api/portfolios/%d/returns/periods", pf)
			if bench != "" {
				path += "?benchmark=" + bench
			}
			var got periodsResponse
			e.get(t, session, path, http.StatusOK, &got)
			if got.Currency != "TRY" {
				t.Errorf("currency = %q, want TRY", got.Currency)
			}
			today := todayIstanbul(t)
			if len(got.Months) == 0 || got.Months[0].Period != "2024-11" || got.Months[len(got.Months)-1].Period != today[:7] {
				t.Fatalf("months = %s, want 2024-11 to %s", asJSON(t, got.Months), today[:7])
			}
			if len(got.Years) == 0 || got.Years[0].Period != "2024" || got.Years[len(got.Years)-1].Period != today[:4] {
				t.Fatalf("years = %s, want 2024 to %s", asJSON(t, got.Years), today[:4])
			}
			checkBucketShape(t, got.Months)
			checkBucketShape(t, got.Years)
			checkBucketsMatchHistory(t, e, session, fmt.Sprint(pf), bench, got)

			byPeriod := map[string]periodBucket{}
			for _, b := range append(got.Months, got.Years...) {
				byPeriod[b.Period] = b
			}
			// Not a comparison of two nulls: 2025 and its June have returns.
			for _, period := range []string{"2025", "2025-06", "2025-04"} {
				if byPeriod[period].Returns.TWR == nil {
					t.Errorf("%s: twr is null, want a return", period)
				}
			}
			switch bench {
			case "USD":
				for _, b := range append(got.Months, got.Years...) {
					if b.Benchmark == nil {
						t.Errorf("%s: USD benchmark null, want covered", b.Period)
					}
				}
			case "XU100":
				// XU100 starts on 10 March 2025: March and the year 2025
				// open before it, April is covered.
				for period, covered := range map[string]bool{"2025-02": false, "2025-03": false, "2025": false, "2025-04": true, "2026": true} {
					if (byPeriod[period].Benchmark != nil) != covered {
						t.Errorf("%s: benchmark = %s, want covered %v", period, asJSON(t, byPeriod[period].Benchmark), covered)
					}
				}
			}
		})
	}
}

// TestPeriodReturnsDepositsOnly: a year whose only activity is deposits
// earns zero by ADR 0011's rules, and a year that opens at a value of zero
// has no return, never a division by zero.
func TestPeriodReturnsDepositsOnly(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "deposits@example.com")
	kept := e.portfolio(t, userID)
	e.cash(t, userID, kept, "2024-12-01", "1000")
	e.cash(t, userID, kept, "2025-03-01", "500")
	emptied := e.portfolio(t, userID)
	e.cash(t, userID, emptied, "2024-06-01", "1000")
	e.cash(t, userID, emptied, "2024-12-01", "-1000")
	e.cash(t, userID, emptied, "2025-05-01", "500")

	year := func(t *testing.T, pf int64, period string) periodBucket {
		t.Helper()
		var got periodsResponse
		e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods", pf), http.StatusOK, &got)
		for _, b := range got.Years {
			if b.Period == period {
				return b
			}
		}
		t.Fatalf("no %s bucket in %s", period, asJSON(t, got.Years))
		return periodBucket{}
	}
	if got, want := asJSON(t, year(t, kept, "2025").Returns), `{"simple":"0.000000","twr":"0.000000","mwr":"0.000000"}`; got != want {
		t.Errorf("deposits only: 2025 returns = %s, want %s", got, want)
	}
	if got, want := asJSON(t, year(t, emptied, "2025").Returns), `{"simple":null,"twr":null,"mwr":null}`; got != want {
		t.Errorf("opens at zero: 2025 returns = %s, want %s", got, want)
	}
}

// TestPeriodReturnsStartedThisMonth: one month and one year, both partial.
func TestPeriodReturnsStartedThisMonth(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "new@example.com")
	pf := e.portfolio(t, userID)
	today := todayIstanbul(t)
	e.cash(t, userID, pf, today, "1000")

	var got periodsResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods", pf), http.StatusOK, &got)
	if len(got.Months) != 1 || len(got.Years) != 1 {
		t.Fatalf("months = %s, years = %s, want one of each", asJSON(t, got.Months), asJSON(t, got.Years))
	}
	for _, b := range []periodBucket{got.Months[0], got.Years[0]} {
		if !b.Partial || b.From != today || b.To != today {
			t.Errorf("%s: %s, want partial %s..%s", b.Period, asJSON(t, b), today, today)
		}
	}
	if got.Months[0].Period != today[:7] || got.Years[0].Period != today[:4] {
		t.Errorf("periods = %s, %s, want %s, %s", got.Months[0].Period, got.Years[0].Period, today[:7], today[:4])
	}

	// No transaction at all: no buckets, as empty arrays.
	empty := e.portfolio(t, userID)
	var raw map[string]json.RawMessage
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods", empty), http.StatusOK, &raw)
	if string(raw["months"]) != "[]" || string(raw["years"]) != "[]" {
		t.Errorf("empty portfolio: months = %s, years = %s, want []", raw["months"], raw["years"])
	}
}

// TestPeriodReturnsScope: `all` sums the caller's portfolios and only
// theirs; another user's portfolio is not found; an unknown benchmark is a
// 400.
func TestPeriodReturnsScope(t *testing.T) {
	e := newPeriodsEnv(t)
	ctx := context.Background()
	x := e.instrument(t, "XXX")
	for date, close := range map[string]string{"2025-02-01": "10", "2025-04-15": "12", "2025-08-01": "9"} {
		if err := price.NewRepo(e.conn).Upsert(ctx, x, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}

	userID, session := e.user(t, "owner@example.com")
	p1 := e.portfolio(t, userID)
	e.cash(t, userID, p1, "2025-02-01", "1000")
	e.buy(t, userID, lot{portfolioID: p1, instrumentID: x, date: "2025-02-01", qty: "50", px: "10"})
	p2 := e.portfolio(t, userID)
	e.cash(t, userID, p2, "2025-05-10", "500")
	e.buy(t, userID, lot{portfolioID: p2, instrumentID: x, date: "2025-05-10", qty: "20", px: "12"})

	otherID, _ := e.user(t, "other@example.com")
	other := e.portfolio(t, otherID)
	e.cash(t, otherID, other, "2023-01-01", "999")

	var got periodsResponse
	e.get(t, session, "/api/portfolios/all/returns/periods", http.StatusOK, &got)
	if len(got.Months) == 0 || got.Months[0].Period != "2025-02" {
		t.Fatalf("all: months = %s, want the caller's first month 2025-02", asJSON(t, got.Months))
	}
	checkBucketShape(t, got.Months)
	checkBucketsMatchHistory(t, e, session, "all", "", got)

	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods", other), http.StatusNotFound, nil)
	e.get(t, session, "/api/portfolios/abc/returns/periods", http.StatusNotFound, nil)
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods?benchmark=FOO", p1), http.StatusBadRequest, nil)
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods?benchmark=instrument:%d", p1, x+100), http.StatusBadRequest, nil)
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/returns/periods?benchmark=instrument:%d", p1, x), http.StatusOK, nil)
}

// TestUnknownBenchmarkIsBadRequest: the history, periods and years routes
// share scopeWithBenchmark, which answers an unknown benchmark with the same
// 400 body.
func TestUnknownBenchmarkIsBadRequest(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "owner@example.com")
	p := e.portfolio(t, userID)

	for _, route := range []string{"history", "returns/periods", "years"} {
		var got map[string]string
		e.get(t, session, fmt.Sprintf("/api/portfolios/%d/%s?benchmark=FOO", p, route), http.StatusBadRequest, &got)
		if got["error"] != "unknown benchmark" {
			t.Errorf("%s: error = %q, want %q", route, got["error"], "unknown benchmark")
		}
	}
}
