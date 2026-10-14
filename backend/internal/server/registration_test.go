package server

import (
	"bytes"
	"encoding/json"
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

// TestRegistrationToggle: registration starts closed (register answers 403,
// the public config says closed); an admin opens and closes it, and
// reopening lets register through.
func TestRegistrationToggle(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	admin := &http.Client{Jar: jar}
	creds := map[string]string{"email": "admin@example.com", "password": "correct-horse"}
	seedAdmin(t, conn, creds["email"], creds["password"])
	if resp := postJSON(t, admin, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}

	setOpen := func(open bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]bool{"open": open})
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/admin/registration", bytes.NewReader(body))
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatalf("PUT registration: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("PUT registration status = %d, want 204", resp.StatusCode)
		}
	}
	configOpen := func() bool {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/auth/config")
		if err != nil {
			t.Fatalf("GET config: %v", err)
		}
		defer resp.Body.Close()
		var cfg struct {
			RegistrationOpen bool `json:"registration_open"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return cfg.RegistrationOpen
	}

	registerClosed := func(when string) {
		t.Helper()
		// An invalid body still gets 403: the toggle is checked before validation.
		resp := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/register", map[string]string{"email": "bad"})
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode != http.StatusForbidden || body["error"] != "registration closed" {
			t.Fatalf("register %s = %d %v, want 403 registration closed", when, resp.StatusCode, body)
		}
	}

	// Unset means closed (GLG-1117).
	if configOpen() {
		t.Fatal("registration open by default, want closed")
	}
	registerClosed("by default")

	setOpen(true)
	if !configOpen() {
		t.Fatal("config says closed after opening")
	}
	setOpen(false)
	if configOpen() {
		t.Fatal("config says open after closing")
	}
	registerClosed("while closed")

	setOpen(true)
	bob := map[string]string{"email": "bob@example.com", "password": "correct-horse"}
	if resp := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/register", bob); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register after reopening status = %d, want 201", resp.StatusCode)
	}

	// A non-admin cannot flip it.
	jar2, _ := cookiejar.New(nil)
	bobClient := &http.Client{Jar: jar2}
	postJSON(t, bobClient, srv.URL+"/api/auth/login", bob)
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/admin/registration", bytes.NewReader([]byte(`{"open":false}`)))
	resp, err := bobClient.Do(req)
	if err != nil {
		t.Fatalf("non-admin PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin PUT status = %d, want 403", resp.StatusCode)
	}
}
