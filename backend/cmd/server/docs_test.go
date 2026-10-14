package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/config"
)

// API_DOCS_DIR with COOKIE_SECURE=true is a production-like setup: the docs
// page loads a third-party script on the app's origin, so it stays unmounted
// and the request falls through to the app's handler.
func TestMountDocsOffWhenCookieSecure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "swagger.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		secure bool
		want   int
	}{{false, http.StatusOK}, {true, http.StatusNotFound}} {
		h := mountDocs(http.NotFoundHandler(), config.Config{APIDocsDir: dir, CookieSecure: tc.secure}, testLogger())
		for _, path := range []string{"/api/docs/", "/api/docs/swagger.json"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != tc.want {
				t.Errorf("COOKIE_SECURE=%v GET %s: got %d, want %d", tc.secure, path, rec.Code, tc.want)
			}
		}
	}
}
