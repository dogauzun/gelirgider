package statement

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// openTestDB returns an empty database with two users, each owning one
// portfolio (ids 1 and 2).
func openTestDB(t *testing.T) *ImportRepo {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at)
		 VALUES (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		        (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at)
		 VALUES (1, 1, 'Main', '2026-01-01T00:00:00Z'),
		        (2, 2, 'Main', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return NewImportRepo(conn)
}

func TestCreateDuplicateHash(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	if _, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "midas", Currency: "TRY", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", SHA256: "abc"}, nil, nil, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Another period, so the same-statement check (GLG-361) does not answer
	// first.
	if _, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "midas", Currency: "TRY", PeriodStart: "2026-02-01", PeriodEnd: "2026-02-28", SHA256: "abc"}, nil, nil, nil); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second Commit: got %v, want ErrDuplicate", err)
	}
	// The same file in another portfolio is a different import.
	if _, err := repo.Commit(ctx, NewImport{UserID: 2, PortfolioID: 2, Broker: "midas", Currency: "TRY", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", SHA256: "abc"}, nil, nil, nil); err != nil {
		t.Fatalf("Commit for other user: %v", err)
	}
}

func TestDeleteRemovesTransactionsAndIsUserScoped(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	imp, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "midas", Currency: "TRY", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", SHA256: "abc"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	for range 2 {
		if _, err := repo.db.Exec(
			`INSERT INTO transactions (user_id, portfolio_id, type, date, amount, fee, currency, note, created_at, import_id)
			 VALUES (1, 1, 'deposit', '2026-01-02', '100', '0', 'TRY', '', '2026-01-02T00:00:00Z', ?)`, imp.ID,
		); err != nil {
			t.Fatalf("seed transaction: %v", err)
		}
	}

	imports, err := repo.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(imports) != 1 || imports[0].TransactionCount != 2 {
		t.Fatalf("List = %+v, want one import with TransactionCount 2", imports)
	}

	// Another user cannot delete it, and nothing is removed.
	if err := repo.Delete(ctx, 2, imp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete as other user: got %v, want ErrNotFound", err)
	}
	if got := countTransactions(t, repo, imp.ID); got != 2 {
		t.Fatalf("transactions after foreign delete = %d, want 2", got)
	}

	if err := repo.Delete(ctx, 1, imp.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := countTransactions(t, repo, imp.ID); got != 0 {
		t.Fatalf("transactions after delete = %d, want 0", got)
	}
	if imports, err := repo.List(ctx, 1); err != nil || len(imports) != 0 {
		t.Fatalf("List after delete = %+v, %v; want no import", imports, err)
	}
}

// TestCommitRollsBackOnFailedRow: a row insert that fails after the import
// row and an earlier row went in leaves nothing behind (GLG-1273).
func TestCommitRollsBackOnFailedRow(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	row := transaction.Transaction{PortfolioID: 1, Type: "deposit", Date: "2026-01-02", Amount: decimal.NewFromInt(100), Currency: "TRY"}
	bad := row
	bad.PortfolioID = 999 // no such portfolio: the foreign key fails
	_, err := repo.Commit(ctx,
		NewImport{UserID: 1, PortfolioID: 1, Broker: "garanti", Currency: "TRY", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", SHA256: "abc"},
		[]transaction.Transaction{row, bad}, map[string]int64{"SOME FUND": 1}, nil)
	if !errors.Is(err, portfolio.ErrNotFound) {
		t.Fatalf("Commit: got %v, want the failed row's portfolio.ErrNotFound", err)
	}

	if imports, err := repo.List(ctx, 1); err != nil || len(imports) != 0 {
		t.Fatalf("List = %+v, %v; want no import", imports, err)
	}
	var n int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); err != nil || n != 0 {
		t.Fatalf("transactions = %d, %v; want 0", n, err)
	}
	if _, err := repo.Alias(ctx, 1, "garanti", "SOME FUND"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alias: got %v, want ErrNotFound", err)
	}
}

// TestDeletedImportIDNotReused covers GLG-1009: SQLite handed an undone
// import's id to the next import, so a stale tab's undo of the old import
// deleted the new one. The new id must be above the deleted one, and the
// repeated delete must find nothing and leave the new import in place.
func TestDeletedImportIDNotReused(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	old, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "csv", Currency: "TRY", PeriodStart: "2026-10-01", PeriodEnd: "2026-10-02", SHA256: "abc"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Commit old: %v", err)
	}
	if err := repo.Delete(ctx, 1, old.ID); err != nil {
		t.Fatalf("Delete old: %v", err)
	}
	next, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "csv", Currency: "TRY", PeriodStart: "2026-08-20", PeriodEnd: "2026-08-20", SHA256: "def"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Commit next: %v", err)
	}
	if next.ID <= old.ID {
		t.Fatalf("new import id = %d, want above deleted id %d", next.ID, old.ID)
	}
	if err := repo.Delete(ctx, 1, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated Delete: got %v, want ErrNotFound", err)
	}
	if imports, err := repo.List(ctx, 1); err != nil || len(imports) != 1 || imports[0].ID != next.ID {
		t.Fatalf("List after repeated delete = %+v, %v; want only import %d", imports, err, next.ID)
	}
}

func TestOverlapping(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	if _, err := repo.Commit(ctx, NewImport{UserID: 1, PortfolioID: 1, Broker: "midas", Currency: "TRY", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", SHA256: "abc"}, nil, nil, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	cases := []struct {
		name            string
		broker          string
		start, end      string
		wantOverlapping bool
	}{
		{"touching at the end", "midas", "2026-01-31", "2026-02-28", true},
		{"contained", "midas", "2026-01-10", "2026-01-20", true},
		{"disjoint", "midas", "2026-02-01", "2026-02-28", false},
		{"other broker", "other", "2026-01-10", "2026-01-20", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := repo.Overlapping(ctx, 1, 1, c.broker, c.start, c.end)
			if err != nil {
				t.Fatalf("Overlapping: %v", err)
			}
			if (len(got) > 0) != c.wantOverlapping {
				t.Fatalf("Overlapping returned %d imports, want overlapping=%v", len(got), c.wantOverlapping)
			}
		})
	}

	// Another user's import of the same period is invisible.
	got, err := repo.Overlapping(ctx, 2, 2, "midas", "2026-01-10", "2026-01-20")
	if err != nil {
		t.Fatalf("Overlapping for other user: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Overlapping for other user returned %d imports, want 0", len(got))
	}
}

func countTransactions(t *testing.T, repo *ImportRepo, importID int64) int {
	t.Helper()
	var n int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM transactions WHERE import_id = ?", importID).Scan(&n); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return n
}

// A Midas statement prints its trades before its cash section, so the rows
// reach the preview grouped by section rather than dated (GLG-230).
func TestSortByDateKeepsSectionOrderWithinADate(t *testing.T) {
	rows := []previewRow{
		{Key: "trade-0", Date: "2025-09-26"},
		{Key: "div-0", Date: "2025-09-26"},
		{Key: "div-1", Date: "2025-09-30"},
		{Key: "cash-0", Date: "2025-09-11"},
	}
	sortByDate(rows)

	want := []string{"cash-0", "trade-0", "div-0", "div-1"}
	for i, key := range want {
		if rows[i].Key != key {
			t.Fatalf("rows[%d].Key: got %q, want %q", i, rows[i].Key, key)
		}
	}
}

// TestAliasUpsertIsUserScoped covers GLG-735: a second pick for the same
// (user, broker, symbol) replaces the first, and one user's pick is never
// read for another.
func TestAliasUpsertIsUserScoped(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'TEFAS', 'A', 'TRY'), (2, 'BBB', 'TEFAS', 'B', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	upsert := func(userID, instrumentID int64) {
		t.Helper()
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback()
		if err := upsertAlias(ctx, tx, ImportAlias{UserID: userID, Broker: "garanti", Symbol: "SOME FUND", InstrumentID: instrumentID}); err != nil {
			t.Fatalf("upsertAlias: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	upsert(1, 1)
	upsert(1, 2)
	if id, err := repo.Alias(ctx, 1, "garanti", "SOME FUND"); err != nil || id != 2 {
		t.Errorf("Alias = %d, %v; want 2", id, err)
	}
	if _, err := repo.Alias(ctx, 2, "garanti", "SOME FUND"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's Alias: got %v, want ErrNotFound", err)
	}
	if _, err := repo.Alias(ctx, 1, "midas", "SOME FUND"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other broker's Alias: got %v, want ErrNotFound", err)
	}
	var n int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM import_aliases").Scan(&n); err != nil || n != 1 {
		t.Errorf("alias rows = %d, %v; want 1", n, err)
	}
}

// TestLearnedNameNeverReplacesAPick covers GLG-766: a learned name is no
// pick, replaces an earlier learned name, never replaces a pick, and a pick
// replaces it.
func TestLearnedNameNeverReplacesAPick(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'TEFAS', 'A', 'TRY'), (2, 'BBB', 'TEFAS', 'B', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	write := func(learned bool, symbol string, instrumentID int64) {
		t.Helper()
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback()
		if learned {
			err = repo.LearnNameTx(ctx, tx, ImportAlias{UserID: 1, Broker: "garanti", Symbol: symbol, InstrumentID: instrumentID, PeriodEnd: "2025-09-30"})
		} else {
			err = upsertAlias(ctx, tx, ImportAlias{UserID: 1, Broker: "garanti", Symbol: symbol, InstrumentID: instrumentID})
		}
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	write(true, "LEARNED FUND", 1)
	write(true, "LEARNED FUND", 2)
	write(false, "PICKED FUND", 1)
	write(true, "PICKED FUND", 2)
	write(true, "REPICKED FUND", 2)
	write(false, "REPICKED FUND", 1)
	// Instrument 0 forgets a learned name and leaves a pick alone.
	write(true, "FORGOTTEN FUND", 1)
	write(true, "FORGOTTEN FUND", 0)
	write(true, "PICKED FUND", 0)

	if _, err := repo.Alias(ctx, 1, "garanti", "LEARNED FUND"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Alias of a learned name: got %v, want ErrNotFound", err)
	}
	for symbol, want := range map[string]int64{"PICKED FUND": 1, "REPICKED FUND": 1} {
		if id, err := repo.Alias(ctx, 1, "garanti", symbol); err != nil || id != want {
			t.Errorf("Alias(%s) = %d, %v; want %d", symbol, id, err, want)
		}
	}
	learned, err := repo.LearnedNames(ctx, 1, "garanti")
	if err != nil || fmt.Sprint(learned) != "map[LEARNED FUND:2]" {
		t.Errorf("LearnedNames = %v, %v; want map[LEARNED FUND:2]", learned, err)
	}
	if other, err := repo.LearnedNames(ctx, 2, "garanti"); err != nil || len(other) != 0 {
		t.Errorf("other user's LearnedNames = %v, %v; want none", other, err)
	}
}

// TestLearnedNameFollowsPeriod covers GLG-773: statements committed out of
// period order leave the newest statement's name, for a learned name and for
// a forgotten one, and a name learned before periods were stored counts as
// older than any statement.
func TestLearnedNameFollowsPeriod(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'TEFAS', 'A', 'TRY'), (2, 'BBB', 'TEFAS', 'B', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	learn := func(name, periodEnd string, instrumentID int64) {
		t.Helper()
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback()
		if err := repo.LearnNameTx(ctx, tx, ImportAlias{UserID: 1, Broker: "garanti", Symbol: name, InstrumentID: instrumentID, PeriodEnd: periodEnd}); err != nil {
			t.Fatalf("learn: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	// November names BBB, September names AAA: either commit order ends on
	// November's BBB.
	learn("OUT OF ORDER FUND", "2025-11-30", 2)
	learn("OUT OF ORDER FUND", "2025-09-30", 1)
	learn("IN ORDER FUND", "2025-09-30", 1)
	learn("IN ORDER FUND", "2025-11-30", 2)
	// An older statement's forget leaves a newer name; a same-period one
	// forgets it.
	learn("KEPT FUND", "2025-11-30", 2)
	learn("KEPT FUND", "2025-09-30", 0)
	learn("FORGOTTEN FUND", "2025-11-30", 2)
	learn("FORGOTTEN FUND", "2025-11-30", 0)
	// A row from before migration 025 has no period and gives way to any.
	if _, err := repo.db.Exec(`INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at, learned) VALUES (1, 'garanti', 'OLD FUND', 1, '2025-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatalf("seed old name: %v", err)
	}
	learn("OLD FUND", "2020-01-31", 2)

	learned, err := repo.LearnedNames(ctx, 1, "garanti")
	want := "map[IN ORDER FUND:2 KEPT FUND:2 OLD FUND:2 OUT OF ORDER FUND:2]"
	if err != nil || fmt.Sprint(learned) != want {
		t.Errorf("LearnedNames = %v, %v; want %s", learned, err, want)
	}
}

// TestForgottenNameFollowsPeriod covers GLG-821: a name a newer statement
// forgot is not learned again by an older one committed afterwards, while a
// statement of the same or a later period learns it, for this user and
// broker only.
func TestForgottenNameFollowsPeriod(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'TEFAS', 'A', 'TRY'), (2, 'BBB', 'TEFAS', 'B', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	learn := func(userID int64, broker, name, periodEnd string, instrumentID int64) {
		t.Helper()
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback()
		if err := repo.LearnNameTx(ctx, tx, ImportAlias{UserID: userID, Broker: broker, Symbol: name, InstrumentID: instrumentID, PeriodEnd: periodEnd}); err != nil {
			t.Fatalf("learn: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
	// November prints ORNEK FON for AAA and BBB, September for AAA only.
	learn(1, "garanti", "ORNEK FON", "2025-11-30", 0)
	learn(1, "garanti", "ORNEK FON", "2025-09-30", 1)
	// An older forget leaves a newer one; a later statement learns the name.
	learn(1, "garanti", "RELEARNED FON", "2025-11-30", 0)
	learn(1, "garanti", "RELEARNED FON", "2025-09-30", 0)
	learn(1, "garanti", "RELEARNED FON", "2025-10-31", 1)
	learn(1, "garanti", "RELEARNED FON", "2025-11-30", 2)
	// The forget is user 1's at garanti only.
	learn(2, "garanti", "ORNEK FON", "2025-09-30", 1)
	learn(1, "midas", "ORNEK FON", "2025-09-30", 1)

	for _, c := range []struct {
		userID int64
		broker string
		want   string
	}{
		{1, "garanti", "map[RELEARNED FON:2]"},
		{2, "garanti", "map[ORNEK FON:1]"},
		{1, "midas", "map[ORNEK FON:1]"},
	} {
		learned, err := repo.LearnedNames(ctx, c.userID, c.broker)
		if err != nil || fmt.Sprint(learned) != c.want {
			t.Errorf("LearnedNames(%d, %s) = %v, %v; want %s", c.userID, c.broker, learned, err, c.want)
		}
	}
}

// TestLearnedName covers the prefix rule of GLG-766: equal, cut shorter or
// longer resolves; matches of two instruments or none do not.
func TestLearnedName(t *testing.T) {
	learned := map[string]int64{
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON":   1,
		"BETA PORTFÖY KISA VADELİ BORÇLANMA":  2,
		"BETA PORTFÖY KISA VADELİ SERBEST FO": 3,
		"BETA PORTFÖY KISA VADELİ SERBEST":    3,
	}
	for symbol, want := range map[string]int64{
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON":      1,
		"ALFA PORTFÖY DENGELİ":                   1,
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON (TL)": 1,
		"BETA PORTFÖY KISA VADELİ SERBEST":       3,
		"BETA PORTFÖY KISA VADELİ":               0,
		"GAMA PORTFÖY":                           0,
		"":                                       0,
	} {
		if got := learnedName(symbol, learned); got != want {
			t.Errorf("learnedName(%q) = %d, want %d", symbol, got, want)
		}
	}
}
