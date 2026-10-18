package portfolio

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestNameTakenIgnoresCase checks that the duplicate check folds case,
// Turkish i letters included, on create and rename. Renaming a portfolio to
// another case of its own name, a name differing only by a diacritic, and
// another user's same name stay allowed.
func TestNameTakenIgnoresCase(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	p, err := repo.Create(ctx, 1, "Portföy")
	if err != nil {
		t.Fatalf("create Portföy: %v", err)
	}
	is, err := repo.Create(ctx, 1, "İş")
	if err != nil {
		t.Fatalf("create İş: %v", err)
	}
	for _, name := range []string{"portföy", "PORTFÖY", "iş", "IŞ", "ış", "İŞ"} {
		if _, err := repo.Create(ctx, 1, name); !errors.Is(err, ErrNameTaken) {
			t.Errorf("create %q: err = %v, want ErrNameTaken", name, err)
		}
	}
	if err := repo.Rename(ctx, 1, is.ID, "portföy"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("rename İş to portföy: err = %v, want ErrNameTaken", err)
	}
	if err := repo.Rename(ctx, 1, p.ID, "PORTFÖY"); err != nil {
		t.Errorf("rename Portföy to PORTFÖY: %v", err)
	}
	if err := repo.Rename(ctx, 1, is.ID, "IŞ"); err != nil {
		t.Errorf("rename İş to IŞ: %v", err)
	}
	if _, err := repo.Create(ctx, 1, "Portfoy"); err != nil {
		t.Errorf("create Portfoy: %v", err)
	}
	if _, err := repo.Create(ctx, 2, "portföy"); err != nil {
		t.Errorf("other user create portföy: %v", err)
	}
}

// TestNameTakenUnderConcurrency covers the duplicate check, which ran
// apart from the insert or update, so simultaneous creates, or renames, to
// one name all passed it and left several portfolios with that name.
func TestNameTakenUnderConcurrency(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, created_at)
		VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := NewRepo(conn)
	ctx := context.Background()

	// race runs n calls of fn at once and counts the ones that succeed.
	race := func(n int, fn func(i int) error) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		start := make(chan struct{})
		ok := 0
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := fn(i)
				if err != nil && !errors.Is(err, ErrNameTaken) {
					t.Errorf("call %d: %v", i, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					ok++
				}
			}()
		}
		close(start)
		wg.Wait()
		return ok
	}

	// One round of creates often serializes by chance; ten rarely all do.
	for round := range 10 {
		name := fmt.Sprintf("Yeni %d", round)
		if n := race(16, func(int) error {
			_, err := repo.Create(ctx, 1, name)
			return err
		}); n != 1 {
			t.Errorf("concurrent creates of %q: %d succeeded, want 1", name, n)
		}
	}

	var ids []int64
	for _, name := range []string{"R0", "R1", "R2", "R3", "R4", "R5", "R6", "R7"} {
		p, err := repo.Create(ctx, 1, name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids = append(ids, p.ID)
	}
	if n := race(len(ids), func(i int) error {
		return repo.Rename(ctx, 1, ids[i], "Ortak")
	}); n != 1 {
		t.Errorf("concurrent renames: %d succeeded, want 1", n)
	}
}

// TestDeletedIDNotReused checks that a deleted portfolio's id is not reused.
// SQLite handed the highest deleted rowid to the next insert, so a bookmark of
// a deleted portfolio could open a newer one. The id after a delete must be
// above the deleted one.
func TestDeletedIDNotReused(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, created_at)
		VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	deleted, err := repo.Create(ctx, 1, "Old")
	if err != nil {
		t.Fatalf("create Old: %v", err)
	}
	if _, err := repo.Delete(ctx, 1, deleted.ID, false); err != nil {
		t.Fatalf("delete Old: %v", err)
	}
	next, err := repo.Create(ctx, 1, "New")
	if err != nil {
		t.Fatalf("create New: %v", err)
	}
	if next.ID <= deleted.ID {
		t.Fatalf("new id = %d, want above deleted id %d", next.ID, deleted.ID)
	}
}

// TestNameTakenCollapsesSpace checks that names differing only in inner
// whitespace render alike, so checkName stores them collapsed and the
// duplicate check folds whitespace runs, also for a name stored uncollapsed
// before the fix.
func TestNameTakenCollapsesSpace(t *testing.T) {
	for in, want := range map[string]string{
		"x  y": "x y", "x\u00a0y": "x y", " x\u2003 y ": "x y",
	} {
		if got, ok := checkName(httptest.NewRecorder(), in); !ok || got != want {
			t.Errorf("checkName(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := NewRepo(conn)
	ctx := context.Background()
	if _, err := repo.Create(ctx, 1, "x  y"); err != nil {
		t.Fatalf("create legacy name: %v", err)
	}
	other, err := repo.Create(ctx, 1, "z")
	if err != nil {
		t.Fatalf("create z: %v", err)
	}
	if _, err := repo.Create(ctx, 1, "X Y"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("create X Y: err = %v, want ErrNameTaken", err)
	}
	if err := repo.Rename(ctx, 1, other.ID, "x y"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("rename z to x y: err = %v, want ErrNameTaken", err)
	}
}

// TestDeleteRemovesStatementImports covers a portfolio with a
// statement import, which used to fail to delete with a FOREIGN KEY error. Both
// paths are checked: cascade with an imported transaction, and a plain delete
// of a portfolio whose import has no transactions left. Another user's import
// must survive.
func TestDeleteRemovesStatementImports(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Cascade', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Plain', '2026-01-01T00:00:00Z'),
		 (3, 2, 'Other', '2026-01-01T00:00:00Z')`,
		`INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at) VALUES
		 (7, 1, 1, 'midas', '2026-01-01', '2026-01-31', 'a', '2026-02-01T00:00:00Z'),
		 (8, 1, 2, 'midas', '2026-01-01', '2026-01-31', 'b', '2026-02-01T00:00:00Z'),
		 (9, 2, 3, 'midas', '2026-01-01', '2026-01-31', 'c', '2026-02-01T00:00:00Z')`,
		`INSERT INTO transactions (user_id, portfolio_id, type, date, amount, fee, currency, created_at, import_id)
		 VALUES (1, 1, 'deposit', '2026-01-02', '100', '0', 'TRY', '2026-01-02T00:00:00Z', 7)`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	repo := NewRepo(conn)
	ctx := context.Background()
	if _, err := repo.Delete(ctx, 1, 1, true); err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	if _, err := repo.Delete(ctx, 1, 2, false); err != nil {
		t.Fatalf("plain delete: %v", err)
	}

	var ids []int64
	rows, err := conn.Query("SELECT id FROM statement_imports ORDER BY id")
	if err != nil {
		t.Fatalf("query imports: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("imports left = %v, want [9]", ids)
	}
}

// TestDeleteRemovesAllocationTargets checks that deleting a portfolio takes
// its allocation targets with it and leaves the all-portfolios set, the
// user's other portfolio and another user's targets alone.
func TestDeleteRemovesAllocationTargets(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Gone', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Kept', '2026-01-01T00:00:00Z'),
		 (3, 2, 'Other', '2026-01-01T00:00:00Z')`,
		`INSERT INTO allocation_targets (user_id, portfolio_id, asset_class, weight) VALUES
		 (1, 1, 'bist', '0.4'),
		 (1, 2, 'bist', '0.5'),
		 (1, NULL, 'bist', '0.6'),
		 (2, 3, 'bist', '0.7')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := NewRepo(conn).Delete(context.Background(), 1, 1, false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var got string
	if err := conn.QueryRow("SELECT group_concat(weight) FROM (SELECT weight FROM allocation_targets ORDER BY weight)").Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != "0.5,0.6,0.7" {
		t.Fatalf("weights left = %q, want 0.5,0.6,0.7", got)
	}
}

// TestDeleteRemovesGoals checks that deleting a portfolio takes its goal with
// it and leaves the all-portfolios goal, the user's other portfolio's goal
// and another user's goal alone.
func TestDeleteRemovesGoals(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Gone', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Kept', '2026-01-01T00:00:00Z'),
		 (3, 2, 'Other', '2026-01-01T00:00:00Z')`,
		`INSERT INTO goals (user_id, portfolio_id, amount, unit, target_date, created_at) VALUES
		 (1, 1, '100', 'TRY', '2030-01-01', '2026-01-01T00:00:00Z'),
		 (1, 2, '200', 'TRY', '2030-01-01', '2026-01-01T00:00:00Z'),
		 (1, NULL, '300', 'TRY', '2030-01-01', '2026-01-01T00:00:00Z'),
		 (2, 3, '400', 'TRY', '2030-01-01', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := NewRepo(conn).Delete(context.Background(), 1, 1, false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var got string
	if err := conn.QueryRow("SELECT group_concat(amount) FROM (SELECT amount FROM goals ORDER BY amount)").Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != "200,300,400" {
		t.Fatalf("amounts left = %q, want 200,300,400", got)
	}
}

// TestDeleteRemovesSavingPlans checks that deleting a portfolio takes its
// saving plans with it and leaves the user's other portfolio's plan alone.
func TestDeleteRemovesSavingPlans(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Gone', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Kept', '2026-01-01T00:00:00Z')`,
		`INSERT INTO saving_plans (user_id, portfolio_id, currency, amount, day_of_month, start_date, next_date, created_at) VALUES
		 (1, 1, 'TRY', '100', 5, '2026-01-05', '2026-01-05', '2026-01-01T00:00:00Z'),
		 (1, 2, 'TRY', '200', 5, '2026-01-05', '2026-01-05', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := NewRepo(conn).Delete(context.Background(), 1, 1, false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var got string
	if err := conn.QueryRow("SELECT group_concat(amount) FROM (SELECT amount FROM saving_plans ORDER BY amount)").Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != "200" {
		t.Fatalf("amounts left = %q, want 200", got)
	}
}

// TestDeleteRemovesTermDeposits checks that deleting a portfolio takes its
// term deposits and their DEPOSIT instruments with it, and leaves the
// deposits of the user's other portfolio and of another user alone.
func TestDeleteRemovesTermDeposits(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'Gone', '2026-01-01T00:00:00Z'),
		 (2, 1, 'Kept', '2026-01-01T00:00:00Z'),
		 (3, 2, 'Other', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
		 (1, 'D1', 'DEPOSIT', 'Deposit', 'TRY'),
		 (2, 'D2', 'DEPOSIT', 'Deposit', 'TRY'),
		 (3, 'D3', 'DEPOSIT', 'Deposit', 'TRY')`,
		`INSERT INTO term_deposits (user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate, start_date, maturity_date, created_at) VALUES
		 (1, 1, 1, 'TRY', '1000', '0.45', '0.15', '2026-01-01', '2026-02-01', '2026-01-01T00:00:00Z'),
		 (1, 2, 2, 'TRY', '1000', '0.45', '0.15', '2026-01-01', '2026-02-01', '2026-01-01T00:00:00Z'),
		 (2, 3, 3, 'TRY', '1000', '0.45', '0.15', '2026-01-01', '2026-02-01', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := NewRepo(conn).Delete(context.Background(), 1, 1, false); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, q := range []string{
		"SELECT instrument_id FROM term_deposits ORDER BY 1",
		"SELECT id FROM instruments ORDER BY 1",
	} {
		rows, err := conn.Query(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		var got []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			got = append(got, id)
		}
		rows.Close()
		if fmt.Sprint(got) != "[2 3]" {
			t.Errorf("%s = %v, want [2 3]", q, got)
		}
	}
}
