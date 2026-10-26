package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/shopspring/decimal"
)

func TestWalkRowFactors(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	repo := fx.NewRepo(conn)
	if err := repo.Upsert(ctx, "USD", "2024-01-02", "40", "manual"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	type row struct{ date, currency string }
	rows := []row{{"2024-01-01", "USD"}, {"2024-01-02", "USD"}, {"2024-01-02", "EUR"}, {"2024-01-03", "TRY"}}
	got := map[int]string{}
	missing, err := walkRowFactors(ctx, repo, len(rows), func(i int) (string, string) { return rows[i].date, rows[i].currency }, "TRY", func(i int, f decimal.Decimal) {
		got[i] = f.String()
	})
	if err != nil {
		t.Fatalf("walkRowFactors: %v", err)
	}
	// USD before its first rate and EUR with no rate at all are not visited and are reported.
	if len(got) != 2 || got[1] != "40" || got[3] != "1" {
		t.Errorf("visited %v, want rows 1 (40) and 3 (1)", got)
	}
	if len(missing) != 2 || !missing["USD"] || !missing["EUR"] {
		t.Errorf("missing = %v, want USD and EUR", missing)
	}
}
