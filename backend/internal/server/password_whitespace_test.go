package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// TestPasswordWhitespaceOnly checks that a password made of nothing but
// whitespace or invisible format characters is rejected with 400, even when
// it is long enough to pass the character minimum (GLG-900).
func TestPasswordWhitespaceOnly(t *testing.T) {
	srv := newTestServer(t)

	const wantMsg = "password must not be only whitespace"
	for _, password := range []string{"        ", strings.Repeat("\u200b", 8), " \u200c\u200d\u2060\ufeff\t\u00a0 "} {
		resp := postJSON(t, &http.Client{}, srv.URL+"/api/auth/register", map[string]string{
			"email":    "blank@example.com",
			"password": password,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("register with password %q status = %d, want %d", password, resp.StatusCode, http.StatusBadRequest)
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode register error: %v", err)
		}
		if body.Error != wantMsg {
			t.Fatalf("register with password %q error = %q, want %q", password, body.Error, wantMsg)
		}
	}
}

// TestPasswordKeepsPadding checks that the whitespace rule does not trim the
// password: one padded with spaces around real characters still registers,
// logs in exactly as typed, and does not log in trimmed.
func TestPasswordKeepsPadding(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	creds := map[string]string{"email": "padded@example.com", "password": "  correct-horse  "}
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register with padded password status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login with padded password status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	trimmed := map[string]string{"email": creds["email"], "password": "correct-horse"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", trimmed); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with trimmed password status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
