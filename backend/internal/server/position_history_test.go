package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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

// TestPositionHistory sweeps one holding:
//
//	day 1: buy 10 @ 100, no price yet -> close null, 10, cost 1000, valued at cost 1000
//	day 2: price 110                  -> 10 * 110 = 1100
//	day 3: no new row                 -> carries 110
//	day 4: 2:1 split, price 60        -> 20 @ avg 50, cost 1000, 20 * 60 = 1200
//	day 5: sell 5                     -> 15, cost 1000 - 5*50 = 750, 15 * 60 = 900
//
// XU100 is 1000 on day 2 and 1100 on day 5, the dollar 30 then 33 TRY. Another
// user asking for the portfolio gets a 404.
func TestPositionHistory(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('POS', 'BIST', 'Pos A.S.', 'TRY')`)
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
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	aliceSession, err := sessions.CreateSession(context.Background(), alice.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	bobSession, err := sessions.CreateSession(context.Background(), bob.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, alice.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-01-05", Quantity: decimal.RequireFromString("5"), Price: decimal.RequireFromString("60"), Amount: decimal.RequireFromString("300"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, alice.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, p := range []struct{ date, close string }{{"2024-01-02", "110"}, {"2024-01-04", "60"}} {
		if err := prices.Upsert(ctx, instrumentID, p.date, p.close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := split.NewRepo(conn).Upsert(ctx, instrumentID, "2024-01-04", decimal.RequireFromString("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	for _, date := range []string{"2024-01-02", "2024-01-05"} {
		level, rate := "1000", "30"
		if date == "2024-01-05" {
			level, rate = "1100", "33"
		}
		if err := benchmark.NewRepo(conn).UpsertMany(ctx, []benchmark.Row{{Code: benchmark.CodeXU100, Date: date, Value: level, Source: "test"}}); err != nil {
			t.Fatalf("benchmarks.UpsertMany: %v", err)
		}
		if err := fx.NewRepo(conn).Upsert(ctx, "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	get := func(session string, query ...string) *httptest.ResponseRecorder {
		url := fmt.Sprintf("/api/portfolios/%d/instruments/%d/history?to=2024-01-05", pf.ID, instrumentID)
		for _, q := range query {
			url += "&" + q
		}
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	rec := get(aliceSession.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var got positionHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Currency != "TRY" {
		t.Errorf("currency = %q, want TRY", got.Currency)
	}
	want := []struct{ date, close, qty, cost, value string }{
		{"2024-01-01", "", "10", "1000.00", "1000.00"},
		{"2024-01-02", "110", "10", "1000.00", "1100.00"},
		{"2024-01-03", "110", "10", "1000.00", "1100.00"},
		{"2024-01-04", "60", "20", "1000.00", "1200.00"},
		{"2024-01-05", "60", "15", "750.00", "900.00"},
	}
	if len(got.Points) != len(want) {
		t.Fatalf("points = %+v, want %d", got.Points, len(want))
	}
	for i, w := range want {
		p := got.Points[i]
		close := "" // stands for null
		if p.Close != nil {
			close = *p.Close
		}
		if p.Date != w.date || close != w.close || p.Quantity != w.qty || p.CostBasis != w.cost || p.MarketValue != w.value {
			t.Errorf("point[%d] = date %s close %s qty %s cost %s value %s, want %+v", i, p.Date, close, p.Quantity, p.CostBasis, p.MarketValue, w)
		}
	}

	// Values 1100, 1100, 1200, 900 from the first close; the sell on day 5
	// (flow -300) is the only flow.
	if got.ReturnsFrom == nil || *got.ReturnsFrom != "2024-01-02" {
		t.Errorf("returns_from = %v, want 2024-01-02", got.ReturnsFrom)
	}
	for name, r := range map[string]*string{"twr": got.Returns.TWR, "mwr": got.Returns.MWR} {
		if r == nil {
			t.Errorf("returns.%s = null, want 0.090909", name)
		} else if *r != "0.090909" {
			t.Errorf("returns.%s = %s, want 0.090909", name, *r)
		}
	}
	// simple = (900 - 1100 + 300) / (1100 - 300), the sell leaving the base.
	if got.Returns.Simple == nil || *got.Returns.Simple != "0.125000" {
		t.Errorf("returns.simple = %v, want 0.125000", got.Returns.Simple)
	}
	wantTWR := []string{"null", "0.000000", "0.000000", "0.090909", "0.090909"}
	for i, w := range wantTWR {
		twr := "null"
		if got.Points[i].Twr != nil {
			twr = *got.Points[i].Twr
		}
		if twr != w {
			t.Errorf("point[%d].twr = %s, want %s", i, twr, w)
		}
	}

	if got.Benchmark != nil {
		t.Errorf("benchmark = %+v, want null without ?benchmark=", got.Benchmark)
	}

	// The shadow buys 1100 / 1000 units on day 2 and sells 300 / 1100 of them
	// on day 5, so its twr is the benchmark's own 10%. The dollar, level 1,
	// goes 30 to 33 the same way; GOLD has no level stored.
	str := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	for _, tt := range []struct {
		code   string
		twr    string
		points []string
	}{
		{"XU100", "0.100000", []string{"null", "0.000000", "0.000000", "0.000000", "0.100000"}},
		{"USD", "0.100000", []string{"null", "0.000000", "0.000000", "0.000000", "0.100000"}},
		{"GOLD", "null", []string{"null", "null", "null", "null", "null"}},
	} {
		rec := get(aliceSession.ID, "benchmark="+tt.code)
		var b positionHistoryResponse
		if rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&b) != nil || b.Benchmark == nil {
			t.Fatalf("%s: status = %d, benchmark %+v; body: %s", tt.code, rec.Code, b.Benchmark, rec.Body.String())
		}
		if b.Benchmark.Code != tt.code || str(b.Benchmark.Returns.TWR) != tt.twr {
			t.Errorf("%s: benchmark = %s twr %s, want twr %s", tt.code, b.Benchmark.Code, str(b.Benchmark.Returns.TWR), tt.twr)
		}
		if tt.twr == "null" && (b.Benchmark.Returns.Simple != nil || b.Benchmark.Returns.MWR != nil) {
			t.Errorf("%s: returns = %+v, want all null", tt.code, b.Benchmark.Returns)
		}
		for i, w := range tt.points {
			if g := str(b.Points[i].BenchmarkTwr); g != w {
				t.Errorf("%s: point[%d].benchmark_twr = %s, want %s", tt.code, i, g, w)
			}
		}
	}
	for _, code := range []string{"instrument:1", "bogus"} {
		if rec := get(aliceSession.ID, "benchmark="+code); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", code, rec.Code)
		}
	}

	if rec := get(bobSession.ID); rec.Code != http.StatusNotFound {
		t.Errorf("bob status = %d, want 404", rec.Code)
	}
}

// TestPositionHistoryDeposit checks that a deposit's position history carries
// the close from its terms: 1 on the start day, +0.0008 a day, flat from
// maturity. The setup is depositHistoryEnv's.
func TestPositionHistoryDeposit(t *testing.T) {
	e, session, portfolioID, instrumentID := depositHistoryEnv(t)
	var got positionHistoryResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/instruments/%d/history?from=2026-01-10&to=2026-01-14", portfolioID, instrumentID), http.StatusOK, &got)
	want := []struct{ date, close, value string }{
		{"2026-01-10", "1", "1000.00"},
		{"2026-01-11", "1.0008", "1000.80"},
		{"2026-01-12", "1.0016", "1001.60"},
		{"2026-01-13", "1.0024", "1002.40"},
		{"2026-01-14", "1.0024", "1002.40"},
	}
	if len(got.Points) != len(want) {
		t.Fatalf("points = %+v, want %d", got.Points, len(want))
	}
	for i, w := range want {
		p := got.Points[i]
		if p.Close == nil || p.Date != w.date || *p.Close != w.close || p.Quantity != "1000" || p.MarketValue != w.value {
			t.Errorf("point[%d] = %+v, want %+v", i, p, w)
		}
	}
}

// TestPositionFlow checks the money a row moves in or out of the position:
// a buy and its fee in, a sell or a dividend net of its fee out, the rest zero.
func TestPositionFlow(t *testing.T) {
	tests := []struct {
		name string
		tx   transaction.Transaction
		want string
	}{
		{"buy", transaction.Transaction{Type: transaction.TypeBuy, Amount: decimal.RequireFromString("1000"), Fee: decimal.RequireFromString("2")}, "1002"},
		{"sell", transaction.Transaction{Type: transaction.TypeSell, Amount: decimal.RequireFromString("300"), Fee: decimal.RequireFromString("1")}, "-299"},
		{"dividend", transaction.Transaction{Type: transaction.TypeDividend, Amount: decimal.RequireFromString("25"), Fee: decimal.RequireFromString("3.75")}, "-21.25"},
		{"split", transaction.Transaction{Type: transaction.TypeSplit}, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := positionFlow(tt.tx); !got.Equal(decimal.RequireFromString(tt.want)) {
				t.Errorf("positionFlow = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestPositionHistoryShortIsUndefined checks that a sell with no holding,
// valued negative on the first day with a close, answers null returns and a
// null twr on every point.
func TestPositionHistoryShortIsUndefined(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "short@example.com")
	portfolioID := e.portfolio(t, userID)
	instrumentID := e.instrument(t, "SHORT")
	sell := transaction.Transaction{PortfolioID: portfolioID, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-01-01", Quantity: decimal.RequireFromString("5"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("500"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(e.conn).Create(context.Background(), userID, sell); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
	if err := price.NewRepo(e.conn).Upsert(context.Background(), instrumentID, "2024-01-01", "100", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	var got positionHistoryResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/instruments/%d/history?to=2024-01-03", portfolioID, instrumentID), http.StatusOK, &got)
	if got.ReturnsFrom == nil || *got.ReturnsFrom != "2024-01-01" {
		t.Errorf("returns_from = %v, want 2024-01-01", got.ReturnsFrom)
	}
	if got.Returns.Simple != nil || got.Returns.TWR != nil || got.Returns.MWR != nil {
		t.Errorf("returns = %+v, want all null", got.Returns)
	}
	for i, p := range got.Points {
		if p.Twr != nil {
			t.Errorf("point[%d].twr = %s, want null", i, *p.Twr)
		}
	}
}
