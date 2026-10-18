package us_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata" // ensure America/New_York and Europe/Istanbul load even without system tzdata

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/us"
)

// TestSyncHistoryStoresSplitFactor checks that Tiingo's splitFactor on
// the middle of three rows becomes exactly one split row, exact decimal,
// source tiingo, while every close is still stored. The one
// positive divCash becomes exactly one dividend row, exact decimal, USD,
// source tiingo; a 0 and a negative divCash produce none.
func TestSyncHistoryStoresSplitFactor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"date":"2026-08-28T00:00:00.000Z","close":500.0,"splitFactor":1.0,"divCash":0.0},
			{"date":"2026-08-31T00:00:00.000Z","close":126.1,"splitFactor":4.0,"divCash":0.26},
			{"date":"2026-09-01T00:00:00.000Z","close":127.25,"splitFactor":1.0,"divCash":-0.1}
		]`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple Inc', 'USD')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	splits := split.NewRepo(conn)
	f := &us.Fetcher{TiingoBaseURL: srv.URL, TiingoKey: "test-key"}
	in := instrument.Instrument{ID: 1, Symbol: "AAPL", Market: "US"}
	prices := price.NewRepo(conn)
	dividends := dividend.NewRepo(conn)
	n, err := (&us.Syncer{Fetcher: f, Prices: prices, Splits: splits, Dividends: dividends}).SyncHistory(ctx, in, "2026-09-01")
	if err != nil {
		t.Fatalf("SyncHistory: %v", err)
	}
	if n != 3 {
		t.Fatalf("SyncHistory wrote %d closes, want 3", n)
	}
	latest, err := prices.Latest(ctx, []int64{1}, "2026-08-31")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if p := latest[1]; p.Date != "2026-08-31" || p.Close.String() != "126.1" || p.Source != "tiingo" {
		t.Fatalf("ex-date close = %+v, want 2026-08-31 126.1 tiingo", p)
	}

	got, err := splits.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Date != "2026-08-31" || got[0].Ratio.String() != "4" || got[0].Source != "tiingo" {
		t.Fatalf("splits = %+v, want one (2026-08-31, 4, tiingo)", got)
	}

	divs, err := dividends.ListFor(ctx, []int64{1})
	if err != nil {
		t.Fatalf("dividends ListFor: %v", err)
	}
	if d := divs[1]; len(d) != 1 || d[0].Date != "2026-08-31" || d[0].Amount.String() != "0.26" || d[0].Currency != "USD" || d[0].Source != "tiingo" {
		t.Fatalf("dividends = %+v, want one (2026-08-31, 0.26, USD, tiingo)", divs)
	}
}

// TestSyncHistoryCountsClosesWhenSplitFails: the closes commit as one batch
// before the splits; when a split upsert then fails, the returned count is
// still every close stored, not the index of the failing split.
func TestSyncHistoryCountsClosesWhenSplitFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"date":"2026-08-28T00:00:00.000Z","close":500.0,"splitFactor":4.0},
			{"date":"2026-08-31T00:00:00.000Z","close":126.1,"splitFactor":1.0},
			{"date":"2026-09-01T00:00:00.000Z","close":127.25,"splitFactor":1.0}
		]`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAPL', 'US', 'Apple Inc', 'USD')`,
		`DROP TABLE splits`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	f := &us.Fetcher{TiingoBaseURL: srv.URL, TiingoKey: "test-key"}
	in := instrument.Instrument{ID: 1, Symbol: "AAPL", Market: "US"}
	s := &us.Syncer{Fetcher: f, Prices: price.NewRepo(conn), Splits: split.NewRepo(conn), Dividends: dividend.NewRepo(conn)}
	n, err := s.SyncHistory(ctx, in, "2026-09-01")
	if err == nil {
		t.Fatalf("SyncHistory with a failing split upsert: got nil error, want one")
	}
	if n != 3 {
		t.Fatalf("SyncHistory = %d, want 3 (every close was committed)", n)
	}
}

// TestFetchCloseKeepsExactDecimal checks that the close is parsed from
// the JSON text, so there is no float64 noise and no rounding of sub-cent
// prices.
func TestFetchCloseKeepsExactDecimal(t *testing.T) {
	for _, tc := range []struct{ c, want string }{
		{"128.9", "128.9"},
		{"0.00012", "0.00012"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("symbol"); got != "AAPL" {
				t.Errorf("request symbol = %q, want AAPL", got)
			}
			if got := r.Header.Get("X-Finnhub-Token"); got != "test-key" {
				t.Errorf("request token header = %q, want test-key", got)
			}
			w.Write([]byte(`{"c":` + tc.c + `,"d":1.5,"dp":1.2,"h":130.0,"l":0.0001,"o":128.5,"pc":127.4,"t":1694649600}`))
		}))

		f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
		c, ok, err := f.FetchClose(context.Background(), "AAPL", time.Unix(1694649600, 0))
		srv.Close()
		if err != nil {
			t.Fatalf("c=%s: FetchClose: %v", tc.c, err)
		}
		if !ok {
			t.Fatalf("c=%s: FetchClose ok = false, want true", tc.c)
		}
		if got := c.Close.String(); got != tc.want {
			t.Fatalf("Close = %s, want exactly %s", got, tc.want)
		}
	}
}

// TestFetchCloseUnknownSymbolIsSkippedNotError covers Finnhub's
// unknown-symbol shape: HTTP 200 with every quote field zeroed, not a 404.
func TestFetchCloseUnknownSymbolIsSkippedNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"c":0,"d":null,"dp":null,"h":0,"l":0,"o":0,"pc":0,"t":0}`))
	}))
	defer srv.Close()

	f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
	_, ok, err := f.FetchClose(context.Background(), "NOTASYMBOL", time.Now())
	if err != nil {
		t.Fatalf("FetchClose: %v", err)
	}
	if ok {
		t.Fatalf("FetchClose ok = true, want false for a zeroed/unknown-symbol response")
	}
}

// TestFetchCloseNon200IsError covers a rate-limit/auth failure: unlike an
// unknown symbol, it is an error, so Sync counts the symbol as failed and
// moves on to the next one; it does not stop the run.
func TestFetchCloseNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
	if _, _, err := f.FetchClose(context.Background(), "AAPL", time.Now()); err == nil {
		t.Fatalf("FetchClose: got nil error, want one for a 429 response")
	}
}

// TestFetchCloseNoAPIKeyReturnsClearError covers the "not configured" case
// (no key set): must return an error, not panic or silently no-op, and
// must not make a request.
func TestFetchCloseNoAPIKeyReturnsClearError(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	f := &us.Fetcher{BaseURL: srv.URL}
	if _, _, err := f.FetchClose(context.Background(), "AAPL", time.Now()); err == nil {
		t.Fatalf("FetchClose: got nil error, want one when APIKey is empty")
	}
	if called {
		t.Fatalf("FetchClose made a request with no API key configured")
	}
}

// TestSyncWritesTrackedInstrumentOnly covers Sync end to end against a real
// instrument.Repo/price.Repo backed by in-memory SQLite: one US instrument
// referenced by a transaction (tracked) and one that is not (untracked)
// must result in exactly one price row, for the tracked one.
func TestSyncWritesTrackedInstrumentOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("symbol") {
		case "AAPL":
			w.Write([]byte(`{"c":230.5,"pc":228.75,"t":1790107200}`))
		default:
			t.Fatalf("unexpected symbol requested: %s", r.URL.Query().Get("symbol"))
		}
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	// AAPL (id 1) is tracked via a transaction; MSFT (id 2) is a known
	// instrument with no transaction, so it must not be fetched or priced.
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple Inc', 'USD'),
		       (2, 'MSFT', 'US', 'Microsoft Corp', 'USD')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at)
		VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO portfolios (id, user_id, name, created_at)
		VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO transactions
		(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		VALUES (1, 1, 1, 1, 'buy', '2026-09-10', '10', '200.00', '2000.00', '0', 'USD', '2026-09-10T00:00:00Z')`); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	instruments := instrument.NewRepo(conn)
	prices := price.NewRepo(conn)

	f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
	n, err := us.Sync(ctx, f, instruments, prices, time.Unix(1790107200, 0))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("Sync wrote %d prices, want 1 (MSFT is untracked)", n)
	}

	latest, err := prices.Latest(ctx, []int64{1, 2}, "2026-09-22")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	aapl, ok := latest[1]
	if !ok {
		t.Fatalf("no price stored for AAPL")
	}
	if aapl.Date != "2026-09-22" || aapl.Close.String() != "230.5" || aapl.Source != "finnhub" {
		t.Fatalf("AAPL price = %+v, want 2026-09-22 close=230.5 source=finnhub", aapl)
	}
	if _, ok := latest[2]; ok {
		t.Fatalf("price stored for MSFT, want none (untracked)")
	}
}

// TestSyncContinuesPastFailingSymbol checks that a Finnhub 500 on
// the first tracked symbol fails that symbol only; Sync still fetches and
// stores the second one and returns nil, since not every symbol failed.
func TestSyncContinuesPastFailingSymbol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") == "AAPL" {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"c":410.25,"pc":405.5,"t":1790107200}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'AAPL', 'US', 'Apple Inc', 'USD'),
			(2, 'MSFT', 'US', 'Microsoft Corp', 'USD')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2026-09-10', '1', '200', '200', '0', 'USD', '2026-09-10T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2026-09-10', '1', '400', '400', '0', 'USD', '2026-09-10T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	n, err := us.Sync(ctx, &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}, instrument.NewRepo(conn), prices, time.Unix(1790107200, 0))
	if err != nil || n != 1 {
		t.Fatalf("Sync = %d, %v, want 1 row and nil", n, err)
	}
	latest, err := prices.Latest(ctx, []int64{1, 2}, "2026-09-22")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if _, ok := latest[1]; ok {
		t.Fatalf("price stored for AAPL, want none (its fetch failed)")
	}
	if msft, ok := latest[2]; !ok || msft.Close.String() != "410.25" {
		t.Fatalf("MSFT price = %+v (stored %v), want close 410.25", msft, ok)
	}
}

// TestFetchCloseDatesByQuoteTime checks that the close is dated by the
// quote's own t, not by the run time. Before the open c is still the last
// session's close; during the session c is live, so pc is stored under the
// session before (Friday, on a Monday).
func TestFetchCloseDatesByQuoteTime(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	tests := []struct {
		name      string
		quote     string
		now       time.Time
		wantDate  string
		wantClose string
	}{
		{
			// The 2026-09-23 05:12 NY repro: c is the 09-22 close, pc 09-21's.
			name:      "pre-open, t on the previous day",
			quote:     `{"c":339.75,"pc":338.98,"t":1790107200}`, // Tue 2026-09-22 16:00 NY
			now:       time.Date(2026, 9, 23, 5, 12, 0, 0, newYork),
			wantDate:  "2026-09-22",
			wantClose: "339.75",
		},
		{
			name:      "after the close, t today",
			quote:     `{"c":339.75,"pc":338.98,"t":1790107200}`,
			now:       time.Date(2026, 9, 22, 16, 5, 0, 0, newYork),
			wantDate:  "2026-09-22",
			wantClose: "339.75",
		},
		{
			name:      "Monday during the session",
			quote:     `{"c":340.1,"pc":337.5,"t":1790004600}`, // Mon 2026-09-21 11:30 NY
			now:       time.Date(2026, 9, 21, 11, 31, 0, 0, newYork),
			wantDate:  "2026-09-18",
			wantClose: "337.5",
		},
		{
			// Good Friday 2026-04-03 was closed: pc is Thursday's close.
			name:      "Monday after Good Friday during the session",
			quote:     `{"c":340.1,"pc":337.5,"t":1775489400}`, // Mon 2026-04-06 11:30 NY
			now:       time.Date(2026, 4, 6, 11, 31, 0, 0, newYork),
			wantDate:  "2026-04-02",
			wantClose: "337.5",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.quote))
			}))
			defer srv.Close()

			f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
			c, ok, err := f.FetchClose(context.Background(), "AAPL", tc.now)
			if err != nil || !ok {
				t.Fatalf("FetchClose: ok=%v err=%v", ok, err)
			}
			if c.Date != tc.wantDate || !c.Close.Equal(decimal.RequireFromString(tc.wantClose)) {
				t.Fatalf("FetchClose = %s %s, want %s %s", c.Date, c.Close, tc.wantDate, tc.wantClose)
			}
		})
	}
}

// TestLastSessionSkipsMarketHolidays checks that a run after the close on
// an NYSE holiday, weekend-shifted or not, gets the session before it, and
// that a Saturday New Year's Day is not observed on the Friday before.
func TestLastSessionSkipsMarketHolidays(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"ordinary Tuesday", time.Date(2026, 9, 22, 17, 0, 0, 0, newYork), "2026-09-22"},
		{"Good Friday", time.Date(2026, 4, 3, 17, 0, 0, 0, newYork), "2026-04-02"},
		{"Good Friday in March", time.Date(2024, 3, 29, 17, 0, 0, 0, newYork), "2024-03-28"},
		{"Juneteenth", time.Date(2026, 6, 19, 17, 0, 0, 0, newYork), "2026-06-18"},
		{"Thanksgiving", time.Date(2026, 11, 26, 17, 0, 0, 0, newYork), "2026-11-25"},
		{"July 4 on Saturday, observed Friday", time.Date(2026, 7, 3, 17, 0, 0, 0, newYork), "2026-07-02"},
		{"July 4 on Sunday, observed Monday", time.Date(2027, 7, 5, 17, 0, 0, 0, newYork), "2027-07-02"},
		{"Christmas on Saturday, observed Friday", time.Date(2021, 12, 24, 17, 0, 0, 0, newYork), "2021-12-23"},
		{"New Year's Day on Saturday is not observed", time.Date(2021, 12, 31, 17, 0, 0, 0, newYork), "2021-12-31"},
		{"Memorial Day, before the close", time.Date(2026, 5, 26, 10, 0, 0, 0, newYork), "2026-05-22"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := us.LastSession(tc.now); got != tc.want {
				t.Fatalf("LastSession(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

// TestSyncRecentStoresClosesAndSplits checks that every tracked US
// instrument gets its recent Tiingo window re-read. A 500 on one symbol
// (AAA, first by symbol order) does not stop the rest; both others' closes
// are stored and AAPL's splitFactor 2 becomes one split row.
func TestSyncRecentStoresClosesAndSplits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("startDate"); got != "2026-09-16" {
			t.Errorf("startDate = %q, want 2026-09-16", got)
		}
		switch r.URL.Path {
		case "/tiingo/daily/AAA/prices":
			w.WriteHeader(http.StatusInternalServerError)
		case "/tiingo/daily/AAPL/prices":
			w.Write([]byte(`[
				{"date":"2026-09-21T00:00:00.000Z","close":240.0,"splitFactor":1.0},
				{"date":"2026-09-22T00:00:00.000Z","close":121.5,"splitFactor":2.0}
			]`))
		case "/tiingo/daily/MSFT/prices":
			w.Write([]byte(`[{"date":"2026-09-22T00:00:00.000Z","close":430.25,"splitFactor":1.0}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'AAPL', 'US', 'Apple Inc', 'USD'), (2, 'MSFT', 'US', 'Microsoft Corp', 'USD'), (3, 'AAA', 'US', 'Failing', 'USD')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2026-09-10', '1', '1', '1', '0', 'USD', '2026-09-10T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2026-09-10', '1', '1', '1', '0', 'USD', '2026-09-10T00:00:00Z'),
			(3, 1, 1, 3, 'buy', '2026-09-10', '1', '1', '1', '0', 'USD', '2026-09-10T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	splits := split.NewRepo(conn)
	f := &us.Fetcher{TiingoBaseURL: srv.URL, TiingoKey: "test-key"}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	n, err := (&us.Syncer{Fetcher: f, Instruments: instrument.NewRepo(conn), Prices: prices, Splits: splits, Dividends: dividend.NewRepo(conn)}).SyncRecent(ctx, now)
	if err != nil {
		t.Fatalf("SyncRecent: %v", err)
	}
	if n != 3 {
		t.Fatalf("SyncRecent wrote %d closes, want 3", n)
	}

	latest, err := prices.Latest(ctx, []int64{1, 2, 3}, "2026-09-22")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if p := latest[1]; p.Close.String() != "121.5" || p.Source != "tiingo" {
		t.Fatalf("AAPL = %+v, want 121.5 tiingo", p)
	}
	if p := latest[2]; p.Close.String() != "430.25" || p.Source != "tiingo" {
		t.Fatalf("MSFT = %+v, want 430.25 tiingo", p)
	}
	if _, ok := latest[3]; ok {
		t.Fatalf("price stored for AAA, want none (500)")
	}

	got := map[int64][]split.Split{}
	for _, id := range []int64{1, 2, 3} {
		ss, err := splits.List(ctx, id)
		if err != nil {
			t.Fatalf("List(%d): %v", id, err)
		}
		if len(ss) > 0 {
			got[id] = ss
		}
	}
	if len(got) != 1 || len(got[1]) != 1 || got[1][0].Date != "2026-09-22" || got[1][0].Ratio.String() != "2" || got[1][0].Source != "tiingo" {
		t.Fatalf("splits = %+v, want one AAPL (2026-09-22, 2, tiingo)", got)
	}
}

// TestSyncStoresSectorOfHeldInstrumentsOnce checks the profile pass of Sync:
// a held instrument with an unset sector gets Finnhub's raw industry, an
// empty profile stores "" and is not requested on the next run, and an
// instrument whose sector is set is never requested.
func TestSyncStoresSectorOfHeldInstrumentsOnce(t *testing.T) {
	profiles := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Query().Get("symbol")
		if r.URL.Path != "/stock/profile2" {
			w.Write([]byte(`{"c":10,"pc":9,"t":1790107200}`))
			return
		}
		profiles[sym]++
		if sym == "AAPL" {
			w.Write([]byte(`{"finnhubIndustry":"Technology"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency, sector) VALUES
			(1, 'AAPL', 'US', 'Apple Inc', 'USD', NULL),
			(2, 'ZZZZ', 'US', 'No Profile Inc', 'USD', NULL),
			(3, 'MSFT', 'US', 'Microsoft Corp', 'USD', 'Technology'),
			(4, 'NVDA', 'US', 'Nvidia Corp', 'USD', NULL)`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2026-09-10', '1', '200', '200', '0', 'USD', '2026-09-10T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2026-09-10', '1', '5', '5', '0', 'USD', '2026-09-10T00:00:00Z'),
			(3, 1, 1, 3, 'buy', '2026-09-10', '1', '400', '400', '0', 'USD', '2026-09-10T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	instruments := instrument.NewRepo(conn)
	f := &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}
	for range 2 {
		if _, err := us.Sync(ctx, f, instruments, price.NewRepo(conn), time.Unix(1790107200, 0)); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	}

	want := map[int64]string{1: "Technology", 2: "", 3: "Technology"}
	for id, w := range want {
		in, err := instruments.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %d: %v", id, err)
		}
		if !in.Sector.Valid || in.Sector.String != w {
			t.Errorf("instrument %d sector = %+v, want %q", id, in.Sector, w)
		}
	}
	if in, _ := instruments.Get(ctx, 4); in.Sector.Valid {
		t.Errorf("untracked NVDA sector = %+v, want unset", in.Sector)
	}
	if profiles["AAPL"] != 1 || profiles["ZZZZ"] != 1 || len(profiles) != 2 {
		t.Errorf("profile requests = %v, want one each for AAPL and ZZZZ only", profiles)
	}
}
