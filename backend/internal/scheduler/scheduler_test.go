package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// TestDueSourcesWeekendOnlyEveryDay covers the weekend rule: a weekday runs
// every source, a Saturday or Sunday only the EveryDay ones.
func TestDueSourcesWeekendOnlyEveryDay(t *testing.T) {
	sources := []Source{{Name: "bist"}, {Name: "crypto", EveryDay: true}, {Name: "tcmb"}}

	fri := time.Date(2026, 9, 11, 19, 0, 0, 0, time.UTC)
	if got := dueSources(sources, fri); len(got) != 3 {
		t.Fatalf("Friday: %d sources due, want all 3", len(got))
	}
	for _, day := range []time.Time{fri.AddDate(0, 0, 1), fri.AddDate(0, 0, 2)} {
		got := dueSources(sources, day)
		if len(got) != 1 || got[0].Name != "crypto" {
			t.Fatalf("%s: due = %v, want only crypto", day.Weekday(), got)
		}
	}
}

// newTestRunner returns a runner over sources with a settings repo on a
// fresh database.
func newTestRunner(t *testing.T, sources []Source) (*runner, *settings.Repo) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	repo := settings.NewRepo(conn, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &runner{repo: repo, sources: sources, loc: price.Istanbul, logger: logger, state: map[string]runState{}}, repo
}

// TestTickFollowsLiveSettings covers the minute loop with an injected clock:
// a source runs once after its time, not before; a disabled source never
// runs; a time changed between ticks applies without a restart; a
// malformed time falls back to the default.
func TestTickFollowsLiveSettings(t *testing.T) {
	ctx := context.Background()
	calls := map[string]int{}
	src := func(name string) Source {
		return Source{Name: name, Sync: func(ctx context.Context, date time.Time) (int, error) {
			calls[name]++
			return 1, nil
		}}
	}
	r, repo := newTestRunner(t, []Source{src("a"), src("off"), src("bad")})
	set := func(key, value string) {
		if err := repo.Set(ctx, key, value); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	set(EnabledKey("off"), "false")
	set(AtKey("bad"), "25:99")

	// Tuesday 2026-09-08, Europe/Istanbul.
	at := func(hh, mm int) time.Time { return time.Date(2026, 9, 8, hh, mm, 0, 0, r.loc) }

	r.tick(ctx, at(18, 59))
	if calls["a"] != 0 || calls["bad"] != 0 {
		t.Fatalf("before 19:00: calls = %v, want none", calls)
	}
	r.tick(ctx, at(19, 0))
	r.tick(ctx, at(19, 1))
	if calls["a"] != 1 || calls["bad"] != 1 || calls["off"] != 0 {
		t.Fatalf("after 19:00: calls = %v, want a and bad once, off never", calls)
	}

	// Next day, the time moves to 20:00 between ticks, no new runner.
	next := func(hh, mm int) time.Time { return at(hh, mm).AddDate(0, 0, 1) }
	r.tick(ctx, next(18, 0))
	set(AtKey("a"), "20:00")
	r.tick(ctx, next(19, 30))
	if calls["a"] != 1 {
		t.Fatalf("next day 19:30 with at=20:00: a calls = %d, want still 1", calls["a"])
	}
	r.tick(ctx, next(20, 0))
	if calls["a"] != 2 {
		t.Fatalf("next day 20:00: a calls = %d, want 2", calls["a"])
	}
	if calls["off"] != 0 {
		t.Fatalf("disabled source ran %d times", calls["off"])
	}
}

// TestRunAfterShutdownLogsNoSettingsErrors checks that Run, handed an already
// cancelled context because a shutdown cut the startup catch-up short, returns
// without logging every settings read as failed.
func TestRunAfterShutdownLogsNoSettingsErrors(t *testing.T) {
	r, repo := newTestRunner(t, []Source{{Name: "a"}, {Name: "b"}})
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	Run(ctx, repo, nil, r.sources, logger)

	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("unexpected ERROR log after shutdown: %s", buf.String())
	}
}

// TestTickRetriesHourlyAndHardFailureDoesNotBlockNext covers the retry
// rules: zero rows retries an hour later, an error is not retried that
// day, and one source failing does not stop the next.
func TestTickRetriesHourlyAndHardFailureDoesNotBlockNext(t *testing.T) {
	ctx := context.Background()
	failCalls, emptyCalls := 0, 0
	r, _ := newTestRunner(t, []Source{
		{Name: "fail", Sync: func(ctx context.Context, date time.Time) (int, error) {
			failCalls++
			return 0, errors.New("boom")
		}},
		{Name: "empty", Sync: func(ctx context.Context, date time.Time) (int, error) {
			emptyCalls++
			return 0, nil
		}},
	})
	at := func(hh, mm int) time.Time { return time.Date(2026, 9, 8, hh, mm, 0, 0, r.loc) }

	r.tick(ctx, at(19, 0))
	r.tick(ctx, at(19, 30))
	if failCalls != 1 || emptyCalls != 1 {
		t.Fatalf("19:30: fail=%d empty=%d, want 1 and 1", failCalls, emptyCalls)
	}
	r.tick(ctx, at(20, 0))
	if failCalls != 1 || emptyCalls != 2 {
		t.Fatalf("20:00: fail=%d empty=%d, want 1 and 2", failCalls, emptyCalls)
	}
}

// TestTickSkipsSourceCaughtUpToday covers a source with no Have (like
// bist-splits): a catch-up that wrote rows after today's fetch time keeps
// the first tick from syncing it again.
func TestTickSkipsSourceCaughtUpToday(t *testing.T) {
	ctx := context.Background()
	calls := 0
	r, repo := newTestRunner(t, []Source{{Name: "splits", EveryDay: true, Sync: func(ctx context.Context, date time.Time) (int, error) {
		calls++
		return 5, nil
	}}})
	if err := repo.Set(ctx, AtKey("splits"), "00:00"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	r.runs = NewRunLog(conn)

	CatchUp(ctx, r.sources, CatchUpLookbackDays, r.runs, "catch-up", r.logger)
	if calls != 1 {
		t.Fatalf("catch-up: calls = %d, want 1", calls)
	}
	r.tick(ctx, time.Now())
	if calls != 1 {
		t.Fatalf("first tick after catch-up: calls = %d, want still 1", calls)
	}
}

// TestTickSkipsSourceWhoseCatchUpFailedToday checks that a
// catch-up that ended in an error after today's fetch time is
// done for the day, so the first tick does not fetch again.
func TestTickSkipsSourceWhoseCatchUpFailedToday(t *testing.T) {
	ctx := context.Background()
	calls := 0
	r, repo := newTestRunner(t, []Source{{Name: "bist", EveryDay: true, Sync: func(ctx context.Context, date time.Time) (int, error) {
		calls++
		return 0, errors.New("Forbidden")
	}}})
	if err := repo.Set(ctx, AtKey("bist"), "00:00"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	r.runs = NewRunLog(conn)

	CatchUp(ctx, r.sources, CatchUpLookbackDays, r.runs, "catch-up", r.logger)
	if calls != 1 {
		t.Fatalf("catch-up: calls = %d, want 1", calls)
	}
	r.tick(ctx, time.Now())
	if calls != 1 {
		t.Fatalf("first tick after failed catch-up: calls = %d, want still 1", calls)
	}
}

// TestTickRunsSourceAfterBackfillToday checks that a backfill that wrote
// rows after today's fetch time, having walked only past dates, does not
// settle the day, so the tick still fetches.
func TestTickRunsSourceAfterBackfillToday(t *testing.T) {
	ctx := context.Background()
	calls := 0
	r, repo := newTestRunner(t, []Source{{Name: "bist", EveryDay: true, Sync: func(ctx context.Context, date time.Time) (int, error) {
		calls++
		return 5, nil
	}}})
	if err := repo.Set(ctx, AtKey("bist"), "00:00"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	r.runs = NewRunLog(conn)

	if _, err := r.runs.Record(ctx, "bist", KindBackfill, func() (int, error) { return 3, nil }); err != nil {
		t.Fatalf("Record backfill: %v", err)
	}
	r.tick(ctx, time.Now())
	if calls != 1 {
		t.Fatalf("tick after backfill: calls = %d, want 1", calls)
	}
}

// TestTickRunsSourceWhoseRunWasStoppedByShutdown checks that a run cut
// short by shutdown (context.Canceled) is not a failure and does not settle
// the day, so the first tick after a restart still fetches.
func TestTickRunsSourceWhoseRunWasStoppedByShutdown(t *testing.T) {
	ctx := context.Background()
	calls := 0
	r, repo := newTestRunner(t, []Source{{Name: "bist", EveryDay: true, Sync: func(ctx context.Context, date time.Time) (int, error) {
		calls++
		return 5, nil
	}}})
	if err := repo.Set(ctx, AtKey("bist"), "00:00"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	r.runs = NewRunLog(conn)

	r.runs.Record(ctx, "bist", KindScheduled, func() (int, error) {
		return 0, fmt.Errorf("fetch bulletin: %w", context.Canceled)
	})
	if st, err := r.runs.Latest(ctx); err != nil || st["bist"].LastRun != nil {
		t.Fatalf("Latest after canceled run = %+v, %v; want no run", st["bist"].LastRun, err)
	}
	r.tick(ctx, time.Now())
	if calls != 1 {
		t.Fatalf("first tick after canceled run: calls = %d, want 1", calls)
	}
}

// TestRecordDeletesRunStoppedByShutdownWithCause checks that a run stopped by
// shutdown is still dropped: since Go 1.26 signal.NotifyContext cancels with a
// cause, and net/http returns that cause, which does not wrap context.Canceled.
func TestRecordDeletesRunStoppedByShutdownWithCause(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	runs := NewRunLog(conn)

	ctx, cancel := context.WithCancelCause(context.Background())
	runs.Record(ctx, "bist", KindCatchUp, func() (int, error) {
		cancel(errors.New("terminated signal received"))
		return 0, fmt.Errorf("get: %w", context.Cause(ctx))
	})
	if st, err := runs.Latest(context.Background()); err != nil || st["bist"].LastRun != nil {
		t.Fatalf("Latest after run stopped by shutdown = %+v, %v; want no run", st["bist"].LastRun, err)
	}
}

// TestClearUnfinishedDropsRunCutOffByHardKill checks that ClearUnfinished drops
// a run whose process was killed and never reached Finish, so after a restart
// Latest reports the last finished run instead of a running one.
func TestClearUnfinishedDropsRunCutOffByHardKill(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	old := NewRunLog(conn)
	old.Record(ctx, "bist", KindScheduled, func() (int, error) { return 5, nil })
	if _, err := old.Start(ctx, "bist", KindBackfill); err != nil { // killed before Finish
		t.Fatalf("Start: %v", err)
	}

	runs := NewRunLog(conn) // the restarted process
	if err := runs.ClearUnfinished(ctx); err != nil {
		t.Fatalf("ClearUnfinished: %v", err)
	}
	st, err := runs.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if last := st["bist"].LastRun; last == nil || last.FinishedAt == nil || last.Kind != KindScheduled {
		t.Fatalf("Latest bist = %+v; want the finished scheduled run", last)
	}
}

// TestLatestIgnoresNoOpBackfill checks that a backfill that finished
// with 0 rows and no error, having fetched nothing, is neither the source's
// last run nor its last success. A failed manual run before it stays the
// last run. A backfill that wrote rows still counts.
func TestLatestIgnoresNoOpBackfill(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	runs := NewRunLog(conn)
	runs.Record(ctx, "us", KindManual, func() (int, error) { return 0, errors.New("finnhub: API key not configured") })
	runs.Record(ctx, "us", KindBackfill, func() (int, error) { return 0, nil })
	runs.Record(ctx, "bist", KindBackfill, func() (int, error) { return 3, nil })

	st, err := runs.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if last := st["us"].LastRun; last == nil || last.Kind != KindManual || last.Error == nil {
		t.Fatalf("us last run = %+v; want the failed manual run", last)
	}
	if at := st["us"].LastSuccessAt; at != nil {
		t.Fatalf("us last success = %q; want none", *at)
	}
	if b := st["bist"]; b.LastRun == nil || b.LastRun.Kind != KindBackfill || b.LastSuccessAt == nil {
		t.Fatalf("bist = %+v; want the backfill that wrote rows as last run and success", b)
	}
}

// TestCatchUpWalksBackUntilARowIsWritten covers the startup walk: today and
// yesterday have nothing published (0 rows, no error), the day before does.
func TestCatchUpWalksBackUntilARowIsWritten(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var dates []string
	src := Source{
		Name: "walker",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			dates = append(dates, date.Format(time.DateOnly))
			if len(dates) < 3 {
				return 0, nil
			}
			return 1, nil
		},
	}

	CatchUp(context.Background(), []Source{src}, 7, nil, "catch-up", logger)

	if len(dates) != 3 {
		t.Fatalf("Sync called %d times (%v), want 3", len(dates), dates)
	}
	for i := 1; i < len(dates); i++ {
		if dates[i] >= dates[i-1] {
			t.Fatalf("dates = %v, want each call one day further back", dates)
		}
	}
}

// TestCatchUpSkipsWhenHaveReportsStored covers the restart case: the date is
// already stored, so nothing is fetched.
func TestCatchUpSkipsWhenHaveReportsStored(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	calls := 0
	src := Source{
		Name: "stored",
		Have: func(ctx context.Context, date string) (bool, error) { return true, nil },
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			calls++
			return 1, nil
		},
	}

	CatchUp(context.Background(), []Source{src}, 7, nil, "catch-up", logger)

	if calls != 0 {
		t.Fatalf("Sync called %d times, want 0 when Have reports the date stored", calls)
	}
}

// TestCatchUpOneFailureDoesNotBlockNextSource mirrors the daily run's
// contract: a source that errors is logged and the next source still runs.
func TestCatchUpOneFailureDoesNotBlockNextSource(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	failCalls, okCalls := 0, 0
	sources := []Source{
		{
			Name: "fail-source",
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				failCalls++
				return 0, errors.New("boom")
			},
		},
		{
			Name: "ok-source",
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				okCalls++
				return 3, nil
			},
		},
	}

	CatchUp(context.Background(), sources, 7, nil, "catch-up", logger)

	if failCalls != 1 {
		t.Fatalf("failCalls = %d, want 1 (an error is not retried on an earlier date)", failCalls)
	}
	if okCalls != 1 {
		t.Fatalf("okCalls = %d, want 1 (must run despite the prior source's failure)", okCalls)
	}
}

// TestCatchUpLogsUnderCallerPrefix checks that the on-demand BIST fetch
// reuses CatchUp, and its failure is logged under its own prefix, not
// as a startup catch-up that never ran.
func TestCatchUpLogsUnderCallerPrefix(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	src := Source{
		Name: "bist on-demand THYAO",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			return 0, errors.New("403 Forbidden")
		},
	}

	CatchUp(context.Background(), []Source{src}, 7, nil, "on-demand", logger)

	if out := buf.String(); !strings.Contains(out, `msg="on-demand: fetch failed"`) || strings.Contains(out, "catch-up") {
		t.Fatalf("log = %q, want the failure under the on-demand prefix and no catch-up", out)
	}
}

// TestCatchUpStopsQuietlyOnNothingToFetch covers a source with nothing
// tracked: one Sync call, no walk back, no WARN.
func TestCatchUpStopsQuietlyOnNothingToFetch(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	calls := 0
	src := Source{
		Name: "empty",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			calls++
			return 0, ErrNothingToFetch
		},
	}

	CatchUp(context.Background(), []Source{src}, 7, nil, "catch-up", logger)

	if calls != 1 {
		t.Fatalf("Sync called %d times, want 1", calls)
	}
	if strings.Contains(buf.String(), "level=WARN") || strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("unexpected WARN/ERROR log: %s", buf.String())
	}
}

// TestCatchUpCancelledMidFetchLogsNoError checks that a shutdown during
// a catch-up fetch logs nothing at ERROR. It makes Sync fail with
// signal.NotifyContext's cause, which does not wrap context.Canceled, but
// nothing failed.
func TestCatchUpCancelledMidFetchLogsNoError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithCancelCause(context.Background())
	src := Source{
		Name: "bist",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			cancel(errors.New("terminated signal received"))
			return 0, fmt.Errorf("get: %w", context.Cause(ctx))
		},
	}

	CatchUp(ctx, []Source{src}, 7, nil, "catch-up", logger)

	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("unexpected ERROR log after shutdown: %s", buf.String())
	}
}

// TestAttemptCancelledMidFetchLogsNoError checks that a shutdown during
// a scheduled fetch logs nothing at ERROR. It makes Sync fail with
// signal.NotifyContext's cause, which does not wrap context.Canceled, but
// nothing failed.
func TestAttemptCancelledMidFetchLogsNoError(t *testing.T) {
	var buf bytes.Buffer
	ctx, cancel := context.WithCancelCause(context.Background())
	src := Source{
		Name: "bist",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			cancel(errors.New("terminated signal received"))
			return 0, fmt.Errorf("get: %w", context.Cause(ctx))
		},
	}
	r, _ := newTestRunner(t, []Source{src})
	r.logger = slog.New(slog.NewTextHandler(&buf, nil))

	// Tuesday 2026-09-08 22:00 Europe/Istanbul, past the default fetch time.
	r.tick(ctx, time.Date(2026, 9, 8, 22, 0, 0, 0, r.loc))

	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("unexpected ERROR log after shutdown: %s", buf.String())
	}
}

// TestCatchUpNotConfiguredLogsWarn checks that a source whose provider key is
// unset is an expected state: logged once at WARN, never at ERROR.
func TestCatchUpNotConfiguredLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	src := Source{
		Name: "xu100",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			return 0, fmt.Errorf("get: %w", ErrNotConfigured)
		},
	}

	CatchUp(context.Background(), []Source{src}, 7, nil, "catch-up", logger)

	out := buf.String()
	if n := strings.Count(out, "level=WARN"); n != 1 || !strings.Contains(out, `msg="catch-up: fetch failed"`) || strings.Contains(out, "level=ERROR") {
		t.Fatalf("log = %q, want one catch-up fetch failed WARN and no ERROR", out)
	}
}

// TestCatchUpTimedOutMidFetchLogsOneError checks that the on-demand
// BIST fetch runs CatchUp under the save's timeout, and a deadline that
// passes during Sync is a failure, not a shutdown, so it is logged once.
func TestCatchUpTimedOutMidFetchLogsOneError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	src := Source{
		Name: "bist on-demand ASELS",
		Sync: func(ctx context.Context, date time.Time) (int, error) {
			<-ctx.Done()
			return 0, fmt.Errorf("get: %w", ctx.Err())
		},
	}

	CatchUp(ctx, []Source{src}, 7, nil, "on-demand", logger)

	out := buf.String()
	if n := strings.Count(out, "level=ERROR"); n != 1 || !strings.Contains(out, `msg="on-demand: fetch failed" source="bist on-demand ASELS"`) {
		t.Fatalf("log = %q, want one on-demand fetch failed ERROR naming the instrument", out)
	}
}
