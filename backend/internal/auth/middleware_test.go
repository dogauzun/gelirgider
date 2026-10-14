package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestRequireAdmin: a non-admin gets 403, an admin gets 200, and a demotion
// applies to the very next request on the same session.
func TestRequireAdmin(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	users := user.NewRepo(conn)
	sessions := NewRepo(conn)
	u, err := users.Create(context.Background(), "ada@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	s, err := sessions.CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := sessions.RequireUser(RequireAdmin(users, ok))
	get := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/x", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: s.ID})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := get(); got != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403", got)
	}
	if err := users.SetAdmin(context.Background(), u.ID, true); err != nil {
		t.Fatalf("SetAdmin(true): %v", err)
	}
	if got := get(); got != http.StatusOK {
		t.Errorf("admin: status = %d, want 200", got)
	}
	if err := users.SetAdmin(context.Background(), u.ID, false); err != nil {
		t.Fatalf("SetAdmin(false): %v", err)
	}
	if got := get(); got != http.StatusForbidden {
		t.Errorf("demoted: status = %d, want 403", got)
	}
}
