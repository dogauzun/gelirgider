package main

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/evds"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/tuik"
	"github.com/dogauzun/gelirgider/backend/internal/us"
)

// bulletinCSV returns a minimal one-instrument bulletin body for date and
// close, in the bulletin's real shape: Turkish header row (matched by name),
// English header row, then one data row.
func bulletinCSV(date, close string) string {
	return "TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n" +
		"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n" +
		date + ";THYAO.E;TURK HAVA YOLLARI;EQT;" + close + "\n"
}

func bulletinZip(t *testing.T, csv string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("bulletin.csv")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(csv)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

// TestRunBackfillSkipsAlreadySyncedDatesOnRerun covers the ticket's core
// case: a fake fetcher serving closes for two of three weekdays and a 404
// for the third. The first run must store rows for the two served dates and
// leave the 404 date unstored; skip a non-404 error entirely, since none
// occurs here. The second run must re-fetch nothing for the two already
// synced dates (only the still-unsynced 404 date is retried).
//
// runBackfill also backfills FX over the same date range; the FX fetcher
// points at the same test server, which 404s every FX bulletin path (no
// case matches it), the same "not yet available" signal as a BIST 404 -
// exercising the second source without needing a second fake dataset.
func TestRunBackfillSkipsAlreadySyncedDatesOnRerun(t *testing.T) {
	// Three consecutive weekdays: Tue, Wed, Thu.
	day1, day2, day3 := "2024-01-02", "2024-01-03", "2024-01-04"
	zip1 := bulletinZip(t, bulletinCSV(day1, "10.25"))
	zip2 := bulletinZip(t, bulletinCSV(day2, "11.75"))

	var mu sync.Mutex
	requests := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()

		switch {
		case strings.Contains(r.URL.Path, "20240102"):
			w.Write(zip1)
		case strings.Contains(r.URL.Path, "20240103"):
			w.Write(zip2)
		default:
			http.NotFound(w, r) // day3: no bulletin published
		}
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	fetcher := &bist.Fetcher{BaseURL: srv.URL + "/"}
	fxFetcher := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	gold := goldStub(t, conn)
	// No US instruments are seeded, so instrument.Repo.Tracked(ctx, "US")
	// is empty and us.Sync returns before ever touching usFetcher; a zero
	// value is fine here.
	usFetcher := &us.Fetcher{}
	logger := testLogger()
	// "now" = the Friday after day3, so yesterday = day3 and the walked
	// range is exactly day1..day3.
	now := time.Date(2024, 1, 5, 12, 0, 0, 0, price.Istanbul)

	ctx := context.Background()
	if err := runBackfill(ctx, logger, conn, sourceFetchers{bist: fetcher, tcmb: fxFetcher, us: usFetcher, crypto: gold, tefas: &tefas.Fetcher{BaseURL: srv.URL}, now: fixedNow(now)}, day1); err != nil {
		t.Fatalf("first runBackfill: %v", err)
	}

	assertPriceRow(t, conn, day1, "10.25")
	assertPriceRow(t, conn, day2, "11.75")
	assertNoPriceRow(t, conn, day3)

	mu.Lock()
	firstRunRequests := len(requests)
	mu.Unlock()
	// 3 BIST paths (day1..day3) + 3 distinct FX paths (day1..day3): the FX
	// fetcher's URL pattern never matches the BIST-only switch cases above,
	// so every FX request 404s and none collide with a BIST path.
	if firstRunRequests != 6 {
		t.Fatalf("first run made %d distinct-path requests, want 6", firstRunRequests)
	}

	// Second run: day1 and day2 already have a bist_bulletin row and must
	// be skipped outright (no new request); day3 has none yet, so it is
	// retried (and still 404s).
	mu.Lock()
	for k := range requests {
		requests[k] = 0
	}
	mu.Unlock()

	if err := runBackfill(ctx, logger, conn, sourceFetchers{bist: fetcher, tcmb: fxFetcher, us: usFetcher, crypto: gold, tefas: &tefas.Fetcher{BaseURL: srv.URL}, now: fixedNow(now)}, day1); err != nil {
		t.Fatalf("second runBackfill: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for path, n := range requests {
		if strings.Contains(path, "20240102") && n != 0 {
			t.Fatalf("day1 was re-fetched on second run (%d requests), want 0", n)
		}
		if strings.Contains(path, "20240103") && n != 0 {
			t.Fatalf("day2 was re-fetched on second run (%d requests), want 0", n)
		}
	}
	assertPriceRow(t, conn, day1, "10.25")
	assertPriceRow(t, conn, day2, "11.75")
	assertNoPriceRow(t, conn, day3)
}

// TestBackfillHistorySkipsUS covers what the startup walk must not do: with
// a tracked US instrument and a US transaction in the database, the history
// walk writes BIST/TCMB rows only and never asks Finnhub for a quote. The
// one stub here serves every source, so a reintroduced US fetch would show
// up as a /quote request (and, on the real Finnhub base URL, as a finnhub
// price row or an error) — all three are asserted.
func TestBackfillHistorySkipsUS(t *testing.T) {
	day1 := "2024-01-02" // Tuesday
	zip1 := bulletinZip(t, bulletinCSV(day1, "10.25"))

	var mu sync.Mutex
	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		if strings.Contains(r.URL.Path, "20240102") {
			w.Write(zip1)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (2, 'AAPL', 'US', 'Apple Inc', 'USD')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 2, 'buy', '2024-01-02', '10', '200.00', '2000.00', '0', 'USD', '2024-01-02T00:00:00Z')`,
		// A BIST buy too, so the BIST walk has a start date.
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (2, 1, 1, 1, 'buy', '2024-01-02', '10', '10.25', '102.50', '0', 'TRY', '2024-01-02T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Wednesday 2024-01-03 -> yesterday is day1, so the walked range is
	// exactly day1 (the earliest transaction date, from = "").
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)

	walked, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", noGold)
	if err != nil {
		t.Fatalf("backfillHistory: %v", err)
	}
	if !walked {
		t.Fatalf("backfillHistory reported nothing to walk, want a walk over %s", day1)
	}

	assertPriceRow(t, conn, day1, "10.25")

	var finnhubRows int
	if err := conn.QueryRow("SELECT COUNT(*) FROM prices WHERE source = ?", us.SourceFinnhub).Scan(&finnhubRows); err != nil {
		t.Fatalf("count finnhub rows: %v", err)
	}
	if finnhubRows != 0 {
		t.Fatalf("history walk wrote %d finnhub price rows, want 0", finnhubRows)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range paths {
		if strings.Contains(p, "quote") {
			t.Fatalf("history walk requested a Finnhub quote (%s), want none", p)
		}
	}
}

// TestRunBackfillWithoutUSFetcher checks that -backfill-prices without
// SECRET_KEY, which leaves no US fetcher, still backfills BIST and skips US
// (a US transaction is seeded, so the US walk would otherwise run).
func TestRunBackfillWithoutUSFetcher(t *testing.T) {
	day1 := "2024-01-02" // Tuesday
	zip1 := bulletinZip(t, bulletinCSV(day1, "10.25"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "20240102") {
			w.Write(zip1)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`,
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (2, 'AAPL', 'US', 'Apple Inc', 'USD')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 2, 'buy', '2024-01-02', '10', '200.00', '2000.00', '0', 'USD', '2024-01-02T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (2, 1, 1, 1, 'buy', '2024-01-02', '10', '10.25', '102.50', '0', 'TRY', '2024-01-02T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)
	if err := runBackfill(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, crypto: goldStub(t, conn), tefas: &tefas.Fetcher{BaseURL: srv.URL}, now: fixedNow(now)}, ""); err != nil {
		t.Fatalf("runBackfill: %v", err)
	}
	assertPriceRow(t, conn, day1, "10.25")
}

// TestBackfillHistoryStartsBISTAtFirstBISTTransaction checks that TCMB
// starts at the earliest transaction of any type, BIST at the earliest
// transaction on a BIST instrument, and a cash-only database fetches no
// bulletin at all.
func TestBackfillHistoryStartsBISTAtFirstBISTTransaction(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 'deposit', '2024-01-02', '1000', '0', 'EUR', '2024-01-02T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Friday -> yesterday is Thursday 2024-01-04; the full range is Tue..Thu.
	now := time.Date(2024, 1, 5, 12, 0, 0, 0, price.Istanbul)
	walk := func() (bp, tp []string) {
		t.Helper()
		mu.Lock()
		paths = nil
		mu.Unlock()
		if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", noGold); err != nil {
			t.Fatalf("backfillHistory: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, p := range paths {
			if strings.Contains(p, "thb") {
				bp = append(bp, p)
			} else {
				tp = append(tp, p)
			}
		}
		return bp, tp
	}

	// Cash only: TCMB walks Tue..Thu, BIST fetches nothing.
	bistPaths, tcmbPaths := walk()
	if len(bistPaths) != 0 {
		t.Fatalf("cash-only walk fetched bulletins %v, want none", bistPaths)
	}
	if len(tcmbPaths) != 3 {
		t.Fatalf("cash-only walk fetched %d TCMB paths %v, want 3", len(tcmbPaths), tcmbPaths)
	}

	// A BIST buy on Thursday: BIST starts there, TCMB still at Tuesday.
	if _, err := conn.ExecContext(ctx, `INSERT INTO transactions
		(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		VALUES (2, 1, 1, 1, 'buy', '2024-01-04', '10', '10.25', '102.50', '0', 'TRY', '2024-01-04T00:00:00Z')`); err != nil {
		t.Fatalf("seed buy: %v", err)
	}
	bistPaths, tcmbPaths = walk()
	if len(bistPaths) != 1 || !strings.Contains(bistPaths[0], "20240104") {
		t.Fatalf("walk fetched bulletins %v, want only 2024-01-04", bistPaths)
	}
	if len(tcmbPaths) != 3 {
		t.Fatalf("walk fetched %d TCMB paths %v, want 3", len(tcmbPaths), tcmbPaths)
	}
}

// TestBackfillUSWritesLatestCloseOnce covers the normal path: a single
// tracked US instrument gets exactly one price row, dated by the quote's
// session, with source "finnhub" - one Sync call, not a per-date loop.
func TestBackfillUSWritesLatestCloseOnce(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"c":230.5,"pc":229,"t":1788811200}`)) // Mon 2026-09-07 16:00 NY
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
	if _, err := conn.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at)
		VALUES (1, 'u@example.com', 'x', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO portfolios (id, user_id, name, created_at)
		VALUES (1, 1, 'Main', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO transactions
		(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		VALUES (1, 1, 1, 1, 'buy', '2026-09-01', '10', '200.00', '2000.00', '0', 'USD', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	// Tuesday 2026-09-08 12:00 Istanbul is 05:00 New York, before the open,
	// so c is Monday 2026-09-07's close.
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, price.Istanbul)

	if err := backfillUS(ctx, testLogger(), conn, &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}, now); err != nil {
		t.Fatalf("backfillUS: %v", err)
	}
	if requests != 1 {
		t.Fatalf("fetcher called %d times, want 1 (one Sync call, not a per-date loop)", requests)
	}

	var close, source string
	if err := conn.QueryRow("SELECT close, source FROM prices WHERE date = ?", "2026-09-07").Scan(&close, &source); err != nil {
		t.Fatalf("price row for 2026-09-07: %v", err)
	}
	if close != "230.5" || source != "finnhub" {
		t.Fatalf("price = %s/%s, want 230.5/finnhub", close, source)
	}
}

// TestBackfillHistoryFetchesCryptoInOneRequest checks that a tracked
// CRYPTO pair gets its whole range from one klines request, weekends
// included, stored under source "btcturk".
func TestBackfillHistoryFetchesCryptoInOneRequest(t *testing.T) {
	var mu sync.Mutex
	var klines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/klines/history" {
			http.NotFound(w, r) // TCMB
			return
		}
		mu.Lock()
		klines = append(klines, r.URL.RawQuery)
		mu.Unlock()
		// Fri 2024-01-05 .. Mon 2024-01-08, 00:00 UTC.
		w.Write([]byte(`{"s":"ok","t":[1704412800,1704499200,1704585600,1704672000],"c":[1500000.5,1510000,1520000,1530000]}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 1, 'buy', '2024-01-05', '0.1', '1500000', '150000', '0', 'TRY', '2024-01-05T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Tuesday -> yesterday is Monday 2024-01-08.
	now := time.Date(2024, 1, 9, 12, 0, 0, 0, price.Istanbul)
	if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", noGold); err != nil {
		t.Fatalf("backfillHistory: %v", err)
	}

	// from is the pair's own first transaction, 2024-01-05 00:00 UTC.
	if len(klines) != 1 || !strings.Contains(klines[0], "symbol=BTCTRY") || !strings.Contains(klines[0], "from=1704412800") {
		t.Fatalf("klines requests = %v, want one for BTCTRY from 1704412800", klines)
	}
	for date, want := range map[string]string{"2024-01-05": "1500000.5", "2024-01-06": "1510000", "2024-01-07": "1520000", "2024-01-08": "1530000"} {
		var close string
		if err := conn.QueryRow("SELECT close FROM prices WHERE date = ? AND source = ?", date, btcturk.SourceBtcTurk).Scan(&close); err != nil {
			t.Fatalf("btcturk price for %s: %v", date, err)
		}
		if close != want {
			t.Fatalf("btcturk price for %s = %s, want %s", date, close, want)
		}
	}

	// Every day up to yesterday is stored now: a restart requests nothing.
	if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", noGold); err != nil {
		t.Fatalf("second backfillHistory: %v", err)
	}
	if len(klines) != 1 {
		t.Fatalf("klines requests after rerun = %v, want still one", klines)
	}
}

// TestBackfillHistoryFetchesGoldFromHistoryStart checks that a
// transaction before PAXG-TRY's first candle starts the gold backfill at
// btcturk.GoldHistoryStart, with one klines request, and the gram price lands in
// prices and benchmark_values though nobody holds gold.
func TestBackfillHistoryFetchesGoldFromHistoryStart(t *testing.T) {
	var mu sync.Mutex
	var klines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		klines = append(klines, r.URL.RawQuery)
		mu.Unlock()
		// 2022-06-30 00:00 UTC, TRY per troy ounce.
		w.Write([]byte(`{"s":"ok","t":[1656547200],"c":[30550]}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'GRAM', 'GOLD', 'Gram Altın', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2022-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2022-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 'deposit', '2022-01-03', '1000', '0', 'TRY', '2022-01-03T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	disabled := map[string]bool{"bist": true, "tcmb": true, "crypto": true, "tefas": true, "us": true}
	now := time.Date(2022, 7, 2, 12, 0, 0, 0, price.Istanbul)
	if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", disabled); err != nil {
		t.Fatalf("backfillHistory: %v", err)
	}

	if len(klines) != 1 || !strings.Contains(klines[0], "symbol=PAXGTRY") || !strings.Contains(klines[0], "from=1656547200") {
		t.Fatalf("klines requests = %v, want one for PAXGTRY from 1656547200", klines)
	}
	var price, value string
	if err := conn.QueryRow("SELECT close FROM prices WHERE instrument_id = 1 AND date = '2022-06-30' AND source = ?", btcturk.SourcePAXG).Scan(&price); err != nil {
		t.Fatalf("GOLD/GRAM price: %v", err)
	}
	if err := conn.QueryRow("SELECT value FROM benchmark_values WHERE code = 'GOLD' AND date = '2022-06-30'").Scan(&value); err != nil {
		t.Fatalf("GOLD benchmark value: %v", err)
	}
	if price != "982.2053" || value != "982.2053" {
		t.Fatalf("gram price = %s, benchmark = %s, want 982.2053 for both", price, value)
	}
}

// TestBackfillGoldSkipsCompleteRange checks that with a GOLD/GRAM price
// on every day from the start to yesterday (UTC), a restart makes no
// BtcTurk request.
func TestBackfillGoldSkipsCompleteRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected BtcTurk request %s", r.URL)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency)
			VALUES (1, 'GRAM', 'GOLD', 'Gram Altın', 'TRY')`,
		`INSERT INTO prices (instrument_id, date, close, source)
			VALUES (1, '2022-06-30', '982.2053', 'btcturk_paxg'), (1, '2022-07-01', '990.1', 'btcturk_paxg')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Yesterday (UTC) is 2022-07-01.
	now := time.Date(2022, 7, 2, 12, 0, 0, 0, price.Istanbul)
	f := sourceFetchers{crypto: &btcturk.Fetcher{BaseURL: srv.URL}, now: fixedNow(now)}
	gold, _ := scheduler.Find(schedulerSources(conn, testLogger(), f), scheduler.NameGold)
	written, err := gold.Backfill(ctx, "2022-06-30")
	if err != nil || written != 0 {
		t.Fatalf("gold Backfill = %d, %v, want 0, nil", written, err)
	}
}

// TestBackfillHistoryFetchesXU100 checks that without an EVDS key the
// xu100 backfill is skipped, with no request and no run in the run log;
// with one it stores the BIST 100 closes from the earliest transaction,
// not only SyncXU100's 14-day window.
func TestBackfillHistoryFetchesXU100(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Write([]byte(`{"items":[{"Tarih":"01-06-2023","TP_MK_F_BILESIK":"5500.10000000"},{"Tarih":"02-01-2024","TP_MK_F_BILESIK":"7700.53000000"}]}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2023-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2023-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 'deposit', '2023-06-01', '1000', '0', 'TRY', '2023-06-01T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	disabled := map[string]bool{"bist": true, "tcmb": true, "crypto": true, "tefas": true, "us": true, "gold": true}
	// Wednesday -> yesterday is 2024-01-02: one request, under a year.
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)
	backfill := func(f *evds.Fetcher) {
		t.Helper()
		if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{}, tcmb: &tcmb.Fetcher{}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{}, tefas: &tefas.Fetcher{}, xu100: f, now: fixedNow(now)}, "", disabled); err != nil {
			t.Fatalf("backfillHistory: %v", err)
		}
	}
	xu100Runs := func() (runs, failed int) {
		t.Helper()
		if err := conn.QueryRow("SELECT COUNT(*), COUNT(error) FROM source_runs WHERE source = 'xu100' AND kind = 'backfill'").Scan(&runs, &failed); err != nil {
			t.Fatalf("count xu100 runs: %v", err)
		}
		return runs, failed
	}

	backfill(&evds.Fetcher{BaseURL: srv.URL})
	if runs, _ := xu100Runs(); runs != 0 || len(paths) != 0 {
		t.Fatalf("without a key: %d xu100 runs, requests %v, want none", runs, paths)
	}

	backfill(&evds.Fetcher{BaseURL: srv.URL, APIKey: "test-key"})
	// From 14 days before the transaction, so a close precedes it.
	if len(paths) != 1 || !strings.Contains(paths[0], "startDate=19-05-2023&endDate=02-01-2024") {
		t.Fatalf("requests = %v, want one for 19-05-2023..02-01-2024", paths)
	}
	var value string
	if err := conn.QueryRow("SELECT value FROM benchmark_values WHERE code = 'XU100' AND date = '2023-06-01'").Scan(&value); err != nil {
		t.Fatalf("XU100 2023-06-01: %v", err)
	}
	if value != "5500.1" {
		t.Fatalf("XU100 2023-06-01 = %s, want 5500.1", value)
	}
	if runs, failed := xu100Runs(); runs != 1 || failed != 0 {
		t.Fatalf("xu100 runs = %d, failed = %d, want one successful run", runs, failed)
	}
}

// TestBackfillHistorySkipsStoredXU100 checks that with a BIST 100
// close on every weekday up to yesterday, bar a holiday week and the
// transaction's own date, and one before it, a restart makes no EVDS
// request. A missing last month is fetched alone; after an earlier
// transaction, so is the short stretch before the stored closes.
func TestBackfillHistorySkipsStoredXU100(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2023-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2023-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 'deposit', '2023-06-01', '1000', '0', 'TRY', '2023-06-01T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	repo := benchmark.NewRepo(conn)
	for d := time.Date(2023, 5, 19, 0, 0, 0, 0, time.UTC); !d.After(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		// The transaction's 2023-06-01 and Kurban Bayramı 2023-06-27..30
		// have no close, as holidays.
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday || (d.Month() == time.June && (d.Day() == 1 || d.Day() >= 27)) {
			continue
		}
		if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: benchmark.CodeXU100, Date: d.Format(time.DateOnly), Value: "7000", Source: "evds"}}); err != nil {
			t.Fatalf("seed xu100: %v", err)
		}
	}

	disabled := map[string]bool{"bist": true, "tcmb": true, "crypto": true, "tefas": true, "us": true, "gold": true}
	// Wednesday -> yesterday is 2024-01-02.
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)
	backfill := func() {
		t.Helper()
		paths = nil
		if _, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{}, tcmb: &tcmb.Fetcher{}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{}, tefas: &tefas.Fetcher{}, xu100: &evds.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}, now: fixedNow(now)}, "", disabled); err != nil {
			t.Fatalf("backfillHistory: %v", err)
		}
	}

	backfill()
	if len(paths) != 0 {
		t.Fatalf("stored range: requests %v, want none", paths)
	}

	if _, err := conn.ExecContext(ctx, "DELETE FROM benchmark_values WHERE code = 'XU100' AND date > '2023-12-01'"); err != nil {
		t.Fatalf("delete tail: %v", err)
	}
	backfill()
	if len(paths) != 1 || !strings.Contains(paths[0], "startDate=02-12-2023&endDate=02-01-2024") {
		t.Fatalf("missing tail: requests %v, want one for 02-12-2023..02-01-2024", paths)
	}

	// 9 days before the first stored close: under the holiday tolerance,
	// but no close precedes it.
	if _, err := conn.ExecContext(ctx, `INSERT INTO transactions
		(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
		VALUES (2, 1, 1, 'deposit', '2023-05-10', '1000', '0', 'TRY', '2023-05-10T00:00:00Z')`); err != nil {
		t.Fatalf("seed earlier transaction: %v", err)
	}
	backfill()
	want := []string{"startDate=27-04-2023&endDate=18-05-2023", "startDate=02-12-2023&endDate=02-01-2024"}
	if len(paths) != 2 || !strings.Contains(paths[0], want[0]) || !strings.Contains(paths[1], want[1]) {
		t.Fatalf("leading gap: requests %v, want %v", paths, want)
	}
}

// TestBackfillHistoryContinuesAfterFailingSource checks that a BIST
// bulletin server answering 500 does not stop the TCMB and gold walks
// behind it. The FX rates are written, every run is in the run log, and
// the error still reports the BIST failure.
func TestBackfillHistoryContinuesAfterFailingSource(t *testing.T) {
	const fxXML = `<Tarih_Date><Currency CurrencyCode="USD"><Unit>1</Unit><ForexBuying>29.7000</ForexBuying></Currency></Tarih_Date>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "thb") {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/v1/klines/history" {
			w.Write([]byte(`{"s":"ok","t":[1704153600],"c":[62000]}`)) // PAXGTRY 2024-01-02
			return
		}
		w.Write([]byte(fxXML))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY'),
			(2, 'GRAM', 'GOLD', 'Gram Altın', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 1, 'buy', '2024-01-02', '10', '10.25', '102.50', '0', 'TRY', '2024-01-02T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (2, 1, 1, 'deposit', '2024-01-02', '1000', '0', 'USD', '2024-01-02T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Wednesday -> yesterday is Tuesday 2024-01-02, the only walked date.
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)
	walked, err := backfillHistory(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "bist") {
		t.Fatalf("backfillHistory error = %v, want the bist failure", err)
	}

	var rate string
	if err := conn.QueryRow("SELECT rate FROM fx_rates WHERE currency = 'USD' AND date = '2024-01-02'").Scan(&rate); err != nil {
		t.Fatalf("USD rate for 2024-01-02: %v", err)
	}
	if rate != "29.7" {
		t.Fatalf("USD rate = %s, want 29.7", rate)
	}

	runs := map[string]bool{} // source -> failed
	rows, err := conn.Query("SELECT source, error IS NOT NULL FROM source_runs WHERE kind = 'backfill'")
	if err != nil {
		t.Fatalf("query source runs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var failed bool
		if err := rows.Scan(&source, &failed); err != nil {
			t.Fatalf("scan source run: %v", err)
		}
		runs[source] = failed
	}
	if failed, ok := runs["bist"]; !ok || !failed {
		t.Fatalf("bist backfill run = %v (present %v), want a failed run", failed, ok)
	}
	for _, source := range []string{"tcmb", "gold"} {
		if failed, ok := runs[source]; !ok || failed {
			t.Fatalf("%s backfill run failed = %v (present %v), want a successful run", source, failed, ok)
		}
	}
	if !walked {
		t.Fatalf("backfillHistory reported nothing to walk")
	}
}

// TestBackfillHistoryWalksSchedulerSources checks that the startup
// backfill walks exactly the scheduler sources that have a Backfill, bar
// tufe, in their order, so a new source is added in schedulerSources
// alone. Every walk gets a run in the run log, failed or not.
func TestBackfillHistoryWalksSchedulerSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (1, 1, 1, 'deposit', '2024-01-02', '1000', '0', 'USD', '2024-01-02T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Wednesday -> yesterday is Tuesday 2024-01-02, the only walked date.
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, price.Istanbul)
	f := sourceFetchers{
		bist:   &bist.Fetcher{BaseURL: srv.URL + "/"},
		tcmb:   &tcmb.Fetcher{BaseURL: srv.URL + "/"},
		us:     &us.Fetcher{},
		crypto: &btcturk.Fetcher{BaseURL: srv.URL},
		tefas:  &tefas.Fetcher{BaseURL: srv.URL},
		tufe:   &tuik.Fetcher{BaseURL: srv.URL},
		xu100:  &evds.Fetcher{BaseURL: srv.URL, APIKey: "test-key"},
		now:    fixedNow(now),
	}
	if walked, _ := backfillHistory(ctx, testLogger(), conn, f, "", nil); !walked {
		t.Fatalf("backfillHistory reported nothing to walk")
	}

	var want []string
	for _, src := range schedulerSources(conn, testLogger(), f) {
		if src.Backfill != nil && src.Name != "tufe" {
			want = append(want, src.Name)
		}
	}
	var got []string
	rows, err := conn.Query("SELECT source FROM source_runs WHERE kind = 'backfill' ORDER BY id")
	if err != nil {
		t.Fatalf("query source runs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			t.Fatalf("scan source run: %v", err)
		}
		got = append(got, source)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("backfill runs = %v, want the scheduler sources with a Backfill %v", got, want)
	}
}

// TestBackfillHistoryStopsAfterCancel checks that a shutdown that
// cancels ctx during the crypto walk stops it there. No WARN is logged for
// the cancelled pair or the one after it, and the TEFAS walk behind it
// never starts.
func TestBackfillHistoryStopsAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel() // the shutdown arrives during the first request
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY'),
			(2, 'ETH-TRY', 'CRYPTO', 'Ether', 'TRY'),
			(3, 'AFT', 'TEFAS', 'Fund', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2024-01-05', '0.1', '1500000', '150000', '0', 'TRY', '2024-01-05T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2024-01-05', '1', '90000', '90000', '0', 'TRY', '2024-01-05T00:00:00Z'),
			(3, 1, 1, 3, 'buy', '2024-01-05', '10', '1.5', '15', '0', 'TRY', '2024-01-05T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	prev := slog.Default()
	slog.SetDefault(logger) // instrument.EachTracked logs a failing pair there
	defer slog.SetDefault(prev)
	disabled := map[string]bool{"bist": true, "tcmb": true, "us": true}
	now := time.Date(2024, 1, 9, 12, 0, 0, 0, price.Istanbul)
	_, err = backfillHistory(ctx, logger, conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: &btcturk.Fetcher{BaseURL: srv.URL}, tefas: &tefas.Fetcher{BaseURL: srv.URL}, xu100: &evds.Fetcher{}, now: fixedNow(now)}, "", disabled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("backfillHistory error = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "tefas") {
		t.Fatalf("backfillHistory error = %v, want the tefas walk never started", err)
	}
	if logs.Len() > 0 {
		t.Fatalf("logged after cancel:\n%s", logs.String())
	}
}

// TestBackfillTEFASAllFundsFailed checks that a TEFAS backfill in
// which every tracked fund fails returns an error, so its run is stored as
// failed rather than as a success with 0 rows.
func TestBackfillTEFASAllFundsFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'AFT', 'TEFAS', 'Fund A', 'TRY'),
			(2, 'TTE', 'TEFAS', 'Fund B', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2024-01-05', '10', '1.5', '15', '0', 'TRY', '2024-01-05T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2024-01-05', '10', '2.5', '25', '0', 'TRY', '2024-01-05T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	now := time.Date(2024, 1, 9, 12, 0, 0, 0, price.Istanbul)
	r := testRun(conn, now)
	_, err = backfillTEFAS(ctx, r, &tefas.Syncer{Fetcher: &tefas.Fetcher{BaseURL: srv.URL}, Instruments: r.instruments, Prices: r.prices})
	if err == nil || !strings.Contains(err.Error(), "all 2 funds failed") {
		t.Fatalf("backfillTEFAS error = %v, want all 2 funds failed", err)
	}
}

// TestBackfillCryptoSkippedPairDoesNotHideFailure checks that a
// skipped pair does not hide a failure: one pair already priced for every
// day, the other failing, is still an "all failed" run, since the only pair
// fetched failed.
func TestBackfillCryptoSkippedPairDoesNotHideFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(1, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY'),
			(2, 'ETH-TRY', 'CRYPTO', 'Ether', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at)
			VALUES (1, 'u@example.com', 'x', '2024-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
			VALUES (1, 1, 'Main', '2024-01-01T00:00:00Z')`,
		`INSERT INTO transactions
			(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2024-01-05', '1', '1', '1', '0', 'TRY', '2024-01-05T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2024-01-05', '1', '1', '1', '0', 'TRY', '2024-01-05T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, d := range []string{"2024-01-05", "2024-01-06", "2024-01-07", "2024-01-08"} {
		if err := prices.Upsert(ctx, 1, d, "100", btcturk.SourceBtcTurk); err != nil {
			t.Fatalf("seed price: %v", err)
		}
	}

	now := time.Date(2024, 1, 9, 12, 0, 0, 0, price.Istanbul)
	_, err = backfillCrypto(ctx, testRun(conn, now), &btcturk.Fetcher{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "all 1 pairs failed") {
		t.Fatalf("backfillCrypto error = %v, want all 1 pairs failed", err)
	}
}

// TestBackfillRangeSkipsHolidayOnRerun checks that a weekday whose
// bulletin 404s between two days that have one is a holiday. The first
// walk requests it once and remembers it; the next walk must not request
// it again.
func TestBackfillRangeSkipsHolidayOnRerun(t *testing.T) {
	day1, holiday, day3 := "2024-01-02", "2024-01-03", "2024-01-04"
	zip1 := bulletinZip(t, bulletinCSV(day1, "10.25"))
	zip3 := bulletinZip(t, bulletinCSV(day3, "11.75"))

	var mu sync.Mutex
	holidayRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "20240102"):
			w.Write(zip1)
		case strings.Contains(r.URL.Path, "20240104"):
			w.Write(zip3)
		default:
			if strings.Contains(r.URL.Path, "20240103") && strings.Contains(r.URL.Path, "thb") {
				mu.Lock()
				holidayRequests++
				mu.Unlock()
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	gold := goldStub(t, conn)

	// Friday -> yesterday is day3.
	now := time.Date(2024, 1, 5, 12, 0, 0, 0, price.Istanbul)
	ctx := context.Background()
	for run := 1; run <= 2; run++ {
		if err := runBackfill(ctx, testLogger(), conn, sourceFetchers{bist: &bist.Fetcher{BaseURL: srv.URL + "/"}, tcmb: &tcmb.Fetcher{BaseURL: srv.URL + "/"}, us: &us.Fetcher{}, crypto: gold, tefas: &tefas.Fetcher{BaseURL: srv.URL}, now: fixedNow(now)}, day1); err != nil {
			t.Fatalf("runBackfill %d: %v", run, err)
		}
	}

	assertPriceRow(t, conn, day1, "10.25")
	assertPriceRow(t, conn, day3, "11.75")
	assertNoPriceRow(t, conn, holiday)
	if holidayRequests != 1 {
		t.Fatalf("holiday bulletin requested %d times over two walks, want 1", holidayRequests)
	}
}

// goldStub seeds GOLD/GRAM, which instrument.Seed creates in a real
// database, and returns a btcturk fetcher whose klines are an empty series:
// runBackfill always walks gold, and these tests are about other sources.
func goldStub(t *testing.T, conn *sql.DB) *btcturk.Fetcher {
	t.Helper()
	if _, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('GRAM', 'GOLD', 'Gram Altın', 'TRY')`); err != nil {
		t.Fatalf("seed GOLD/GRAM: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"s":"ok","t":[],"c":[]}`))
	}))
	t.Cleanup(srv.Close)
	return &btcturk.Fetcher{BaseURL: srv.URL}
}

// noGold disables the gold walk for backfillHistory tests about other
// sources, whose stub servers do not serve it.
var noGold = map[string]bool{"gold": true}

// fixedNow is a sourceFetchers clock stopped at now.
func fixedNow(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testRun is a backfillRun over conn's repos that ends at now, with no
// start date override.
func testRun(conn *sql.DB, now time.Time) backfillRun {
	return backfillRun{logger: testLogger(), instruments: instrument.NewRepo(conn), prices: price.NewRepo(conn), transactions: transaction.NewRepo(conn), now: now}
}

func assertPriceRow(t *testing.T, conn *sql.DB, date, wantClose string) {
	t.Helper()
	var close, source string
	err := conn.QueryRow("SELECT close, source FROM prices WHERE date = ?", date).Scan(&close, &source)
	if err != nil {
		t.Fatalf("price row for %s: %v", date, err)
	}
	if close != wantClose {
		t.Fatalf("price for %s = %s, want %s", date, close, wantClose)
	}
	if source != "bist_bulletin" {
		t.Fatalf("source for %s = %s, want bist_bulletin", date, source)
	}
}

func assertNoPriceRow(t *testing.T, conn *sql.DB, date string) {
	t.Helper()
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM prices WHERE date = ?", date).Scan(&count); err != nil {
		t.Fatalf("count price rows for %s: %v", date, err)
	}
	if count != 0 {
		t.Fatalf("price rows for %s = %d, want 0", date, count)
	}
}
