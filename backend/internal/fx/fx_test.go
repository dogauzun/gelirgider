package fx_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/shopspring/decimal"
)

// TestUpsertAndLatest covers that Upsert overwrites an existing row for the
// same (currency, date) instead of duplicating it, and that Latest picks
// the newest date per currency while ignoring rows dated after asOf and
// omitting a currency with no row, all in one query.
func TestUpsertAndLatest(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	repo := fx.NewRepo(conn)

	if err := repo.Upsert(ctx, "USD", "2024-01-01", "32.00", "tcmb"); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if err := repo.Upsert(ctx, "USD", "2024-01-01", "32.10", "tcmb"); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM fx_rates WHERE currency = 'USD' AND date = '2024-01-01'").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}

	if err := repo.Upsert(ctx, "USD", "2024-01-02", "32.50", "tcmb"); err != nil {
		t.Fatalf("Upsert USD later date: %v", err)
	}
	if err := repo.Upsert(ctx, "EUR", "2024-01-01", "35.00", "tcmb"); err != nil {
		t.Fatalf("Upsert EUR: %v", err)
	}
	if err := repo.Upsert(ctx, "EUR", "2023-12-31", "34.80", "tcmb"); err != nil {
		t.Fatalf("Upsert EUR earlier date: %v", err)
	}

	// A row dated after asOf must not become the "current" rate.
	if err := repo.Upsert(ctx, "USD", "2030-01-01", "999.00", "tcmb"); err != nil {
		t.Fatalf("Upsert USD future date: %v", err)
	}

	latest, err := repo.Latest(ctx, []string{"USD", "EUR", "GBP"}, "2024-01-02")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := latest["USD"]; got.Date != "2024-01-02" || got.Rate.String() != "32.5" {
		t.Fatalf("Latest[USD] = %+v, want date 2024-01-02 rate 32.50 (future row ignored)", got)
	}
	if got := latest["EUR"]; got.Date != "2024-01-01" || got.Rate.String() != "35" {
		t.Fatalf("Latest[EUR] = %+v, want date 2024-01-01 rate 35.00", got)
	}
	if got, ok := latest["GBP"]; ok || len(latest) != 2 {
		t.Fatalf("Latest = %+v, want GBP (no rows) omitted, got %+v", latest, got)
	}
}

// TestRange covers that Range returns rows within [from, to] ordered by
// date then currency, and excludes rows outside the window.
func TestRange(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	repo := fx.NewRepo(conn)

	seed := []fx.Rate{
		{Currency: "USD", Date: "2024-01-01", Rate: decimal.RequireFromString("32.00"), Source: "tcmb"},
		{Currency: "EUR", Date: "2024-01-01", Rate: decimal.RequireFromString("35.00"), Source: "tcmb"},
		{Currency: "USD", Date: "2024-01-02", Rate: decimal.RequireFromString("32.10"), Source: "tcmb"},
		{Currency: "USD", Date: "2023-12-31", Rate: decimal.RequireFromString("31.90"), Source: "tcmb"}, // before window
		{Currency: "USD", Date: "2024-02-01", Rate: decimal.RequireFromString("33.00"), Source: "tcmb"}, // after window
	}
	for _, s := range seed {
		if err := repo.Upsert(ctx, s.Currency, s.Date, s.Rate.String(), s.Source); err != nil {
			t.Fatalf("seed Upsert %+v: %v", s, err)
		}
	}

	got, err := repo.Range(ctx, []string{"USD", "EUR"}, "2024-01-01", "2024-01-02")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	want := []fx.Rate{
		{Currency: "EUR", Date: "2024-01-01", Rate: decimal.RequireFromString("35.00"), Source: "tcmb"},
		{Currency: "USD", Date: "2024-01-01", Rate: decimal.RequireFromString("32.00"), Source: "tcmb"},
		{Currency: "USD", Date: "2024-01-02", Rate: decimal.RequireFromString("32.10"), Source: "tcmb"},
	}
	if len(got) != len(want) {
		t.Fatalf("Range returned %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if fmt.Sprint(got[i]) != fmt.Sprint(want[i]) {
			t.Fatalf("Range[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestHasSource covers the backfill-skip lookup: true only when a row for
// the exact (date, source) pair exists.
func TestHasSource(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	repo := fx.NewRepo(conn)

	if err := repo.Upsert(ctx, "USD", "2024-01-01", "32.00", "tcmb"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	has, err := repo.HasSource(ctx, "2024-01-01", "tcmb")
	if err != nil {
		t.Fatalf("HasSource: %v", err)
	}
	if !has {
		t.Fatal("HasSource = false, want true")
	}

	has, err = repo.HasSource(ctx, "2024-01-01", "manual")
	if err != nil {
		t.Fatalf("HasSource (wrong source): %v", err)
	}
	if has {
		t.Fatal("HasSource = true for unseeded source, want false")
	}

	has, err = repo.HasSource(ctx, "2024-01-02", "tcmb")
	if err != nil {
		t.Fatalf("HasSource (wrong date): %v", err)
	}
	if has {
		t.Fatal("HasSource = true for unseeded date, want false")
	}
}
