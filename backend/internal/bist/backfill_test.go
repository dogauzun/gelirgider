package bist_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
)

// TestBackfillRangeStoredRangeLogsNothing covers GLG-688: a walk over a
// fully stored range must not log one line per skipped day.
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
// sources page reads that log by scheduler name (GLG-1154).
func TestBulletinBackfillSourceName(t *testing.T) {
	if got := bist.BulletinBackfillSource(nil, nil, nil).Name; got != "bist" {
		t.Fatalf("Name = %q, want bist", got)
	}
}
