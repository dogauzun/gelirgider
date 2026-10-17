package btcturk_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/shopspring/decimal"
)

// TestSyncStoresFinishedCandlesExactly runs Sync against a real SQLite repo
// and a fixture of three candles, the last one today's unfinished day:
// closes must be stored exactly (no float noise, small prices intact),
// under UTC dates, the unfinished candle skipped, and BTC-USD must be
// requested as BTCUSDT.
func TestSyncStoresFinishedCandlesExactly(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	d2, d1 := today.AddDate(0, 0, -2), today.AddDate(0, 0, -1)

	var mu sync.Mutex
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Query().Get("symbol"))
		mu.Unlock()
		if r.URL.Path != "/v1/klines/history" || r.URL.Query().Get("resolution") != "1D" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprintf(w, `{"s":"ok","t":[%d,%d,%d],"o":[1,1,1],"h":[1,1,1],"l":[1,1,1],"c":[3345720.0,0.00024084,3500000.0]}`,
			d2.Unix(), d1.Unix(), today.Unix())
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
			(1, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY'),
			(2, 'BTC-USD', 'CRYPTO', 'Bitcoin', 'USD')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2026-09-10', '1', '1', '1', '0', 'TRY', '2026-09-10T00:00:00Z'),
			(2, 1, 1, 2, 'buy', '2026-09-10', '1', '1', '1', '0', 'USD', '2026-09-10T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	f := &btcturk.Fetcher{BaseURL: srv.URL}
	n, err := btcturk.Sync(ctx, f, instrument.NewRepo(conn), prices, today)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 4 {
		t.Fatalf("Sync wrote %d rows, want 4 (2 finished candles x 2 pairs)", n)
	}

	sort.Strings(requested)
	if fmt.Sprint(requested) != "[BTCTRY BTCUSDT]" {
		t.Fatalf("requested symbols = %v, want [BTCTRY BTCUSDT]", requested)
	}

	rows, err := prices.Range(ctx, []int64{1, 2}, "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	got := fmt.Sprint(rows)
	want := fmt.Sprint([]price.Price{
		{InstrumentID: 1, Date: d2.Format(time.DateOnly), Close: decimal.RequireFromString("3345720"), Source: "btcturk"},
		{InstrumentID: 1, Date: d1.Format(time.DateOnly), Close: decimal.RequireFromString("0.00024084"), Source: "btcturk"},
		{InstrumentID: 2, Date: d2.Format(time.DateOnly), Close: decimal.RequireFromString("3345720"), Source: "btcturk"},
		{InstrumentID: 2, Date: d1.Format(time.DateOnly), Close: decimal.RequireFromString("0.00024084"), Source: "btcturk"},
	})
	if got != want {
		t.Fatalf("stored prices =\n%s\nwant\n%s", got, want)
	}
}

// TestLastCandleDay covers that the last finished candle is counted from
// the UTC date: at 22:30 UTC it is still the 12th there, though already
// the 13th in Istanbul (price.TestYesterday gives a day later).
func TestLastCandleDay(t *testing.T) {
	now := time.Date(2026, 10, 12, 22, 30, 0, 0, time.UTC)
	want := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	if got := btcturk.LastCandleDay(now.In(price.Istanbul)); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("LastCandleDay(%s) = %s, want %s", now, got, want)
	}
}

func TestPairRejectsOtherQuotes(t *testing.T) {
	if _, err := btcturk.Pair("BTC-EUR"); err == nil {
		t.Fatalf("Pair(BTC-EUR): got nil error, want one")
	}
}

// TestSyncFailsWhenEveryPairFails: with the only tracked pair answering 500,
// Sync must return an error, so the run is stored as failed and not as a
// success with 0 rows.
func TestSyncFailsWhenEveryPairFails(t *testing.T) {
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
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 1, 'buy', '2026-09-10', '1', '1', '1', '0', 'TRY', '2026-09-10T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	f := &btcturk.Fetcher{BaseURL: srv.URL}
	n, err := btcturk.Sync(ctx, f, instrument.NewRepo(conn), price.NewRepo(conn), time.Now())
	if err == nil {
		t.Fatalf("Sync with every pair failing: wrote %d, got nil error, want one", n)
	}
}

// TestSyncGoldCountsPricesWhenBenchmarkFails: the GOLD/GRAM price batch
// commits before the GOLD benchmark batch; when the benchmark write fails,
// the returned count is still the days stored as prices.
func TestSyncGoldCountsPricesWhenBenchmarkFails(t *testing.T) {
	d1 := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"s":"ok","t":[%d],"o":[1],"h":[1],"l":[1],"c":[62206.9536]}`, d1.Unix())
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'GRAM', 'GOLD', 'Gram Altın', 'TRY')`,
		`DROP TABLE benchmark_values`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	s := &btcturk.GoldSyncer{Fetcher: &btcturk.Fetcher{BaseURL: srv.URL}, Instruments: instrument.NewRepo(conn), Prices: price.NewRepo(conn), Benchmarks: benchmark.NewRepo(conn)}

	n, err := s.SyncGold(ctx, d1.AddDate(0, 0, -7), d1.AddDate(0, 0, 2))
	if err == nil {
		t.Fatalf("SyncGold with a failing benchmark write: got nil error, want one")
	}
	if n != 1 {
		t.Fatalf("SyncGold = %d, want 1 (the GOLD/GRAM price was committed)", n)
	}
}

// TestSyncGoldStoresGramPrice: without a GOLD/GRAM row SyncGold must fail
// and write nothing; with it, a PAXGTRY close of 62206.9536 TRY/oz must be
// stored as 2000.0000 TRY/g under the candle's UTC date, both as the
// GOLD/GRAM price and as the GOLD benchmark, source btcturk_paxg (ADR 0017).
func TestSyncGoldStoresGramPrice(t *testing.T) {
	d1 := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") != "PAXGTRY" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprintf(w, `{"s":"ok","t":[%d],"o":[1],"h":[1],"l":[1],"c":[62206.9536]}`, d1.Unix())
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	prices, benchmarks := price.NewRepo(conn), benchmark.NewRepo(conn)
	s := &btcturk.GoldSyncer{Fetcher: &btcturk.Fetcher{BaseURL: srv.URL}, Instruments: instrument.NewRepo(conn), Prices: prices, Benchmarks: benchmarks}
	from, to := d1.AddDate(0, 0, -7), d1.AddDate(0, 0, 2)

	if n, err := s.SyncGold(ctx, from, to); err == nil {
		t.Fatalf("SyncGold without GOLD/GRAM: wrote %d, got nil error, want one", n)
	}
	var rows int
	if err := conn.QueryRowContext(ctx,
		"SELECT (SELECT COUNT(*) FROM prices) + (SELECT COUNT(*) FROM benchmark_values)").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("SyncGold without GOLD/GRAM wrote %d rows, want 0", rows)
	}

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'GRAM', 'GOLD', 'Gram Altın', 'TRY')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	n, err := s.SyncGold(ctx, from, to)
	if err != nil || n != 1 {
		t.Fatalf("SyncGold = %d, %v; want 1, nil", n, err)
	}
	date := d1.Format(time.DateOnly)
	gotPrices, err := prices.Range(ctx, []int64{1}, "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatalf("prices.Range: %v", err)
	}
	if got, want := fmt.Sprint(gotPrices), fmt.Sprint([]price.Price{
		{InstrumentID: 1, Date: date, Close: decimal.RequireFromString("2000.0000"), Source: "btcturk_paxg"},
	}); got != want {
		t.Fatalf("stored prices = %s, want %s", got, want)
	}
	gotGold, err := benchmarks.Range(ctx, benchmark.CodeGOLD, "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatalf("benchmarks.Range: %v", err)
	}
	if got, want := fmt.Sprint(gotGold), fmt.Sprint([]benchmark.Value{
		{Code: "GOLD", Date: date, Value: decimal.RequireFromString("2000.0000"), Source: "btcturk_paxg"},
	}); got != want {
		t.Fatalf("stored GOLD benchmark = %s, want %s", got, want)
	}
}
