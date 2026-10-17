package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

func TestOpen_SchemaAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	repo := user.NewRepo(conn)
	created, err := repo.Create(context.Background(), "alice@example.com", "hash123")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	fetched, err := repo.GetByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if fetched.ID != created.ID || fetched.Email != created.Email {
		t.Fatalf("fetched user %+v does not match created %+v", fetched, created)
	}
	conn.Close()

	// Reopening the same file must not fail or re-apply schema, and data
	// must still be there.
	conn2, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer conn2.Close()

	repo2 := user.NewRepo(conn2)
	again, err := repo2.GetByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail after reopen: %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("expected user to persist across reopen, got %+v", again)
	}
}

func TestBackup(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "source.db")
	conn, err := db.Open(srcPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	// VACUUM INTO needs SQLite 3.27+; modernc.org/sqlite bundles a much
	// newer version, but assert it so a downgrade fails loudly here rather
	// than as a mysterious VACUUM INTO error.
	var version string
	if err := conn.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatalf("sqlite_version: %v", err)
	}
	if version < "3.27.0" {
		t.Skipf("VACUUM INTO needs SQLite 3.27+, got %s", version)
	}

	repo := user.NewRepo(conn)
	created, err := repo.Create(context.Background(), "bob@example.com", "hash456")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(context.Background(), conn, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Backup must refuse to overwrite an existing target.
	if err := db.Backup(context.Background(), conn, backupPath); err == nil {
		t.Fatalf("expected second Backup to the same path to fail")
	}

	copyConn, err := db.Open(backupPath)
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	defer copyConn.Close()

	copyRepo := user.NewRepo(copyConn)
	fetched, err := copyRepo.GetByEmail(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("GetByEmail from backup: %v", err)
	}
	if fetched.ID != created.ID || fetched.Email != created.Email {
		t.Fatalf("backup user %+v does not match original %+v", fetched, created)
	}
}

// Pragmas must hold on every pooled connection, not just the one that
// happened to serve the first Exec after Open.
func TestOpen_PragmasOnEveryPooledConn(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	// Hold several connections at once so the pool has to open new ones.
	var held []*sql.Conn
	for i := 0; i < 3; i++ {
		c, err := conn.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn %d: %v", i, err)
		}
		defer c.Close()
		held = append(held, c)
	}

	for i, c := range held {
		var busy, fk int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if busy != 5000 {
			t.Errorf("conn %d busy_timeout = %d, want 5000", i, busy)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d foreign_keys = %d, want 1", i, fk)
		}
	}
}

// A transaction that reads before it writes must wait for a concurrent
// writer instead of failing at once with SQLITE_BUSY: SQLite never runs the
// busy handler when a read transaction tries to upgrade to a write lock.
func TestOpen_ReadThenWriteTxWaitsForConcurrentWriter(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()
	repo := user.NewRepo(conn)
	ctx := context.Background()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("select: %v", err)
	}

	// Another connection writes and commits between the SELECT and the
	// INSERT. Once tx holds the write lock this writer has to wait, so give
	// it a moment and move on.
	done := make(chan error, 1)
	go func() {
		_, err := repo.Create(context.Background(), "other@example.com", "hash")
		done <- err
	}()
	select {
	case err := <-done:
		done <- err
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)",
		"tx@example.com", "hash", time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert after concurrent write: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
}

// The database and its snapshots hold password hashes and encrypted
// provider keys: Open and Backup must create every file owner-only, whatever
// the process umask.
func TestOpenAndBackup_FilesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no unix permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()
	if _, err := user.NewRepo(conn).Create(context.Background(), "carol@example.com", "hash789"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	backupPath := filepath.Join(dir, "backup.db")
	if err := db.Backup(context.Background(), conn, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	for _, p := range []string{path, path + "-wal", path + "-shm", backupPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s mode = %o, want 600", filepath.Base(p), mode)
		}
	}
}

func TestInList(t *testing.T) {
	placeholders, args := db.InList([]string{"USD", "EUR", "TRY"})
	if placeholders != "?,?,?" {
		t.Errorf("placeholders = %q, want %q", placeholders, "?,?,?")
	}
	if len(args) != 3 || args[0] != "USD" || args[2] != "TRY" {
		t.Errorf("args = %v, want [USD EUR TRY]", args)
	}
}

// TestOpen_RejectsSchemaFromNewerVersion verifies that a database holding a
// schema file this binary does not embed (a rollback, or a backup restored
// from a newer image) is refused, naming the file.
func TestOpen_RejectsSchemaFromNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := conn.Exec("INSERT INTO schema_version (filename, applied_at, checksum) VALUES ('999_future.sql', '2030-01-01T00:00:00Z', 'x')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	conn.Close()

	conn, err = db.Open(path)
	if err == nil {
		conn.Close()
		t.Fatal("expected error for unknown schema file, got nil")
	}
	if !strings.Contains(err.Error(), "999_future.sql") {
		t.Fatalf("expected error to name 999_future.sql, got: %v", err)
	}
}

func openUpsertTable(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec("CREATE TABLE upsert_t (k TEXT PRIMARY KEY, v TEXT NOT NULL)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := conn.Exec("INSERT INTO upsert_t (k, v) VALUES ('a', 'old')"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return conn
}

func TestUpsert(t *testing.T) {
	const upd = "UPDATE upsert_t SET v = ? WHERE k = ?"
	const ins = "INSERT INTO upsert_t (k, v) VALUES (?, ?)"
	tests := []struct {
		name    string
		key     string
		updSQL  string
		insSQL  string
		wantErr string
	}{
		{name: "update hits", key: "a", updSQL: upd, insSQL: ins},
		{name: "update misses", key: "b", updSQL: upd, insSQL: ins},
		{name: "update error", key: "a", updSQL: "UPDATE nope SET v = ?", insSQL: ins, wantErr: "update:"},
		{name: "insert error", key: "b", updSQL: upd, insSQL: "INSERT INTO nope VALUES (?, ?)", wantErr: "insert:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := openUpsertTable(t)
			ctx := context.Background()
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			defer tx.Rollback()

			updArgs, insArgs := []any{"new", tt.key}, []any{tt.key, "new"}
			if tt.updSQL != upd {
				updArgs = []any{"new"}
			}
			err = db.Upsert(ctx, tx, tt.updSQL, updArgs, tt.insSQL, insArgs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Upsert error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			var v string
			var n int
			if err := tx.QueryRow("SELECT v FROM upsert_t WHERE k = ?", tt.key).Scan(&v); err != nil || v != "new" {
				t.Errorf("row %q = %q, %v; want \"new\"", tt.key, v, err)
			}
			if err := tx.QueryRow("SELECT COUNT(*) FROM upsert_t WHERE k = ?", tt.key).Scan(&n); err != nil || n != 1 {
				t.Errorf("rows for %q = %d, %v; want 1", tt.key, n, err)
			}
		})
	}
}

func TestCheckAffected(t *testing.T) {
	conn := openUpsertTable(t)
	notFound := errors.New("not found")
	for _, tt := range []struct {
		key  string
		want error
	}{{"a", nil}, {"zzz", notFound}} {
		res, err := conn.Exec("UPDATE upsert_t SET v = 'x' WHERE k = ?", tt.key)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got := db.CheckAffected(res, notFound); !errors.Is(got, tt.want) {
			t.Errorf("CheckAffected for %q = %v, want %v", tt.key, got, tt.want)
		}
	}
}
