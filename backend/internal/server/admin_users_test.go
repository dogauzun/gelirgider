package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestAdminUsers covers the user management API: a non-admin gets 403; an
// admin creates a user, resets their password (their old session is signed
// out, the new password logs in), and cannot demote the last admin.
func TestAdminUsers(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	newClient := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New: %v", err)
		}
		return &http.Client{Jar: jar}
	}
	do := func(client *http.Client, method, path string, body any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	login := func(client *http.Client, email, password string) int {
		return do(client, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}).StatusCode
	}

	admin, bob := newClient(), newClient()
	adminID := seedAdmin(t, conn, "admin@example.com", "password1")
	openRegistration(t, conn)
	if resp := do(bob, "POST", "/api/auth/register", map[string]string{"email": "bob@example.com", "password": "password1"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register bob: status %d", resp.StatusCode)
	}
	login(admin, "admin@example.com", "password1")
	login(bob, "bob@example.com", "password1")

	if got := do(bob, "GET", "/api/admin/users", nil).StatusCode; got != http.StatusForbidden {
		t.Errorf("non-admin list: status %d, want 403", got)
	}

	resp := do(admin, "POST", "/api/admin/users", map[string]string{"email": "carol@example.com", "password": "password1", "locale": "en"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d, want 201", resp.StatusCode)
	}
	var carol struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&carol)

	carolClient := newClient()
	if got := login(carolClient, "carol@example.com", "password1"); got != http.StatusOK {
		t.Fatalf("carol login: status %d", got)
	}
	if got := do(admin, "POST", fmt.Sprintf("/api/admin/users/%d/password", carol.ID), map[string]string{"password": "password2"}).StatusCode; got != http.StatusNoContent {
		t.Fatalf("reset password: status %d, want 204", got)
	}
	if got := do(carolClient, "GET", "/api/me", nil).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("carol old session after reset: status %d, want 401", got)
	}
	if got := login(newClient(), "carol@example.com", "password2"); got != http.StatusOK {
		t.Errorf("carol login with new password: status %d, want 200", got)
	}

	resp = do(admin, "PUT", fmt.Sprintf("/api/admin/users/%d/admin", adminID), map[string]bool{"is_admin": false})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("demote last admin: status %d, want 409", resp.StatusCode)
	}
	// With a second admin, demoting yourself is allowed.
	if got := do(admin, "PUT", fmt.Sprintf("/api/admin/users/%d/admin", carol.ID), map[string]bool{"is_admin": true}).StatusCode; got != http.StatusNoContent {
		t.Fatalf("promote carol: status %d, want 204", got)
	}
	if got := do(admin, "PUT", fmt.Sprintf("/api/admin/users/%d/admin", adminID), map[string]bool{"is_admin": false}).StatusCode; got != http.StatusNoContent {
		t.Errorf("demote self with another admin: status %d, want 204", got)
	}
	carolAdmin := newClient()
	login(carolAdmin, "carol@example.com", "password2")
	if got := do(carolAdmin, "PUT", "/api/admin/users/9999/admin", map[string]bool{"is_admin": true}).StatusCode; got != http.StatusNotFound {
		t.Errorf("unknown id: status %d, want 404", got)
	}
}

// TestAdminDeleteUser deletes bob through the API and checks, for every
// table with a user_id column found in the schema, that bob has no rows left
// and carol's rows are intact, and that every other table (market data,
// settings) keeps its row count. A new user_id table that is not seeded
// below, or not deleted by user.Repo.Delete, fails this test.
func TestAdminDeleteUser(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	do := func(client *http.Client, method, path string, body any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	clients := map[string]*http.Client{}
	ids := map[string]int64{}
	seedAdmin(t, conn, "admin@example.com", "password1")
	openRegistration(t, conn) // also the settings row that must survive the delete
	users := user.NewRepo(conn)
	for _, name := range []string{"admin", "bob", "carol"} {
		jar, _ := cookiejar.New(nil)
		clients[name] = &http.Client{Jar: jar}
		email := name + "@example.com"
		// Register seeds a portfolio; login makes a session.
		creds := map[string]string{"email": email, "password": "password1"}
		if name != "admin" {
			if got := do(clients[name], "POST", "/api/auth/register", creds); got != http.StatusCreated {
				t.Fatalf("register %s: status %d", name, got)
			}
		}
		if got := do(clients[name], "POST", "/api/auth/login", creds); got != http.StatusOK {
			t.Fatalf("login %s: status %d", name, got)
		}
		u, err := users.GetByEmail(context.Background(), email)
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		ids[name] = u.ID
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// Global rows that must survive the delete.
	exec("INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'THYAO', 'BIST', 'THY', 'TRY')")
	exec("INSERT INTO prices (instrument_id, date, close, source) VALUES (1, '2026-01-02', '300', 'test')")
	exec("INSERT INTO fx_rates (currency, date, rate, source) VALUES ('USD', '2026-01-02', '35', 'tcmb')")
	exec("INSERT INTO splits (instrument_id, date, ratio, source) VALUES (1, '2026-01-03', '2', 'manual')")
	exec("INSERT INTO dividends (instrument_id, date, amount, currency, source) VALUES (1, '2026-01-04', '1', 'TRY', 'manual')")
	for _, name := range []string{"bob", "carol"} {
		uid := ids[name]
		var pid int64
		if err := conn.QueryRow("SELECT id FROM portfolios WHERE user_id = ?", uid).Scan(&pid); err != nil {
			t.Fatalf("portfolio of %s: %v", name, err)
		}
		res, err := conn.Exec(`INSERT INTO statement_imports (user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at)
			VALUES (?, ?, 'midas', '2026-01-01', '2026-01-31', ?, '2026-02-01T00:00:00Z')`, uid, pid, name)
		if err != nil {
			t.Fatalf("insert import: %v", err)
		}
		importID, _ := res.LastInsertId()
		exec(`INSERT INTO transactions (user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at, import_id)
			VALUES (?, ?, 1, 'buy', '2026-01-02', '10', '300', '3000', '0', 'TRY', '2026-01-02T00:00:00Z', ?)`, uid, pid, importID)
		exec(`INSERT INTO transactions (user_id, portfolio_id, type, date, amount, fee, currency, created_at)
			VALUES (?, ?, 'deposit', '2026-01-01', '5000', '0', 'TRY', '2026-01-01T00:00:00Z')`, uid, pid)
		exec("INSERT INTO manual_prices (user_id, instrument_id, date, close) VALUES (?, 1, '2026-01-05', '310')", uid)
		exec("INSERT INTO dividend_dismissals (user_id, portfolio_id, instrument_id, date) VALUES (?, ?, 1, '2026-01-04')", uid, pid)
		exec("INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at) VALUES (?, 'garanti', 'FUND', 1, '2026-02-01T00:00:00Z')", uid)
		exec("INSERT INTO import_forgotten_names (user_id, broker, symbol, period_end) VALUES (?, 'garanti', 'FUND', '2026-01-31')", uid)
		exec("INSERT INTO login_challenges (token_hash, user_id, created_at, expires_at) VALUES (?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:05:00Z')", name, uid)
		exec("INSERT INTO totp_recovery_codes (user_id, code_hash) VALUES (?, ?)", uid, name)
		// A deposit and its DEPOSIT instrument: both go with the user.
		exec("INSERT INTO instruments (id, symbol, market, name, currency) VALUES (?, ?, 'DEPOSIT', 'Deposit', 'TRY')", 100+uid, name)
		exec(`INSERT INTO term_deposits (user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate, start_date, maturity_date, created_at)
			VALUES (?, ?, ?, 'TRY', '1000', '0.45', '0.15', '2026-01-01', '2026-02-01', '2026-01-01T00:00:00Z')`, uid, pid, 100+uid)
	}

	// Discover tables from the schema, split by whether they have user_id.
	var tables, userTables, otherTables []string
	rows, err := conn.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	for _, table := range tables {
		cols, err := conn.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatalf("table_info %s: %v", table, err)
		}
		hasUserID := false
		for cols.Next() {
			var col string
			cols.Scan(&col)
			hasUserID = hasUserID || col == "user_id"
		}
		cols.Close()
		switch {
		case hasUserID:
			userTables = append(userTables, table)
		case table != "users":
			otherTables = append(otherTables, table)
		}
	}
	if len(userTables) < 6 {
		t.Fatalf("user_id tables = %v, want at least the 6 known ones", userTables)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	carolBefore := map[string]int{}
	for _, table := range userTables {
		for _, name := range []string{"bob", "carol"} {
			if count("SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", ids[name]) == 0 {
				t.Fatalf("%s has no rows for %s: seed it in this test", table, name)
			}
		}
		carolBefore[table] = count("SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", ids["carol"])
	}
	otherBefore := map[string]int{}
	for _, table := range otherTables {
		otherBefore[table] = count("SELECT COUNT(*) FROM " + table)
	}

	bobPath := fmt.Sprintf("/api/admin/users/%d", ids["bob"])
	if got := do(clients["carol"], "DELETE", bobPath, nil); got != http.StatusForbidden {
		t.Errorf("non-admin delete: status %d, want 403", got)
	}
	if got := do(clients["admin"], "DELETE", fmt.Sprintf("/api/admin/users/%d", ids["admin"]), nil); got != http.StatusConflict {
		t.Errorf("delete self: status %d, want 409", got)
	}
	if got := do(clients["admin"], "DELETE", "/api/admin/users/9999", nil); got != http.StatusNotFound {
		t.Errorf("delete unknown: status %d, want 404", got)
	}
	if err := users.Delete(context.Background(), ids["admin"]); err != user.ErrLastAdmin {
		t.Errorf("delete last admin: err %v, want ErrLastAdmin", err)
	}
	if got := do(clients["admin"], "DELETE", bobPath, nil); got != http.StatusNoContent {
		t.Fatalf("delete bob: status %d, want 204", got)
	}
	if got := do(clients["admin"], "DELETE", bobPath, nil); got != http.StatusNotFound {
		t.Errorf("delete bob again: status %d, want 404", got)
	}
	if got := do(clients["bob"], "GET", "/api/me", nil); got != http.StatusUnauthorized {
		t.Errorf("bob session after delete: status %d, want 401", got)
	}

	if n := count("SELECT COUNT(*) FROM users WHERE id = ?", ids["bob"]); n != 0 {
		t.Errorf("users row of bob still there")
	}
	if n := count("SELECT COUNT(*) FROM users WHERE id IN (?, ?)", ids["admin"], ids["carol"]); n != 2 {
		t.Errorf("admin and carol users rows = %d, want 2", n)
	}
	for _, table := range userTables {
		if n := count("SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", ids["bob"]); n != 0 {
			t.Errorf("%s: %d rows left for bob, want 0", table, n)
		}
		if n := count("SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", ids["carol"]); n != carolBefore[table] {
			t.Errorf("%s: %d rows for carol, want %d", table, n, carolBefore[table])
		}
	}
	if n := count("SELECT COUNT(*) FROM instruments WHERE id = ?", 100+ids["bob"]); n != 0 {
		t.Errorf("bob's DEPOSIT instrument still there")
	}
	if n := count("SELECT COUNT(*) FROM instruments WHERE id = ?", 100+ids["carol"]); n != 1 {
		t.Errorf("carol's DEPOSIT instrument rows = %d, want 1", n)
	}
	otherBefore["instruments"]-- // bob's DEPOSIT instrument went with him
	for _, table := range otherTables {
		if n := count("SELECT COUNT(*) FROM " + table); n != otherBefore[table] {
			t.Errorf("%s: %d rows, want %d untouched", table, n, otherBefore[table])
		}
	}
}
