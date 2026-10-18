package watchlist

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// TestInsertDuplicateAndOwnership checks that a second Insert of one
// instrument for one user answers ErrDuplicate and leaves one row, and that
// another user sees none of it and cannot delete it.
func TestInsertDuplicateAndOwnership(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		        (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'TEST', 'BIST', 'Test', 'TRY')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	repo, ctx := NewRepo(conn), context.Background()

	target, side := decimal.RequireFromString("70.5"), "above"
	first, err := repo.Insert(ctx, Item{UserID: 1, InstrumentID: 1, TargetPrice: &target, TargetSide: &side, Note: "wait"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := repo.Insert(ctx, Item{UserID: 1, InstrumentID: 1}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second Insert error = %v, want ErrDuplicate", err)
	}
	items, err := repo.List(ctx, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %v, %v, want one row", items, err)
	}
	if got := items[0]; got.ID != first.ID || got.TargetPrice == nil || !got.TargetPrice.Equal(target) || *got.TargetSide != side || got.Note != "wait" {
		t.Errorf("List row = %+v, want the first insert %+v", got, first)
	}

	if items, err := repo.List(ctx, 2); err != nil || len(items) != 0 {
		t.Errorf("user 2 List = %v, %v, want none", items, err)
	}
	if err := repo.Delete(ctx, 2, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("user 2 Delete = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, 1, first.ID); err != nil {
		t.Errorf("owner Delete = %v, want nil", err)
	}
}
