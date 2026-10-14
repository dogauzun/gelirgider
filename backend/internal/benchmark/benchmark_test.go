package benchmark_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestRepo covers Upsert overwriting the same key, rejection of invalid
// values, Latest before/at/after rows, Range bounds and ordering, and Have.
func TestRepo(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	repo := benchmark.NewRepo(conn)
	x := benchmark.CodeXU100

	for _, bad := range []string{"0", "abc", "-1"} {
		if err := repo.Upsert(ctx, x, "2024-01-01", bad, "test"); err == nil {
			t.Errorf("Upsert(%q) = nil, want error", bad)
		}
	}

	if err := repo.Upsert(ctx, x, "2024-01-02", "9000.10", "a"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := repo.Upsert(ctx, x, "2024-01-02", "9100.25", "b"); err != nil {
		t.Fatalf("Upsert overwrite: %v", err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM benchmark_values WHERE code = ? AND date = '2024-01-02'", x).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}

	// Inserted out of order to check Range ordering.
	for _, r := range [][2]string{{"2024-01-05", "9300"}, {"2024-01-03", "9200"}} {
		if err := repo.Upsert(ctx, x, r[0], r[1], "a"); err != nil {
			t.Fatalf("Upsert %s: %v", r[0], err)
		}
	}
	if err := repo.Upsert(ctx, benchmark.CodeTUFE, "2024-01-01", "1984.02", "a"); err != nil {
		t.Fatalf("Upsert TUFE: %v", err)
	}

	if _, ok, err := repo.Latest(ctx, x, "2024-01-01"); err != nil || ok {
		t.Errorf("Latest before rows: ok=%v err=%v, want ok=false", ok, err)
	}
	v, ok, err := repo.Latest(ctx, x, "2024-01-02")
	if err != nil || !ok || v.Value.String() != "9100.25" || v.Source != "b" {
		t.Errorf("Latest at row = %+v ok=%v err=%v, want 9100.25 from b", v, ok, err)
	}
	v, ok, err = repo.Latest(ctx, x, "2024-01-04")
	if err != nil || !ok || v.Date != "2024-01-03" {
		t.Errorf("Latest between rows = %+v ok=%v err=%v, want 2024-01-03", v, ok, err)
	}
	v, ok, err = repo.Latest(ctx, x, "2030-01-01")
	if err != nil || !ok || v.Date != "2024-01-05" {
		t.Errorf("Latest after rows = %+v ok=%v err=%v, want 2024-01-05", v, ok, err)
	}

	got, err := repo.Range(ctx, x, "2024-01-02", "2024-01-03")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(got) != 2 || got[0].Date != "2024-01-02" || got[1].Date != "2024-01-03" {
		t.Errorf("Range bounds = %+v, want 2024-01-02, 2024-01-03", got)
	}
	got, err = repo.Range(ctx, x, "2000-01-01", "2030-01-01")
	if err != nil {
		t.Fatalf("Range all: %v", err)
	}
	if len(got) != 3 || got[0].Date != "2024-01-02" || got[1].Date != "2024-01-03" || got[2].Date != "2024-01-05" {
		t.Errorf("Range ordering = %+v", got)
	}

	if have, err := repo.Have(ctx, x, "2024-01-03"); err != nil || !have {
		t.Errorf("Have existing = %v, %v; want true", have, err)
	}
	if have, err := repo.Have(ctx, x, "2024-01-04"); err != nil || have {
		t.Errorf("Have missing = %v, %v; want false", have, err)
	}
}
