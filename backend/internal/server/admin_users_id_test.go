package server

import (
	"bytes"
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
)

// TestDeletedUserIDNotReused covers GLG-1013: SQLite handed a deleted user's
// id to the next user, so a stale admin tab's "make admin", "reset password"
// or "delete" hit the newer account. The new id must be above the deleted
// one, and every stale action must answer 404 and leave the new user as it
// was.
func TestDeletedUserIDNotReused(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	admin := &http.Client{Jar: jar}
	do := func(method, path string, body any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	create := func(email string) int64 {
		t.Helper()
		resp := do("POST", "/api/admin/users", map[string]string{"email": email, "password": "Passw0rd!x"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status %d", email, resp.StatusCode)
		}
		var u struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&u)
		return u.ID
	}

	seedAdmin(t, conn, "admin@example.com", "password1")
	if got := do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "password1"}).StatusCode; got != http.StatusOK {
		t.Fatalf("login: status %d", got)
	}

	oldID := create("old@example.com")
	if got := do("DELETE", fmt.Sprintf("/api/admin/users/%d", oldID), nil).StatusCode; got != http.StatusNoContent {
		t.Fatalf("delete old: status %d, want 204", got)
	}
	newID := create("new@example.com")
	if newID <= oldID {
		t.Fatalf("new user id = %d, want above deleted id %d", newID, oldID)
	}

	for _, stale := range []struct {
		method, path string
		body         any
	}{
		{"PUT", fmt.Sprintf("/api/admin/users/%d/admin", oldID), map[string]bool{"is_admin": true}},
		{"POST", fmt.Sprintf("/api/admin/users/%d/password", oldID), map[string]string{"password": "Other0ne!x"}},
		{"DELETE", fmt.Sprintf("/api/admin/users/%d", oldID), nil},
	} {
		if got := do(stale.method, stale.path, stale.body).StatusCode; got != http.StatusNotFound {
			t.Errorf("stale %s %s: status %d, want 404", stale.method, stale.path, got)
		}
	}

	var isAdmin int
	if err := conn.QueryRow("SELECT is_admin FROM users WHERE id = ? AND email = 'new@example.com'", newID).Scan(&isAdmin); err != nil {
		t.Fatalf("new user after stale actions: %v", err)
	}
	if isAdmin != 0 {
		t.Errorf("new user is_admin = %d, want 0", isAdmin)
	}
	jar2, _ := cookiejar.New(nil)
	b, _ := json.Marshal(map[string]string{"email": "new@example.com", "password": "Passw0rd!x"})
	resp, err := (&http.Client{Jar: jar2}).Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("login new: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("new user login with own password: status %d, want 200", resp.StatusCode)
	}
}
