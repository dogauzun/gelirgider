package bist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/split"
)

// The fixture holds a cash-only row (CASHD), a rights-issue row (SEKFK),
// two bonus rows (YEOTK from both columns, HRKET with a thousands
// separator), a bonus-plus-cash row (BOTHX), a bonus row for a symbol with
// no instrument (NOSYM) and a bonus-plus-cash row dated after today
// (FUTRE). Splits land for YEOTK, HRKET and BOTHX, none for FUTRE: a planned
// split must not apply before its ex-date. Dividends land for CASHD, BOTHX and
// FUTRE, whose planned dividend is stored ahead of its ex-date.
func TestSyncCapitalEventsStoresPastSplitsAndAllDividendsOfKnownSymbols(t *testing.T) {
	page, err := os.ReadFile("testdata/sermaye.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(page)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'CASHD', 'BIST', 'Cash', 'TRY'), (2, 'SEKFK', 'BIST', 'Rights', 'TRY'),
		       (3, 'YEOTK', 'BIST', 'Yeo', 'TRY'), (4, 'HRKET', 'BIST', 'Hareket', 'TRY'),
		       (5, 'FUTRE', 'BIST', 'Future', 'TRY'), (6, 'BOTHX', 'BIST', 'Both', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	splits := split.NewRepo(conn)
	dividends := dividend.NewRepo(conn)

	body, err := (&bist.Fetcher{CapitalURL: srv.URL}).FetchPage(ctx)
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	n, err := bist.SyncCapitalEvents(ctx, body, instrument.NewRepo(conn), splits, dividends,
		time.Date(2026, 9, 23, 19, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SyncCapitalEvents: %v", err)
	}
	if n != 6 {
		t.Fatalf("SyncCapitalEvents wrote %d, want 6 (3 splits + 3 dividends)", n)
	}

	wantSplits := map[int64]struct{ date, ratio string }{
		3: {"2026-07-31", "2.338"},
		4: {"2026-09-21", "14"},
		6: {"2026-05-15", "2"},
	}
	for id := int64(1); id <= 6; id++ {
		got, err := splits.List(ctx, id)
		if err != nil {
			t.Fatalf("List %d: %v", id, err)
		}
		w, ok := wantSplits[id]
		if !ok {
			if len(got) != 0 {
				t.Errorf("instrument %d: got splits %+v, want none", id, got)
			}
			continue
		}
		if len(got) != 1 || got[0].Date != w.date || got[0].Ratio.String() != w.ratio || got[0].Source != "isyatirim" {
			t.Errorf("instrument %d: got %+v, want one %s ratio %s source isyatirim", id, got, w.date, w.ratio)
		}
	}

	// Gross TRY per share = SHHE_NAKIT_TM_ORAN / 100.
	gotDivs, err := dividends.ListFor(ctx, []int64{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatalf("ListFor: %v", err)
	}
	wantDivs := map[int64]struct{ date, amount string }{
		1: {"2026-01-05", "0.1698"},
		5: {"2026-10-05", "0.5"},
		6: {"2026-05-15", "10.3799"},
	}
	if len(gotDivs) != len(wantDivs) {
		t.Errorf("got dividends for %d instruments, want %d: %+v", len(gotDivs), len(wantDivs), gotDivs)
	}
	for id, w := range wantDivs {
		got := gotDivs[id]
		if len(got) != 1 || got[0].Date != w.date || got[0].Amount.String() != w.amount ||
			got[0].Currency != "TRY" || got[0].Source != "isyatirim" {
			t.Errorf("instrument %d: got dividends %+v, want one %s amount %s TRY source isyatirim", id, got, w.date, w.amount)
		}
	}
}

// A page without the capital increase table fails instead of storing
// nothing, so a redesign is noticed.
func TestSyncCapitalEventsFailsWithoutTable(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	if _, err := bist.SyncCapitalEvents(context.Background(), "<html><body>redesigned</body></html>", instrument.NewRepo(conn),
		split.NewRepo(conn), dividend.NewRepo(conn), time.Now()); err == nil {
		t.Fatal("SyncCapitalEvents: want error for a page without the table")
	}
}
