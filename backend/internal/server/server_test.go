package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

func TestHealthHandler(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	New(logger, conn, false, settings.NewRepo(conn, nil), nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("body = %q, want %q", got, `{"status":"ok"}`)
	}
}

func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	seedAdmin(t, conn, "setup-admin@example.com", "password123")

	// Valid JSON that would register successfully if the body were not capped.
	body := []byte(`{"email":"big@example.com","password":"secret123","pad":"` + strings.Repeat("a", maxBodyBytes) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	New(logger, conn, false, settings.NewRepo(conn, nil), nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if got, want := rec.Body.String(), `{"error":"request body: larger than 64 KB"}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// TestCrossOriginRequestRejected covers GLG-407: a signed-in POST from
// another origin, even a same-site one sending a text/plain form, gets 403,
// while the app's own frontend, same-origin by Sec-Fetch-Site or by an
// Origin that matches Host, still gets through.
func TestCrossOriginRequestRejected(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	adminID := seedAdmin(t, conn, "setup-admin@example.com", "password123")
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), adminID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, http.StatusForbidden},
		{"same-site", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://other.example"}, http.StatusForbidden},
		{"foreign origin, no Sec-Fetch-Site", map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://example.com"}, http.StatusCreated},
		{"origin matches host", map[string]string{"Origin": "http://example.com"}, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/portfolios", strings.NewReader(`{"name":"`+tc.name+`"}`))
			req.Header.Set("Content-Type", "text/plain")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			req.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestHealthHandlerDegradedWhenDBClosed(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	conn.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	New(logger, conn, false, settings.NewRepo(conn, nil), nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), `"status":"degraded"`) {
		t.Fatalf("body = %q, want degraded status", rec.Body.String())
	}
}

func TestWithRecoverReturns500AndKeepsServing(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := withRecover(logger, mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	// The server must keep serving subsequent requests after a panic.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status after panic = %d, want %d", rec2.Code, http.StatusOK)
	}
}

// TestInternalErrorIsLogged covers GLG-132: the graceful 500 path used to
// drop the error, leaving only the INFO request line. Dropping the
// transactions table makes the summary handler's repository call fail; the
// response must stay generic while exactly one ERROR line names the failing
// query, the request and the user.
func TestInternalErrorIsLogged(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	if _, err := conn.Exec(`DROP TABLE transactions`); err != nil {
		t.Fatalf("drop transactions: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"error":"internal error"}` {
		t.Errorf("body = %s, want the generic error; detail must stay in the log", body)
	}

	var errorLines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if entry["level"] == "ERROR" {
			errorLines = append(errorLines, entry)
		}
	}
	if len(errorLines) != 1 {
		t.Fatalf("got %d ERROR log lines, want 1: %s", len(errorLines), logs.String())
	}

	entry := errorLines[0]
	msg, _ := entry["error"].(string)
	if !strings.Contains(msg, "query transactions") || !strings.Contains(msg, "no such table") {
		t.Errorf("error = %q, want the failing query", msg)
	}
	if entry["method"] != http.MethodGet {
		t.Errorf("method = %v, want %s", entry["method"], http.MethodGet)
	}
	if want := fmt.Sprintf("/api/portfolios/%d/summary", pf.ID); entry["path"] != want {
		t.Errorf("path = %v, want %s", entry["path"], want)
	}
	if entry["user_id"] != float64(usr.ID) {
		t.Errorf("user_id = %v, want %d", entry["user_id"], usr.ID)
	}
}

// TestUnknownRouteJSONError covers GLG-980: signed in, an unknown /api/ path
// and a wrong method answer with the JSON error body, not ServeMux's plain
// text, and the 405 keeps its Allow header. Signed out, an unknown path is
// still 401.
func TestUnknownRouteJSONError(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil)
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), seedAdmin(t, conn, "admin@example.com", "password1"))
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, tc := range []struct {
		signedIn   bool
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{true, http.MethodGet, "/api/nonexistent", http.StatusNotFound, `{"error":"not found"}`, ""},
		{true, http.MethodGet, "/api/admin/nonexistent", http.StatusNotFound, `{"error":"not found"}`, ""},
		{true, http.MethodPut, "/api/portfolios", http.StatusMethodNotAllowed, `{"error":"method not allowed"}`, "GET, HEAD, POST"},
		{true, http.MethodGet, "/api/admin/users/1", http.StatusMethodNotAllowed, `{"error":"method not allowed"}`, "DELETE"},
		{false, http.MethodGet, "/api/nonexistent", http.StatusUnauthorized, `{"error":"unauthorized"}`, ""},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.signedIn {
			r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)

		if rec.Code != tc.wantStatus {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s %s: Content-Type %q, want application/json", tc.method, tc.path, ct)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != tc.wantBody {
			t.Errorf("%s %s: body %q, want %s", tc.method, tc.path, body, tc.wantBody)
		}
		if got := rec.Header().Get("Allow"); got != tc.wantAllow {
			t.Errorf("%s %s: Allow %q, want %q", tc.method, tc.path, got, tc.wantAllow)
		}
	}
}
