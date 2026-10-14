package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestLoginUnknownEmailComparesDummyHash: an unknown email still goes through
// a bcrypt compare, so it is not answered faster than a wrong password.
func TestLoginUnknownEmailComparesDummyHash(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	calls := 0
	orig := dummyHash
	t.Cleanup(func() { dummyHash = orig })
	dummyHash = func() []byte {
		calls++
		hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
		}
		return hash
	}

	users := user.NewRepo(conn)
	admin, err := users.Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.SetAdmin(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	h := &handler{users: users, sessions: NewRepo(conn)}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"nobody@example.com","password":"whatever-pass"}`))
	rec := httptest.NewRecorder()
	h.login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if calls != 1 {
		t.Errorf("dummy hash compared %d times, want 1", calls)
	}
}
