package tefas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/shopspring/decimal"
)

// fixture is a fonFiyatBilgiGetir response: two published NAVs and today's
// unpublished fiyat 0 placeholder.
const fixture = `{"resultList":[
 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2026-09-10","fiyat":14.9},
 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2026-09-11","fiyat":14.934257},
 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2026-09-14","fiyat":0}]}`

// TestSyncStoresNAVsExactly runs Sync against a real SQLite repo: only the
// tracked fund is requested, with the honest User-Agent and periyod 1; the
// NAVs are stored as exact decimals with source tefas, overwriting a manual
// row; the zero placeholder is dropped. SyncHistory then skips the fund
// because its prices already reach back to the transaction.
func TestSyncStoresNAVsExactly(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		requested = append(requested, fmt.Sprint(req["fonKodu"], "/", req["periyod"]))
		if r.URL.Path != "/api/funds/fonFiyatBilgiGetir" || r.Header.Get("User-Agent") != "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(fixture))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(9001, 'YAC', 'TEFAS', 'tracked', 'TRY'),
			(9002, 'AAK', 'TEFAS', 'untracked', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 9001, 'buy', '2026-09-08', '10', '14', '140', '0', 'TRY', '2026-09-08T00:00:00Z')`,
		`INSERT INTO prices (instrument_id, date, close, source) VALUES (9001, '2026-09-10', '1', 'test')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	f := &tefas.Fetcher{BaseURL: srv.URL}
	n, err := tefas.Sync(ctx, f, instrument.NewRepo(conn), prices, time.Now())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 2 {
		t.Fatalf("Sync wrote %d rows, want 2", n)
	}
	if fmt.Sprint(requested) != "[YAC/1]" {
		t.Fatalf("requested %v, want [YAC/1]", requested)
	}

	rows, err := prices.Range(ctx, []int64{9001, 9002}, "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	got := fmt.Sprint(rows)
	want := fmt.Sprint([]price.Price{
		{InstrumentID: 9001, Date: "2026-09-10", Close: decimal.RequireFromString("14.9"), Source: "tefas"},
		{InstrumentID: 9001, Date: "2026-09-11", Close: decimal.RequireFromString("14.934257"), Source: "tefas"},
	})
	if got != want {
		t.Fatalf("stored prices =\n%s\nwant\n%s", got, want)
	}

	in, err := instrument.NewRepo(conn).Get(ctx, 9001)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	s := &tefas.Syncer{Fetcher: f, Instruments: instrument.NewRepo(conn), Prices: prices}
	if n, err := s.SyncHistory(ctx, in, "2026-09-08", now); err != nil || n != 0 || len(requested) != 1 {
		t.Fatalf("SyncHistory with history stored: n=%d err=%v requested=%v, want no request", n, err, requested)
	}
}

// TestSyncStopsQuietlyAfterCancel checks that a shutdown that cancels
// ctx during the first fund's fetch stops Sync there. The second fund is
// not fetched and no WARN is logged, since nothing failed. The cancel
// carries a cause, like signal.NotifyContext's, so the fetch error does
// not wrap context.Canceled.
func TestSyncStopsQuietlyAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		requested = append(requested, fmt.Sprint(req["fonKodu"]))
		cancel(errors.New("terminated signal received")) // the shutdown arrives during the first request
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
			(9001, 'YAC', 'TEFAS', 'first', 'TRY'),
			(9002, 'AAK', 'TEFAS', 'second', 'TRY')`,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
		`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
		`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
			(1, 1, 1, 9001, 'buy', '2026-09-08', '10', '14', '140', '0', 'TRY', '2026-09-08T00:00:00Z'),
			(2, 1, 1, 9002, 'buy', '2026-09-08', '10', '14', '140', '0', 'TRY', '2026-09-08T00:00:00Z')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	_, err = tefas.Sync(ctx, &tefas.Fetcher{BaseURL: srv.URL}, instrument.NewRepo(conn), price.NewRepo(conn), time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sync error = %v, want context.Canceled", err)
	}
	if len(requested) != 1 {
		t.Fatalf("requested %v, want only the first fund", requested)
	}
	if logs.Len() > 0 {
		t.Fatalf("logged after cancel:\n%s", logs.String())
	}
}

// TestSyncRenamesFund checks that the newest row's fonUnvan renames a
// fund whose stored name differs; the same name or an empty fonUnvan writes
// nothing and keeps the stored name.
func TestSyncRenamesFund(t *testing.T) {
	for _, tc := range []struct {
		name, unvan, want string
		renamed           bool
	}{
		{"differs", "BULLS PORTFÖY İKİNCİ SERBEST FON", "BULLS PORTFÖY İKİNCİ SERBEST FON", true},
		{"same", "İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST", "İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST", false},
		{"empty", "", "İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"resultList":[
 {"fonKodu":"IIE","fonUnvan":"OLD NAME","tarih":"2026-09-10","fiyat":1.5},
 {"fonKodu":"IIE","fonUnvan":%q,"tarih":"2026-09-11","fiyat":1.6}]}`, tc.unvan)
			}))
			defer srv.Close()

			conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("db.Open: %v", err)
			}
			defer conn.Close()
			ctx := context.Background()
			for _, q := range []string{
				`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (9001, 'IIE', 'TEFAS', 'İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST', 'TRY')`,
				`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`,
				`INSERT INTO portfolios (id, user_id, name, created_at) VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`,
				`INSERT INTO transactions (id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at) VALUES
					(1, 1, 1, 9001, 'buy', '2026-09-08', '10', '1', '10', '0', 'TRY', '2026-09-08T00:00:00Z')`,
			} {
				if _, err := conn.ExecContext(ctx, q); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(prev)

			instruments := instrument.NewRepo(conn)
			if _, err := tefas.Sync(ctx, &tefas.Fetcher{BaseURL: srv.URL}, instruments, price.NewRepo(conn), time.Now()); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			in, err := instruments.Get(ctx, 9001)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if in.Name != tc.want {
				t.Fatalf("name = %q, want %q", in.Name, tc.want)
			}
			if got := strings.Contains(logs.String(), "tefas fund renamed"); got != tc.renamed {
				t.Fatalf("rename logged = %v, want %v:\n%s", got, tc.renamed, logs.String())
			}
		})
	}
}

// TestLookup checks that a code with NAVs is found under the newest
// row's fonUnvan, TEFAS's null resultList is not found without an error,
// and an HTTP failure is an error.
func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		switch req["fonKodu"] {
		case "GJH":
			w.Write([]byte(`{"resultList":[
 {"fonKodu":"GJH","fonUnvan":"OLD NAME","tarih":"2026-09-29","fiyat":1.5},
 {"fonKodu":"GJH","fonUnvan":"NEW NAME","tarih":"2026-09-30","fiyat":1.6}]}`))
		case "NOP":
			w.Write([]byte(`{"errorMessage":"fon bulunamadi","resultList":null}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	f := &tefas.Fetcher{BaseURL: srv.URL}
	ctx := context.Background()

	if name, found, err := f.Lookup(ctx, "GJH"); err != nil || !found || name != "NEW NAME" {
		t.Errorf("Lookup(GJH) = %q, %v, %v; want NEW NAME, true, nil", name, found, err)
	}
	if name, found, err := f.Lookup(ctx, "NOP"); err != nil || found || name != "" {
		t.Errorf("Lookup(NOP) = %q, %v, %v; want not found, nil", name, found, err)
	}
	if _, found, err := f.Lookup(ctx, "ERR"); err == nil || found {
		t.Errorf("Lookup(ERR) found=%v err=%v, want an error", found, err)
	}
}

// TestSyncHistoryRefetchesAHole checks that a fund whose stored series
// starts on time but stops months before now is fetched again and the
// stub's full series is stored, while a fund whose series also reaches
// now makes no request.
func TestSyncHistoryRefetchesAHole(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"resultList":[
	 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2023-09-15","fiyat":1.1},
	 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2024-06-03","fiyat":1.2},
	 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2025-04-08","fiyat":1.3},
	 {"fonKodu":"YAC","fonUnvan":"X","tarih":"2026-09-14","fiyat":1.4}]}`))
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (9001, 'YAC', 'TEFAS', 'X', 'TRY')`,
		// the first 3 months of the 36-month range, then nothing
		`INSERT INTO prices (instrument_id, date, close, source) VALUES
			(9001, '2023-09-15', '1.1', 'tefas'), (9001, '2023-10-20', '1.1', 'tefas'), (9001, '2023-12-15', '1.1', 'tefas')`,
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	instruments := instrument.NewRepo(conn)
	in, err := instruments.Get(ctx, 9001)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	s := &tefas.Syncer{Fetcher: &tefas.Fetcher{BaseURL: srv.URL}, Instruments: instruments, Prices: prices}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	if n, err := s.SyncHistory(ctx, in, "2023-09-15", now); err != nil || n != 4 || requests != 1 {
		t.Fatalf("SyncHistory with a hole: n=%d err=%v requests=%d, want 4 rows from 1 request", n, err, requests)
	}
	rows, err := prices.Range(ctx, []int64{9001}, "2024-01-01", "2026-09-15")
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("stored %d rows after 2024-01-01, want the 3 fetched ones: %v", len(rows), rows)
	}
	if n, err := s.SyncHistory(ctx, in, "2023-09-15", now); err != nil || n != 0 || requests != 1 {
		t.Fatalf("SyncHistory with full coverage: n=%d err=%v requests=%d, want no request", n, err, requests)
	}
}
