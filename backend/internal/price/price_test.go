package price_test

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

func TestValidClose(t *testing.T) {
	cases := map[string]bool{
		"123.45":                   true,
		"0.000001":                 true,
		"123456789012345.12345678": true,
		"0":                        false,
		"0.00":                     false,
		"-1":                       false,
		"1e5":                      false,
		"1234567890123456":         false, // 16 integer digits
		"1.123456789":              false, // 9 fraction digits
	}
	for in, want := range cases {
		if got := price.ValidClose(in); got != want {
			t.Errorf("ValidClose(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidDate(t *testing.T) {
	cases := map[string]bool{
		"2024-01-02":           true,
		"2024-02-29":           true,  // leap year
		"2026-02-31":           false, // impossible day
		"2026-00-00":           false, // month and day zero
		"2026-13-01":           false, // month out of range
		"2026-1-2":             false, // not zero padded
		"2026-01-02T00:00:00Z": false,
		"":                     false,
	}
	for in, want := range cases {
		if got := price.ValidDate(in); got != want {
			t.Errorf("ValidDate(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestCheckDate covers the shared user-date check: each caller prefixes its
// field name to these message tails, and the frontend translates the
// prefixed messages, so the tails must not change (GLG-1275).
func TestCheckDate(t *testing.T) {
	tomorrow := time.Now().In(price.Istanbul).AddDate(0, 0, 1).Format(time.DateOnly)
	cases := map[string]string{
		price.Today(): "",
		"1990-01-01":  "",
		"2026-02-31":  "must be YYYY-MM-DD",
		"abc":         "must be YYYY-MM-DD",
		"1989-12-31":  "must be on or after 1990-01-01",
		tomorrow:      "must not be in the future",
	}
	for in, want := range cases {
		got := ""
		if err := price.CheckDate(in); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("CheckDate(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestYesterday covers that yesterday is counted from the Istanbul date,
// not the UTC one: at 22:30 UTC it is already the next day in Istanbul
// (btcturk.TestLastCandleDay gives a day earlier for the same instant).
func TestYesterday(t *testing.T) {
	now := time.Date(2026, 10, 12, 22, 30, 0, 0, time.UTC)
	want := time.Date(2026, 10, 12, 0, 0, 0, 0, price.Istanbul)
	if got := price.Yesterday(now); !got.Equal(want) || got.Location() != price.Istanbul {
		t.Errorf("Yesterday(%s) = %s, want %s", now, got, want)
	}
}

// TestUpsertAndLatest covers that Upsert overwrites an existing row for the
// same (instrument, date) instead of duplicating it, and that Latest picks
// the newest date per instrument while ignoring rows dated after asOf, in
// one call for several ids, omitting an id with no row (GLG-1234).
func TestUpsertAndLatest(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY'),
		       (2, 'GARAN', 'BIST', 'Garanti Bankasi', 'TRY'),
		       (3, 'AKBNK', 'BIST', 'Akbank', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	ctx := context.Background()
	repo := price.NewRepo(conn)

	if err := repo.Upsert(ctx, 1, "2024-01-01", "100.00", "test"); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if err := repo.Upsert(ctx, 1, "2024-01-01", "105.50", "test"); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM prices WHERE instrument_id = 1 AND date = '2024-01-01'").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}

	hist, err := repo.History(ctx, 1, 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 1 || hist[0].Close.String() != "105.5" {
		t.Fatalf("History = %+v, want single row with close 105.50", hist)
	}

	if err := repo.Upsert(ctx, 1, "2024-01-02", "110.00", "test"); err != nil {
		t.Fatalf("Upsert instrument 1 later date: %v", err)
	}
	if err := repo.Upsert(ctx, 2, "2024-01-01", "50.00", "test"); err != nil {
		t.Fatalf("Upsert instrument 2: %v", err)
	}
	if err := repo.Upsert(ctx, 2, "2023-12-31", "49.00", "test"); err != nil {
		t.Fatalf("Upsert instrument 2 earlier date: %v", err)
	}

	// A row dated after asOf must not become the last price.
	if err := repo.Upsert(ctx, 1, "2030-01-01", "5.00", "test"); err != nil {
		t.Fatalf("Upsert instrument 1 future date: %v", err)
	}

	latest, err := repo.Latest(ctx, []int64{1, 2, 3}, "2024-01-02")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if _, ok := latest[3]; ok || len(latest) != 2 {
		t.Fatalf("Latest = %+v, want ids 1 and 2 only (3 has no price)", latest)
	}
	if got := latest[1]; got.Date != "2024-01-02" || got.Close.String() != "110" {
		t.Fatalf("Latest[1] = %+v, want date 2024-01-02 close 110.00 (future row ignored)", got)
	}
	if got := latest[2]; got.Date != "2024-01-01" || got.Close.String() != "50" {
		t.Fatalf("Latest[2] = %+v, want date 2024-01-01 close 50.00", got)
	}
}

// TestManualLatest covers ManualRepo.Latest in one call for several ids:
// each id's newest row on or before asOf, a future row ignored, an id with
// no row of this user's omitted even when another user has one (GLG-1234).
func TestManualLatest(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY'),
		       (2, 'GARAN', 'BIST', 'Garanti Bankasi', 'TRY'),
		       (3, 'AKBNK', 'BIST', 'Akbank', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	users := user.NewRepo(conn)
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create bob: %v", err)
	}

	ctx := context.Background()
	repo := price.NewManualRepo(conn)
	for _, row := range []struct {
		userID, id  int64
		date, close string
	}{
		{alice.ID, 1, "2024-01-01", "100"},
		{alice.ID, 1, "2024-01-02", "110"},
		{alice.ID, 1, "2030-01-01", "5"},
		{alice.ID, 2, "2023-12-31", "49"},
		{bob.ID, 3, "2024-01-02", "70"},
	} {
		if err := repo.Upsert(ctx, row.userID, row.id, row.date, row.close); err != nil {
			t.Fatalf("Upsert %+v: %v", row, err)
		}
	}

	latest, err := repo.Latest(ctx, alice.ID, []int64{1, 2, 3}, "2024-01-02")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := latest[1]; got.Date != "2024-01-02" || got.Close.String() != "110" || got.Source != "manual" {
		t.Errorf("Latest[1] = %+v, want date 2024-01-02 close 110 source manual (future row ignored)", got)
	}
	if got := latest[2]; got.Date != "2023-12-31" || got.Close.String() != "49" {
		t.Errorf("Latest[2] = %+v, want date 2023-12-31 close 49", got)
	}
	if _, ok := latest[3]; ok || len(latest) != 2 {
		t.Errorf("Latest = %+v, want ids 1 and 2 only (3 is bob's)", latest)
	}

	rng, err := repo.Range(ctx, alice.ID, []int64{1, 2, 3}, "2024-01-01", "2024-01-02")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	var got []string
	for _, p := range rng {
		got = append(got, fmt.Sprintf("%d %s %s %s", p.InstrumentID, p.Date, p.Close, p.Source))
	}
	if want := []string{"1 2024-01-01 100 manual", "1 2024-01-02 110 manual"}; !slices.Equal(got, want) {
		t.Errorf("Range = %q, want %q (bob's row for 3 excluded)", got, want)
	}
}

// TestManualUpsertConcurrentNewKey covers GLG-1019: concurrent saves of the
// same new manual price all succeed and leave one row. Before the fix two
// saves could both miss on UPDATE and both INSERT, and the second failed on
// the primary key.
func TestManualUpsertConcurrentNewKey(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}

	ctx := context.Background()
	repo := price.NewManualRepo(conn)
	for d := 1; d <= 28; d++ {
		date := fmt.Sprintf("2025-03-%02d", d)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		start := make(chan struct{})
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- repo.Upsert(ctx, usr.ID, 1, date, "10")
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("Upsert %s: %v", date, err)
			}
		}
	}

	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM manual_prices WHERE user_id = ?", usr.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 28 {
		t.Fatalf("rows = %d, want 28", n)
	}
}

// TestFetchUpsertConcurrentNewKey covers GLG-1025: the fetch writers
// (prices, fx rates, splits, dividends, benchmark values) let concurrent
// writes of one new key all succeed and leave one row. Before the fix two
// writers could both miss on UPDATE and both INSERT, and the second failed
// on the primary key. One test for all five: they share the shape.
func TestFetchUpsertConcurrentNewKey(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	ctx := context.Background()
	prices, rates := price.NewRepo(conn), fx.NewRepo(conn)
	splits, dividends, benchmarks := split.NewRepo(conn), dividend.NewRepo(conn), benchmark.NewRepo(conn)
	one := decimal.NewFromInt(1)
	writers := []struct {
		table  string
		upsert func(date string) error
	}{
		{"prices", func(d string) error { return prices.Upsert(ctx, 1, d, "10", "test") }},
		{"fx_rates", func(d string) error { return rates.Upsert(ctx, "USD", d, "40", "test") }},
		{"splits", func(d string) error { return splits.Upsert(ctx, 1, d, one, "test") }},
		{"dividends", func(d string) error { return dividends.Upsert(ctx, 1, d, one, "TRY", "test") }},
		{"benchmark_values", func(d string) error { return benchmarks.Upsert(ctx, benchmark.CodeXU100, d, "10", "test") }},
	}
	for _, w := range writers {
		t.Run(w.table, func(t *testing.T) {
			for d := 1; d <= 28; d++ {
				date := fmt.Sprintf("2025-03-%02d", d)
				var wg sync.WaitGroup
				errs := make(chan error, 8)
				start := make(chan struct{})
				for range 8 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						errs <- w.upsert(date)
					}()
				}
				close(start)
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatalf("Upsert %s: %v", date, err)
					}
				}
			}
			var n int
			if err := conn.QueryRow("SELECT COUNT(*) FROM " + w.table).Scan(&n); err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != 28 {
				t.Fatalf("rows = %d, want 28", n)
			}
		})
	}
}
