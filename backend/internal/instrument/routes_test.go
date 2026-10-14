package instrument_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

// TestFromPath checks that a malformed and an unknown id both get the 404
// the price and position history handlers answered before FromPath, and that
// a known id hands back its instrument.
func TestFromPath(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	repo := instrument.NewRepo(conn)
	id, err := repo.Insert(context.Background(), "US", "ZZZT", "Test Corp", "USD")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	for _, tc := range []struct {
		value string
		code  int
		body  string
	}{
		{strconv.FormatInt(id, 10), http.StatusOK, ""},
		{strconv.FormatInt(id+1, 10), http.StatusNotFound, `{"error":"instrument not found"}`},
		{"x", http.StatusNotFound, `{"error":"instrument not found"}`},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("instrument_id", tc.value)
		w := httptest.NewRecorder()
		in, ok := instrument.FromPath(w, r, repo, "instrument_id")
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%s: %d %q, want %d %q", tc.value, w.Code, w.Body.String(), tc.code, tc.body)
		}
		if want := tc.code == http.StatusOK; ok != want {
			t.Errorf("%s: ok = %v, want %v", tc.value, ok, want)
		}
		if ok && (in.ID != id || in.Symbol != "ZZZT") {
			t.Errorf("%s: instrument = %+v, want id %d symbol ZZZT", tc.value, in, id)
		}
	}
}
