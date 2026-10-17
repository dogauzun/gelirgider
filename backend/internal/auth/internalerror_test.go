package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteInternalErrorClientCancel: a request whose client went away
// (its context is cancelled) logs nothing at ERROR and answers 499, not 500.
// The same error on a live request is still an internal error.
func TestWriteInternalErrorClientCancel(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	queryErr := fmt.Errorf("query portfolios: %w", context.Canceled)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodGet, "/api/portfolios/all/history", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	WriteInternalError(rec, r, queryErr)
	if rec.Code != 499 {
		t.Errorf("cancelled request: status = %d, want 499", rec.Code)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("cancelled request logged at ERROR: %s", logs.String())
	}

	logs.Reset()
	r = httptest.NewRequest(http.MethodGet, "/api/portfolios/all/history", nil)
	rec = httptest.NewRecorder()
	WriteInternalError(rec, r, errors.New("query portfolios: disk I/O error"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("live request: status = %d, want 500", rec.Code)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("live request not logged at ERROR: %q", logs.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"valid", `{"name":"x"}`, true},
		{"malformed", `{"name":`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			var v struct{ Name string }
			if got := DecodeJSON(rec, r, &v); got != tc.ok {
				t.Fatalf("DecodeJSON = %v, want %v", got, tc.ok)
			}
			if tc.ok {
				if v.Name != "x" || rec.Code != http.StatusOK {
					t.Fatalf("v = %+v, code = %d", v, rec.Code)
				}
				return
			}
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"invalid request body"`) {
				t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestDecodeOptionalJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
		want string
	}{
		{"empty", ``, true, ""},
		{"valid", `{"name":"x"}`, true, "x"},
		{"malformed", `{"name":`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			var v struct{ Name string }
			if got := DecodeOptionalJSON(rec, r, &v); got != tc.ok {
				t.Fatalf("DecodeOptionalJSON = %v, want %v", got, tc.ok)
			}
			if v.Name != tc.want {
				t.Fatalf("Name = %q, want %q", v.Name, tc.want)
			}
			if tc.ok {
				if rec.Code != http.StatusOK {
					t.Fatalf("code = %d", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"invalid request body"`) {
				t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
			}
		})
	}
}
