package instrument_test

import (
	"context"
	"encoding/json"
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

// TestSetSectorOverride covers PATCH /api/admin/instruments/{id}: the key shows
// in the search result, null or no key clears it, an unknown key or a DEPOSIT
// instrument is 400, an unknown id is 404, and a symbol refresh leaves it.
func TestSetSectorOverride(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	repo := instrument.NewRepo(conn)
	mux := http.NewServeMux()
	instrument.RegisterRoutes(mux, repo)
	instrument.RegisterAdminRoutes(mux, repo)

	id, err := repo.Insert(ctx, "BIST", "ZZZB", "Test Bank", "TRY")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	res, err := conn.ExecContext(ctx, `INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-ZZ1', 'DEPOSIT', 'Deposit', 'TRY')`)
	if err != nil {
		t.Fatalf("insert deposit: %v", err)
	}
	depositID, _ := res.LastInsertId()

	do := func(target, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, target, strings.NewReader(body)))
		return w
	}
	override := func() any {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/instruments?q=ZZZB", nil))
		var found []map[string]any
		if err := json.NewDecoder(w.Body).Decode(&found); err != nil || len(found) != 1 {
			t.Fatalf("search ZZZB: %v, %d results", err, len(found))
		}
		return found[0]["sector_override"]
	}
	path := "/api/admin/instruments/" + strconv.FormatInt(id, 10)

	if got := override(); got != nil {
		t.Fatalf("fresh override = %v, want null", got)
	}
	if w := do(path, `{"sector_override":"financials"}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sector_override":"financials"`) {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}
	if got := override(); got != "financials" {
		t.Fatalf("override after set = %v, want financials", got)
	}

	// A symbol refresh must not touch the override.
	if err := instrument.SeedFile(ctx, conn, []byte("symbol,name\nZZZB,Test Bank\n"), "BIST", "TRY"); err != nil {
		t.Fatalf("SeedFile: %v", err)
	}
	if got := override(); got != "financials" {
		t.Fatalf("override after seed = %v, want financials", got)
	}

	for _, body := range []string{`{"sector_override":"banking"}`, `{"sector_override":"unknown"}`, `{"sector_override":""}`} {
		if w := do(path, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, w.Code)
		}
	}
	if got := override(); got != "financials" {
		t.Fatalf("override after refused sets = %v, want financials", got)
	}
	if w := do("/api/admin/instruments/"+strconv.FormatInt(depositID, 10), `{"sector_override":"financials"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "term deposits have no classification") {
		t.Errorf("deposit: %d %s, want 400", w.Code, w.Body.String())
	}
	if w := do(path+"9", `{"sector_override":"financials"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", w.Code)
	}

	for _, body := range []string{`{"sector_override":null}`, `{}`} {
		do(path, `{"sector_override":"energy"}`)
		if w := do(path, body); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sector_override":null`) {
			t.Fatalf("clear with %s: %d %s", body, w.Code, w.Body.String())
		}
		if got := override(); got != nil {
			t.Fatalf("override after clear with %s = %v, want null", body, got)
		}
	}
}
