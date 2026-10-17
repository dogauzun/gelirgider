package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed tzdata: the distroless deploy image has none

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/config"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/demo"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/evds"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/server"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/tuik"
	"github.com/dogauzun/gelirgider/backend/internal/us"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// version is stamped at build time: go build -ldflags "-X main.version=...".
var version = "dev"

// General API info for the generated spec (make swagger, ADR 0013).
//
//	@title			gelirgider API
//	@version		1
//	@BasePath		/api
//	@description	Auth is the `session` cookie set by `POST /api/auth/login`; routes that need it answer 401 without it. Swagger 2.0 has no cookie security scheme, so the spec declares none.
//	@description	Routes under `/api/admin/` answer 403 `admin only` for a signed-in non-admin.
//	@description	Every error body is `{"error": "<message>"}`.
//	@description	Request bodies are capped at 64 KB, except the two statement import routes; a larger body with a declared Content-Length answers 413 `request body: larger than 64 KB`; one sent without a length is cut off at 64 KB and answers the route's 400.
//	@description	Rate-limited routes answer 429 `too many requests` with a `Retry-After` header.
//	@description	A POST, PUT, PATCH or DELETE from another origin answers 403 `cross-origin request`.
func main() {
	fetchSource := flag.String("fetch", "", "run one scheduler source for -date and exit (no server); one of "+strings.Join(fetchSourceNames, ", "))
	fetchDate := flag.String("date", price.Today(), "date YYYY-MM-DD for -fetch; default today in Europe/Istanbul")
	var backfillPrices bool
	var backfillFrom string
	var healthcheck bool
	var demoSeed bool
	flag.BoolVar(&backfillPrices, "backfill-prices", false, "backfill historical BIST bulletin prices, TCMB FX rates, BtcTurk crypto closes, TEFAS fund NAVs, Tiingo US closes, the gram gold series and EVDS BIST 100 closes, plus the latest US prices, then exit")
	flag.StringVar(&backfillFrom, "backfill-from", "", "override the BIST/TCMB/crypto/TEFAS/US/gold/XU100 backfill start date (YYYY-MM-DD); default is the earliest transaction date (for BIST, the earliest BIST instrument transaction; for US, each instrument's own earliest transaction)")
	resetEmail := flag.String("set-password", "", "set the password of the user with this email, read from stdin, then exit (no server). Operator escape hatch for a forgotten password; there is no self-service reset")
	addSplit := flag.String("add-split", "", "record a split as MARKET:SYMBOL,YYYY-MM-DD,RATIO (ratio = new shares per old share, date = ex-date), then exit (no server). Operator fallback for splits no provider covers")
	backupPath := flag.String("backup", "", "write a consistent copy of the database to path via VACUUM INTO, then exit (no server)")
	flag.BoolVar(&demoSeed, "demo", false, "fill an empty database with a synthetic demo user, portfolio and two years of prices, then exit (no server); refuses a database that has a user")
	flag.BoolVar(&healthcheck, "healthcheck", false, "GET /api/health on localhost and exit 0/1 (for use as a container HEALTHCHECK; distroless image has no shell/curl)")
	flag.Parse()

	// Without this check a mistyped one-shot run (-date alone, an empty
	// -fetch) starts a second server with its own scheduler.
	// -date defaults to today and the one-shot flags branch on
	// a non-empty value, so only flag.Visit tells a flag was set.
	setFlags := map[string]string{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = f.Value.String() })
	if err := orphanFlag(setFlags); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		if !isHealthy(port) {
			os.Exit(1)
		}
		os.Exit(0)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server.Version = version
	// auth.WriteInternalError logs the error behind every 500 through the
	// default logger, so it lands in the same JSON stream as the request
	// lines without threading this logger through six packages of handlers.
	slog.SetDefault(logger)
	cfg := config.Load()
	if cfg.TrustProxy {
		if cfg.TrustedProxyHops < 1 {
			logger.Error("TRUSTED_PROXY_HOPS must be an integer of at least 1")
			os.Exit(1)
		}
		ratelimit.TrustedProxyHops = cfg.TrustedProxyHops
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
	defer conn.Close()

	if err := instrument.Seed(context.Background(), conn); err != nil {
		logger.Error("failed to seed instruments", "error", err)
		os.Exit(1)
	}

	// secretSettings returns the settings repo, exiting when SECRET_KEY is
	// not valid. Only the server needs it outright (the provider keys are
	// encrypted with it): -backup, -healthcheck and the other flags must
	// work without it, and runFetch checks it for the us and xu100 sources.
	hasSecret := len(cfg.SecretKey) == 32
	secretSettings := func() *settings.Repo {
		if !hasSecret {
			fmt.Fprintln(os.Stderr, secretKeyHelp)
			os.Exit(1)
		}
		return settings.NewRepo(conn, cfg.SecretKey)
	}

	if *backupPath != "" {
		if err := runBackup(conn, *backupPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if demoSeed {
		if err := demo.Seed(context.Background(), conn, price.Today()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("demo data seeded, log in as %s with password %s\n", demo.Email, demo.Password)
		return
	}

	if *resetEmail != "" {
		password, err := readPassword(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := auth.SetPassword(context.Background(), user.NewRepo(conn), auth.NewRepo(conn), *resetEmail, password); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("password set for %s, all its sessions deleted\n", *resetEmail)
		return
	}

	if *addSplit != "" {
		if err := runAddSplit(context.Background(), conn, *addSplit); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *fetchSource != "" {
		if code := runFetch(conn, cfg, logger, *fetchSource, *fetchDate); code != 0 {
			os.Exit(code)
		}
		return
	}

	if backfillPrices {
		// Only the US and XU100 fetches need SECRET_KEY: without it,
		// backfill every other source and skip those.
		var settingsRepo *settings.Repo
		if hasSecret {
			settingsRepo = secretSettings()
		}
		fetchers := newSourceFetchers(settingsRepo)
		if !hasSecret {
			fetchers.us, fetchers.xu100 = nil, nil
			logger.Warn("backfill: SECRET_KEY not set or not valid, skipping us and xu100")
		}
		// TEFAS is opt-in: skipped unless its source is on. The
		// setting is plain text, so it needs no SECRET_KEY.
		if !scheduler.Enabled(context.Background(), settings.NewRepo(conn, nil), scheduler.NameTEFAS, logger) {
			fetchers.tefas = nil
			logger.Info("backfill: tefas source is off, skipping tefas")
		}
		if err := runBackfill(context.Background(), logger, conn, fetchers, backfillFrom); err != nil {
			logger.Error("backfill failed", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	serve(conn, cfg, logger, secretSettings())
}

// secretKeyHelp is what a run that needs SECRET_KEY prints before it exits
// without one.
const secretKeyHelp = "SECRET_KEY must be set to base64 of 32 random bytes (generate one with: openssl rand -base64 32)"

// runFetch runs the -fetch scheduler source once for date and returns
// the exit code: 2 for an unknown source, 1 for a malformed date, a
// missing SECRET_KEY or a failed fetch.
func runFetch(conn *sql.DB, cfg config.Config, logger *slog.Logger, source, date string) int {
	day, err := time.Parse(time.DateOnly, date)
	if err != nil {
		logger.Error("invalid -date, want YYYY-MM-DD", "value", date, "error", err)
		return 1
	}
	// Only the us and xu100 sources read settings (the provider keys
	// are encrypted with SECRET_KEY): those exit without it, the
	// others run without it.
	var settingsRepo *settings.Repo
	if len(cfg.SecretKey) == 32 {
		settingsRepo = settings.NewRepo(conn, cfg.SecretKey)
	}
	src, ok := scheduler.Find(schedulerSources(conn, logger, newSourceFetchers(settingsRepo)), source)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown -fetch source %q, want one of %s\n", source, strings.Join(fetchSourceNames, ", "))
		return 2
	}
	if settingsRepo == nil && (src.Name == scheduler.NameUS || src.Name == scheduler.NameXU100) {
		fmt.Fprintln(os.Stderr, secretKeyHelp)
		return 1
	}
	written, err := src.Sync(context.Background(), day)
	if errors.Is(err, scheduler.ErrNothingToFetch) {
		fmt.Printf("nothing to fetch for %s\n", src.Name)
		return 0
	}
	if err != nil {
		logger.Error("fetch failed", "source", src.Name, "date", date, "error", err)
		return 1
	}
	fmt.Printf("fetched %d rows for %s on %s\n", written, src.Name, date)
	return 0
}

// runBackup writes the -backup copy of conn to path through VACUUM INTO,
// then prints path and its size.
func runBackup(conn *sql.DB, path string) error {
	if err := db.Backup(context.Background(), conn, path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	fmt.Printf("%s %d\n", path, info.Size())
	return nil
}

// serve runs the HTTP server and the scheduler on conn until SIGINT or
// SIGTERM, then shuts the server down.
func serve(conn *sql.DB, cfg config.Config, logger *slog.Logger, settingsRepo *settings.Repo) {
	sources := schedulerSources(conn, logger, newSourceFetchers(settingsRepo))
	// One run log for the scheduler and the admin API: it holds the
	// per-source "running" flag both respect.
	runs := scheduler.NewRunLog(conn)
	if err := runs.ClearUnfinished(context.Background()); err != nil {
		logger.Error("clear unfinished source runs failed", "error", err)
		os.Exit(1)
	}
	handler := server.New(logger, conn, cfg.CookieSecure, settingsRepo, runs, sources...)
	handler = mountDocs(handler, cfg, logger)
	// Timeouts keep a slow or idle client from holding a connection open
	// (slowloris) when the backend is exposed without a proxy. ReadTimeout
	// covers a 5 MB statement upload over a slow link; it does not cut off
	// the handler, since net/http clears the read deadline once the body is
	// read. WriteTimeout stays off, since a statement preview can take 15 s
	// or more.
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Catch up first, so a backend started after a source's fetch time (or
	// after downtime) has today's data instead of waiting for the next run;
	// then fill the history back to the earliest transaction, so a dev or
	// restored database needs no manual -backfill-prices run; then hand over
	// to the minute loop. Each step and each source follows the schedule
	// settings (ADR 0010). All of it runs off the HTTP server's goroutine:
	// startup never blocks on network I/O, and only one network walker runs
	// at a time.
	go func() {
		sc := scheduler.Load(ctx, settingsRepo, sources, logger)
		if sc.CatchUp {
			scheduler.CatchUp(ctx, sc.EnabledSources(sources), scheduler.CatchUpLookbackDays, runs, "catch-up", logger)
		}
		// A shutdown during the catch-up: the backfill and Run would
		// only fail on the cancelled ctx.
		if ctx.Err() != nil {
			return
		}
		if sc.Backfill {
			backfillStartupHistory(ctx, logger, conn, sc, settingsRepo)
		}
		scheduler.Run(ctx, settingsRepo, runs, sources, logger)
	}()

	go func() {
		logger.Info("listening", "version", version, "addr", srv.Addr, "db_path", cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Info("shutting down")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
}

// mountDocs puts Swagger UI and the generated spec in front of handler,
// outside the app's mux so they need no session. Dev only (ADR 0013): off
// without API_DOCS_DIR, and off with COOKIE_SECURE=true, a production setup
// where a CDN script on the app's origin could act as any admin who opens
// the page.
func mountDocs(handler http.Handler, cfg config.Config, logger *slog.Logger) http.Handler {
	if cfg.APIDocsDir == "" {
		return handler
	}
	if cfg.CookieSecure {
		logger.Warn("api docs off: API_DOCS_DIR is ignored when COOKIE_SECURE=true")
		return handler
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/docs/", server.Docs(cfg.APIDocsDir))
	mux.Handle("/", handler)
	logger.Info("api docs on", "url", "http://localhost:"+cfg.Port+"/api/docs/", "dir", cfg.APIDocsDir)
	return mux
}

// readPassword reads the first line of r as the new password, without the
// trailing newline. Anything after that line is ignored, so an operator can
// pipe a file or heredoc.
func readPassword(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// isHealthy reports whether a GET to /api/health on localhost:port succeeds
// with a 2xx status. Used by the -healthcheck flag as the container
// HEALTHCHECK for the distroless image (no shell/curl available there).
func isHealthy(port string) bool {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:"+port+"/api/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 300
}

// runAddSplit parses MARKET:SYMBOL,YYYY-MM-DD,RATIO and upserts the split
// with source "manual" (ADR 0008). Upsert validates the ratio > 0.
// The date is checked like any user-entered date (price.CheckDate): one
// after today (Europe/Istanbul) is rejected, since holdings ignore it until
// then, so it is almost always a typo, and so is one before
// price.MinDate.
func runAddSplit(ctx context.Context, conn *sql.DB, arg string) error {
	parts := strings.Split(arg, ",")
	if len(parts) != 3 {
		return fmt.Errorf("-add-split %q: want MARKET:SYMBOL,YYYY-MM-DD,RATIO", arg)
	}
	if err := price.CheckDate(parts[1]); err != nil {
		return fmt.Errorf("-add-split date %q: %w", parts[1], err)
	}
	market, symbol, ok := strings.Cut(parts[0], ":")
	if !ok {
		return fmt.Errorf("-add-split %q: want MARKET:SYMBOL,YYYY-MM-DD,RATIO", arg)
	}
	in, err := instrument.NewRepo(conn).BySymbol(ctx, strings.ToUpper(market), strings.ToUpper(symbol))
	if err != nil {
		return fmt.Errorf("%s:%s: %w", market, symbol, err)
	}
	ratio, err := decimal.NewFromString(parts[2])
	if err != nil {
		return fmt.Errorf("-add-split ratio %q: %w", parts[2], err)
	}
	if err := split.NewRepo(conn).Upsert(ctx, in.ID, parts[1], ratio, "manual"); err != nil {
		return err
	}
	fmt.Printf("split stored: %s:%s (instrument %d) %s ratio %s source manual\n", in.Market, in.Symbol, in.ID, parts[1], ratio)
	return nil
}

// fetchSourceNames is the -fetch help list; TestFindSource keeps it equal
// to the schedulerSources names.
var fetchSourceNames = []string{scheduler.NameBIST, scheduler.NameBISTSplits, scheduler.NameTCMB, scheduler.NameUS, scheduler.NameCrypto, scheduler.NameTEFAS, scheduler.NameTUFE, scheduler.NameXU100, scheduler.NameGold}

// sourceFetchers holds the fetchers the scheduler sources fetch with, and
// now, the clock their Backfill walks end at (the day before now()), so a
// test can point them at stub servers and a fixed date. gold shares
// crypto's BtcTurk fetcher. A nil fetcher is safe only for a source that
// never runs.
type sourceFetchers struct {
	bist   *bist.Fetcher
	tcmb   *tcmb.Fetcher
	us     *us.Fetcher
	crypto *btcturk.Fetcher
	tefas  *tefas.Fetcher
	tufe   *tuik.Fetcher
	xu100  *evds.Fetcher
	now    func() time.Time
}

// newSourceFetchers returns the production fetchers. us and xu100 read
// their provider keys from settingsRepo on every fetch, so a nil
// settingsRepo (no SECRET_KEY) is fine as long as they never run.
func newSourceFetchers(settingsRepo *settings.Repo) sourceFetchers {
	return sourceFetchers{
		bist:   bist.NewFetcher(),
		tcmb:   tcmb.NewFetcher(),
		us:     us.NewFetcher(settingsRepo),
		crypto: btcturk.NewFetcher(),
		tefas:  tefas.NewFetcher(),
		tufe:   tuik.NewFetcher(),
		xu100:  evds.NewFetcher(settingsRepo),
		now:    time.Now,
	}
}

// schedulerSources is every scheduler source, in run order. Its names are
// the source.<name>.* settings keys (scheduler.Load), and the admin sources
// API (server.New) lists them in this order. The startup backfill and
// -backfill-prices walk their Backfill funcs (backfillHistory).
func schedulerSources(conn *sql.DB, logger *slog.Logger, f sourceFetchers) []scheduler.Source {
	fxRepo := fx.NewRepo(conn)
	priceRepo := price.NewRepo(conn)
	benchmarkRepo := benchmark.NewRepo(conn)
	instrumentRepo := instrument.NewRepo(conn)
	transactionRepo := transaction.NewRepo(conn)
	goldSyncer := &btcturk.GoldSyncer{Fetcher: f.crypto, Instruments: instrumentRepo, Prices: priceRepo, Benchmarks: benchmarkRepo}
	usSyncer := &us.Syncer{Fetcher: f.us, Instruments: instrumentRepo, Prices: priceRepo, Splits: split.NewRepo(conn), Dividends: dividend.NewRepo(conn)}
	tefasSyncer := &tefas.Syncer{Fetcher: f.tefas, Instruments: instrumentRepo, Prices: priceRepo}
	// run is the backfillRun of one Backfill call, ending at that call's
	// f.now().
	run := func(from string) backfillRun {
		return backfillRun{logger: logger, instruments: instrumentRepo, prices: priceRepo, transactions: transactionRepo, from: from, now: f.now()}
	}
	return []scheduler.Source{
		{
			Name: scheduler.NameBIST,
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				return bist.Sync(ctx, f.bist, instrumentRepo, priceRepo, date)
			},
			Have: func(ctx context.Context, date string) (bool, error) {
				return priceRepo.HasSource(ctx, date, bist.SourceBistBulletin)
			},
			Backfill: func(ctx context.Context, from string) (int, error) {
				return run(from).backfillDates(ctx, bist.BulletinBackfillSource(f.bist, instrumentRepo, priceRepo), "BIST")
			},
		},
		{
			// BIST bonus issues (ADR 0008) and cash dividends (ADR
			// 0009). The page holds the whole current year, so one
			// read is the backfill: no Have, and
			// zero rows means nothing to store, not "not published yet".
			Name: scheduler.NameBISTSplits,
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				n, err := bist.SyncCapitalEvents(ctx, f.bist, instrumentRepo, split.NewRepo(conn), dividend.NewRepo(conn), date)
				if err == nil && n == 0 {
					return 0, scheduler.ErrNothingToFetch
				}
				return n, err
			},
		},
		{
			Name: scheduler.NameTCMB,
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				return tcmb.Sync(ctx, f.tcmb, fxRepo, date)
			},
			Have: func(ctx context.Context, date string) (bool, error) {
				return fxRepo.HasSource(ctx, date, tcmb.SourceTCMB)
			},
			Backfill: func(ctx context.Context, from string) (int, error) {
				return run(from).backfillDates(ctx, tcmb.BackfillSource(f.tcmb, fxRepo), "")
			},
		},
		{
			Name: scheduler.NameUS,
			// Finnhub serves only the latest close, which us.Sync stores
			// under the last closed session whatever date the catch-up
			// walks, so Have looks that session up.
			Have: func(ctx context.Context, _ string) (bool, error) {
				return priceRepo.HasSource(ctx, us.LastSession(f.now()), us.SourceFinnhub)
			},
			Sync: func(ctx context.Context, _ time.Time) (int, error) {
				if err := requireTracked(ctx, instrumentRepo, logger, "US"); err != nil {
					return 0, err
				}
				written, err := us.Sync(ctx, f.us, instrumentRepo, priceRepo, f.now())
				if err != nil {
					return written, err
				}
				// Finnhub has no split data: re-read a recent Tiingo
				// window for splits.
				if _, err := usSyncer.SyncRecent(ctx, f.now()); err != nil && ctx.Err() == nil {
					// A shutdown mid-run is not a failure.
					logger.Error("us: recent tiingo sync failed", "error", err)
				}
				return written, nil
			},
			Backfill: func(ctx context.Context, from string) (int, error) {
				return backfillUSHistory(ctx, run(from), usSyncer)
			},
		},
		{
			Name:     scheduler.NameCrypto,
			EveryDay: true,
			Have:     haveDayBefore(priceRepo, btcturk.SourceBtcTurk),
			Sync: trackedSync(instrumentRepo, logger, "CRYPTO", func(ctx context.Context, date time.Time) (int, error) {
				return btcturk.Sync(ctx, f.crypto, instrumentRepo, priceRepo, date)
			}),
			Backfill: func(ctx context.Context, from string) (int, error) {
				return backfillCrypto(ctx, run(from), f.crypto)
			},
		},
		{
			// TEFAS publishes day D's NAV around 13:30 the same day
			// (ADR 0004). Every run re-reads the last month, so a
			// missed day heals on the next run.
			Name:      scheduler.NameTEFAS,
			DefaultAt: "14:00",
			Have: func(ctx context.Context, date string) (bool, error) {
				return priceRepo.HasSource(ctx, date, tefas.SourceTEFAS)
			},
			Sync: trackedSync(instrumentRepo, logger, "TEFAS", func(ctx context.Context, date time.Time) (int, error) {
				return tefas.Sync(ctx, f.tefas, instrumentRepo, priceRepo, date)
			}),
			Backfill: func(ctx context.Context, from string) (int, error) {
				return backfillTEFAS(ctx, run(from), tefasSyncer)
			},
		},
		{
			// TÜİK releases month M's CPI on the 3rd of M+1 at 10:00
			// (ADR 0012), a calendar date rather than a weekday, so the
			// source runs every day. Have makes the runs a no-op once the
			// month is stored. One download is the whole history, so
			// Sync is also the backfill and from is ignored.
			// Have looks up the month before date, the latest that can
			// be published.
			Name:      scheduler.NameTUFE,
			DefaultAt: "11:00",
			EveryDay:  true,
			Have: func(ctx context.Context, date string) (bool, error) {
				d, err := time.Parse(time.DateOnly, date)
				if err != nil {
					return false, err
				}
				prev := time.Date(d.Year(), d.Month()-1, 1, 0, 0, 0, 0, time.UTC)
				return benchmarkRepo.Have(ctx, benchmark.CodeTUFE, prev.Format(time.DateOnly))
			},
			Sync: func(ctx context.Context, _ time.Time) (int, error) {
				return tuik.SyncTUFE(ctx, f.tufe, benchmarkRepo)
			},
			Backfill: func(ctx context.Context, _ string) (int, error) {
				return tuik.SyncTUFE(ctx, f.tufe, benchmarkRepo)
			},
		},
		{
			// BIST 100 closes from TCMB EVDS (ADR 0012), weekdays only.
			// 20:00 because Borsa İstanbul publishes its own index file
			// at about 18:20 and EVDS's update time is unknown: a close
			// this run misses lands with the next day's run through
			// SyncXU100's 14-day window. The owner can change
			// source.xu100.at in the admin area.
			Name:      scheduler.NameXU100,
			DefaultAt: "20:00",
			Have: func(ctx context.Context, date string) (bool, error) {
				return benchmarkRepo.Have(ctx, benchmark.CodeXU100, date)
			},
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				return evds.SyncXU100(ctx, f.xu100, benchmarkRepo, date)
			},
			Backfill: func(ctx context.Context, from string) (int, error) {
				start, ok, err := backfillStartDate(ctx, transactionRepo, from, "")
				if err != nil || !ok {
					return 0, err
				}
				return evds.BackfillXU100(ctx, f.xu100, benchmarkRepo, start, price.Yesterday(f.now()))
			},
		},
		{
			// Gram gold from BtcTurk PAXG-TRY (ADR 0017), every day like
			// crypto. No tracked-instrument check: the GOLD benchmark
			// needs the series whether or not anyone holds gold.
			Name:     scheduler.NameGold,
			EveryDay: true,
			Have:     haveDayBefore(priceRepo, btcturk.SourcePAXG),
			Sync: func(ctx context.Context, date time.Time) (int, error) {
				return goldSyncer.SyncGold(ctx, date.AddDate(0, 0, -7), date.Add(24*time.Hour))
			},
			// The backfill stores the series from the backfill start
			// date (from, or the earliest transaction of any market) up
			// to now via SyncGoldHistory, which clamps the range and
			// skips it when it is already stored. No
			// transaction and no from is nothing to fetch.
			Backfill: func(ctx context.Context, from string) (int, error) {
				start, ok, err := backfillStartDate(ctx, transactionRepo, from, "")
				if err != nil || !ok {
					return 0, err
				}
				return goldSyncer.SyncGoldHistory(ctx, start.Format(time.DateOnly), f.now())
			},
		},
	}
}

// requireTracked returns ErrNothingToFetch when no one tracks an
// instrument of market, so the source skips its fetch.
func requireTracked(ctx context.Context, instruments *instrument.Repo, logger *slog.Logger, market string) error {
	name := strings.ToLower(market)
	tracked, err := instruments.Tracked(ctx, market)
	if err != nil {
		return fmt.Errorf("list tracked %s instruments: %w", name, err)
	}
	if len(tracked) == 0 {
		logger.Info(name + ": no tracked instruments, nothing to fetch")
		return scheduler.ErrNothingToFetch
	}
	return nil
}

// trackedSync runs sync only when someone tracks an instrument of market
// (requireTracked).
func trackedSync(instruments *instrument.Repo, logger *slog.Logger, market string, sync scheduler.SyncFunc) scheduler.SyncFunc {
	return func(ctx context.Context, date time.Time) (int, error) {
		if err := requireTracked(ctx, instruments, logger, market); err != nil {
			return 0, err
		}
		return sync(ctx, date)
	}
}

// haveDayBefore is the Have of a btcturk source: btcturk stores finished
// candles only, so the day before date is the newest one that can have a
// row.
func haveDayBefore(prices *price.Repo, source string) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, date string) (bool, error) {
		d, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return false, err
		}
		return prices.HasSource(ctx, d.AddDate(0, 0, -1).Format(time.DateOnly), source)
	}
}

// oneShotArgs names the argument of each one-shot string flag; main runs
// the job only for a non-empty value, so an empty one would start a server.
var oneShotArgs = map[string]string{
	"fetch":        "a SOURCE, one of " + strings.Join(fetchSourceNames, ", "),
	"backup":       "a PATH",
	"set-password": "an EMAIL",
	"add-split":    "MARKET:SYMBOL,YYYY-MM-DD,RATIO",
}

// orphanFlag returns a usage error when a one-shot flag is set to an empty
// value or a parameter flag is set without the flag it parameterises; set
// maps each flag flag.Visit saw to its value.
func orphanFlag(set map[string]string) error {
	for name, arg := range oneShotArgs {
		if v, ok := set[name]; ok && v == "" {
			return fmt.Errorf("-%s needs %s", name, arg)
		}
	}
	if _, ok := set["date"]; ok && set["fetch"] == "" {
		return errors.New("-date needs -fetch SOURCE")
	}
	if _, ok := set["backfill-from"]; ok && set["backfill-prices"] != "true" {
		return errors.New("-backfill-from needs -backfill-prices")
	}
	return nil
}
