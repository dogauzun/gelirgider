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
