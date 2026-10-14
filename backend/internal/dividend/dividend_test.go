package dividend_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
)

// TestUpsertAndListFor covers overwrite on the same (instrument, date),
// input rejection, and ListFor ordering by date.
func TestUpsertAndListFor(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple', 'USD')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	ctx := context.Background()
	repo := dividend.NewRepo(conn)
	d := decimal.RequireFromString

	if err := repo.Upsert(ctx, 1, "2024-08-12", d("0.24"), "USD", "manual"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Upsert(ctx, 1, "2024-08-12", d("0.25"), "USD", "tiingo"); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	if err := repo.Upsert(ctx, 1, "2024-05-10", d("0.25"), "USD", "tiingo"); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	for _, bad := range []struct{ date, amount string }{
		{"2024-11-08", "0"},
		{"2024-11-08", "-0.25"},
		{"2024-11-8", "0.25"},
	} {
		if err := repo.Upsert(ctx, 1, bad.date, d(bad.amount), "USD", "manual"); err == nil {
			t.Errorf("Upsert(%s, %s) accepted, want error", bad.date, bad.amount)
		}
	}

	all, err := repo.ListFor(ctx, []int64{1, 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, ok := all[2]; ok {
		t.Errorf("instrument 2 has a key, want none")
	}
	got := all[1]
	if len(got) != 2 {
		t.Fatalf("got %d dividends, want 2: %+v", len(got), got)
	}
	if got[0].Date != "2024-05-10" {
		t.Errorf("first = %+v, want 2024-05-10", got[0])
	}
	if got[1].Date != "2024-08-12" || !got[1].Amount.Equal(d("0.25")) || got[1].Currency != "USD" || got[1].Source != "tiingo" {
		t.Errorf("second = %+v, want 2024-08-12 amount 0.25 USD source tiingo", got[1])
	}
}
