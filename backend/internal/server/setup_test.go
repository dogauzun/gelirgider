package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestSetupFlow covers first-time setup: the token is logged at startup,
// register and login answer 403 setup_required until setup is done, a
// wrong token or a wrong password for an existing email gets 403, the right
// token creates an admin with a session, register then answers registration
// closed, and a second setup call gets 409.
func TestSetupFlow(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(logger, conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	var token string
	sc := bufio.NewScanner(&logs)
	for sc.Scan() {
		var line struct {
			Level      string `json:"level"`
			SetupToken string `json:"setup_token"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.SetupToken != "" && line.Level == "WARN" {
			token = line.SetupToken
		}
	}
	if len(token) != 64 {
		t.Fatalf("setup token %q not logged at WARN as 64 hex chars", token)
	}

	needsSetup := func() bool {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/setup/status")
		if err != nil {
			t.Fatalf("GET status: %v", err)
		}
		defer resp.Body.Close()
		var got struct {
			NeedsSetup bool `json:"needs_setup"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return got.NeedsSetup
	}
	if !needsSetup() {
		t.Fatal("needs_setup = false before setup")
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	admin := map[string]string{"token": token, "email": "admin@example.com", "password": "password123"}

	if resp := postJSON(t, client, srv.URL+"/api/setup/admin", map[string]string{"token": "wrong", "email": "admin@example.com", "password": "password123"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token status = %d, want 403", resp.StatusCode)
	}

	bob := map[string]string{"email": "bob@example.com", "password": "password123"}
	setupRequired := func(path string) {
		t.Helper()
		resp := postJSON(t, http.DefaultClient, srv.URL+path, bob)
		var body map[string]string
		json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode != http.StatusForbidden || body["error"] != "setup_required" {
			t.Fatalf("%s before setup = %d %v, want 403 setup_required", path, resp.StatusCode, body)
		}
	}
	setupRequired("/api/auth/register")
	// A user left over on a dev DB cannot log in before setup either; setup
	// promotes it only with its own password.
	hash, _ := bcrypt.GenerateFromPassword([]byte(bob["password"]), bcrypt.MinCost)
	if _, err := user.NewRepo(conn).Create(context.Background(), bob["email"], string(hash)); err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	setupRequired("/api/auth/login")
	if resp := postJSON(t, client, srv.URL+"/api/setup/admin", map[string]string{"token": token, "email": "bob@example.com", "password": "wrongpass1"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("existing user wrong password status = %d, want 403", resp.StatusCode)
	}

	if resp := postJSON(t, client, srv.URL+"/api/setup/admin", admin); resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, want 201", resp.StatusCode)
	}

	resp, err := client.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me: %v", err)
	}
	defer resp.Body.Close()
	var me struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&me) != nil || me.Email != "admin@example.com" || !me.IsAdmin {
		t.Fatalf("/api/me after setup: status %d, %+v", resp.StatusCode, me)
	}

	if needsSetup() {
		t.Fatal("needs_setup = true after setup")
	}
	if resp := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/login", bob); resp.StatusCode != http.StatusOK {
		t.Fatalf("login after setup status = %d, want 200", resp.StatusCode)
	}
	// Registration stays closed after setup until the admin opens it (GLG-1117).
	carol := map[string]string{"email": "carol@example.com", "password": "password123"}
	regResp := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/register", carol)
	var regBody map[string]string
	json.NewDecoder(regResp.Body).Decode(&regBody)
	if regResp.StatusCode != http.StatusForbidden || regBody["error"] != "registration closed" {
		t.Fatalf("register after setup = %d %v, want 403 registration closed", regResp.StatusCode, regBody)
	}
	if resp := postJSON(t, client, srv.URL+"/api/setup/admin", admin); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup status = %d, want 409", resp.StatusCode)
	}
}
