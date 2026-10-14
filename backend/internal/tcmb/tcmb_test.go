package tcmb_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
)

// bulletinXML is a truncated copy of a real TCMB bulletin: USD and EUR (both
// tracked and both with a rate), GBP (a currency the app doesn't track), and
// JPY (untracked, and also has an empty ForexBuying like a suspended entry).
func bulletinXML(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "bulletin.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return body
}

func TestFetchRatesParsesTrackedCurrenciesOnly(t *testing.T) {
	body := bulletinXML(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	rates, err := f.FetchRates(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRates: %v", err)
	}
	if len(rates) != 2 {
		t.Fatalf("got %d rates, want 2 (GBP untracked, JPY untracked+empty): %+v", len(rates), rates)
	}

	want := map[string]string{
		"USD": "48.4305",
		"EUR": "52.109",
	}
	for _, r := range rates {
		wantRate, ok := want[r.Currency]
		if !ok {
			t.Fatalf("unexpected currency in result: %s", r.Currency)
		}
		if r.Rate.String() != wantRate {
			t.Fatalf("rate for %s = %s, want %s", r.Currency, r.Rate.String(), wantRate)
		}
		if r.Date != "2026-09-11" {
			t.Fatalf("date for %s = %s, want 2026-09-11", r.Currency, r.Date)
		}
		delete(want, r.Currency)
	}
	if len(want) != 0 {
		t.Fatalf("missing currencies in result: %v", want)
	}
}

// TestFetchRatesSkipsEmptyForexBuying isolates the empty-ForexBuying skip
// for a tracked currency (bulletinXML's empty entry, JPY, is also untracked,
// so it alone wouldn't prove this branch).
func TestFetchRatesSkipsEmptyForexBuying(t *testing.T) {
	const xmlBody = `<Tarih_Date>
<Currency CurrencyCode="USD"><Unit>1</Unit><ForexBuying></ForexBuying></Currency>
</Tarih_Date>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(xmlBody))
	}))
	defer srv.Close()

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	rates, err := f.FetchRates(context.Background(), time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRates: %v", err)
	}
	if len(rates) != 0 {
		t.Fatalf("got %d rates, want 0 (USD has empty ForexBuying): %+v", len(rates), rates)
	}
}

// TestFetchRatesRejectsZeroUnit: a Unit of 0 used to panic in Div and take
// down the sync goroutine; it must be an error instead.
func TestFetchRatesRejectsZeroUnit(t *testing.T) {
	const xmlBody = `<Tarih_Date>
<Currency CurrencyCode="USD"><Unit>0</Unit><ForexBuying>41.10</ForexBuying></Currency>
</Tarih_Date>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(xmlBody))
	}))
	defer srv.Close()

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	rates, err := f.FetchRates(context.Background(), time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("FetchRates: got nil error and %+v, want error for Unit 0", rates)
	}
}

// TestFetchRates404IsNotAnError covers a non-business day: the scheduler
// treats a nil slice + nil error the same as "not yet available", not a
// failure.
func TestFetchRates404IsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	rates, err := f.FetchRates(context.Background(), time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRates: %v", err)
	}
	if len(rates) != 0 {
		t.Fatalf("got %d rates, want 0", len(rates))
	}
}

// TestSyncWritesTwoRows covers Sync end to end: fetch, then upsert into a
// real fx.Repo backed by an in-memory SQLite database.
func TestSyncWritesTwoRows(t *testing.T) {
	body := bulletinXML(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	fxRepo := fx.NewRepo(conn)

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	n, err := tcmb.Sync(ctx, f, fxRepo, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 2 {
		t.Fatalf("Sync wrote %d rows, want 2", n)
	}

	latest, err := fxRepo.Latest(ctx, []string{"USD", "EUR"}, "2026-09-11")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	usd, ok := latest["USD"]
	if !ok || usd.Rate.String() != "48.4305" || usd.Source != "tcmb" {
		t.Fatalf("USD rate = %+v, want rate=48.4305 source=tcmb", usd)
	}
	eur, ok := latest["EUR"]
	if !ok || eur.Rate.String() != "52.109" || eur.Source != "tcmb" {
		t.Fatalf("EUR rate = %+v, want rate=52.109 source=tcmb", eur)
	}
}

// TestSyncOverwritesExistingRow covers that Sync's upsert replaces a prior
// row for the same (currency, date) rather than erroring or duplicating it.
func TestSyncOverwritesExistingRow(t *testing.T) {
	body := bulletinXML(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	fxRepo := fx.NewRepo(conn)
	if err := fxRepo.Upsert(ctx, "USD", "2026-09-11", "1.00", "manual"); err != nil {
		t.Fatalf("seed manual rate: %v", err)
	}

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	if _, err := tcmb.Sync(ctx, f, fxRepo, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	latest, err := fxRepo.Latest(ctx, []string{"USD"}, "2026-09-11")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := latest["USD"]; got.Rate.String() != "48.4305" || got.Source != "tcmb" {
		t.Fatalf("USD rate = %+v, want rate=48.4305 source=tcmb (manual rate should be overwritten)", got)
	}
}

// TestFetchRatesRejectsOversizedBody checks that a valid bulletin
// padded past the 8 MB read cap fails the fetch instead of being read whole.
func TestFetchRatesRejectsOversizedBody(t *testing.T) {
	body := append(bulletinXML(t), bytes.Repeat([]byte(" "), 8<<20)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	f := &tcmb.Fetcher{BaseURL: srv.URL + "/"}
	if _, err := f.FetchRates(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("FetchRates: got nil error for a body over the cap, want an error")
	}
}
