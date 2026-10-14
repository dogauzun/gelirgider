package bist

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
)

// BackfillMinDate is the earliest date the bulletin fetcher can read: BIST
// bulletins before this date are .xls, not the .zip/.csv this package
// parses (see docs/adr/0001-bist-price-source.md).
const BackfillMinDate = "2015-11-30"

// ClampBackfillStart returns start, or BackfillMinDate as an Istanbul
// midnight if start is earlier.
func ClampBackfillStart(start time.Time) time.Time {
	first, _ := price.ParseDate(BackfillMinDate)
	if start.Before(first) {
		return first
	}
	return start
}

// maxHolidayRun is the longest run of consecutive empty weekdays
// BackfillRange remembers as holidays. Turkish market closures run at
// most a few weekdays; a longer empty run looks more like a source outage,
// so it is left unmarked and retried on the next walk.
const maxHolidayRun = 5

// BackfillSource is one data source's date-scoped operations for
// BackfillRange: whether a date is already synced, how to fetch and store
// one, and (optional) how to remember a date that had no data.
type BackfillSource struct {
	Name      string
	HasSource func(ctx context.Context, date string) (bool, error)
	Sync      func(ctx context.Context, date time.Time) (int, error)
	MarkEmpty func(ctx context.Context, date string) error
}

// BulletinBackfillSource is the bulletin as a BackfillSource, walked one
// weekday at a time, for the startup backfill and the on-demand one alike.
// Its Name is scheduler.NameBIST, the scheduler source name, since the
// on-demand backfill queue logs the walk in source_runs under it; the price
// rows it checks and marks are SourceBistBulletin's.
func BulletinBackfillSource(f *Fetcher, instruments *instrument.Repo, prices *price.Repo) BackfillSource {
	return BackfillSource{
		Name: scheduler.NameBIST,
		HasSource: func(ctx context.Context, date string) (bool, error) {
			return prices.HasSource(ctx, date, SourceBistBulletin)
		},
		Sync: func(ctx context.Context, d time.Time) (int, error) {
			return Sync(ctx, f, instruments, prices, d)
		},
		MarkEmpty: func(ctx context.Context, date string) error {
			return prices.MarkEmpty(ctx, date, SourceBistBulletin)
		},
	}
}

// BackfillRange walks every weekday in [start, end], skipping dates
// src.HasSource already reports as present, and calling src.Sync for the
// rest with RequestPause between requests. It returns the rows written and
// stops on the first sync failure (a "not available yet" result is 0 rows
// and a nil error, handled inside src.Sync the same way the daily scheduler
// treats it, so it is not a failure) — so a partial run can be resumed by
// running again. Shared by the -backfill-prices flag and the on-demand
// backfill after a transaction is saved. A stored date is
// skipped without a log line, so a walk with nothing to fetch logs nothing,
// like the gold and xu100 backfills.
//
// A date that returned no data is a holiday once a later date in the walk
// is synced, since the source then demonstrably serves data: it
// is passed to src.MarkEmpty so the next walk skips it instead of
// requesting it again. Empty dates at the end of the range may just not be
// published yet, so they stay unmarked.
func BackfillRange(ctx context.Context, logger *slog.Logger, src BackfillSource, start, end time.Time) (int, error) {
	total := 0
	var empty []string // empty dates since the last synced one
	markEmpty := func() {
		if src.MarkEmpty != nil && len(empty) <= maxHolidayRun {
			for _, e := range empty {
				if err := src.MarkEmpty(ctx, e); err != nil {
					logger.Warn("backfill: mark empty date failed", "source", src.Name, "date", e, "error", err)
				}
			}
		}
		empty = nil
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		dateStr := d.Format(time.DateOnly)

		has, err := src.HasSource(ctx, dateStr)
		if err != nil {
			return total, fmt.Errorf("check existing %s for %s: %w", src.Name, dateStr, err)
		}
		if has {
			markEmpty() // no log line: a stored range is silent
			continue
		}

		written, err := src.Sync(ctx, d)
		if err != nil {
			return total, fmt.Errorf("sync %s %s: %w", src.Name, dateStr, err)
		}
		if written == 0 {
			empty = append(empty, dateStr)
		} else {
			markEmpty()
		}
		total += written
		logger.Info("backfill: synced", "source", src.Name, "date", dateStr, "written", written)

		select {
		case <-ctx.Done():
			return total, ctx.Err() // stopped during the pause
		case <-time.After(RequestPause):
		}
	}
	return total, nil
}
