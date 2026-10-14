package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/evds"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/us"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestProviderKeys covers GLG-302: with no key the US sync reports "not
// configured"; after PUT /api/admin/providers/finnhub the next sync sends
// the new key, read live from settings; GET never returns the key, only
// configured and its last 4 characters; an unknown provider is 404.
// POST .../test (GLG-308) tests the body's key or the stored one: a key the
// provider accepts gives ok true, a 401 gives "invalid key", no key at all
// gives 400, and the key is never echoed.
func TestProviderKeys(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	secrets := settings.NewRepo(conn, bytes.Repeat([]byte{1}, 32))

	const key = "finnhub-secret-abcd"
	var gotToken string
	finnhub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Finnhub-Token")
		if gotToken != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"c":190.5,"pc":189,"t":1718395200}`))
	}))
	defer finnhub.Close()
	tiingo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token tiingo-key" || r.URL.Path != "/tiingo/daily/AAPL/prices" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer tiingo.Close()
	fetcher := &us.Fetcher{BaseURL: finnhub.URL, TiingoBaseURL: tiingo.URL, Settings: secrets}
	handler := newHandler(logger, conn, false, deps{cfg: secrets, bistFetcher: bist.NewFetcher(), usFetcher: fetcher, tcmbFetcher: tcmb.NewFetcher(), btcturkFetcher: btcturk.NewFetcher(), tefasFetcher: tefas.NewFetcher()})

	users := user.NewRepo(conn)
	admin, err := users.Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.SetAdmin(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, _ := res.LastInsertId()
	aapl := instrument.Instrument{ID: id, Symbol: "AAPL", Market: "US"}
	now := time.Date(2024, 6, 17, 12, 0, 0, 0, time.UTC)

	if _, err := us.SyncOne(ctx, fetcher, price.NewRepo(conn), aapl, now); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("sync without key err = %v, want not configured", err)
	}

	if rec := call(http.MethodPut, "/api/admin/providers/finnhub", `{"api_key":"`+key+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPut, "/api/admin/providers/nope", `{"api_key":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("PUT unknown status = %d, want 404", rec.Code)
	}

	if wrote, err := us.SyncOne(ctx, fetcher, price.NewRepo(conn), aapl, now); err != nil || !wrote {
		t.Fatalf("sync with key = %v, %v; want true, nil", wrote, err)
	}
	if gotToken != key {
		t.Fatalf("X-Finnhub-Token = %q, want %q", gotToken, key)
	}

	rec := call(http.MethodGet, "/api/admin/providers", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), key) {
		t.Fatalf("GET = %d %s; want 200 without the key", rec.Code, rec.Body)
	}
	var got []providerStatus
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []providerStatus{{"finnhub", true, "abcd", ""}, {"tiingo", false, "", ""}, {"evds", false, "", ""}}
	if !slices.Equal(got, want) {
		t.Fatalf("GET = %+v, want %+v", got, want)
	}

	for _, tc := range []struct{ path, body, want string }{
		{"/api/admin/providers/finnhub/test", "", `{"ok":true}`},                                               // stored key
		{"/api/admin/providers/finnhub/test", `{"api_key":"wrong-key"}`, `{"ok":false,"error":"invalid key"}`}, // 401
		{"/api/admin/providers/tiingo/test", `{"api_key":"tiingo-key"}`, `{"ok":true}`},                        // before saving
	} {
		rec := call(http.MethodPost, tc.path, tc.body)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != tc.want {
			t.Errorf("POST %s %s = %d %s, want 200 %s", tc.path, tc.body, rec.Code, got, tc.want)
		}
	}
	if rec := call(http.MethodPost, "/api/admin/providers/tiingo/test", ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("POST tiingo test without key = %d %s, want 400 not configured", rec.Code, rec.Body)
	}

	// GLG-320: after a SECRET_KEY change the stored finnhub key does not
	// decrypt. GET still lists every provider and flags that one; testing
	// the stored key is a 400, not a 500.
	otherSecrets := settings.NewRepo(conn, bytes.Repeat([]byte{2}, 32))
	otherFetcher := &us.Fetcher{BaseURL: finnhub.URL, TiingoBaseURL: tiingo.URL, Settings: otherSecrets}
	handler = newHandler(logger, conn, false, deps{cfg: otherSecrets, bistFetcher: bist.NewFetcher(), usFetcher: otherFetcher, tcmbFetcher: tcmb.NewFetcher(), btcturkFetcher: btcturk.NewFetcher(), tefasFetcher: tefas.NewFetcher()})
	rec = call(http.MethodGet, "/api/admin/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET with other SECRET_KEY = %d %s, want 200", rec.Code, rec.Body)
	}
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want = []providerStatus{{"finnhub", true, "", "undecryptable"}, {"tiingo", false, "", ""}, {"evds", false, "", ""}}
	if !slices.Equal(got, want) {
		t.Fatalf("GET with other SECRET_KEY = %+v, want %+v", got, want)
	}
	if rec := call(http.MethodPost, "/api/admin/providers/finnhub/test", ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "stored key unreadable") {
		t.Errorf("POST finnhub test with unreadable key = %d %s, want 400 stored key unreadable", rec.Code, rec.Body)
	}

	// GLG-517: the evds key test goes to the EVDS fetcher, which sends the
	// key in the "key" header. Never the real EVDS host in tests.
	evdsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer evdsSrv.Close()
	mux := http.NewServeMux()
	registerProviderRoutes(mux, secrets, fetcher, &evds.Fetcher{BaseURL: evdsSrv.URL})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/providers/evds/test", strings.NewReader(`{"api_key":"test-key"}`)))
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"ok":true}` {
		t.Errorf("POST evds test = %d %s, want 200 {\"ok\":true}", rec.Code, got)
	}
}
