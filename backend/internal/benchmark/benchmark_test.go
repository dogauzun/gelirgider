package benchmark_test

import (
	"context"
	"maps"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestRepo covers UpsertMany overwriting the same key, rejection of invalid
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
		if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: x, Date: "2024-01-01", Value: bad, Source: "test"}}); err == nil {
			t.Errorf("UpsertMany(%q) = nil, want error", bad)
		}
	}

	if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: x, Date: "2024-01-02", Value: "9000.10", Source: "a"}}); err != nil {
		t.Fatalf("UpsertMany: %v", err)
	}
	if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: x, Date: "2024-01-02", Value: "9100.25", Source: "b"}}); err != nil {
		t.Fatalf("UpsertMany overwrite: %v", err)
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
		if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: x, Date: r[0], Value: r[1], Source: "a"}}); err != nil {
			t.Fatalf("UpsertMany %s: %v", r[0], err)
		}
	}
	if err := repo.UpsertMany(ctx, []benchmark.Row{{Code: benchmark.CodeTUFE, Date: "2024-01-01", Value: "1984.02", Source: "a"}}); err != nil {
		t.Fatalf("UpsertMany TUFE: %v", err)
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

// TestUpsertMany covers a batch of new rows, existing rows updated, and a
// mix of both in one call, and that a batch with an invalid value stores
// none of its rows.
func TestUpsertMany(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	repo := benchmark.NewRepo(conn)
	x := benchmark.CodeXU100
	check := func(step string, want map[string]string) {
		t.Helper()
		rows, err := conn.Query("SELECT code, date, value, source FROM benchmark_values")
		if err != nil {
			t.Fatalf("query benchmark_values: %v", err)
		}
		defer rows.Close()
		got := map[string]string{}
		for rows.Next() {
			var code, date, value, source string
			if err := rows.Scan(&code, &date, &value, &source); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got[code+" "+date] = value + " " + source
		}
		if !maps.Equal(got, want) {
			t.Fatalf("%s: benchmark_values = %v, want %v", step, got, want)
		}
	}

	if err := repo.UpsertMany(ctx, nil); err != nil {
		t.Fatalf("empty UpsertMany: %v", err)
	}
	check("empty", map[string]string{})

	if err := repo.UpsertMany(ctx, []benchmark.Row{
		{Code: x, Date: "2024-01-01", Value: "9000.10", Source: "a"},
		{Code: x, Date: "2024-01-02", Value: "9100.20", Source: "a"},
	}); err != nil {
		t.Fatalf("new rows: %v", err)
	}
	check("new rows", map[string]string{"XU100 2024-01-01": "9000.10 a", "XU100 2024-01-02": "9100.20 a"})

	if err := repo.UpsertMany(ctx, []benchmark.Row{
		{Code: x, Date: "2024-01-01", Value: "9001", Source: "b"},
		{Code: x, Date: "2024-01-02", Value: "9101", Source: "b"},
	}); err != nil {
		t.Fatalf("updated rows: %v", err)
	}
	check("updated rows", map[string]string{"XU100 2024-01-01": "9001 b", "XU100 2024-01-02": "9101 b"})

	if err := repo.UpsertMany(ctx, []benchmark.Row{
		{Code: x, Date: "2024-01-02", Value: "9102", Source: "c"},
		{Code: x, Date: "2024-01-03", Value: "9103", Source: "c"},
	}); err != nil {
		t.Fatalf("mixed rows: %v", err)
	}
	want := map[string]string{"XU100 2024-01-01": "9001 b", "XU100 2024-01-02": "9102 c", "XU100 2024-01-03": "9103 c"}
	check("mixed rows", want)

	if err := repo.UpsertMany(ctx, []benchmark.Row{
		{Code: x, Date: "2024-01-04", Value: "9104", Source: "d"},
		{Code: x, Date: "2024-01-05", Value: "0", Source: "d"},
	}); err == nil {
		t.Fatal("batch with a zero value = nil, want error")
	}
	check("invalid value", want)
}
