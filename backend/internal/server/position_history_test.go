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
	"github.com/dogauzun/gelirgider/backend/internal/db"
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
// Another user asking for the portfolio gets a 404.
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

	get := func(session string) *httptest.ResponseRecorder {
		url := fmt.Sprintf("/api/portfolios/%d/instruments/%d/history?to=2024-01-05", pf.ID, instrumentID)
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
