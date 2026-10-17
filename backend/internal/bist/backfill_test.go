package bist_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// TestBackfillRangeStoredRangeLogsNothing checks that a walk over a
// fully stored range does not log one line per skipped day.
func TestBackfillRangeStoredRangeLogsNothing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	src := bist.BackfillSource{
		Name:      "test",
		HasSource: func(context.Context, string) (bool, error) { return true, nil },
		Sync: func(context.Context, time.Time) (int, error) {
			t.Fatal("Sync called for a stored date")
			return 0, nil
		},
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	written, err := bist.BackfillRange(context.Background(), logger, src, start, start.AddDate(0, 0, 30))
	if err != nil || written != 0 {
		t.Fatalf("BackfillRange = %d, %v; want 0, nil", written, err)
	}
	if buf.Len() > 0 {
		t.Fatalf("stored range logged:\n%s", buf.String())
	}
}

// TestBulletinBackfillSourceName pins the bulletin walk's Name to the
// scheduler source name, not the prices source value: the on-demand
// backfill queue logs the walk in source_runs under it, and the admin
// sources page reads that log by scheduler name.
func TestBulletinBackfillSourceName(t *testing.T) {
	if got := bist.BulletinBackfillSource(nil, nil, nil).Name; got != "bist" {
		t.Fatalf("Name = %q, want bist", got)
	}
}

// listCount counts the BIST instrument listings (instrument.Repo.All) made
// through countingDriver.
var listCount atomic.Int32

// countingDriver wraps a SQL driver and counts the statements that list the
// BIST instruments. database/sql falls back to Prepare for a wrapped conn
// that has no context methods, so counting Prepare sees every statement.
type countingDriver struct{ driver.Driver }

func (d countingDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	return countingConn{c}, err
}

type countingConn struct{ driver.Conn }

func (c countingConn) Prepare(query string) (driver.Stmt, error) {
	if strings.Contains(query, "FROM instruments WHERE market = ? ORDER BY symbol") {
		listCount.Add(1)
	}
	return c.Conn.Prepare(query)
}

// TestBulletinBackfillSourceListsInstrumentsOnce walks two dates with data
// and checks the BIST instruments are listed once for the whole walk, not
// once per date, and that both dates' prices are stored.
func TestBulletinBackfillSourceListsInstrumentsOnce(t *testing.T) {
	// The bulletin of each date carries that date in its rows.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		day := "2026-09-11"
		if strings.Contains(r.URL.Path, "20260910") {
			day = "2026-09-10"
		}
		w.Write(bulletinZip(t, strings.ReplaceAll(bulletinCSV, "2026-09-11", day)))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	sql.Register("counting-sqlite", countingDriver{conn.Driver()})
	counted, err := sql.Open("counting-sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer counted.Close()

	src := bist.BulletinBackfillSource(&bist.Fetcher{BaseURL: srv.URL + "/"}, instrument.NewRepo(counted), price.NewRepo(counted))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) // a Thursday
	listCount.Store(0)
	written, err := bist.BackfillRange(context.Background(), logger, src, start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("BackfillRange: %v", err)
	}
	if written != 8 {
		t.Fatalf("BackfillRange wrote %d prices, want 8 (4 per date)", written)
	}
	if n := listCount.Load(); n != 1 {
		t.Fatalf("instruments listed %d times for a two-date walk, want 1", n)
	}
}
