package split_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/split"
)

// TestUpsertAndList covers overwrite on the same (instrument, date), input
// rejection, and List ordering by date.
func TestUpsertAndList(t *testing.T) {
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
	repo := split.NewRepo(conn)
	d := decimal.RequireFromString

	if err := repo.Upsert(ctx, 1, "2020-08-31", d("2"), "manual"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Upsert(ctx, 1, "2020-08-31", d("4"), "tiingo"); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	if err := repo.Upsert(ctx, 1, "2014-06-09", d("7"), "tiingo"); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	for _, bad := range []struct{ date, ratio string }{
		{"2021-01-01", "0"},
		{"2021-01-01", "-2"},
		{"2021-1-1", "2"},
	} {
		if err := repo.Upsert(ctx, 1, bad.date, d(bad.ratio), "manual"); err == nil {
			t.Errorf("Upsert(%s, %s) accepted, want error", bad.date, bad.ratio)
		}
	}

	got, err := repo.List(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d splits, want 2: %+v", len(got), got)
	}
	if got[0].Date != "2014-06-09" || !got[0].Ratio.Equal(d("7")) {
		t.Errorf("first = %+v, want 2014-06-09 ratio 7", got[0])
	}
	if got[1].Date != "2020-08-31" || !got[1].Ratio.Equal(d("4")) || got[1].Source != "tiingo" {
		t.Errorf("second = %+v, want 2020-08-31 ratio 4 source tiingo", got[1])
	}
}

// TestListMany covers the one-query lookup: each instrument's splits come
// back date-ascending under its own key, and an instrument without splits
// has no key.
func TestListMany(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple', 'USD'),
		       (2, 'NVDA', 'US', 'Nvidia', 'USD'),
		       (3, 'MSFT', 'US', 'Microsoft', 'USD')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	ctx := context.Background()
	repo := split.NewRepo(conn)
	d := decimal.RequireFromString
	for _, s := range []struct {
		id    int64
		date  string
		ratio string
	}{
		{1, "2020-08-31", "4"},
		{1, "2014-06-09", "7"},
		{2, "2024-06-10", "10"},
	} {
		if err := repo.Upsert(ctx, s.id, s.date, d(s.ratio), "tiingo"); err != nil {
			t.Fatalf("upsert %d %s: %v", s.id, s.date, err)
		}
	}

	got, err := repo.ListMany(ctx, []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("ListMany: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListMany keys = %d, want 2 (instrument 3 has no splits)", len(got))
	}
	if _, ok := got[3]; ok {
		t.Fatalf("ListMany has a key for instrument 3, want none")
	}
	if len(got[1]) != 2 || got[1][0].Date != "2014-06-09" || got[1][1].Date != "2020-08-31" {
		t.Fatalf("ListMany[1] = %+v, want 2014-06-09 then 2020-08-31", got[1])
	}
	if len(got[2]) != 1 || !got[2][0].Ratio.Equal(d("10")) {
		t.Fatalf("ListMany[2] = %+v, want one split of ratio 10", got[2])
	}

	empty, err := repo.ListMany(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("ListMany(nil) = %v, %v; want empty map, nil", empty, err)
	}
}
