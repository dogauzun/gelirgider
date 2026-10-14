package dividend_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
)

// TestDismissConcurrent checks that simultaneous dismisses of the same
// dividend (a double click) all succeed and store one row.
func TestDismissConcurrent(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	ctx := context.Background()
	repo := dividend.NewRepo(conn)
	const dates, clicks = 20, 8
	var wg sync.WaitGroup
	errs := make(chan error, dates*clicks)
	for d := 1; d <= dates; d++ {
		date := fmt.Sprintf("2026-05-%02d", d)
		start := make(chan struct{})
		for range clicks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := repo.Dismiss(ctx, 1, 1, 1, date); err != nil {
					errs <- err
				}
			}()
		}
		close(start)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Dismiss: %v", err)
	}

	got, err := repo.Dismissed(ctx, 1, 1)
	if err != nil {
		t.Fatalf("Dismissed: %v", err)
	}
	if len(got) != dates {
		t.Errorf("got %d dismissals, want %d", len(got), dates)
	}
}
