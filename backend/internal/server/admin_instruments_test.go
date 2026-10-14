package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// TestAdminAddInstrument covers POST /api/admin/instruments (GLG-561): an
// admin adds NVDA with the symbol upper-cased, a second identical POST gives
// the same id, a pair no seed list uses, an empty name and a name or symbol
// made only of combining marks (GLG-1003) are 400, a symbol over 32 or a
// name over 300 characters is 400 and stored nothing (GLG-1310), a
// non-admin gets 403, and the new row is found by GET /api/instruments.
func TestAdminAddInstrument(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	srv := httptest.NewServer(New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil))
	t.Cleanup(srv.Close)

	newClient := func(email string) *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New: %v", err)
		}
		c := &http.Client{Jar: jar}
		if resp := postJSON(t, c, srv.URL+"/api/auth/login", map[string]string{"email": email, "password": "password1"}); resp.StatusCode != http.StatusOK {
			t.Fatalf("login %s: status %d", email, resp.StatusCode)
		}
		return c
	}
	type inst struct {
		ID       int64  `json:"id"`
		Symbol   string `json:"symbol"`
		Market   string `json:"market"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}
	add := func(c *http.Client, body map[string]string) (int, inst) {
		t.Helper()
		resp := postJSON(t, c, srv.URL+"/api/admin/instruments", body)
		var got inst
		json.NewDecoder(resp.Body).Decode(&got)
		return resp.StatusCode, got
	}

	seedAdmin(t, conn, "admin@example.com", "password1")
	openRegistration(t, conn)
	admin := newClient("admin@example.com")
	if resp := postJSON(t, http.DefaultClient, srv.URL+"/api/auth/register", map[string]string{"email": "bob@example.com", "password": "password1"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register bob: status %d", resp.StatusCode)
	}
	bob := newClient("bob@example.com")

	nvda := map[string]string{"market": "US", "symbol": " nvda ", "name": "NVIDIA", "currency": "USD"}
	status, first := add(admin, nvda)
	want := inst{ID: first.ID, Symbol: "NVDA", Market: "US", Name: "NVIDIA", Currency: "USD"}
	if status != http.StatusOK || first.ID == 0 || first != want {
		t.Fatalf("first add: status %d, got %+v, want 200 %+v", status, first, want)
	}
	if status, again := add(admin, nvda); status != http.StatusOK || again.ID != first.ID {
		t.Errorf("second add: status %d, id %d, want 200 id %d", status, again.ID, first.ID)
	}

	for _, body := range []map[string]string{
		{"market": "US", "symbol": "NVDA", "name": "NVIDIA", "currency": "TRY"},
		{"market": "US", "symbol": "NVDA", "name": "  ", "currency": "USD"},
		{"market": "US", "symbol": "NVDA", "name": "NVI\nDIA", "currency": "USD"},
		{"market": "US", "symbol": "", "name": "NVIDIA", "currency": "USD"},
		// Only combining marks render blank (GLG-1003).
		{"market": "US", "symbol": "AMD", "name": "̶̶̶", "currency": "USD"},
		{"market": "US", "symbol": "́́", "name": "Blank", "currency": "USD"},
		// Over-long values would be listed in every user's search (GLG-1310).
		{"market": "US", "symbol": strings.Repeat("Y", 1000), "name": "long", "currency": "USD"},
		{"market": "US", "symbol": "LONGNAME", "name": strings.Repeat("ş", 301), "currency": "USD"},
	} {
		if status, _ := add(admin, body); status != http.StatusBadRequest {
			t.Errorf("add %v: status %d, want 400", body, status)
		}
	}

	for _, q := range []string{"YYYY", "LONGNAME"} {
		resp, err := admin.Get(srv.URL + "/api/instruments?q=" + q)
		if err != nil {
			t.Fatalf("search %s: %v", q, err)
		}
		var found []inst
		json.NewDecoder(resp.Body).Decode(&found)
		resp.Body.Close()
		if len(found) != 0 {
			t.Errorf("search %s after a rejected add: got %+v, want none", q, found)
		}
	}
	// The caps themselves are accepted.
	if status, _ := add(admin, map[string]string{"market": "US", "symbol": strings.Repeat("Z", 32), "name": strings.Repeat("ş", 300), "currency": "USD"}); status != http.StatusOK {
		t.Errorf("add at the caps: status %d, want 200", status)
	}

	if status, _ := add(bob, nvda); status != http.StatusForbidden {
		t.Errorf("non-admin add: status %d, want 403", status)
	}

	resp, err := admin.Get(srv.URL + "/api/instruments?q=nvda")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	var found []inst
	json.NewDecoder(resp.Body).Decode(&found)
	if len(found) != 1 || found[0] != want {
		t.Errorf("search nvda: got %+v, want [%+v]", found, want)
	}
}
