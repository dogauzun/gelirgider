package allocation

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

func tg(class, w string) Target {
	return Target{AssetClass: class, Weight: decimal.RequireFromString(w)}
}

// TestRepo checks that List orders by Classes, that Replace swaps a set whole,
// that the all-portfolios set (nil) is independent of a portfolio's set and
// of another user's, and that an empty Replace clears.
func TestRepo(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'a@example.com', 'x', '2026-09-11T00:00:00Z'), (2, 'b@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ctx := context.Background()
	r := NewRepo(conn)
	one := int64(1)

	if err := r.Replace(ctx, 1, &one, []Target{tg("cash", "10"), tg("equity", "60.5"), tg("fund", "29.5")}); err != nil {
		t.Fatalf("replace portfolio: %v", err)
	}
	if err := r.Replace(ctx, 1, nil, []Target{tg("crypto", "100")}); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	got, err := r.List(ctx, 1, &one)
	if err != nil {
		t.Fatalf("list portfolio: %v", err)
	}
	if want := fmt.Sprint([]Target{tg("equity", "60.5"), tg("fund", "29.5"), tg("cash", "10")}); fmt.Sprint(got) != want {
		t.Errorf("portfolio set = %v, want %v", got, want)
	}
	if got, _ := r.List(ctx, 1, nil); fmt.Sprint(got) != fmt.Sprint([]Target{tg("crypto", "100")}) {
		t.Errorf("all set = %v, want crypto 100 only", got)
	}
	if got, _ := r.List(ctx, 2, nil); len(got) != 0 {
		t.Errorf("other user's all set = %v, want empty", got)
	}

	if err := r.Replace(ctx, 1, &one, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got, _ := r.List(ctx, 1, &one); len(got) != 0 {
		t.Errorf("after clear = %v, want empty", got)
	}
	if got, _ := r.List(ctx, 1, nil); len(got) != 1 {
		t.Errorf("clearing the portfolio set touched the all set: %v", got)
	}
}
