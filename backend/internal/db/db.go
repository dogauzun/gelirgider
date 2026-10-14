// Package db opens the SQLite database and applies schema files on startup.
// This is the only package allowed to import the sqlite driver.
package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// Open opens (creating if needed) the SQLite database at path, enables
// foreign keys and WAL mode, and applies any schema files not yet recorded
// in schema_version. Safe to call repeatedly on the same path.
func Open(path string) (*sql.DB, error) {
	// The file holds password hashes and encrypted provider keys. SQLite
	// creates it 0644 under the usual umask, and gives the -wal and -shm
	// files the main file's mode, so creating the file owner-only here (and
	// tightening one left 0644 by an older version) covers all three
	// (GLG-1128).
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create db file: %w", err)
	}
	err = f.Chmod(0o600)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("chmod db file: %w", err)
	}

	// Every pragma goes in the DSN, not a PRAGMA statement: database/sql
	// pools connections and a PRAGMA only configures the one connection it
	// ran on. foreign_keys is per-connection, so as a statement it left
	// constraints unenforced on every other pooled connection; busy_timeout
	// likewise let a later connection fail a contended write immediately
	// with SQLITE_BUSY.
	//
	// _txlock=immediate makes BeginTx take the write lock up front, where
	// busy_timeout applies. A deferred transaction that reads first has to
	// upgrade to a write lock later, and SQLite fails that upgrade at once
	// with SQLITE_BUSY instead of waiting when another connection writes.
	// Transactions begun with ReadOnly stay deferred.
	//
	// mmap_size maps up to 256 MiB of the file instead of reading each page
	// with a syscall into a 2 MB per-connection cache: the history sweep's
	// price range is larger than that cache, and its page reads were half
	// of the sweep's time (GLG-1363).
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=mmap_size(268435456)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := applySchema(db, schemaFS, "schema"); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}

// InList returns the "?,?,?" placeholder list for an IN clause over vals,
// plus vals as the matching args. vals must not be empty: "IN ()" is a
// syntax error, so callers return early on an empty list.
func InList[T any](vals []T) (placeholders string, args []any) {
	args = make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(vals)), ","), args
}

// IsForeignKeyViolation reports whether err is a foreign key failure, which
// an insert hits when its parent row (a portfolio, a user) was deleted after
// the caller checked it: the delete commits between the check and the
// insert (GLG-1002). Callers map it to their own not-found error. Matched on
// the message, like the unique checks (GLG-369), so it survives a datastore
// change: "FOREIGN KEY constraint failed" on SQLite, "violates foreign key
// constraint" on PostgreSQL.
func IsForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "foreign key constraint")
}

// Backup writes a consistent snapshot of conn's database to path using
// VACUUM INTO. It fails if path already exists: VACUUM INTO refuses to
// overwrite, so callers must pick a unique target name rather than deleting
// it first. The snapshot is created owner-only, like the database itself.
func Backup(ctx context.Context, conn *sql.DB, path string) error {
	// VACUUM INTO accepts an empty target and keeps its mode; O_EXCL keeps
	// the refusal to overwrite.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	f.Close()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		os.Remove(path)
		return fmt.Errorf("vacuum into %s: %w", path, err)
	}
	return nil
}

// applySchema applies every .sql file found in dir (within schemaFiles), in
// sorted order, recording each in schema_version along with its SHA-256
// checksum. A file already recorded with a different checksum than the one
// on disk is a hard error: schema files must never be edited after merge.
func applySchema(db *sql.DB, schemaFiles fs.FS, dir string) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		filename TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL,
		checksum TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	if err := ensureChecksumColumn(ctx, db); err != nil {
		return err
	}

	entries, err := fs.ReadDir(schemaFiles, dir)
	if err != nil {
		return fmt.Errorf("read schema dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		content, err := fs.ReadFile(schemaFiles, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		sum := checksum(content)

		var recorded string
		var found bool
		switch err := db.QueryRowContext(ctx, "SELECT checksum FROM schema_version WHERE filename = ?", name).Scan(&recorded); err {
		case nil:
			found = true
		case sql.ErrNoRows:
			found = false
		default:
			return fmt.Errorf("check %s: %w", name, err)
		}

		if found {
			if recorded == "" {
				// Backfill: this row predates the checksum column.
				if _, err := db.ExecContext(ctx, "UPDATE schema_version SET checksum = ? WHERE filename = ?", sum, name); err != nil {
					return fmt.Errorf("backfill checksum for %s: %w", name, err)
				}
				continue
			}
			if recorded != sum {
				return fmt.Errorf("schema file %s has changed since it was applied", name)
			}
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version (filename, applied_at, checksum) VALUES (?, ?, ?)", name, time.Now().UTC().Format(time.RFC3339), sum); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}

	return nil
}

// ensureChecksumColumn adds schema_version.checksum for databases created
// before this column existed. No-op once the column is present.
func ensureChecksumColumn(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(schema_version)")
	if err != nil {
		return fmt.Errorf("inspect schema_version: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("inspect schema_version: %w", err)
		}
		if name == "checksum" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect schema_version: %w", err)
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE schema_version ADD COLUMN checksum TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add checksum column: %w", err)
	}
	return nil
}

func checksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
