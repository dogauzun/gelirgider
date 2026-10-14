package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// TestPasswordTooLong checks that a password over bcrypt's 72-byte limit is
// rejected with 400 and a validation message on both register and change
// password, instead of failing inside bcrypt and returning 500.
func TestPasswordTooLong(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	const wantMsg = "password must be at most 72 bytes"
	tooLong := strings.Repeat("a", 73)

	resp := postJSON(t, client, srv.URL+"/api/auth/register", map[string]string{
		"email":    "long@example.com",
		"password": tooLong,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("register with long password status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode register error: %v", err)
	}
	if body.Error != wantMsg {
		t.Fatalf("register error = %q, want %q", body.Error, wantMsg)
	}

	// A 72-byte password is still accepted.
	creds := map[string]string{"email": "ok@example.com", "password": strings.Repeat("a", 72)}
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register with 72-byte password status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	change := map[string]string{"current_password": creds["password"], "new_password": tooLong}
	if resp := postJSON(t, client, srv.URL+"/api/account/password", change); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("change to long password status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestPasswordTooShortMultiByte checks that the minimum length counts
// characters, not bytes: "şşşş" is 8 bytes but only 4 characters, so it must
// be rejected like any other 4-character password.
func TestPasswordTooShortMultiByte(t *testing.T) {
	srv := newTestServer(t)

	const wantMsg = "password must be at least 8 characters"
	resp := postJSON(t, &http.Client{}, srv.URL+"/api/auth/register", map[string]string{
		"email":    "short@example.com",
		"password": "şşşş",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("register with 4-character password status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode register error: %v", err)
	}
	if body.Error != wantMsg {
		t.Fatalf("register error = %q, want %q", body.Error, wantMsg)
	}
}
