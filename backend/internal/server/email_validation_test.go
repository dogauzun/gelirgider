package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestRegisterRejectsMalformedEmail checks that the API, not just the
// browser's <input type="email">, rejects addresses that cannot work as a
// login identifier (including one with an invisible zero-width space, which
// would pass for another address, GLG-900), and that an address already in the database still logs
// in even when it would fail that check.
func TestRegisterRejectsMalformedEmail(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	seedAdmin(t, conn, "setup-admin@example.com", "password123")
	openRegistration(t, conn)

	srv := httptest.NewServer(New(logger, conn, false, settings.NewRepo(conn, nil), nil))
	defer srv.Close()
	client := srv.Client()

	const password = "correct-horse"

	for _, email := range []string{"a@", "@b", "a@b", "a b@c.com", "x@y@z", "zw\u200b@example.com"} {
		resp := postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{
			"email":    email,
			"password": password,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("register %q status = %d, want %d", email, resp.StatusCode, http.StatusBadRequest)
			continue
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode register error for %q: %v", email, err)
		}
		if body.Error != "invalid email" {
			t.Errorf("register %q error = %q, want %q", email, body.Error, "invalid email")
		}
	}

	if resp := postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{
		"email":    "alice@example.com",
		"password": password,
	}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register valid email status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	// A row created before this validation existed must keep logging in.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	if _, err := user.NewRepo(conn).Create(context.Background(), "a@", string(hash)); err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", map[string]string{
		"email":    "a@",
		"password": password,
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("login with pre-existing malformed email status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestRegisterEmailTooLong checks the RFC 5321 cap: a 254-character address
// registers, a 255-character one gets a 400 with a validation message.
func TestRegisterEmailTooLong(t *testing.T) {
	srv := newTestServer(t)
	client := srv.Client()

	const domain = "@qa.test"
	tooLong := strings.Repeat("a", 255-len(domain)) + domain
	resp := postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{"email": tooLong, "password": "correct-horse"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("register 255-character email status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode register error: %v", err)
	}
	if want := "email must be at most 254 characters"; body.Error != want {
		t.Fatalf("register error = %q, want %q", body.Error, want)
	}

	atLimit := strings.Repeat("a", 254-len(domain)) + domain
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{"email": atLimit, "password": "correct-horse"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register 254-character email status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}
