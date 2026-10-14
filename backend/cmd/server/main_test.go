package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// A malformed date sorts after today as a string, so it must be rejected
// as malformed before the future-date check.
func TestRunAddSplitRejectsMalformedDate(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	for _, date := range []string{"9999-13-12", "abc", "9999-5-12"} {
		err := runAddSplit(context.Background(), conn, "BIST:THYAO,"+date+",2")
		if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
			t.Errorf("date %q: got %v, want a YYYY-MM-DD error", date, err)
		}
	}
}

// Every name the -fetch help lists resolves to a scheduler source, and the
// list has no source missing.
func TestFindSource(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	sources := schedulerSources(conn, slog.New(slog.NewTextHandler(io.Discard, nil)), newSourceFetchers(nil))
	if len(sources) != len(fetchSourceNames) {
		t.Errorf("schedulerSources has %d sources, fetchSourceNames lists %d", len(sources), len(fetchSourceNames))
	}
	for _, name := range fetchSourceNames {
		if src, ok := findSource(sources, name); !ok || src.Name != name || src.Sync == nil {
			t.Errorf("findSource(%q) = %+v, %v", name, src, ok)
		}
	}
	if _, ok := findSource(sources, "nope"); ok {
		t.Error("findSource(\"nope\") resolved")
	}
}

// A parameter flag without the flag it belongs to, or a one-shot flag set
// to an empty value, is a usage error, not a server start.
// Set maps each flag flag.Visit saw to its value; want is the
// text the message must hold, "" for no error.
func TestOrphanFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  map[string]string
		want string
	}{
		{"date alone", map[string]string{"date": "2026-10-05"}, "-fetch"},
		{"date with empty fetch", map[string]string{"fetch": "", "date": "2026-10-05"}, "-fetch"},
		{"empty fetch", map[string]string{"fetch": ""}, "bist-splits"},
		{"empty backup", map[string]string{"backup": ""}, "-backup"},
		{"empty set-password", map[string]string{"set-password": ""}, "-set-password"},
		{"empty add-split", map[string]string{"add-split": ""}, "-add-split"},
		{"backfill-from alone", map[string]string{"backfill-from": "2025-01-01"}, "-backfill-prices"},
		{"backfill-from with false backfill", map[string]string{"backfill-prices": "false", "backfill-from": "2025-01-01"}, "-backfill-prices"},
		{"fetch with date", map[string]string{"fetch": "bist", "date": "2026-10-05"}, ""},
		{"backfill with from", map[string]string{"backfill-prices": "true", "backfill-from": "2025-01-01"}, ""},
		{"backup with path", map[string]string{"backup": "/data/b.db"}, ""},
		{"no flags", nil, ""},
	} {
		err := orphanFlag(tc.set)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: got %v, want no error", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: got %v, want an error naming %s", tc.name, err, tc.want)
		}
	}
}
