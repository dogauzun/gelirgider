package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Run kinds, the source_runs.kind values.
const (
	KindScheduled = "scheduled" // Run's minute loop
	KindCatchUp   = "catchup"   // CatchUp at startup
	KindBackfill  = "backfill"  // history walk: at startup, -backfill-prices or on demand
	KindManual    = "manual"    // an admin's run now
)

// RunLog records market-data runs in source_runs, one row per source per
// run (ADR 0010), for the admin sources API. Global, no user_id.
//
// It also holds the in-memory "running" flag per source: the process makes
// one RunLog and shares it between the scheduler and the admin run now
// endpoint, so a source never runs twice at once.
//
// shortcut: no pruning; a few rows per source per day. Add a retention
// delete when the table size matters.
type RunLog struct {
	db      *sql.DB
	mu      sync.Mutex
	running map[string]bool // by source name
}

// ErrAlreadyRunning is returned by Record and Go, without running fn, while
// the source has a run going. Backfills are not counted: they walk past
// dates, not today, and the backfill queue already runs one at a time.
var ErrAlreadyRunning = errors.New("already running")

// NewRunLog returns a RunLog backed by db.
func NewRunLog(db *sql.DB) *RunLog {
	return &RunLog{db: db}
}

// SourceRun is one source_runs row. FinishedAt is nil while the run is
// going, Error nil on success.
type SourceRun struct {
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Kind       string  `json:"kind"`
	Rows       int     `json:"rows"`
	Error      *string `json:"error"`
}

// RunStatus is one source's latest run and the finished_at of its latest
// successful one. The zero value means the source never ran.
type RunStatus struct {
	LastRun       *SourceRun
	LastSuccessAt *string
}

// Start stores a run of source as started now and returns its id.
func (l *RunLog) Start(ctx context.Context, source, kind string) (int64, error) {
	res, err := l.db.ExecContext(ctx, "INSERT INTO source_runs (source, kind, started_at) VALUES (?, ?, ?)",
		source, kind, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("insert source run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("source run id: %w", err)
	}
	return id, nil
}

// Finish stores run id as finished now with rows written and runErr. ctx
// is the run's own context; the writes ignore its cancellation.
// ErrNothingToFetch is a success. A run stopped by shutdown is deleted
// instead: nothing failed, and the day is not done, so neither the admin
// panel nor RanSince may see it. That is decided on ctx, not only
// on runErr: a context cancelled with a cause, like signal.NotifyContext's
// since Go 1.26, makes net/http return the cause, which does not wrap
// context.Canceled. A timeout (context.DeadlineExceeded) is a
// real failure and is stored.
func (l *RunLog) Finish(ctx context.Context, id int64, rows int, runErr error) error {
	stopped := errors.Is(runErr, context.Canceled) || (runErr != nil && errors.Is(ctx.Err(), context.Canceled))
	ctx = context.WithoutCancel(ctx)
	if stopped {
		if _, err := l.db.ExecContext(ctx, "DELETE FROM source_runs WHERE id = ?", id); err != nil {
			return fmt.Errorf("delete canceled source run %d: %w", id, err)
		}
		return nil
	}
	var msg *string
	if runErr != nil && !errors.Is(runErr, ErrNothingToFetch) {
		s := runErr.Error()
		msg = &s
	}
	if _, err := l.db.ExecContext(ctx, "UPDATE source_runs SET finished_at = ?, rows = ?, error = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), rows, msg, id); err != nil {
		return fmt.Errorf("finish source run %d: %w", id, err)
	}
	return nil
}

// ClearUnfinished deletes every run with no finished_at. It is for server
// startup, before any run starts: a fresh process has no run going, so such
// a row was cut off by a hard kill or crash that never reached Finish, and
// it is dropped like a run stopped by shutdown. Otherwise Latest
// would report it as running until the source runs again.
func (l *RunLog) ClearUnfinished(ctx context.Context) error {
	if _, err := l.db.ExecContext(ctx, "DELETE FROM source_runs WHERE finished_at IS NULL"); err != nil {
		return fmt.Errorf("delete unfinished source runs: %w", err)
	}
	return nil
}

// Record runs fn as one run of source, stored with Start and Finish, and
// returns fn's result unchanged, or ErrAlreadyRunning. A failed log write
// is logged, never returned. A nil l just runs fn.
func (l *RunLog) Record(ctx context.Context, source, kind string, fn func() (int, error)) (int, error) {
	if l == nil {
		return fn()
	}
	release, err := l.claim(source, kind)
	if err != nil {
		return 0, err
	}
	defer release()
	return l.record(ctx, source, kind, fn)
}

// Go is Record in a new goroutine. It returns ErrAlreadyRunning at once,
// starting nothing, while source is running. No caller sees fn's result,
// so Go logs it, the way Record's callers do.
func (l *RunLog) Go(ctx context.Context, source, kind string, fn func() (int, error)) error {
	release, err := l.claim(source, kind)
	if err != nil {
		return err
	}
	go func() {
		defer release()
		rows, err := l.record(ctx, source, kind, fn)
		if err != nil && !errors.Is(err, ErrNothingToFetch) {
			LogFetchFailed(slog.Default(), err, "fetch failed", "source", source, "kind", kind)
			return
		}
		slog.Default().Info("fetch complete", "source", source, "kind", kind, "written", rows)
	}()
	return nil
}

// claim marks source running and returns the func that clears it, or
// ErrAlreadyRunning. A backfill claims nothing.
func (l *RunLog) claim(source, kind string) (func(), error) {
	if kind == KindBackfill {
		return func() {}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running[source] {
		return nil, ErrAlreadyRunning
	}
	if l.running == nil {
		l.running = map[string]bool{}
	}
	l.running[source] = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.running, source)
	}, nil
}

// record is Record without the running flag.
func (l *RunLog) record(ctx context.Context, source, kind string, fn func() (int, error)) (int, error) {
	id, startErr := l.Start(ctx, source, kind)
	if startErr != nil {
		slog.Default().Error("run log: start failed", "source", source, "kind", kind, "error", startErr)
	}
	rows, err := fn()
	if startErr == nil {
		if ferr := l.Finish(ctx, id, rows, err); ferr != nil {
			slog.Default().Error("run log: finish failed", "source", source, "kind", kind, "error", ferr)
		}
	}
	return rows, err
}

// RanSince reports whether source has a run started at or after since that
// finished with an error or wrote rows, i.e. one that settled the day the
// way a scheduled attempt would. A backfill does not count: it walks only up
// to yesterday, so it never fetched today. A nil l reports false.
func (l *RunLog) RanSince(ctx context.Context, source string, since time.Time) (bool, error) {
	if l == nil {
		return false, nil
	}
	// started_at is RFC3339 UTC, so text order is time order.
	var n int
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_runs
		WHERE source = ? AND kind <> ? AND started_at >= ? AND finished_at IS NOT NULL AND (error IS NOT NULL OR rows > 0)`,
		source, KindBackfill, since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("query source runs since: %w", err)
	}
	return n > 0, nil
}

// notNoOpBackfill is Latest's filter, with KindBackfill as its argument: a
// running backfill (finished_at NULL) still shows as running.
const notNoOpBackfill = `NOT (kind = ? AND finished_at IS NOT NULL AND error IS NULL AND rows = 0)`

// Latest returns every source's RunStatus, by source name. A source that
// never ran has no entry.
//
// A backfill that finished with 0 rows and no error fetched nothing, so it
// is skipped for both: otherwise a no-op backfill hides a failed fetch and
// clears the stale warning (as for the on-demand walks).
func (l *RunLog) Latest(ctx context.Context) (map[string]RunStatus, error) {
	out := map[string]RunStatus{}
	rows, err := l.db.QueryContext(ctx, `SELECT source, started_at, finished_at, kind, rows, error FROM source_runs
		WHERE id IN (SELECT MAX(id) FROM source_runs WHERE `+notNoOpBackfill+` GROUP BY source)`, KindBackfill)
	if err != nil {
		return nil, fmt.Errorf("query last source runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var r SourceRun
		if err := rows.Scan(&source, &r.StartedAt, &r.FinishedAt, &r.Kind, &r.Rows, &r.Error); err != nil {
			return nil, fmt.Errorf("scan source run: %w", err)
		}
		out[source] = RunStatus{LastRun: &r}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source runs: %w", err)
	}

	// finished_at is RFC3339 UTC, so the text MAX is the latest.
	ok, err := l.db.QueryContext(ctx, `SELECT source, MAX(finished_at) FROM source_runs
		WHERE finished_at IS NOT NULL AND error IS NULL AND `+notNoOpBackfill+` GROUP BY source`, KindBackfill)
	if err != nil {
		return nil, fmt.Errorf("query last successful source runs: %w", err)
	}
	defer ok.Close()
	for ok.Next() {
		var source, at string
		if err := ok.Scan(&source, &at); err != nil {
			return nil, fmt.Errorf("scan successful source run: %w", err)
		}
		st := out[source]
		st.LastSuccessAt = &at
		out[source] = st
	}
	if err := ok.Err(); err != nil {
		return nil, fmt.Errorf("iterate successful source runs: %w", err)
	}
	return out, nil
}
