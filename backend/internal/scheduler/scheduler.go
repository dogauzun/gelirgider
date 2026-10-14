// Package scheduler runs the daily market-data fetches without an external
// cron: one goroutine ticks once a minute, re-reads the per-source settings
// (see Load) and runs each enabled source whose time of day has passed and
// that has not finished today. CatchUp is the same walk done once at
// startup, so a backend that missed the last scheduled run still has
// today's data.
//
// ponytail: single-instance assumption. Two replicas would both wake up and
// both fetch (harmless, since each source's Sync upserts, but wasteful and
// racy in the logs). Upgrade path when that matters: a lock row in the DB
// (e.g. "INSERT OR IGNORE" into a price_fetch_lock table keyed by date) so
// only one instance wins per day.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// retryCutoffHour is the last hour (Europe/Istanbul) an hourly retry may be
// scheduled for; after this, the day is given up on.
const retryCutoffHour = 23

// SyncFunc fetches and stores data for date, returning the number of rows
// written.
type SyncFunc func(ctx context.Context, date time.Time) (int, error)

// ErrNothingToFetch is returned by a SyncFunc that has nothing to fetch at
// all (e.g. no instrument of its market is tracked), as opposed to zero rows
// meaning "not published yet". CatchUp stops walking back and Run stops
// retrying, both without logging: the source logs its own reason.
var ErrNothingToFetch = errors.New("nothing to fetch")

// The scheduler source names: each Source's Name, the <name> in its
// source.<name>.* settings keys, and the source column of its source_runs
// rows, which the admin sources page reads by this name. A provider's
// on-demand backfill (bist.BackfillSource) logs its runs under the same
// name. They are not the price or fx row source values, which each
// provider package defines next to its fetcher.
const (
	NameBIST       = "bist"
	NameBISTSplits = "bist-splits"
	NameTCMB       = "tcmb"
	NameUS         = "us"
	NameCrypto     = "crypto"
	NameTEFAS      = "tefas"
	NameTUFE       = "tufe"
	NameXU100      = "xu100"
	NameGold       = "gold"
)

// Source is one data source the daily job drives: Name identifies it in log
// lines and settings keys, Sync does the fetch-and-store work.
type Source struct {
	Name string
	Sync SyncFunc
	// DefaultAt is the daily fetch time (HH:MM, Europe/Istanbul) while
	// source.<name>.at is unset or malformed. Empty means DefaultAt.
	DefaultAt string
	// Have optionally reports whether data for date (YYYY-MM-DD) is already
	// stored. CatchUp uses it to stop without fetching when it is already
	// up to date, and Run before a source's first attempt of the day, so a
	// restart does not re-download what CatchUp just stored.
	Have func(ctx context.Context, date string) (bool, error)
	// EveryDay makes Run fetch this source on Saturdays and Sundays too
	// (crypto trades every day). Zero value: weekdays only, since a market
	// closed on weekends publishes nothing and Run would retry it hourly
	// until the cutoff for no row.
	EveryDay bool
	// Backfill optionally fills the source's history up to yesterday from
	// from (YYYY-MM-DD), or from the earliest transaction when from is
	// empty, the same walk as the startup backfill, and returns the rows
	// written. The admin backfill now runs it. nil: no history to fill.
	Backfill func(ctx context.Context, from string) (int, error)
}

// CatchUpLookbackDays bounds how many calendar days CatchUp walks back
// looking for the latest published data: BIST publishes its bulletin around
// 18:30 Europe/Istanbul and weekends/holidays have none, so a week
// comfortably covers any run of non-trading days.
const CatchUpLookbackDays = 7

// CatchUp runs every source once, in order, so a backend started after a
// source's fetch time (or after a stretch of downtime) has today's data
// without waiting for the next evening run. Each source walks back from
// today (Europe/Istanbul) over at most lookbackDays calendar days until
// Sync writes a row. An error from one source is logged and the next source
// still runs. Each source's walk is one run in runs (nil logs none). Every
// log line starts with prefix ("catch-up" at startup), so a caller reusing
// the walk outside the startup catch-up names its own event.
func CatchUp(ctx context.Context, sources []Source, lookbackDays int, runs *RunLog, prefix string, logger *slog.Logger) {
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		runs.Record(ctx, src.Name, KindCatchUp, func() (int, error) {
			return catchUpSource(ctx, src, lookbackDays, time.Now().In(price.Istanbul), prefix, logger)
		})
	}
}

// catchUpSource walks back from today over at most lookbackDays calendar
// days, returning as soon as src already has a date's data (src.Have) or
// Sync writes at least one row for it. Zero rows with no error means the
// date has nothing published, so the walk tries the day before. It returns
// the rows written and the error that ended the walk. An error after ctx is
// cancelled (shutdown, or the client went away) is not logged: nothing
// failed. ctx.Err() decides, not the error: net/http returns
// signal.NotifyContext's cancel cause, which does not wrap context.Canceled,
// while ctx.Err() is context.Canceled. A passed
// deadline (the on-demand fetch's timeout) is a failure and is logged.
func catchUpSource(ctx context.Context, src Source, lookbackDays int, today time.Time, prefix string, logger *slog.Logger) (int, error) {
	for i := 0; i < lookbackDays; i++ {
		d := today.AddDate(0, 0, -i)
		date := d.Format(time.DateOnly)
		if err := ctx.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Error(prefix+": fetch failed", "source", src.Name, "date", date, "error", err)
			}
			return 0, err
		}

		if src.Have != nil {
			has, err := src.Have(ctx, date)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return 0, err
				}
				logger.Error(prefix+": check existing failed", "source", src.Name, "date", date, "error", err)
				return 0, err
			}
			if has {
				logger.Info(prefix+": already stored, skipping fetch", "source", src.Name, "date", date)
				return 0, nil
			}
		}

		written, err := src.Sync(ctx, d)
		if errors.Is(err, ErrNothingToFetch) {
			return written, err
		}
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return written, err
			}
			logger.Error(prefix+": fetch failed", "source", src.Name, "date", date, "error", err)
			return written, err
		}
		if written > 0 {
			logger.Info(prefix+": fetch complete", "source", src.Name, "date", date, "written", written)
			return written, nil
		}
	}
	logger.Warn(prefix+": nothing published in the lookback window", "source", src.Name, "days_checked", lookbackDays)
	return 0, nil
}

// Run ticks once a minute until ctx is canceled. Every tick re-reads the
// settings (Load), so a change applies without a restart, and runs each due
// source (see runner.tick). Per ADR 0003, sources run in sequence, not
// concurrently; each has its own retry state, so one source's retries do
// not change another's cutoff and one source hard-failing does not stop the
// rest. Every Sync attempt is one run in runs (nil logs none).
func Run(ctx context.Context, repo *settings.Repo, runs *RunLog, sources []Source, logger *slog.Logger) {
	r := &runner{repo: repo, runs: runs, sources: sources, loc: price.Istanbul, logger: logger, state: map[string]runState{}}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		r.tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runner is Run's state between ticks.
type runner struct {
	repo    *settings.Repo
	runs    *RunLog
	sources []Source
	loc     *time.Location
	logger  *slog.Logger
	// state is in memory only: after a restart, Have and a run in runs
	// since today's fetch time that wrote rows or failed (e.g. CatchUp's)
	// keep a source from fetching again what is already stored or retrying
	// what already failed today.
	state map[string]runState
}

// runState is one source's progress on day (YYYY-MM-DD, Europe/Istanbul).
type runState struct {
	day     string
	done    bool      // succeeded, hard-failed or gave up for day
	retryAt time.Time // zero, or the earliest next attempt after zero rows
}

// tick runs, in order, every source due at now: enabled, due on this
// weekday (dueSources), past its fetch time today, not done today and past
// its retry time. A tick after shutdown does nothing: Load would log every
// settings read as failed.
func (r *runner) tick(ctx context.Context, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	cfg := Load(ctx, r.repo, r.sources, r.logger)
	now = now.In(r.loc)
	day := now.Format(time.DateOnly)
	for _, src := range dueSources(r.sources, now) {
		if ctx.Err() != nil {
			return
		}
		sc := cfg.Sources[src.Name]
		st := r.state[src.Name]
		if st.day != day {
			st = runState{day: day}
		}
		at, _ := time.Parse("15:04", sc.At) // Load already validated it
		atToday := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, r.loc)
		if !sc.Enabled || st.done || now.Before(atToday) || now.Before(st.retryAt) {
			continue
		}
		r.state[src.Name] = r.attempt(ctx, src, now, atToday, st)
	}
}

// attempt runs src once for now's day and returns its new state. Zero rows
// is retried hourly until retryCutoffHour; an error is logged and not
// retried until the next day, or, after shutdown, neither logged nor
// marked done. A source already running (an admin's run
// now) is left as it was, so the next tick tries again. The first attempt
// of the day is skipped when src already has the day's data (Have) or a
// run since atToday wrote rows or failed (the error case is done for the
// day either way).
func (r *runner) attempt(ctx context.Context, src Source, now, atToday time.Time, st runState) runState {
	if st.retryAt.IsZero() {
		if src.Have != nil {
			if has, err := src.Have(ctx, st.day); err == nil && has {
				r.logger.Info("fetch: already stored, skipping", "source", src.Name, "date", st.day)
				st.done = true
				return st
			}
		}
		if ok, err := r.runs.RanSince(ctx, src.Name, atToday); err == nil && ok {
			r.logger.Info("fetch: already ran since fetch time, skipping", "source", src.Name, "date", st.day)
			st.done = true
			return st
		}
	}
	written, err := r.runs.Record(ctx, src.Name, KindScheduled, func() (int, error) { return src.Sync(ctx, now) })
	switch {
	case errors.Is(err, ErrAlreadyRunning):
		// An admin's run now holds the source: try again next tick.
		r.logger.Info("fetch: already running, retrying next minute", "source", src.Name, "date", st.day)
	case errors.Is(err, ErrNothingToFetch):
		st.done = true
	case err != nil && ctx.Err() != nil:
		// Shutdown: nothing failed and the day is not done. ctx.Err()
		// decides, not err: net/http returns signal.NotifyContext's cancel
		// cause, which does not wrap context.Canceled.
	case err != nil:
		r.logger.Error("fetch failed", "source", src.Name, "date", st.day, "error", err)
		st.done = true
	case written > 0:
		r.logger.Info("fetch complete", "source", src.Name, "date", st.day, "written", written)
		st.done = true
	default:
		st.retryAt = now.Add(time.Hour)
		cutoff := time.Date(now.Year(), now.Month(), now.Day(), retryCutoffHour, 0, 0, 0, r.loc)
		if !st.retryAt.Before(cutoff) {
			r.logger.Info("fetch: not yet available, giving up for today", "source", src.Name, "date", st.day)
			st.done = true
		} else {
			r.logger.Info("fetch: not yet available, retrying in an hour", "source", src.Name, "date", st.day)
		}
	}
	return st
}

// dueSources returns the sources Run fetches on day: all of them on a
// weekday, only those with EveryDay set on a Saturday or Sunday.
func dueSources(sources []Source, day time.Time) []Source {
	if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
		return sources
	}
	var due []Source
	for _, src := range sources {
		if src.EveryDay {
			due = append(due, src)
		}
	}
	return due
}
