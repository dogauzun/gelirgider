package server

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// TestChangePassword covers POST /api/account/password: a wrong current
// password is rejected with 401, and a correct one changes the password so
// the old one no longer logs in.
func TestChangePassword(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	creds := map[string]string{"email": "alice@example.com", "password": "old-password"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	wrongCurrent := map[string]string{"current_password": "not-the-password", "new_password": "new-password"}
	resp := postJSON(t, client, srv.URL+"/api/account/password", wrongCurrent)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "current password is incorrect") {
		t.Fatalf("wrong current password = %d %s, want 401 current password is incorrect", resp.StatusCode, body)
	}

	change := map[string]string{"current_password": "old-password", "new_password": "new-password"}
	if resp := postJSON(t, client, srv.URL+"/api/account/password", change); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	oldLogin := map[string]string{"email": creds["email"], "password": "old-password"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", oldLogin); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with old password status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	newLogin := map[string]string{"email": creds["email"], "password": "new-password"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", newLogin); resp.StatusCode != http.StatusOK {
		t.Fatalf("login with new password status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestChangePasswordSignsOutOtherSessions covers the session half of the
// password change: the two clients here are the two browsers of one user.
// After the change only the session that made it is still valid.
func TestChangePasswordSignsOutOtherSessions(t *testing.T) {
	srv := newTestServer(t)

	jarClient := func() *http.Client {
		t.Helper()
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookiejar.New: %v", err)
		}
		return &http.Client{Jar: jar}
	}
	meStatus := func(client *http.Client) int {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/me")
		if err != nil {
			t.Fatalf("GET /api/me: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	creds := map[string]string{"email": "alice@example.com", "password": "old-password"}
	browserA, browserB := jarClient(), jarClient()
	if resp := postJSON(t, browserA, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	for _, client := range []*http.Client{browserA, browserB} {
		if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
			t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}

	change := map[string]string{"current_password": "old-password", "new_password": "new-password"}
	if resp := postJSON(t, browserA, srv.URL+"/api/account/password", change); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if got := meStatus(browserB); got != http.StatusUnauthorized {
		t.Errorf("other session GET /api/me status = %d, want %d", got, http.StatusUnauthorized)
	}
	if got := meStatus(browserA); got != http.StatusOK {
		t.Errorf("changing session GET /api/me status = %d, want %d", got, http.StatusOK)
	}
}

// TestChangePasswordRateLimited covers the limiter on POST
// /api/account/password: a signed-in session cannot guess the current
// password without bound. The 11th wrong attempt inside the window is 429.
func TestChangePasswordRateLimited(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	creds := map[string]string{"email": "alice@example.com", "password": "old-password"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	wrong := map[string]string{"current_password": "not-the-password", "new_password": "new-password"}
	for i := 1; i <= 10; i++ {
		if resp := postJSON(t, client, srv.URL+"/api/account/password", wrong); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", i, resp.StatusCode, http.StatusUnauthorized)
		}
	}
	if resp := postJSON(t, client, srv.URL+"/api/account/password", wrong); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("attempt 11 status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
}
