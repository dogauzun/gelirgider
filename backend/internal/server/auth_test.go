package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	seedAdmin(t, conn, "setup-admin@example.com", "password123")
	openRegistration(t, conn)

	srv := httptest.NewServer(New(logger, conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)
	return srv
}

// openRegistration turns the sign-up toggle on: an unset toggle means closed
// (GLG-1117).
func openRegistration(t *testing.T, conn *sql.DB) {
	t.Helper()
	if err := settings.NewRepo(conn, nil).Set(context.Background(), "auth.registration_open", "true"); err != nil {
		t.Fatalf("open registration: %v", err)
	}
}

// seedAdmin creates an admin who logs in with password and returns its id.
// With an admin in place first-time setup counts as done, so login is open;
// register also needs openRegistration.
func seedAdmin(t *testing.T, conn *sql.DB, email, password string) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	users := user.NewRepo(conn)
	u, err := users.Create(context.Background(), email, string(hash))
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.SetAdmin(context.Background(), u.ID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	return u.ID
}

func postJSON(t *testing.T, client *http.Client, rawURL string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := client.Post(rawURL, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestAuthFlow covers register -> login -> logout end to end, and confirms
// login rejects a wrong password.
func TestAuthFlow(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	creds := map[string]string{"email": " Alice@Example.com ", "password": "correct-horse"}

	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	wrong := map[string]string{"email": creds["email"], "password": "wrong-password"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with wrong password status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	loginResp := postJSON(t, client, srv.URL+"/api/auth/login", creds)
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	var hasSessionCookie bool
	for _, c := range jar.Cookies(u) {
		if c.Name == "session" {
			hasSessionCookie = true
		}
	}
	if !hasSessionCookie {
		t.Fatalf("expected session cookie to be set after login")
	}

	logoutResp := postJSON(t, client, srv.URL+"/api/auth/logout", nil)
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutResp.StatusCode, http.StatusNoContent)
	}

	// Logout again with no session left must still succeed.
	logoutAgain := postJSON(t, client, srv.URL+"/api/auth/logout", nil)
	if logoutAgain.StatusCode != http.StatusNoContent {
		t.Fatalf("second logout status = %d, want %d", logoutAgain.StatusCode, http.StatusNoContent)
	}
}
