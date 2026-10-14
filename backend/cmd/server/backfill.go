package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/us"
)

// runBackfill backfills the history of every scheduler source (see
// backfillHistory) and, unless there was no history range to walk at all,
// the latest US prices (see backfillUS), whether or not a history source
// failed. This is what -backfill-prices runs; any failure makes its error
// non-nil. A nil f.us (no SECRET_KEY to read the US provider keys with)
// skips US and backfills every other source; a nil f.xu100 skips
// XU100 the same way, and a nil f.tefas (the tefas source off) TEFAS.
func runBackfill(ctx context.Context, logger *slog.Logger, conn *sql.DB, f sourceFetchers, from string) error {
	disabled := map[string]bool{scheduler.NameUS: f.us == nil, scheduler.NameXU100: f.xu100 == nil, scheduler.NameTEFAS: f.tefas == nil}
	walked, err := backfillHistory(ctx, logger, conn, f, from, disabled)
	if !walked || f.us == nil {
		return err
	}
	return errors.Join(err, backfillUS(ctx, logger, conn, f.us, f.now()))
}

// backfillHistory runs the Backfill walk of every scheduler source that has
// one (schedulerSources, in its order) except tufe, each from the start
// date from, or from its own earliest transaction when from is empty, up
// to the day before f.now(), each skipping what it already has. A
// source name set in disabled is skipped; nil skips none. XU100 is also
// skipped without an EVDS key: no key is a skip, not a failed
// run, while an unreadable key still runs, so FetchXU100's error fails it.
// walked is false when there is no range to walk (no transactions, or a
// start date after yesterday); it stays true when a source failed.
// Finnhub's latest close is not part of it: see backfillUS. Each source's
// walk is one backfill run in the run log (source_runs), and runs whatever
// happened to the ones before it: err joins every source's
// error. A cancelled or expired ctx is the exception: the walk stops
// before the next source and err includes ctx's error.
func backfillHistory(ctx context.Context, logger *slog.Logger, conn *sql.DB, f sourceFetchers, from string, disabled map[string]bool) (bool, error) {
	start, ok, err := backfillStartDate(ctx, transaction.NewRepo(conn), from, "")
	if err != nil {
		return false, err
	}
	if !ok {
		logger.Info("backfill: no transactions found, nothing to backfill")
		return false, nil
	}

	end := price.Yesterday(f.now())
	if start.After(end) {
		logger.Info("backfill: start date is after yesterday, nothing to do",
			"start", start.Format(time.DateOnly), "end", end.Format(time.DateOnly))
		return false, nil
	}

	runs := scheduler.NewRunLog(conn)
	var errs []error
	for _, src := range schedulerSources(conn, logger, f) {
		// tufe's Backfill is its Sync, one download of the whole CPI
		// history, which the startup catch-up already runs.
		if src.Backfill == nil || disabled[src.Name] || src.Name == scheduler.NameTUFE {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if src.Name == scheduler.NameXU100 {
			if ok, err := f.xu100.HasKey(ctx); !ok && err == nil {
				logger.Info("backfill: no EVDS API key, skipping xu100")
				continue
			}
		}
		if _, err := runs.Record(ctx, src.Name, scheduler.KindBackfill, func() (int, error) { return src.Backfill(ctx, from) }); err != nil {
			errs = append(errs, fmt.Errorf("backfill %s: %w", src.Name, err))
		}
	}

	if err := ctx.Err(); err != nil {
		errs = append(errs, fmt.Errorf("backfill stopped: %w", err))
	}
	return true, errors.Join(errs...)
}

// backfillRun holds the values that are the same for every source of one
// backfill run: the repos the walks read, the start date override from (""
// for each source's own earliest transaction) and the run's now.
type backfillRun struct {
	logger       *slog.Logger
	instruments  *instrument.Repo
	prices       *price.Repo
	transactions *transaction.Repo
	from         string
	now          time.Time
}

// backfillDates walks src over every weekday from the backfill start date
// (r.from, or the earliest transaction in market, "" for any) to the day
// before r.now; it is the bist and tcmb sources' Backfill. No transaction,
// or a start after yesterday, is nothing to walk.
func (r backfillRun) backfillDates(ctx context.Context, src bist.BackfillSource, market string) (int, error) {
	start, ok, err := backfillStartDate(ctx, r.transactions, r.from, market)
	if err != nil || !ok {
		return 0, err
	}
	end := price.Yesterday(r.now)
	if start.After(end) {
		return 0, nil
	}
	return bist.BackfillRange(ctx, r.logger, src, start, end)
}

// backfillTracked runs sync once for every tracked instrument in market,
// from its own earliest transaction date (or r.from; "" when it has none),
// and returns the rows written. sync reports skipped when it made no
// request because the range is already stored. The loop is
// instrument.EachTracked: it pauses for pause between requests, logs a
// failing instrument and continues, makes the run a failed one when every
// attempted instrument fails, naming them by noun ("funds"), and
// stops on a cancelled ctx with ctx's error and no WARN.
func (r backfillRun) backfillTracked(ctx context.Context, market, noun string, pause time.Duration, sync func(in instrument.Instrument, start string) (written int, skipped bool, err error)) (int, error) {
	tracked, err := r.instruments.Tracked(ctx, market)
	if err != nil {
		return 0, fmt.Errorf("list tracked %s instruments: %w", market, err)
	}
	earliest, err := r.transactions.EarliestDateByInstrument(ctx, market)
	if err != nil {
		return 0, err
	}
	written, _, err := instrument.EachTracked(ctx, tracked, pause, noun, func(in instrument.Instrument) (int, bool, error) {
		start := earliest[in.ID]
		if r.from != "" {
			start = r.from
		}
		n, skipped, err := sync(in, start)
		if !skipped && err == nil {
			r.logger.Info("backfill: synced", "market", market, "symbol", in.Symbol, "start", start, "written", n)
		}
		return n, skipped, err
	})
	return written, err
}

// skippedIfEmpty adapts a history sync that returns (0, nil) for a range
// it already has to backfillTracked: (0, nil) counts as skipped. A request
// that succeeded with no rows counts as skipped too; the history syncs
// cannot tell the two apart.
func skippedIfEmpty(written int, err error) (int, bool, error) {
	return written, written == 0 && err == nil, err
}

// backfillTEFAS backfills every tracked TEFAS fund's NAVs (see
// backfillTracked) with one request per fund, via tefas.Syncer.SyncHistory: TEFAS
// serves a months-back window per fund, not a date, so there is no
// per-date walk. A fund whose prices already reach back that far costs one
// small query and no request.
func backfillTEFAS(ctx context.Context, r backfillRun, s *tefas.Syncer) (int, error) {
	return r.backfillTracked(ctx, "TEFAS", "funds", tefas.RequestPause, func(in instrument.Instrument, start string) (int, bool, error) {
		return skippedIfEmpty(s.SyncHistory(ctx, in, start, r.now))
	})
}

// backfillCrypto backfills every tracked CRYPTO pair's daily closes (see
// backfillTracked) with one BtcTurk request per pair: klines cover any
// range in one call, weekends included, so there is no per-date walk and
// no bist.BackfillRange (it skips weekends). The request runs up to now;
// the fetcher itself drops the unfinished current day, so the newest row
// is yesterday's (UTC, the candle date). A pair without a start date, or
// that already has a row for every day of that range, is skipped.
func backfillCrypto(ctx context.Context, r backfillRun, f *btcturk.Fetcher) (int, error) {
	last := btcturk.LastCandleDay(r.now)
	return r.backfillTracked(ctx, "CRYPTO", "pairs", btcturk.RequestPause, func(in instrument.Instrument, startStr string) (int, bool, error) {
		if startStr == "" {
			return 0, true, nil
		}
		start, err := time.Parse(time.DateOnly, startStr)
		if err != nil {
			return 0, false, fmt.Errorf("parse crypto start date %q: %w", startStr, err)
		}
		if start.After(last) {
			return 0, true, nil
		}
		stored, err := btcturk.PricedEveryDay(ctx, r.prices, in.ID, start, last)
		if err != nil || stored {
			return 0, stored, err
		}
		written, err := btcturk.SyncRange(ctx, f, r.prices, in, start, r.now)
		return written, false, err
	})
}

// backfillUSHistory backfills every tracked US instrument's Tiingo daily
// closes (see backfillTracked), one request per instrument, via
// us.SyncHistory. An instrument whose range is already stored
// costs one small query and no request, so this is cheap on every start.
func backfillUSHistory(ctx context.Context, r backfillRun, s *us.Syncer) (int, error) {
	return r.backfillTracked(ctx, "US", "us instruments", us.RequestPause, func(in instrument.Instrument, start string) (int, bool, error) {
		return skippedIfEmpty(s.SyncHistory(ctx, in, start))
	})
}

// backfillUS fetches US prices once, not per date. Unlike the BIST bulletin
// and TCMB's XML archive, Finnhub's /quote endpoint has no historical-range
// parameter: it only ever answers with the latest close, so walking
// start..end the way runBackfillRange does would just fetch the same value
// once per weekday for no benefit (see docs/adr/0003, "Known risks"). One
// Sync call covers every tracked US symbol (it already loops per symbol
// internally), each close stored under its own quote's session date.
func backfillUS(ctx context.Context, logger *slog.Logger, conn *sql.DB, usFetcher *us.Fetcher, now time.Time) error {
	written, err := us.Sync(ctx, usFetcher, instrument.NewRepo(conn), price.NewRepo(conn), now)
	if err != nil {
		return fmt.Errorf("sync us: %w", err)
	}
	logger.Info("backfill: synced", "source", us.SourceFinnhub, "written", written)
	return nil
}

// startupBackfillTimeout bounds the startup history walk. A first run over
// years of weekdays takes minutes, not hours; the timeout is only there so
// a wedged walk cannot hold a connection open forever. Same value as
// internal/server's onDemandBackfillTimeout.
const startupBackfillTimeout = 2 * time.Hour

// backfillStartupHistory fills the BIST/TCMB/crypto history from the
// earliest transaction date in the background at startup, so a dev database
// or one restored after downtime needs no manual -backfill-prices run. BIST
// and TCMB dates that already have a source row, or are remembered holidays,
// are skipped with one small query each; crypto costs one request per
// tracked pair whose range has a missing day; US
// history costs one request per tracked symbol whose range is not stored
// yet; XU100 costs one EVDS request per year of each missing stretch,
// none when its range is stored. Finnhub's
// latest close is skipped: the startup catch-up already fetched it. sc
// gives the start date override and the disabled sources.
//
// ponytail: this walk and internal/server's on-demand backfill can run at
// the same time and both fetch the same date; the ceiling is a few
// duplicate downloads, since each date's upsert is idempotent and both
// walks skip dates that already have a source row. Upgrade path: one
// shared backfill worker fed by a queue, if the duplicates ever cost
// anything.
func backfillStartupHistory(ctx context.Context, logger *slog.Logger, conn *sql.DB, sc scheduler.Config, settingsRepo *settings.Repo) {
	ctx, cancel := context.WithTimeout(ctx, startupBackfillTimeout)
	defer cancel()

	disabled := map[string]bool{}
	for name, s := range sc.Sources {
		disabled[name] = !s.Enabled
	}
	// A shutdown mid-walk cancels ctx: nothing failed, so nothing is
	// logged. The timeout above is a real failure and is.
	if _, err := backfillHistory(ctx, logger, conn, newSourceFetchers(settingsRepo), sc.BackfillFrom, disabled); err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		logger.Error("startup backfill failed", "error", err)
	}
}

// backfillStartDate resolves the backfill's start date. from, if non-empty,
// overrides the earliest-transaction-date lookup; otherwise it is the
// earliest transaction on an instrument in market (any transaction when
// market is empty). ok is false only when from is empty and no transaction
// matches.
func backfillStartDate(ctx context.Context, transactions *transaction.Repo, from, market string) (time.Time, bool, error) {
	if from != "" {
		d, err := price.ParseDate(from)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("invalid -backfill-from date %q: %w", from, err)
		}
		return bist.ClampBackfillStart(d), true, nil
	}

	earliest, ok, err := transactions.EarliestDate(ctx, market)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("find earliest transaction date: %w", err)
	}
	if !ok {
		return time.Time{}, false, nil
	}
	d, err := price.ParseDate(earliest)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse earliest transaction date %q: %w", earliest, err)
	}
	return bist.ClampBackfillStart(d), true, nil
}
