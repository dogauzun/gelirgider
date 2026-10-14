package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestMeHandler covers GET /api/me: 401 without a session cookie, and the
// user's own id/email with a valid one.
func TestMeHandler(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-cookie status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	u, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid-session status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != u.ID || got.Email != u.Email {
		t.Fatalf("got %+v, want id=%d email=%s", got, u.ID, u.Email)
	}
}

// TestReportingCurrency covers PUT /api/me/reporting-currency: an invalid
// value is rejected with 400, and a valid one is persisted and reflected by
// a subsequent GET /api/me.
func TestReportingCurrency(t *testing.T) {
	srv := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	creds := map[string]string{"email": "alice@example.com", "password": "password123"}
	if resp := postJSON(t, client, srv.URL+"/api/auth/register", creds); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp := postJSON(t, client, srv.URL+"/api/auth/login", creds); resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	invalid := map[string]string{"reporting_currency": "GBP"}
	resp := putJSON(t, client, srv.URL+"/api/me/reporting-currency", invalid)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid currency status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	valid := map[string]string{"reporting_currency": "USD"}
	resp = putJSON(t, client, srv.URL+"/api/me/reporting-currency", valid)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid currency status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	meResp, err := client.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me: %v", err)
	}
	defer meResp.Body.Close()

	var got struct {
		ReportingCurrency string `json:"reporting_currency"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ReportingCurrency != "USD" {
		t.Fatalf("reporting_currency = %q, want %q", got.ReportingCurrency, "USD")
	}
}

func putJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp
}
