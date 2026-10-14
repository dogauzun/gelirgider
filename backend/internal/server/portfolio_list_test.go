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
	"slices"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// TestPortfolioListTotalValueUsesManualPrice checks that the
// list endpoint's total_value reflects the caller's own manual price
// override the same way GET /api/portfolios/{id}/summary does,
// not just the fetched price.
//
//	buy 10 @ 5 (fee 0): cost basis 50, cash -50
//	manual price 9 => market value 10*9 = 90
//	total_value = cash + market value = -50 + 90 = 40
func TestPortfolioListTotalValueUsesManualPrice(t *testing.T) {
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
	usr, err := users.Create(context.Background(), "carol@example.com", "hash")
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
	tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), instrumentID, "2024-01-02", "6", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	manual := price.NewManualRepo(conn)
	if err := manual.Upsert(context.Background(), usr.ID, instrumentID, "2024-01-02", "9"); err != nil {
		t.Fatalf("manual.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/portfolios", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []portfolioListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(portfolios) = %d, want 1", len(got))
	}
	if got[0].TotalValue != "40.00" {
		t.Errorf("total_value = %s, want 40.00 (should use manual price 9, not fetched price 6)", got[0].TotalValue)
	}
	if len(got[0].MissingRates) != 0 {
		t.Errorf("missing_rates = %v, want empty (TRY only, no rate needed)", got[0].MissingRates)
	}
}

// TestPortfolioListMissingRates checks that the list reports the
// excluded currency when a USD deposit with no USD rate is dropped from
// total_value by holdings.Convert, instead of serving a bare understated total.
func TestPortfolioListMissingRates(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "dave@example.com", "hash")
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

	// $700 cash, reporting TRY, fx_rates empty.
	transactions := transaction.NewRepo(conn)
	tx := transaction.Transaction{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Quantity: decimal.Zero, Price: decimal.Zero, Amount: decimal.RequireFromString("700"), Fee: decimal.Zero, Currency: "USD"}
	if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/portfolios", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []portfolioListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(portfolios) = %d, want 1", len(got))
	}
	if got[0].TotalValue != "0.00" {
		t.Errorf("total_value = %s, want 0.00 (USD cash has no rate to convert)", got[0].TotalValue)
	}
	if len(got[0].MissingRates) != 1 || got[0].MissingRates[0] != "USD" {
		t.Errorf("missing_rates = %v, want [USD]", got[0].MissingRates)
	}
}

// TestPortfolioListCashAndUnrealizedPL checks that the list carries the
// same reporting-currency cash and unrealized P/L the summary endpoint
// computes, and nulls unrealized_pl under the same rule.
//
// One TRY position (buy 10 @ 5, price 8 => unrealized 30 TRY) plus $700 cash,
// reporting USD. Without a USD rate nothing converts into USD except USD
// itself, so the TRY unrealized P/L is excluded outright and the field is
// null rather than a 0.00 that reads as a real zero. With USD = 40
// the whole portfolio converts.
func TestPortfolioListCashAndUnrealizedPL(t *testing.T) {
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
	usr, err := users.Create(context.Background(), "erin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.UpdateReportingCurrency(context.Background(), usr.ID, "USD"); err != nil {
		t.Fatalf("users.UpdateReportingCurrency: %v", err)
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
	buy := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transactions.Create(context.Background(), usr.ID, buy); err != nil {
		t.Fatalf("transactions.Create buy: %v", err)
	}
	deposit := transaction.Transaction{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Quantity: decimal.Zero, Price: decimal.Zero, Amount: decimal.RequireFromString("700"), Fee: decimal.Zero, Currency: "USD"}
	if _, err := transactions.Create(context.Background(), usr.ID, deposit); err != nil {
		t.Fatalf("transactions.Create deposit: %v", err)
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), instrumentID, "2024-01-02", "8", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	get := func() portfolioListResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got []portfolioListResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(portfolios) = %d, want 1", len(got))
		}
		return got[0]
	}

	// No USD rate: only the USD cash converts (a currency equal to reporting
	// needs no rate), the TRY side is excluded.
	got := get()
	if got.Currency != "USD" {
		t.Errorf("currency = %s, want USD", got.Currency)
	}
	if len(got.MissingRates) != 1 || got.MissingRates[0] != "TRY" {
		t.Errorf("missing_rates = %v, want [TRY]", got.MissingRates)
	}
	if got.Cash != "700.00" {
		t.Errorf("cash = %s, want 700.00 (USD cash only, TRY excluded)", got.Cash)
	}
	if got.UnrealizedPL != nil {
		t.Errorf("unrealized_pl = %s, want null (the only contributing currency was excluded)", *got.UnrealizedPL)
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	// USD = 40 TRY, so TRY converts at 1/40: cash 700 + (-50/40) = 698.75,
	// unrealized (80 - 50) / 40 = 0.75.
	got = get()
	if len(got.MissingRates) != 0 {
		t.Errorf("missing_rates = %v, want empty", got.MissingRates)
	}
	if got.Cash != "698.75" {
		t.Errorf("cash = %s, want 698.75", got.Cash)
	}
	if got.UnrealizedPL == nil {
		t.Fatalf("unrealized_pl = null, want 0.75")
	}
	if *got.UnrealizedPL != "0.75" {
		t.Errorf("unrealized_pl = %s, want 0.75", *got.UnrealizedPL)
	}
}

// TestPortfolioListClosedPositionCurrencyNotMissing: the list values a
// portfolio through the summary's pipeline, so a closed position's currency
// is looked up like the summary does for its realized P/L. A USD
// instrument bought and sold in full with TRY cash legs (the repository
// stores a row's currency as given) leaves USD in no cash or open-position
// entry, only in the closed position and its realized P/L; with a USD rate
// stored, missing_rates must be empty instead of naming USD.
func TestPortfolioListClosedPositionCurrencyNotMissing(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple', 'USD')`)
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
	usr, err := users.Create(context.Background(), "frank@example.com", "hash")
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
	txs := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-01-02", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("120"), Amount: decimal.RequireFromString("1200"), Fee: decimal.Zero, Currency: "TRY"},
	}
	for _, tx := range txs {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := fx.NewRepo(conn).Upsert(context.Background(), "USD", "2024-01-03", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/portfolios", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []portfolioListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(portfolios) = %d, want 1", len(got))
	}
	if len(got[0].MissingRates) != 0 {
		t.Errorf("missing_rates = %v, want empty (USD has a rate; the closed position's currency must be looked up)", got[0].MissingRates)
	}
	if got[0].TotalValue != "200.00" {
		t.Errorf("total_value = %s, want 200.00", got[0].TotalValue)
	}
}

// TestPortfolioListSharedInstrumentMatchesSummary checks that the list, which
// prices every instrument and loads the rates once for all portfolios, still
// reports each portfolio the totals its own GET /api/portfolios/{id}/summary
// does, when the same USD instrument is held in two of three portfolios (the
// third is empty).
func TestPortfolioListSharedInstrumentMatchesSummary(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(ctx, "gina@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	transactions := transaction.NewRepo(conn)
	var ids []int64
	for i, name := range []string{"A", "B", "Empty"} {
		pf, err := portfolios.Create(ctx, usr.ID, name)
		if err != nil {
			t.Fatalf("portfolios.Create: %v", err)
		}
		ids = append(ids, pf.ID)
		qty := []string{"10", "3", ""}[i]
		if qty == "" {
			continue
		}
		q := decimal.RequireFromString(qty)
		for _, tx := range []transaction.Transaction{
			{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("1000"), Currency: "USD"},
			{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: q, Price: decimal.RequireFromString("100"), Amount: q.Mul(decimal.RequireFromString("100")), Currency: "USD"},
		} {
			if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
				t.Fatalf("transactions.Create: %v", err)
			}
		}
	}
	if err := price.NewRepo(conn).Upsert(ctx, instrumentID, "2024-01-03", "130", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := fx.NewRepo(conn).Upsert(ctx, "USD", "2024-01-03", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	do := func(path string, into any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body: %s", path, rec.Code, rec.Body.String())
		}
		if err := json.NewDecoder(rec.Body).Decode(into); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
	}

	var list []portfolioListResponse
	do("/api/portfolios", &list)
	if len(list) != len(ids) {
		t.Fatalf("len(list) = %d, want %d", len(list), len(ids))
	}
	for i, id := range ids {
		var sum summaryResponse
		do(fmt.Sprintf("/api/portfolios/%d/summary", id), &sum)
		got := list[i]
		if got.ID != id || got.TotalValue != sum.TotalValue || got.Cash != sum.Cash || !slices.Equal(got.MissingRates, sum.MissingRates) {
			t.Errorf("portfolio %d: list = %+v, summary total_value=%s cash=%s missing=%v", id, got, sum.TotalValue, sum.Cash, sum.MissingRates)
		}
		if (got.UnrealizedPL == nil) != (sum.UnrealizedPL == nil) || (got.UnrealizedPL != nil && *got.UnrealizedPL != *sum.UnrealizedPL) {
			t.Errorf("portfolio %d: unrealized_pl list = %v, summary = %v", id, got.UnrealizedPL, sum.UnrealizedPL)
		}
	}
	// A: cash 0 + 10 x 130 = 1300 USD; B: cash 700 + 3 x 130 = 1090 USD; x 40.
	if list[0].TotalValue != "52000.00" || list[1].TotalValue != "43600.00" || list[2].TotalValue != "0.00" {
		t.Errorf("total_value = %s, %s, %s; want 52000.00, 43600.00, 0.00", list[0].TotalValue, list[1].TotalValue, list[2].TotalValue)
	}
}
