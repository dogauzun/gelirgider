package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/us"
)

// addDeposit writes a DEPOSIT instrument and one buy of it for userID in
// portfolioID with SQL, as the deposit routes do, and returns its id.
func addDeposit(t *testing.T, e *transactionTestEnv, email string, portfolioID int64) int64 {
	t.Helper()
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-ZZ1', 'DEPOSIT', 'Deposit ZZ1', 'TRY')`)
	if err != nil {
		t.Fatalf("insert deposit instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("deposit instrument id: %v", err)
	}
	if _, err := e.db.Exec(`INSERT INTO transactions
		(user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		VALUES ((SELECT id FROM users WHERE email = ?), ?, ?, 'buy', '2025-01-02', '1', '10', '10', '0', 'TRY', '2025-01-02T00:00:00Z')`, email, portfolioID, id); err != nil {
		t.Fatalf("insert deposit transaction: %v", err)
	}
	return id
}

// TestDepositInstrumentRefusedByPriceRoutes checks that PUT, DELETE and GET
// on a DEPOSIT instrument's prices answer 404 for every user, while a seeded
// instrument still works.
func TestDepositInstrumentRefusedByPriceRoutes(t *testing.T) {
	e := newTransactionTestEnv(t)
	a := e.login("a@example.com")
	b := e.login("b@example.com")
	depositID := addDeposit(t, e, "a@example.com", e.createPortfolio(a, "P"))

	for email, session := range map[string]string{"a@example.com": a, "b@example.com": b} {
		for _, c := range []struct {
			method, path string
			body         any
		}{
			{http.MethodPut, "/api/instruments/%d/prices/2025-01-02", map[string]any{"close": "10"}},
			{http.MethodDelete, "/api/instruments/%d/prices/2025-01-02", nil},
			{http.MethodGet, "/api/instruments/%d/prices", nil},
		} {
			path := fmt.Sprintf(c.path, depositID)
			rec := e.do(session, c.method, path, c.body)
			if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "instrument not found") {
				t.Errorf("%s: %s %s = %d %s, want 404 instrument not found", email, c.method, path, rec.Code, rec.Body.String())
			}
		}

		seeded := fmt.Sprintf("/api/instruments/%d/prices/2025-01-02", e.instrumentID)
		if rec := e.do(session, http.MethodPut, seeded, map[string]any{"close": "10"}); rec.Code != http.StatusOK {
			t.Errorf("%s: PUT of the seeded instrument = %d %s, want 200", email, rec.Code, rec.Body.String())
		}
		if rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/instruments/%d/prices", e.instrumentID), nil); rec.Code != http.StatusOK {
			t.Errorf("%s: GET of the seeded instrument = %d %s, want 200", email, rec.Code, rec.Body.String())
		}
		if rec := e.do(session, http.MethodDelete, seeded, nil); rec.Code != http.StatusNoContent {
			t.Errorf("%s: DELETE of the seeded instrument = %d %s, want 204", email, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM manual_prices WHERE instrument_id = ?`, depositID).Scan(&n); err != nil || n != 0 {
		t.Errorf("manual prices of the deposit = %d (err %v), want 0", n, err)
	}
}

// TestDepositRowInCSVImportIsUnknownMarket checks that a CSV row with market
// DEPOSIT fails as unknown_market and leaves the existing deposit instrument
// and its transactions as they were.
func TestDepositRowInCSVImportIsUnknownMarket(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	portfolioID := e.createPortfolio(session, "P")
	addDeposit(t, e, "a@example.com", portfolioID)
	count := func(table string) (n int) {
		t.Helper()
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	instruments, txs := count("instruments"), count("transactions")

	file := []byte("date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
		"2025-12-02,buy,DEP-ZZ1,1,10,,0,TRY,,DEPOSIT\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))
	if len(p.Rows) != 1 || p.Rows[0].Status != "error" || p.Rows[0].Code != "unknown_market" {
		t.Errorf("rows = %+v, want one error row with code unknown_market", p.Rows)
	}
	if got := count("instruments"); got != instruments {
		t.Errorf("instruments = %d, want %d", got, instruments)
	}
	if got := count("transactions"); got != txs {
		t.Errorf("transactions = %d, want %d", got, txs)
	}
}

// TestExportCSVKeepsDepositMarket checks that the CSV export of a portfolio
// holding a deposit has its buy row with market DEPOSIT.
func TestExportCSVKeepsDepositMarket(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	portfolioID := e.createPortfolio(session, "P")
	addDeposit(t, e, "a@example.com", portfolioID)

	rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/transactions.csv", portfolioID), nil)
	want := "2025-01-02,buy,DEP-ZZ1,1,10,10,0,TRY,,DEPOSIT\n"
	if rec.Code != http.StatusOK || !strings.HasSuffix(rec.Body.String(), want) {
		t.Errorf("export = %d\n%s\nwant 200 ending in\n%s", rec.Code, rec.Body.String(), want)
	}
}

// TestOnDemandFetchSkipsDeposit checks that the on-demand fetch for a DEPOSIT
// instrument asks no market fetcher for anything. Gold and TCMB are off: the
// first runs for every save and the second is the FX backfill a foreign
// currency deposit needs, neither is the deposit's own market source.
func TestOnDemandFetchSkipsDeposit(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	conn := openBackfillTestDB(t)
	repo := settings.NewRepo(conn, nil)
	ctx := context.Background()
	for _, name := range []string{"gold", "tcmb"} {
		if err := repo.Set(ctx, scheduler.EnabledKey(name), "false"); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	fetch := newOnDemandFetch(&deps{
		cfg: repo, instruments: instrument.NewRepo(conn), prices: price.NewRepo(conn), fx: fx.NewRepo(conn),
		bistFetcher: &bist.Fetcher{BaseURL: srv.URL + "/"}, usFetcher: &us.Fetcher{BaseURL: srv.URL, APIKey: "k"},
		btcturkFetcher: &btcturk.Fetcher{BaseURL: srv.URL}, tefasFetcher: &tefas.Fetcher{BaseURL: srv.URL},
	}, &backfillQueue{})

	fetch(ctx, instrument.Instrument{ID: 1, Symbol: "DEP-ZZ1", Market: instrument.MarketDeposit, Name: "Deposit ZZ1", Currency: "TRY"}, weekdaysBack(2)[0])
	backfillWorkers.Wait()

	if n := requests.Load(); n != 0 {
		t.Fatalf("fetch requests = %d, want 0 for a deposit", n)
	}
}
