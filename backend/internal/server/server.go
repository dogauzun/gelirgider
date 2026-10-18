// Package server wires up the HTTP handlers for the API.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/allocation"
	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/evds"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/statement"
	// Register the CSV, Garanti BBVA, Garanti BBVA Yatırım and Midas parsers in
	// statement.Parsers from their init; the statement package cannot import
	// them back (ADR 0006).
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/csv"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garanti"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garantiyatirim"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/midas"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/us"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// healthTimeout bounds how long the health check waits on the database.
const healthTimeout = 2 * time.Second

// Every new user gets one portfolio on registration, named in the locale the
// registration request carried: tr (the default) or en.
const (
	defaultPortfolioNameTR = "Portföy"
	defaultPortfolioNameEN = "Portfolio"
)

// maxBodyBytes caps every request body. Well above any legitimate JSON
// payload in this API.
const maxBodyBytes = 64 << 10

// Version is the build version /api/health reports. main sets it from the
// -X main.version ldflag; a package var, not a New argument, so the 60-odd
// test callers of New stay untouched.
var Version = "dev"

// New returns the root HTTP handler, with request logging applied.
// conn is used to build the repositories the handlers need; cookieSecure
// controls the Secure flag on the session cookie; cfg is the one install-wide
// settings repo, shared with the rest of the process; runs is the run log
// shared with the scheduler, whose "running" flag keeps a run now from
// overlapping a scheduled run (nil makes a private one, for a handler with
// no scheduler, as in tests); sources is the scheduler's source list, in
// run order, that the admin sources API serves. Variadic so tests that
// never touch it can leave it out.
func New(logger *slog.Logger, conn *sql.DB, cookieSecure bool, cfg *settings.Repo, runs *scheduler.RunLog, sources ...scheduler.Source) http.Handler {
	return newHandler(logger, conn, cookieSecure, deps{cfg: cfg, runs: runs, bistFetcher: bist.NewFetcher(), usFetcher: us.NewFetcher(cfg), tcmbFetcher: tcmb.NewFetcher(), btcturkFetcher: btcturk.NewFetcher(), tefasFetcher: tefas.NewFetcher()}, sources...)
}

// deps are the repos and fetchers the routes share. The caller of
// newHandler sets cfg, runs and the fetchers; newHandler builds the repos
// from conn, once. The summary, history, position history, dividend and
// cash gap handlers embed *deps and reach the repos through it.
type deps struct {
	cfg  *settings.Repo
	runs *scheduler.RunLog
	// conn is the database, for a handler that writes several rows in one SQL
	// transaction. newHandler sets it.
	conn *sql.DB

	bistFetcher    *bist.Fetcher
	usFetcher      *us.Fetcher
	tcmbFetcher    *tcmb.Fetcher
	btcturkFetcher *btcturk.Fetcher
	tefasFetcher   *tefas.Fetcher

	users             *user.Repo
	portfolios        *portfolio.Repo
	instruments       *instrument.Repo
	prices            *price.Repo
	manualPrices      *price.ManualRepo
	transactions      *transaction.Repo
	fx                *fx.Repo
	splits            *split.Repo
	benchmarks        *benchmark.Repo
	dividends         *dividend.Repo
	deposits          *deposit.Repo
	allocationTargets *allocation.Repo
}

// priceSources is what the valuations read prices from: the fetched closes,
// the user's manual prices and the user's deposits.
func (d *deps) priceSources() holdings.PriceSources {
	return holdings.PriceSources{Prices: d.prices, Manual: d.manualPrices, Deposits: d.deposits}
}

// newHandler is New with the market-data fetchers injected in d, so tests
// can point them at httptest.Server stubs instead of the real
// bulletin/Finnhub/TCMB/BtcTurk/TEFAS endpoints rather than
// reaching out over the network. d's repos are built here from conn.
func newHandler(logger *slog.Logger, conn *sql.DB, cookieSecure bool, d deps, sources ...scheduler.Source) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth(logger, conn))

	d.conn = conn
	d.users = user.NewRepo(conn)
	d.portfolios = portfolio.NewRepo(conn)
	d.instruments = instrument.NewRepo(conn)
	d.prices = price.NewRepo(conn)
	d.manualPrices = price.NewManualRepo(conn)
	d.transactions = transaction.NewRepo(conn)
	d.fx = fx.NewRepo(conn)
	d.splits = split.NewRepo(conn)
	d.benchmarks = benchmark.NewRepo(conn)
	d.dividends = dividend.NewRepo(conn)
	d.deposits = deposit.NewRepo(conn)
	d.allocationTargets = allocation.NewRepo(conn)
	cfg, users, portfolios := d.cfg, d.users, d.portfolios
	sessions := auth.NewRepo(conn)

	// Sessions expire after 30 days but nothing deletes them; CreateSession
	// purges on every login, this covers a server that never sees one.
	if err := sessions.DeleteExpiredSessions(context.Background()); err != nil {
		logger.Error("failed to delete expired sessions", "error", err.Error())
	}

	// auth cannot import portfolio directly (portfolio's handlers import
	// auth for auth.UserID, so the reverse import would cycle), so the
	// default-portfolio seeding is passed in as a callback.
	seedUser := func(userID int64, locale string) error {
		name := defaultPortfolioNameTR
		if locale == "en" {
			name = defaultPortfolioNameEN
		}
		_, err := portfolios.Create(context.Background(), userID, name)
		return err
	}
	auth.RegisterRoutes(mux, users, sessions, cfg, cookieSecure, seedUser)
	auth.RegisterSetupRoutes(mux, logger, users, sessions, cookieSecure, seedUser)

	// Routes that require an authenticated user are grouped on their own
	// mux, wrapped once with RequireUser, and mounted under /api/. Future
	// tickets add protected routes via RegisterProtectedRoutes instead of
	// wrapping each handler individually. Public routes registered above
	// with more specific patterns (e.g. "/api/auth/login") still take
	// precedence over this "/api/" subtree per net/http mux rules.
	protected := http.NewServeMux()
	auth.RegisterProtectedRoutes(protected, users, sessions, cfg)
	// Admin-only routes sit on their own mux under /api/admin/, behind
	// RequireAdmin on top of the RequireUser that wraps protected.
	admin := http.NewServeMux()
	if d.runs == nil {
		d.runs = scheduler.NewRunLog(conn)
	}
	// One backfill worker for the on-demand fetch and the admin backfill.
	backfills := &backfillQueue{runs: d.runs}
	auth.RegisterAdminRoutes(admin, users, sessions, seedUser)
	registerScheduleRoutes(admin, cfg, d.runs, backfills, sources)
	admin.Handle("PUT /api/admin/registration", auth.SetRegistrationHandler(cfg))
	registerProviderRoutes(admin, cfg, d.usFetcher, evds.NewFetcher(cfg))
	protected.Handle("/api/admin/", auth.RequireAdmin(users, withJSONMiss(admin)))
	instruments, prices, manualPrices, transactions := d.instruments, d.prices, d.manualPrices, d.transactions
	instrument.RegisterAdminRoutes(admin, instruments)
	instrument.RegisterRoutes(protected, instruments)
	portfolio.RegisterRoutes(protected, portfolios)
	price.RegisterRoutes(protected, prices, manualPrices, instruments)
	onDemandFetch := newOnDemandFetch(&d, backfills)
	transaction.RegisterRoutes(protected, transactions, portfolios, instruments, onDemandFetch)
	// The import asks TEFAS about an unknown fund code only while the tefas
	// source is on: the fetcher is what prices the fund it creates. The
	// setting is read on a context a client disconnect does not cancel, as
	// in onDemandFetch. A lookup it skips asks TEFAS nothing, so
	// it costs the client none of its lookup budget.
	lookupFund := func(ctx context.Context, code string) (string, bool, error) {
		if !scheduler.Enabled(context.WithoutCancel(ctx), cfg, scheduler.NameTEFAS, slog.Default()) {
			return "", false, statement.ErrFundLookupOff
		}
		return d.tefasFetcher.Lookup(ctx, code)
	}
	// A BIST row the instruments do not know may be a share delisted
	// since: the import syncs its date's bulletin, which adds it, while the
	// bist source is on and that bulletin is not stored yet.
	syncBulletin := func(ctx context.Context, date time.Time) (bool, error) {
		if !scheduler.Enabled(context.WithoutCancel(ctx), cfg, scheduler.NameBIST, slog.Default()) {
			return false, nil
		}
		if stored, err := prices.HasSource(ctx, date.Format(time.DateOnly), bist.SourceBistBulletin); err != nil || stored {
			return false, err
		}
		_, err := bist.Sync(ctx, d.bistFetcher, instruments, prices, date)
		return true, err
	}
	statement.RegisterRoutes(protected, statement.Deps{Imports: statement.NewImportRepo(conn), Transactions: transactions, Splits: d.splits, Portfolios: portfolios, Instruments: instruments, Prices: prices, ManualPrices: manualPrices, Deposits: d.deposits, OnDemandFetch: onDemandFetch, LookupFund: lookupFund, SyncBulletin: syncBulletin})
	registerSummaryRoutes(protected, &d)
	registerHistoryRoutes(protected, &d)
	registerCashGapRoutes(protected, &d)
	registerPositionHistoryRoutes(protected, &d)
	registerDividendRoutes(protected, &d)
	registerDepositRoutes(protected, &d, onDemandFetch)
	registerAllocationTargetRoutes(protected, &d)
	mux.Handle("/api/", sessions.RequireUser(withJSONMiss(protected)))

	return withRecover(logger, withLogging(logger, withCrossOriginProtection(withBodyLimit(mux))))
}

// withCrossOriginProtection rejects a POST/PUT/PATCH/DELETE whose
// Sec-Fetch-Site or Origin shows another origin, with 403. The
// session cookie's SameSite=Lax stops cross-site pages, but not another
// origin of the same site, such as another port on the host or a sibling
// subdomain, which can post a text/plain form without a preflight. The dev
// proxy and the deploy proxies keep the browser's Host, so the API's own
// frontend passes as same-origin.
func withCrossOriginProtection(next http.Handler) http.Handler {
	p := http.NewCrossOriginProtection()
	p.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.WriteError(w, http.StatusForbidden, "cross-origin request")
	}))
	return p.Handler(next)
}

// onDemandBackfillTimeout bounds one background bulletin backfill. A full
// walk from bist.BackfillMinDate is ~2800 weekdays at RequestPause plus a
// fetch each, well under this; the bound only stops a hung request from
// holding the backfill lock until the process restarts.
const onDemandBackfillTimeout = 2 * time.Hour

// newOnDemandFetch returns the transaction.OnDemandFetch callback. It is
// called after every transaction save; when the instrument has no price
// row on or before today, it fetches one synchronously, so the
// summary opened next already has it. That guard keeps the synchronous
// part to one fetch per new symbol rather than one per transaction. BIST
// fills the whole market in one bulletin download, so it walks back from
// today over at most scheduler.CatchUpLookbackDays days for the latest
// published bulletin, then starts a background backfill of every bulletin from the
// transaction date to yesterday, so the history chart has a real
// close per trading day. US fetches just the one instrument via us.SyncOne,
// labeled with the same previous-trading-day date the scheduler uses, then
// its Tiingo daily closes from the transaction date in one request,
// skipped when that range is already covered. CRYPTO fetches
// the pair's daily closes from the transaction date to yesterday in one
// BtcTurk request, so it needs no background price backfill.
// TEFAS fetches the fund's NAVs back to the transaction date in one request
// via tefas.Syncer.SyncHistory, skipped when stored prices already reach back that
// far, so it needs no background price backfill either. The gold coins
// have no fetched price (ADR 0017), so they fetch nothing of their own.
//
// Every save, in any market and for cash, also fetches the gram gold
// series from the transaction date, but not before
// btcturk.GoldHistoryStart, in one request via GoldSyncer.SyncGoldHistory,
// the range rule of the startup gold backfill, skipped when stored
// GOLD/GRAM prices already cover it. That fills both the GOLD/GRAM price
// and the GOLD benchmark, as the FX walk below fills
// the USD and EUR ones, while the gold source is enabled.
//
// The background walk also backfills TCMB FX rates over the same range,
// for every market: a TEFAS buy dated in the past needs the
// rates to convert its days on the history chart just as much as a BIST
// one does. Each background walk goes through backfills.
//
// A fetch cut off because the request was cancelled (the client went
// away) failed nothing and is not logged; one that hit the timeout is logged.
// ctx.Err() decides, not the error. The gold
// fetch does not start on a ctx that is already done, so an import's
// spent shared deadline logs no error per target.
//
// It uses d's cfg, fetchers and instrument, price, benchmark, split,
// dividend and fx repos.
func newOnDemandFetch(d *deps, backfills *backfillQueue) transaction.OnDemandFetch {
	settingsRepo, instruments, prices := d.cfg, d.instruments, d.prices
	tcmbSource := tcmb.BackfillSource(d.tcmbFetcher, d.fx)
	tefasSyncer := &tefas.Syncer{Fetcher: d.tefasFetcher, Instruments: instruments, Prices: prices}
	goldSyncer := &btcturk.GoldSyncer{Fetcher: d.btcturkFetcher, Instruments: instruments, Prices: prices, Benchmarks: d.benchmarks}
	usSyncer := &us.Syncer{Fetcher: d.usFetcher, Instruments: instruments, Prices: prices, Splits: d.splits, Dividends: d.dividends}

	return func(ctx context.Context, in instrument.Instrument, date string) {
		sources := []bist.BackfillSource{}
		// A source disabled in settings is not fetched here either; the
		// market's source is marketSource's. The settings are read on
		// settingsCtx, which a client disconnect does not cancel: the
		// transaction is saved and the backfill outlives the request, so it
		// follows the real settings either way.
		settingsCtx := context.WithoutCancel(ctx)
		on := scheduler.Enabled(settingsCtx, settingsRepo, marketSource[in.Market], slog.Default())

		if on && in.Market == "CRYPTO" {
			syncCryptoRange(ctx, in, date, prices, d.btcturkFetcher)
		}

		if on && in.Market == "TEFAS" {
			_, err := tefasSyncer.SyncHistory(ctx, in, date, time.Now())
			logFetchErr(ctx, err, "on-demand tefas price fetch failed", "instrument_id", in.ID, "symbol", in.Symbol)
		}

		if on && in.Market == "BIST" {
			sources = append(sources, fetchBISTOnDemand(ctx, d, in)...)
		}

		if on && in.Market == "US" {
			fetchUSOnDemand(ctx, d, usSyncer, in, date)
		}

		// Gold runs on every save, after the instrument's own fetch so that
		// one keeps the timeout budget: the GOLD benchmark needs the series
		// whatever the transaction is. For a GOLD instrument "gold"
		// is also on, so this is the GOLD/GRAM price fetch too.
		if scheduler.Enabled(settingsCtx, settingsRepo, scheduler.NameGold, slog.Default()) {
			fetchGoldOnDemand(ctx, goldSyncer, date)
		}

		if scheduler.Enabled(settingsCtx, settingsRepo, scheduler.NameTCMB, slog.Default()) {
			sources = append(sources, tcmbSource)
		}
		startBackfill(backfills, in, date, sources)
	}
}

// fetchBISTOnDemand fetches in's latest bulletin when it has no price yet
// and returns the bulletin backfill source for the caller to run; none when
// the latest-price check failed.
func fetchBISTOnDemand(ctx context.Context, d *deps, in instrument.Instrument) []bist.BackfillSource {
	latest, err := d.prices.Latest(ctx, []int64{in.ID}, price.Today())
	logFetchErr(ctx, err, "on-demand price fetch: check latest price failed", "instrument_id", in.ID)
	if err != nil {
		return nil
	}
	if _, hasPrice := latest[in.ID]; !hasPrice {
		syncLatestBulletin(ctx, in, d.instruments, d.prices, d.bistFetcher)
	}
	return []bist.BackfillSource{bist.BulletinBackfillSource(d.bistFetcher, d.instruments, d.prices)}
}

// fetchUSOnDemand fetches in's latest price when it has none yet, then its
// history from date.
func fetchUSOnDemand(ctx context.Context, d *deps, usSyncer *us.Syncer, in instrument.Instrument, date string) {
	latest, err := d.prices.Latest(ctx, []int64{in.ID}, price.Today())
	logFetchErr(ctx, err, "on-demand price fetch: check latest price failed", "instrument_id", in.ID)
	if err != nil {
		return
	}
	if _, hasPrice := latest[in.ID]; !hasPrice {
		_, err := us.SyncOne(ctx, d.usFetcher, d.prices, in, time.Now())
		logFetchErr(ctx, err, "on-demand us price fetch failed", "instrument_id", in.ID, "symbol", in.Symbol)
	}
	_, err = usSyncer.SyncHistory(ctx, in, date)
	logFetchErr(ctx, err, "on-demand us history fetch failed", "instrument_id", in.ID, "symbol", in.Symbol)
}

// fetchGoldOnDemand stores the gold history from date. A ctx already done
// here was spent by the instrument's own fetch, which logged it if it timed
// out, or by an import's shared deadline: the gold fetch is skipped, not
// logged as an error per target.
func fetchGoldOnDemand(ctx context.Context, goldSyncer *btcturk.GoldSyncer, date string) {
	if ctx.Err() != nil {
		slog.Default().Debug("on-demand gold price fetch skipped",
			"source", scheduler.NameGold, "date", date, "reason", ctx.Err().Error())
		return
	}
	_, err := goldSyncer.SyncGoldHistory(ctx, date, time.Now())
	logFetchErr(ctx, err, "on-demand gold price fetch failed", "source", scheduler.NameGold, "date", date)
}

// marketSource maps an instrument market to the scheduler source whose
// enabled setting gates its on-demand fetch. It lists every
// seeded market, which TestMarketSourceCoversSeededMarkets checks.
var marketSource = map[string]string{
	"BIST":   scheduler.NameBIST,
	"US":     scheduler.NameUS,
	"CRYPTO": scheduler.NameCrypto,
	"TEFAS":  scheduler.NameTEFAS,
	"GOLD":   scheduler.NameGold,
}

// syncCryptoRange stores in's BtcTurk daily closes from date to yesterday
// (UTC, the candle date) in one request. from is clamped to at most six
// days before yesterday, so a buy dated today still gets a last price. It
// runs on every save, not only for a pair with no price yet: a backdated
// buy needs the older closes too, and it is one request either way. Errors
// are logged only.
func syncCryptoRange(ctx context.Context, in instrument.Instrument, date string, prices *price.Repo, f *btcturk.Fetcher) {
	from, err := time.Parse(time.DateOnly, date)
	if err != nil {
		slog.Default().Error("on-demand crypto price fetch: bad transaction date", "date", date, "instrument_id", in.ID, "error", err.Error())
		return
	}
	now := time.Now().UTC()
	yesterday := btcturk.LastCandleDay(now)
	if weekBack := yesterday.AddDate(0, 0, -6); from.After(weekBack) {
		from = weekBack
	}
	// to is now rather than yesterday: FetchCandles already drops today's
	// unfinished candle, and this keeps yesterday's whether or not BtcTurk
	// treats to as inclusive.
	_, err = btcturk.SyncRange(ctx, f, prices, in, from, now)
	logFetchErr(ctx, err, "on-demand crypto price fetch failed", "instrument_id", in.ID, "symbol", in.Symbol)
}

// logFetchErr logs a failed on-demand fetch at ERROR with args and the
// error, unless err is nil or ctx was cancelled: a client that went away
// failed nothing. ctx.Err() decides, not err.
func logFetchErr(ctx context.Context, err error, msg string, args ...any) {
	if err == nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	slog.Default().Error(msg, append(args, "error", err.Error())...)
}

// syncLatestBulletin walks back from today and syncs the first published
// bulletin. It is the same walk the startup catch-up does, so it reuses
// scheduler.CatchUp with a single source, its log lines prefixed
// "on-demand" rather than "catch-up"; no Have, since the caller
// only gets here when this instrument has no price row yet.
func syncLatestBulletin(ctx context.Context, in instrument.Instrument, instruments *instrument.Repo, prices *price.Repo, bistFetcher *bist.Fetcher) {
	src := scheduler.Source{
		Name: "bist on-demand " + in.Symbol,
		Sync: func(ctx context.Context, d time.Time) (int, error) {
			return bist.Sync(ctx, bistFetcher, instruments, prices, d)
		},
	}
	scheduler.CatchUp(ctx, []scheduler.Source{src}, scheduler.CatchUpLookbackDays, nil, "on-demand", slog.Default())
}

// backfillQueue serializes backfills: one worker at a time, with the
// requests that arrive while a walk runs kept as pending instead of
// dropped, merged per source to the earliest start date. Several
// transactions saved within seconds, or one import commit fanning out over
// every instrument and cash currency, each get their range walked.
// BackfillRange skips dates that already have a row, so re-walking an
// overlapping range is cheap. An admin backfill now queues a
// source's whole history walk instead, one pending per source.
//
// shortcut: one worker and one pending entry per source name. Upgrade
// path: parallel workers per source, if a single walk is ever too slow.
type backfillQueue struct {
	mu      sync.Mutex
	pending map[string]backfillRequest                    // by source name, earliest start wins
	walks   map[string]func(context.Context) (int, error) // admin backfills, by source name
	running bool
	// runs logs each walk under its source name, which is the scheduler
	// source name. nil logs none.
	runs *scheduler.RunLog
}

// backfillRequest is one source to walk from start to yesterday.
type backfillRequest struct {
	src   bist.BackfillSource
	start time.Time
}

// add queues one request per source and starts the worker if it is idle.
func (q *backfillQueue) add(sources []bist.BackfillSource, start time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.pending == nil {
		q.pending = map[string]backfillRequest{}
	}
	for _, src := range sources {
		if cur, ok := q.pending[src.Name]; ok && !start.Before(cur.start) {
			continue
		}
		q.pending[src.Name] = backfillRequest{src: src, start: start}
	}
	q.start()
}

// addWalk queues walk as the backfill of source name, unless one is
// already pending, and starts the worker if it is idle.
func (q *backfillQueue) addWalk(name string, walk func(context.Context) (int, error)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.walks == nil {
		q.walks = map[string]func(context.Context) (int, error){}
	}
	if _, ok := q.walks[name]; !ok {
		q.walks[name] = walk
	}
	q.start()
}

// start starts the worker if it is idle and there is work. q.mu is held.
func (q *backfillQueue) start() {
	if q.running || len(q.pending)+len(q.walks) == 0 {
		return
	}
	q.running = true
	backfillWorkers.Go(q.drain)
}

// backfillWorkers counts the running drain goroutines of every queue in
// the process, so a test can wait for them before its DB and temp dir go
// away.
var backfillWorkers sync.WaitGroup

// next takes one queued walk, its source name and whether it is an
// on-demand walk rather than an admin one, or reports the queue empty and
// marks the worker stopped. A date range starting after yesterday is
// dropped.
func (q *backfillQueue) next() (name string, walk func(context.Context) (int, error), onDemand, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for name, req := range q.pending {
		delete(q.pending, name)
		end := price.Yesterday(time.Now())
		if req.start.After(end) {
			continue
		}
		return name, func(ctx context.Context) (int, error) {
			return bist.BackfillRange(ctx, slog.Default(), req.src, req.start, end)
		}, true, true
	}
	for name, walk := range q.walks {
		delete(q.walks, name)
		return name, walk, false, true
	}
	q.running = false
	return "", nil, false, false
}

// drain runs every queued walk in turn until the queue is empty. It runs
// on context.Background(), since the request context ends when the
// response goes out. A failing source is logged and the next one still
// runs.
//
// An admin walk is always logged in source_runs. An on-demand walk, queued
// by every saved transaction, is logged only when it wrote rows or failed:
// a no-op one would pile up and hide the daily fetch as the
// source's last run. Its row is written after the walk, so its started_at
// is the finish time.
func (q *backfillQueue) drain() {
	logger := slog.Default()
	for {
		name, walk, onDemand, ok := q.next()
		if !ok {
			return
		}
		logger.Info("backfill started", "source", name)
		ctx, cancel := context.WithTimeout(context.Background(), onDemandBackfillTimeout)
		var written int
		var err error
		if onDemand {
			written, err = walk(ctx)
			if written > 0 || err != nil {
				// WithoutCancel: a walk that hit the timeout still gets its row.
				q.runs.Record(context.WithoutCancel(ctx), name, scheduler.KindBackfill, func() (int, error) { return written, err })
			}
		} else {
			written, err = q.runs.Record(ctx, name, scheduler.KindBackfill, func() (int, error) { return walk(ctx) })
		}
		cancel()
		if err != nil {
			logger.Error("backfill failed", "source", name, "written", written, "error", err.Error())
			continue
		}
		logger.Info("backfill finished", "source", name, "written", written)
	}
}

// startBackfill queues a walk of every weekday from max(date,
// bist.BackfillMinDate) to yesterday (Europe/Istanbul) for each source,
// skipping dates the source already has a row for.
func startBackfill(q *backfillQueue, in instrument.Instrument, date string, sources []bist.BackfillSource) {
	start, err := price.ParseDate(date)
	if err != nil {
		slog.Default().Error("on-demand backfill: bad transaction date", "date", date, "instrument_id", in.ID, "error", err.Error())
		return
	}
	start = bist.ClampBackfillStart(start)
	if start.After(price.Yesterday(time.Now())) {
		return
	}
	q.add(sources, start)
}

// withBodyLimit caps every request body, except the statement routes: the
// upload is a multi-page PDF and the commit carries every reviewed row of it,
// so both set their own larger limit inside their handler (ADR 0006). A body
// whose declared length is over the limit is answered 413 here, since every
// handler maps a decode error to 400; a body of unknown length is still cut
// off by the reader and fails decoding.
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case statement.ParsePath, statement.CommitPath:
		default:
			if r.ContentLength > maxBodyBytes {
				auth.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body: larger than %d KB", maxBodyBytes>>10))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// withJSONMiss answers a request mux has no route for (404) or no route
// for the method (405, Allow kept) with the JSON error body instead of
// ServeMux's plain text.
func withJSONMiss(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			w = jsonMissWriter{w}
		}
		mux.ServeHTTP(w, r)
	})
}

// jsonMissWriter turns ServeMux's http.Error reply into auth.WriteError's.
type jsonMissWriter struct{ http.ResponseWriter }

func (w jsonMissWriter) WriteHeader(code int) {
	auth.WriteError(w.ResponseWriter, code, strings.ToLower(http.StatusText(code)))
}

func (w jsonMissWriter) Write(b []byte) (int, error) { return len(b), nil }

// handleHealth checks the database is reachable before reporting healthy,
// so a missing or corrupted database file doesn't report ok.
//
//	@Summary	Health check
//	@Description	Reports ok and the build version when the database answers. Needs no session.
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	map[string]string	"status is ok, version is the build version (dev for an unstamped build)"
//	@Failure	503	{object}	map[string]string	"status is degraded, error is database unavailable"
//	@Router		/health [get]
func handleHealth(logger *slog.Logger, conn *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()

		if _, err := conn.ExecContext(ctx, "SELECT 1"); err != nil {
			logger.Error("health check failed", "error", err.Error())
			auth.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "degraded",
				"error":  "database unavailable",
			})
			return
		}
		auth.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// The containers' own health check hits this every 30 s; a failing one keeps its line.
		if r.Method == http.MethodGet && r.URL.Path == "/api/health" && rec.status == http.StatusOK {
			return
		}
		fields := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", ratelimit.ClientIP(r),
		}
		if userID, ok := auth.UserID(r.Context()); ok {
			fields = append(fields, "user_id", userID)
		}
		logger.Info("request", fields...)
	})
}

// withRecover recovers from a panic in any inner handler or middleware,
// logs it with a stack trace, and responds with the package's standard
// JSON error shape instead of taking the process down or dropping the
// client's connection. It is the outermost middleware so it can catch
// panics anywhere in the chain, including withLogging.
func withRecover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if err == http.ErrAbortHandler {
					panic(err)
				}
				logger.Error("panic recovered",
					"path", r.URL.Path,
					"method", r.Method,
					"error", err,
					"stack", string(debug.Stack()),
				)
				auth.WriteError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
