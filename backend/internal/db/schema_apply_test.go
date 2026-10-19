package db

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// TestApplySchema_FailurePartway_NoTrace verifies that a file whose second
// statement fails leaves no trace of its first statement, and is not
// recorded in schema_version, because the whole file runs in one
// transaction.
func TestApplySchema_FailurePartway_NoTrace(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	files := fstest.MapFS{
		"schema/001_ok.sql":  &fstest.MapFile{Data: []byte(`CREATE TABLE ok (id INTEGER PRIMARY KEY)`)},
		"schema/002_bad.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE partial (id INTEGER PRIMARY KEY); INSERT INTO does_not_exist VALUES (1)`)},
	}

	if err := applySchema(conn, files); err == nil {
		t.Fatal("expected error from failing statement, got nil")
	}

	var name string
	err = conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='partial'").Scan(&name)
	if err != sql.ErrNoRows {
		t.Fatalf("expected table 'partial' not to exist after rollback, got err=%v name=%q", err, name)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM schema_version WHERE filename = ?", "002_bad.sql").Scan(&count); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no schema_version row for 002_bad.sql, got %d", count)
	}

	if err := conn.QueryRow("SELECT COUNT(*) FROM schema_version WHERE filename = ?", "001_ok.sql").Scan(&count); err != nil {
		t.Fatalf("count schema_version: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 001_ok.sql to still be recorded, got %d", count)
	}
}

// TestApplySchema_ChecksumMismatch verifies that editing an already-applied
// schema file is rejected on the next apply, naming the offending file.
func TestApplySchema_ChecksumMismatch(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	original := fstest.MapFS{
		"schema/001_a.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY)`)},
	}
	if err := applySchema(conn, original); err != nil {
		t.Fatalf("first applySchema: %v", err)
	}

	changed := fstest.MapFS{
		"schema/001_a.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE a (id INTEGER PRIMARY KEY, extra TEXT)`)},
	}
	err = applySchema(conn, changed)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "001_a.sql") {
		t.Fatalf("expected error to name 001_a.sql, got: %v", err)
	}
}

// schemaBefore returns the embedded schema files that sort before prefix, so
// a test can seed a database at the version just before one file.
func schemaBefore(t *testing.T, prefix string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(schemaFS, schemaDir)
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() >= prefix {
			continue
		}
		data, err := fs.ReadFile(schemaFS, schemaDir+"/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		files[schemaDir+"/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	return files
}

// dumpRows returns every row of query as one string per row.
func dumpRows(t *testing.T, conn *sql.DB, query string) []string {
	t.Helper()
	rows, err := conn.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	var out []string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var b strings.Builder
		for _, v := range vals {
			b.WriteString(v.String)
			if !v.Valid {
				b.WriteString("NULL")
			}
			b.WriteString("|")
		}
		out = append(out, b.String())
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestSchema027KeepsPortfolioRows checks the 027 rebuild of portfolios
// with AUTOINCREMENT on a database that already has portfolios with a gap,
// and rows in every table that references a portfolio. Nothing may change
// but the next id, which must land above the highest one present.
func TestSchema027KeepsPortfolioRows(t *testing.T) {
	// db.Open's DSN, so the file runs with foreign keys on as in production.
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	if err := applySchema(conn, schemaBefore(t, "027")); err != nil {
		t.Fatalf("apply schema before 027: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'A', '2026-01-01T00:00:00Z'),
		 (2, 1, 'B', '2026-01-02T00:00:00Z'),
		 (3, 2, 'C', '2026-01-03T00:00:00Z'),
		 (4, 2, 'D', '2026-01-04T00:00:00Z')`,
		`DELETE FROM portfolios WHERE id = 3`,
		`INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at) VALUES
		 (5, 1, 2, 'midas', '2026-01-01', '2026-01-31', 'a', '2026-02-01T00:00:00Z'),
		 (6, 2, 4, 'garanti', '2026-01-01', '2026-01-31', 'b', '2026-02-01T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at, import_id) VALUES
		 (10, 1, 1, NULL, 'deposit', '2026-01-02', NULL, NULL, '100', '0', 'TRY', '2026-01-02T00:00:00Z', NULL),
		 (11, 1, 2, 1, 'buy', '2026-01-03', '2', '10', '20', '0', 'TRY', '2026-01-03T00:00:00Z', 5),
		 (12, 2, 4, 1, 'buy', '2026-01-04', '1', '11', '11', '0', 'TRY', '2026-01-04T00:00:00Z', 6)`,
		`INSERT INTO dividend_dismissals (user_id, portfolio_id, instrument_id, date) VALUES
		 (1, 2, 1, '2026-01-05'),
		 (2, 4, 1, '2026-01-06')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	queries := []string{
		"SELECT * FROM portfolios ORDER BY id",
		"SELECT * FROM transactions ORDER BY id",
		"SELECT * FROM statement_imports ORDER BY id",
		"SELECT * FROM dividend_dismissals ORDER BY portfolio_id, date",
	}
	var before [][]string
	for _, q := range queries {
		before = append(before, dumpRows(t, conn, q))
	}

	if err := applySchema(conn, schemaFS); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	// Applying again is a no-op.
	if err := applySchema(conn, schemaFS); err != nil {
		t.Fatalf("apply schema twice: %v", err)
	}

	for i, q := range queries {
		after := dumpRows(t, conn, q)
		if strings.Join(after, "\n") != strings.Join(before[i], "\n") {
			t.Errorf("%s changed:\nbefore %v\nafter  %v", q, before[i], after)
		}
	}
	if bad := dumpRows(t, conn, "PRAGMA foreign_key_check"); len(bad) != 0 {
		t.Errorf("foreign_key_check = %v, want none", bad)
	}
	if left := dumpRows(t, conn, "SELECT name FROM sqlite_master WHERE name = 'portfolios_copy'"); len(left) != 0 {
		t.Errorf("portfolios_copy left behind")
	}

	if _, err := conn.Exec("DELETE FROM portfolios WHERE id = 4"); err == nil {
		t.Fatal("deleting a referenced portfolio succeeded, want a FOREIGN KEY error")
	}
	if _, err := conn.Exec("DELETE FROM dividend_dismissals WHERE portfolio_id = 4"); err != nil {
		t.Fatalf("delete dismissals: %v", err)
	}
	if _, err := conn.Exec("DELETE FROM transactions WHERE portfolio_id = 4"); err != nil {
		t.Fatalf("delete transactions: %v", err)
	}
	if _, err := conn.Exec("DELETE FROM statement_imports WHERE portfolio_id = 4"); err != nil {
		t.Fatalf("delete imports: %v", err)
	}
	if _, err := conn.Exec("DELETE FROM portfolios WHERE id = 4"); err != nil {
		t.Fatalf("delete portfolio 4: %v", err)
	}
	res, err := conn.Exec(`INSERT INTO portfolios (user_id, name, created_at) VALUES (1, 'E', '2026-01-07T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert portfolio: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	if id != 5 {
		t.Fatalf("next portfolio id = %d, want 5 (above the deleted maximum 4)", id)
	}
}

// TestSchema028KeepsImportAndTransactionRows checks the 028 rebuild of
// statement_imports and transactions with AUTOINCREMENT. Every row and id,
// every import_id reference, every column, index and foreign key must come
// through unchanged; only the next ids change, landing above the highest.
func TestSchema028KeepsImportAndTransactionRows(t *testing.T) {
	// db.Open's DSN, so the file runs with foreign keys on as in production.
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	if err := applySchema(conn, schemaBefore(t, "028")); err != nil {
		t.Fatalf("apply schema before 028: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'A', '2026-01-01T00:00:00Z'),
		 (2, 2, 'B', '2026-01-02T00:00:00Z')`,
		`INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency) VALUES
		 (3, 1, 1, 'midas', '2026-01-01', '2026-01-31', 'a', '2026-02-01T00:00:00Z', 'TRY'),
		 (4, 1, 1, 'midas', '2026-02-01', '2026-02-28', 'b', '2026-03-01T00:00:00Z', NULL),
		 (6, 2, 2, 'garanti', '2026-01-01', '2026-01-31', 'c', '2026-02-01T00:00:00Z', 'USD')`,
		`DELETE FROM statement_imports WHERE id = 4`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id) VALUES
		 (10, 1, 1, NULL, 'deposit', '2026-01-02', NULL, NULL, '100', '0', 'TRY', NULL, '2026-01-02T00:00:00Z', NULL),
		 (11, 1, 1, 1, 'buy', '2026-01-03', '2', '10.50', '21.00', '0.02', 'TRY', 'n', '2026-01-03T00:00:00Z', 3),
		 (13, 2, 2, 1, 'buy', '2026-01-04', '1', '11', '11', '0', 'USD', '', '2026-01-04T00:00:00Z', 6)`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	queries := []string{
		"SELECT * FROM statement_imports ORDER BY id",
		"SELECT * FROM transactions ORDER BY id",
		"SELECT id, typeof(quantity), typeof(price), typeof(amount), typeof(fee), typeof(import_id) FROM transactions ORDER BY id",
		"SELECT * FROM pragma_table_info('statement_imports')",
		"SELECT * FROM pragma_table_info('transactions')",
		"SELECT * FROM pragma_foreign_key_list('statement_imports')",
		"SELECT * FROM pragma_foreign_key_list('transactions')",
		"SELECT type, name, tbl_name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name IN ('statement_imports', 'transactions') ORDER BY name",
		"SELECT * FROM pragma_index_info('sqlite_autoindex_statement_imports_1')",
	}
	var before [][]string
	for _, q := range queries {
		before = append(before, dumpRows(t, conn, q))
	}

	if err := applySchema(conn, schemaFS); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	for i, q := range queries {
		after := dumpRows(t, conn, q)
		if strings.Join(after, "\n") != strings.Join(before[i], "\n") {
			t.Errorf("%s changed:\nbefore %v\nafter  %v", q, before[i], after)
		}
	}
	if bad := dumpRows(t, conn, "PRAGMA foreign_key_check"); len(bad) != 0 {
		t.Errorf("foreign_key_check = %v, want none", bad)
	}
	if left := dumpRows(t, conn, "SELECT name FROM sqlite_master WHERE name LIKE '%_copy'"); len(left) != 0 {
		t.Errorf("copy tables left behind: %v", left)
	}

	// import_id still references statement_imports.
	if _, err := conn.Exec("DELETE FROM statement_imports WHERE id = 6"); err == nil {
		t.Fatal("deleting a referenced import succeeded, want a FOREIGN KEY error")
	}
	if _, err := conn.Exec("DELETE FROM transactions WHERE id = 13"); err != nil {
		t.Fatalf("delete transaction 13: %v", err)
	}
	if _, err := conn.Exec("DELETE FROM statement_imports WHERE id = 6"); err != nil {
		t.Fatalf("delete import 6: %v", err)
	}
	res, err := conn.Exec(`INSERT INTO statement_imports (user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at)
		VALUES (2, 2, 'garanti', '2026-01-01', '2026-01-31', 'c', '2026-02-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert import: %v", err)
	}
	if id, _ := res.LastInsertId(); id != 7 {
		t.Fatalf("next import id = %d, want 7 (above the deleted maximum 6)", id)
	}
	res, err = conn.Exec(`INSERT INTO transactions (user_id, portfolio_id, type, date, amount, fee, currency, created_at)
		VALUES (1, 1, 'deposit', '2026-01-05', '1', '0', 'TRY', '2026-01-05T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	if id, _ := res.LastInsertId(); id != 14 {
		t.Fatalf("next transaction id = %d, want 14 (above the deleted maximum 13)", id)
	}
}

// TestSchema029KeepsUserRows checks the 029 rebuild of users, the parent
// of every table that holds user data, with AUTOINCREMENT. Every row of every
// table (ids included), and users' columns, foreign keys and indexes must
// come through unchanged; only the next user id changes, landing above the
// highest.
func TestSchema029KeepsUserRows(t *testing.T) {
	// db.Open's DSN, so the file runs with foreign keys on as in production.
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	if err := applySchema(conn, schemaBefore(t, "029")); err != nil {
		t.Fatalf("apply schema before 029: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, created_at, reporting_currency, is_admin) VALUES
		 (1, 'a@example.com', 'ha', '2026-01-01T00:00:00Z', 'USD', 1),
		 (2, 'b@example.com', 'hb', '2026-01-02T00:00:00Z', 'TRY', 0),
		 (3, 'gone@example.com', 'hc', '2026-01-03T00:00:00Z', 'TRY', 0),
		 (4, 'd@example.com', 'hd', '2026-01-04T00:00:00Z', 'EUR', 0)`,
		`DELETE FROM users WHERE id = 3`,
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`,
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES
		 ('s1', 1, '2026-01-01T00:00:00Z', '2026-02-01T00:00:00Z'),
		 ('s4', 4, '2026-01-04T00:00:00Z', '2026-02-04T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES
		 (1, 1, 'A', '2026-01-01T00:00:00Z'),
		 (2, 4, 'D', '2026-01-04T00:00:00Z')`,
		`INSERT INTO statement_imports (id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency) VALUES
		 (3, 1, 1, 'midas', '2026-01-01', '2026-01-31', 'a', '2026-02-01T00:00:00Z', 'TRY'),
		 (5, 4, 2, 'garanti', '2026-01-01', '2026-01-31', 'b', '2026-02-01T00:00:00Z', NULL)`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id) VALUES
		 (10, 1, 1, NULL, 'deposit', '2026-01-02', NULL, NULL, '100', '0', 'TRY', NULL, '2026-01-02T00:00:00Z', NULL),
		 (11, 4, 2, 1, 'buy', '2026-01-03', '2', '10.50', '21.00', '0.02', 'TRY', 'n', '2026-01-03T00:00:00Z', 5)`,
		`INSERT INTO manual_prices (user_id, instrument_id, date, close) VALUES
		 (1, 1, '2026-01-05', '10.10'),
		 (4, 1, '2026-01-05', '10.20')`,
		`INSERT INTO dividend_dismissals (user_id, portfolio_id, instrument_id, date) VALUES
		 (1, 1, 1, '2026-01-06'),
		 (4, 2, 1, '2026-01-06')`,
		`INSERT INTO import_aliases (id, user_id, broker, symbol, instrument_id, created_at, learned, period_end) VALUES
		 (7, 1, 'midas', 'THY', 1, '2026-01-07T00:00:00Z', 1, '2026-01-31'),
		 (9, 4, 'garanti', 'THY', 1, '2026-01-07T00:00:00Z', 0, NULL)`,
		`INSERT INTO import_forgotten_names (id, user_id, broker, symbol, period_end) VALUES
		 (2, 1, 'midas', 'OLD', '2026-01-31'),
		 (6, 4, 'garanti', 'OLD', '2026-01-31')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Every table that references users must be seeded, so a new one fails
	// here until it is added above.
	for _, child := range dumpRows(t, conn, `SELECT m.name FROM sqlite_master m, pragma_foreign_key_list(m.name) f
		WHERE m.type = 'table' AND f."table" = 'users' ORDER BY m.name`) {
		child = strings.TrimSuffix(child, "|")
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM "` + child + `"`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", child, err)
		}
		if n == 0 {
			t.Errorf("%s references users but has no seeded row", child)
		}
	}

	// Every row of every table, plus the shape of users.
	queries := []string{
		"SELECT * FROM pragma_table_info('users')",
		"SELECT * FROM pragma_foreign_key_list('users')",
		"SELECT * FROM pragma_index_list('users')",
		"SELECT * FROM pragma_index_info('sqlite_autoindex_users_1')",
		"SELECT type, name, tbl_name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'users' ORDER BY name",
		"SELECT m.name, f.* FROM sqlite_master m, pragma_foreign_key_list(m.name) f WHERE m.type = 'table' ORDER BY m.name, f.id, f.seq",
	}
	for _, table := range dumpRows(t, conn, `SELECT name FROM sqlite_master WHERE type = 'table'
		AND name NOT IN ('schema_version', 'sqlite_sequence') ORDER BY name`) {
		queries = append(queries, `SELECT * FROM "`+strings.TrimSuffix(table, "|")+`"`)
	}
	// Rows are sorted in Go, so a dump does not depend on a table's column
	// count or on the order SQLite happens to return.
	sorted := func(q string) []string {
		rows := dumpRows(t, conn, q)
		sort.Strings(rows)
		return rows
	}
	var before [][]string
	for _, q := range queries {
		before = append(before, sorted(q))
	}

	// Through 029 only: later files add columns to users.
	if err := applySchema(conn, schemaBefore(t, "030")); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	for i, q := range queries {
		after := sorted(q)
		if strings.Join(after, "\n") != strings.Join(before[i], "\n") {
			t.Errorf("%s changed:\nbefore %v\nafter  %v", q, before[i], after)
		}
	}
	if bad := dumpRows(t, conn, "PRAGMA foreign_key_check"); len(bad) != 0 {
		t.Errorf("foreign_key_check = %v, want none", bad)
	}
	if left := dumpRows(t, conn, "SELECT name FROM sqlite_master WHERE name LIKE '%_copy'"); len(left) != 0 {
		t.Errorf("copy tables left behind: %v", left)
	}

	// The references from child tables still hold.
	if _, err := conn.Exec("DELETE FROM users WHERE id = 4"); err == nil {
		t.Fatal("deleting a referenced user succeeded, want a FOREIGN KEY error")
	}
	for _, q := range []string{
		"DELETE FROM import_forgotten_names WHERE user_id = 4",
		"DELETE FROM import_aliases WHERE user_id = 4",
		"DELETE FROM dividend_dismissals WHERE user_id = 4",
		"DELETE FROM manual_prices WHERE user_id = 4",
		"DELETE FROM transactions WHERE user_id = 4",
		"DELETE FROM statement_imports WHERE user_id = 4",
		"DELETE FROM portfolios WHERE user_id = 4",
		"DELETE FROM sessions WHERE user_id = 4",
		"DELETE FROM users WHERE id = 4",
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	res, err := conn.Exec(`INSERT INTO users (email, password_hash, created_at) VALUES ('e@example.com', 'he', '2026-01-08T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if id, _ := res.LastInsertId(); id != 5 {
		t.Fatalf("next user id = %d, want 5 (above the deleted maximum 4)", id)
	}
	var currency string
	var isAdmin int
	if err := conn.QueryRow("SELECT reporting_currency, is_admin FROM users WHERE id = 5").Scan(&currency, &isAdmin); err != nil {
		t.Fatalf("read new user: %v", err)
	}
	if currency != "TRY" || isAdmin != 0 {
		t.Errorf("new user defaults = %q, %d; want TRY, 0", currency, isAdmin)
	}
}

// TestSchema024ConvertsBulletinDates seeds the day.month.year price dates
// the 2020 BIST bulletins left behind, then applies 024: a row
// with no ISO twin is converted, a row with one is dropped.
func TestSchema024ConvertsBulletinDates(t *testing.T) {
	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	before := fstest.MapFS{}
	entries, err := fs.ReadDir(schemaFS, schemaDir)
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() >= "024" {
			continue
		}
		data, err := fs.ReadFile(schemaFS, schemaDir+"/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		before[schemaDir+"/"+e.Name()] = &fstest.MapFile{Data: data}
	}
	if err := applySchema(conn, before); err != nil {
		t.Fatalf("apply schema before 024: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO prices (instrument_id, date, close, source) VALUES
		(1, '1.06.2020', '10.00', 'bist_bulletin'),
		(1, '21.05.2020', '9.00', 'bist_bulletin'),
		(1, '2020-05-21', '9.50', 'bist_bulletin')`); err != nil {
		t.Fatalf("seed prices: %v", err)
	}

	if err := applySchema(conn, schemaFS); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	rows, err := conn.Query("SELECT date, close FROM prices ORDER BY date")
	if err != nil {
		t.Fatalf("query prices: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var date, close string
		if err := rows.Scan(&date, &close); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, date+" "+close)
	}
	want := []string{"2020-05-21 9.50", "2020-06-01 10.00"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("prices = %v, want %v", got, want)
	}
}

// TestSchemaFiles_UniquePrefixes fails when two embedded schema files share a
// numeric prefix. Git does not conflict when the slugs differ, so this test is
// the signal ADR 0002 relies on.
func TestSchemaFiles_UniquePrefixes(t *testing.T) {
	// Both the 032 and the 038 files are already recorded by name and checksum
	// in deployed databases and cannot be renamed. No new prefix gets an
	// exception.
	allowed := map[string]int{"032": 2, "038": 2}

	entries, err := fs.ReadDir(schemaFS, schemaDir)
	if err != nil {
		t.Fatalf("read schema dir: %v", err)
	}
	byPrefix := map[string][]string{}
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			t.Fatalf("schema file %q has no NNN_ prefix", e.Name())
		}
		byPrefix[prefix] = append(byPrefix[prefix], e.Name())
	}
	for prefix, names := range byPrefix {
		limit := 1
		if n, ok := allowed[prefix]; ok {
			limit = n
		}
		if len(names) > limit {
			t.Errorf("prefix %s is used by %d schema files %v, renumber the newer one", prefix, len(names), names)
		}
	}
}
