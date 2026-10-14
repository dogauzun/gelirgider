package demo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/demo"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestSeed covers that the seed creates the admin demo user, its Demo
// portfolio and transactions, one price per calendar day over the two
// years, and that a second run refuses the now non-empty database.
func TestSeed(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("instrument.Seed: %v", err)
	}

	if err := demo.Seed(ctx, conn, "2026-10-14"); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	u, err := user.NewRepo(conn).GetByEmail(ctx, demo.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if !u.IsAdmin || u.ReportingCurrency != "TRY" {
		t.Errorf("user admin=%v currency=%s, want admin TRY", u.IsAdmin, u.ReportingCurrency)
	}
	ps, err := portfolio.NewRepo(conn).List(ctx, u.ID)
	if err != nil || len(ps) != 1 || ps[0].Name != "Demo" {
		t.Fatalf("portfolios = %+v, %v; want one named Demo", ps, err)
	}
	txs, err := transaction.NewRepo(conn).List(ctx, u.ID, ps[0].ID)
	if err != nil || len(txs) != 18 {
		t.Fatalf("transactions = %d, %v; want 18", len(txs), err)
	}

	thyao, err := instrument.NewRepo(conn).BySymbol(ctx, "BIST", "THYAO")
	if err != nil {
		t.Fatalf("BySymbol: %v", err)
	}
	rows, err := price.NewRepo(conn).Range(ctx, []int64{thyao.ID}, "2024-10-14", "2026-10-14")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(rows) != 731 {
		t.Fatalf("THYAO prices = %d, want 731", len(rows))
	}
	day, _ := price.ParseDate("2024-10-14")
	for _, p := range rows {
		if want := day.Format(time.DateOnly); p.Date != want {
			t.Fatalf("price date %s, want %s", p.Date, want)
		}
		day = day.AddDate(0, 0, 1)
	}

	if err := demo.Seed(ctx, conn, "2026-10-14"); !errors.Is(err, demo.ErrNotEmpty) {
		t.Fatalf("second Seed = %v, want ErrNotEmpty", err)
	}
}
