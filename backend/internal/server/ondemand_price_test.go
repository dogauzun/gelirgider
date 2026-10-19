package server

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/btcturk"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/tcmb"
	"github.com/dogauzun/gelirgider/backend/internal/tefas"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/us"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// Buying an instrument with no price row must fetch one
// synchronously, inside the transaction request, instead of waiting for the
// next scheduler run. These tests exercise that at the HTTP layer with
// httptest.Server stubs standing in for the bulletin/Finnhub/TCMB
// endpoints, so they never touch the real network.

// newOnDemandTestEnv is newTransactionTestEnv with the market-data fetchers
// injected, so the on-demand price fetch talks to test stubs instead of the
// real bulletin/Finnhub/TCMB endpoints.
func newOnDemandTestEnv(t *testing.T, bistFetcher *bist.Fetcher, usFetcher *us.Fetcher, tcmbFetcher *tcmb.Fetcher, btcturkFetcher *btcturk.Fetcher) *transactionTestEnv {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn := openBackfillTestDB(t)

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	return &transactionTestEnv{
		t:            t,
		handler:      newHandler(logger, conn, false, deps{cfg: settings.NewRepo(conn, nil), bistFetcher: bistFetcher, usFetcher: usFetcher, tcmbFetcher: tcmbFetcher, btcturkFetcher: btcturkFetcher, tefasFetcher: tefas.NewFetcher()}),
		db:           conn,
		instrumentID: instrumentID,
		users:        user.NewRepo(conn),
		sessions:     auth.NewRepo(conn),
	}
}

// bulletinZip builds an in-memory zip containing one CSV member, matching
// how the real bulletin ships (see internal/bist for the format).
func bulletinZip(t *testing.T, csv string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("thb.csv")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(csv)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

// priceRowCount returns how many prices rows exist for instrumentID,
// optionally narrowed to a source.
func priceRowCount(t *testing.T, env *transactionTestEnv, instrumentID int64, source string) int {
	t.Helper()
	var count int
	var err error
	if source == "" {
		err = env.db.QueryRow(`SELECT COUNT(*) FROM prices WHERE instrument_id = ?`, instrumentID).Scan(&count)
	} else {
		err = env.db.QueryRow(`SELECT COUNT(*) FROM prices WHERE instrument_id = ? AND source = ?`, instrumentID, source).Scan(&count)
	}
	if err != nil {
		t.Fatalf("count prices: %v", err)
	}
	return count
}

// TestOnDemandFetchBIST covers the BIST branch: a buy against a fresh BIST
// instrument with an empty prices table triggers a bulletin fetch starting
// today, walking back one day on 404 until a bulletin is found.
func TestOnDemandFetchBIST(t *testing.T) {
	todayDigits := time.Now().In(price.Istanbul).Format("20060102")

	zipBytes := bulletinZip(t, "TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n"+
		"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n"+
		"2026-09-14;TEST.E;TEST INSTRUMENT;EQT;100.50\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Today's bulletin is not out yet; any earlier date is.
		if strings.Contains(r.URL.Path, todayDigits) {
			http.NotFound(w, r)
			return
		}
		w.Write(zipBytes)
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: srv.URL + "/"}, us.NewFetcher(nil), &tcmb.Fetcher{BaseURL: srv.URL + "/"}, btcturk.NewFetcher())
	instrumentID := env.instrumentID
	session := env.login("alice@example.com")
	portfolioID := env.createPortfolio(session, "Test")

	// Dated today so no background backfill starts: yesterday is
	// before the start date, and this test covers only the synchronous part.
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": price.Today(), "instrument_id": instrumentID, "quantity": "10", "price": "5",
	})

	if got := priceRowCount(t, env, instrumentID, "bist_bulletin"); got == 0 {
		t.Fatalf("bist_bulletin price rows = 0, want at least 1 after on-demand fetch")
	}
}

// TestOnDemandFetchSkipsDisabledSource checks that with bist and tcmb
// disabled in settings, a backdated BIST buy fetches nothing, synchronously
// or in the background.
func TestOnDemandFetchSkipsDisabledSource(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: srv.URL + "/"}, us.NewFetcher(nil), &tcmb.Fetcher{BaseURL: srv.URL + "/"}, btcturk.NewFetcher())
	repo := settings.NewRepo(env.db, nil)
	for _, name := range []string{"bist", "tcmb"} {
		if err := repo.Set(context.Background(), scheduler.EnabledKey(name), "false"); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	session := env.login("carol@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2026-09-01", "instrument_id": env.instrumentID, "quantity": "10", "price": "5",
	})

	time.Sleep(200 * time.Millisecond) // a background backfill would have started by now
	if n := hits.Load(); n != 0 {
		t.Fatalf("fetch requests = %d, want 0 with bist and tcmb disabled", n)
	}
}

// TestOnDemandFetchCancelledKeepsSettings covers a client that
// disconnects mid-save, which cancels the on-demand fetch's ctx. The settings
// reads must still see the real values: no ERROR logged, a disabled TCMB
// queues no backfill, an enabled one still does.
func TestOnDemandFetchCancelledKeepsSettings(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, enabled := range []bool{false, true} {
		var requests atomic.Int32
		conn := openBackfillTestDB(t)
		repo := settings.NewRepo(conn, nil)
		if err := repo.Set(context.Background(), scheduler.EnabledKey("tcmb"), strconv.FormatBool(enabled)); err != nil {
			t.Fatalf("set tcmb enabled: %v", err)
		}
		// Gold runs on every save and needs repos this test does
		// not wire; it covers only the tcmb setting.
		if err := repo.Set(context.Background(), scheduler.EnabledKey("gold"), "false"); err != nil {
			t.Fatalf("disable gold: %v", err)
		}
		fetch := newOnDemandFetch(&deps{cfg: repo, fx: fx.NewRepo(conn), tcmbFetcher: tcmbStub(t, &requests)}, &backfillQueue{})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		fetch(ctx, instrument.Instrument{}, weekdaysBack(2)[0])
		backfillWorkers.Wait()

		if n := requests.Load(); (n > 0) != enabled {
			t.Fatalf("tcmb enabled=%v: requests = %d", enabled, n)
		}
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("ERROR logged for a cancelled request:\n%s", logs.String())
	}
}

// TestOnDemandFetchExpiredSkipsGold checks that once an import's
// shared deadline has run out, the gold step of every later target is
// skipped instead of logging an ERROR about a cut-off DB query.
func TestOnDemandFetchExpiredSkipsGold(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	conn := openBackfillTestDB(t)
	repo := settings.NewRepo(conn, nil)
	if err := repo.Set(context.Background(), scheduler.EnabledKey("tcmb"), "false"); err != nil {
		t.Fatalf("disable tcmb: %v", err)
	}
	fetch := newOnDemandFetch(&deps{cfg: repo, instruments: instrument.NewRepo(conn), prices: price.NewRepo(conn), benchmarks: benchmark.NewRepo(conn), fx: fx.NewRepo(conn), btcturkFetcher: &btcturk.Fetcher{BaseURL: srv.URL}}, &backfillQueue{})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	fetch(ctx, instrument.Instrument{}, weekdaysBack(2)[0])

	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("ERROR logged for an expired deadline:\n%s", logs.String())
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("btcturk requests = %d, want 0", n)
	}
}

// TestOnDemandFetchTefasOptIn covers the tefas source: TEFAS's endpoint is
// disallowed by its robots.txt, so it is off until an admin
// turns it on. With its setting unset, a TEFAS buy asks TEFAS nothing; with
// it on, it does.
func TestOnDemandFetchTefasOptIn(t *testing.T) {
	for _, setting := range []string{"", "true"} {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			w.Write([]byte(`{"resultList":[]}`))
		}))
		conn := openBackfillTestDB(t)
		repo := settings.NewRepo(conn, nil)
		ctx := context.Background()
		for _, name := range []string{"gold", "tcmb"} {
			if err := repo.Set(ctx, scheduler.EnabledKey(name), "false"); err != nil {
				t.Fatalf("disable %s: %v", name, err)
			}
		}
		if setting != "" {
			if err := repo.Set(ctx, scheduler.EnabledKey("tefas"), setting); err != nil {
				t.Fatalf("set tefas enabled: %v", err)
			}
		}
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AFT', 'TEFAS', 'Test Fonu', 'TRY')`)
		if err != nil {
			t.Fatalf("insert tefas instrument: %v", err)
		}
		id, _ := res.LastInsertId()
		fetch := newOnDemandFetch(&deps{cfg: repo, instruments: instrument.NewRepo(conn), prices: price.NewRepo(conn), fx: fx.NewRepo(conn), tefasFetcher: &tefas.Fetcher{BaseURL: srv.URL}}, &backfillQueue{})

		fetch(ctx, instrument.Instrument{ID: id, Symbol: "AFT", Market: "TEFAS", Name: "Test Fonu", Currency: "TRY"}, weekdaysBack(2)[0])
		backfillWorkers.Wait()
		srv.Close()

		if n := requests.Load(); (n > 0) != (setting == "true") {
			t.Errorf("tefas setting %q: requests = %d", setting, n)
		}
	}
}

// TestOnDemandFetchUS covers the US branch: a buy against a fresh US
// instrument fetches its latest close from Finnhub and stores it with
// source "finnhub".
func TestOnDemandFetchUS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"c": 123.45, "pc": 122, "t": 1790107200}`))
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, bist.NewFetcher(), &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key"}, &tcmb.Fetcher{BaseURL: srv.URL + "/"}, btcturk.NewFetcher())

	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple Inc.', 'USD')`)
	if err != nil {
		t.Fatalf("insert us instrument: %v", err)
	}
	usInstrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("us instrument id: %v", err)
	}

	session := env.login("bob@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-01", "instrument_id": usInstrumentID, "quantity": "1", "price": "100",
	})

	if got := priceRowCount(t, env, usInstrumentID, "finnhub"); got != 1 {
		t.Fatalf("finnhub price rows = %d, want 1", got)
	}
}

// TestOnDemandFetchUSNoKeyLogsWarn covers an install with no Finnhub key: an
// unset key is an expected state, so the skipped fetch is logged at WARN, not
// ERROR, and makes no request.
func TestOnDemandFetchUSNoKeyLogsWarn(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, bist.NewFetcher(), &us.Fetcher{BaseURL: srv.URL}, &tcmb.Fetcher{BaseURL: srv.URL + "/"}, btcturk.NewFetcher())
	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple Inc.', 'USD')`)
	if err != nil {
		t.Fatalf("insert us instrument: %v", err)
	}
	usInstrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("us instrument id: %v", err)
	}

	session := env.login("bob@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-01", "instrument_id": usInstrumentID, "quantity": "1", "price": "100",
	})

	if n := requests.Load(); n != 0 {
		t.Fatalf("requests = %d, want 0 with no Finnhub key", n)
	}
	out := logs.String()
	// The gold fetch of the same create logs its own ERROR on this test DB, so
	// only the us line is checked.
	if !strings.Contains(out, `"level":"WARN","msg":"on-demand us price fetch failed"`) || strings.Contains(out, `"level":"ERROR","msg":"on-demand us price fetch failed"`) {
		t.Fatalf("log = %s, want the on-demand us fetch failure at WARN, not ERROR", out)
	}
}

// TestOnDemandFetchUSHistory checks that a US buy dated 30 days ago
// asks Tiingo for daily closes from that date (padded back a week) in one
// request and stores them with source "tiingo".
func TestOnDemandFetchUSHistory(t *testing.T) {
	date := time.Now().UTC().AddDate(0, 0, -30)
	var mu sync.Mutex
	var gotPath, gotStart, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/quote" {
			w.Write([]byte(`{"pc": 123.45}`))
			return
		}
		mu.Lock()
		gotPath, gotStart, gotAuth = r.URL.Path, r.URL.Query().Get("startDate"), r.Header.Get("Authorization")
		mu.Unlock()
		fmt.Fprintf(w, `[{"date":"%sT00:00:00.000Z","close":101.5},{"date":"%sT00:00:00.000Z","close":102.25}]`,
			date.AddDate(0, 0, -1).Format(time.DateOnly), date.Format(time.DateOnly))
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, bist.NewFetcher(), &us.Fetcher{BaseURL: srv.URL, APIKey: "test-key", TiingoBaseURL: srv.URL, TiingoKey: "tiingo-key"}, tcmbStub(t, nil), btcturk.NewFetcher())
	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('BRK.B', 'US', 'Berkshire Hathaway', 'USD')`)
	if err != nil {
		t.Fatalf("insert us instrument: %v", err)
	}
	usInstrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("us instrument id: %v", err)
	}

	session := env.login("heidi@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": date.Format(time.DateOnly), "instrument_id": usInstrumentID, "quantity": "1", "price": "100",
	})

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/tiingo/daily/BRK-B/prices" {
		t.Fatalf("tiingo path = %q, want /tiingo/daily/BRK-B/prices", gotPath)
	}
	if want := date.AddDate(0, 0, -7).Format(time.DateOnly); gotStart != want {
		t.Fatalf("tiingo startDate = %q, want %s (transaction date minus a week)", gotStart, want)
	}
	if gotAuth != "Token tiingo-key" {
		t.Fatalf("tiingo Authorization = %q, want Token tiingo-key", gotAuth)
	}
	if got := priceRowCount(t, env, usInstrumentID, "tiingo"); got != 2 {
		t.Fatalf("tiingo price rows = %d, want 2", got)
	}
}

// TestOnDemandFetchCrypto checks that a buy dated today against a
// fresh CRYPTO pair fetches BtcTurk closes in one request, with from
// clamped back to six days before yesterday so the pair has a last price.
func TestOnDemandFetchCrypto(t *testing.T) {
	yesterday := btcturk.LastCandleDay(time.Now())
	wantFrom := yesterday.AddDate(0, 0, -6)
	var mu sync.Mutex
	var gotSymbol, gotFrom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSymbol, gotFrom = r.URL.Query().Get("symbol"), r.URL.Query().Get("from")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"s":"ok","t":[%d,%d],"c":[0.00024,0.00025]}`, yesterday.AddDate(0, 0, -1).Unix(), yesterday.Unix())
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, bist.NewFetcher(), us.NewFetcher(nil), tcmbStub(t, nil), &btcturk.Fetcher{BaseURL: srv.URL})

	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('PEPE-TRY', 'CRYPTO', 'Pepe', 'TRY')`)
	if err != nil {
		t.Fatalf("insert crypto instrument: %v", err)
	}
	cryptoID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("crypto instrument id: %v", err)
	}

	session := env.login("grace@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": time.Now().In(price.Istanbul).Format(time.DateOnly), "instrument_id": cryptoID, "quantity": "1000000", "price": "0.00024",
	})

	mu.Lock()
	defer mu.Unlock()
	if gotSymbol != "PEPETRY" {
		t.Fatalf("btcturk symbol = %q, want PEPETRY", gotSymbol)
	}
	if want := fmt.Sprint(wantFrom.Unix()); gotFrom != want {
		t.Fatalf("btcturk from = %s, want %s (six days before yesterday)", gotFrom, want)
	}
	if got := priceRowCount(t, env, cryptoID, "btcturk"); got != 2 {
		t.Fatalf("btcturk price rows = %d, want 2", got)
	}
}

// TestOnDemandFetchGold checks that a GOLD/GRAM buy dated 30 days ago,
// with no gold price stored, fetches the PAXGTRY candles from that date in
// one request. A second save finds the range stored and fetches nothing,
// and with the gold source disabled a save fetches nothing either.
func TestOnDemandFetchGold(t *testing.T) {
	yesterday := btcturk.LastCandleDay(time.Now())
	date := yesterday.AddDate(0, 0, -29)
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Query().Get("symbol")+" from "+r.URL.Query().Get("from"))
		mu.Unlock()
		var ts, cs []string
		for d := date; !d.After(yesterday); d = d.AddDate(0, 0, 1) {
			ts, cs = append(ts, fmt.Sprint(d.Unix())), append(cs, "200000")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"s":"ok","t":[%s],"c":[%s]}`, strings.Join(ts, ","), strings.Join(cs, ","))
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, bist.NewFetcher(), us.NewFetcher(nil), tcmbStub(t, nil), &btcturk.Fetcher{BaseURL: srv.URL})
	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('GRAM', 'GOLD', 'Gram Altın', 'TRY')`)
	if err != nil {
		t.Fatalf("insert gold instrument: %v", err)
	}
	gramID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("gold instrument id: %v", err)
	}

	session := env.login("heidi@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	body := map[string]any{"type": "buy", "date": date.Format(time.DateOnly), "instrument_id": gramID, "quantity": "10", "price": "6000"}
	env.createTransaction(session, portfolioID, body)
	env.createTransaction(session, portfolioID, body)

	mu.Lock()
	if want := "PAXGTRY from " + fmt.Sprint(date.Unix()); len(requests) != 1 || requests[0] != want {
		mu.Unlock()
		t.Fatalf("btcturk requests = %v, want one: %s", requests, want)
	}
	mu.Unlock()
	if got := priceRowCount(t, env, gramID, "btcturk_paxg"); got != 30 {
		t.Fatalf("btcturk_paxg price rows = %d, want 30", got)
	}

	if _, err := env.db.Exec(`DELETE FROM prices`); err != nil {
		t.Fatalf("delete prices: %v", err)
	}
	if err := settings.NewRepo(env.db, nil).Set(context.Background(), scheduler.EnabledKey("gold"), "false"); err != nil {
		t.Fatalf("disable gold: %v", err)
	}
	env.createTransaction(session, portfolioID, body)
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("btcturk requests = %v, want no new one with gold disabled", requests)
	}
}

// TestOnDemandFetchGoldBenchmark checks that on an empty database a
// backdated BIST buy fills the GOLD benchmark from the transaction date in
// one BtcTurk request, a second save makes none, and with the gold source
// disabled a save makes none either.
func TestOnDemandFetchGoldBenchmark(t *testing.T) {
	yesterday := btcturk.LastCandleDay(time.Now())
	date := yesterday.AddDate(0, 0, -9)
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Query().Get("symbol")+" from "+r.URL.Query().Get("from"))
		mu.Unlock()
		var ts, cs []string
		for d := date; !d.After(yesterday); d = d.AddDate(0, 0, 1) {
			ts, cs = append(ts, fmt.Sprint(d.Unix())), append(cs, "200000")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"s":"ok","t":[%s],"c":[%s]}`, strings.Join(ts, ","), strings.Join(cs, ","))
	}))
	defer srv.Close()
	bulletinSrv := httptest.NewServer(http.NotFoundHandler())
	defer bulletinSrv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: bulletinSrv.URL + "/"}, us.NewFetcher(nil), tcmbStub(t, nil), &btcturk.Fetcher{BaseURL: srv.URL})
	// The instrument seed creates GOLD/GRAM in production; the test DB has
	// only the schema.
	if _, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('GRAM', 'GOLD', 'Gram Altın', 'TRY')`); err != nil {
		t.Fatalf("insert gold instrument: %v", err)
	}

	session := env.login("ivan@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	body := map[string]any{"type": "buy", "date": date.Format(time.DateOnly), "instrument_id": env.instrumentID, "quantity": "10", "price": "5"}
	env.createTransaction(session, portfolioID, body)
	env.createTransaction(session, portfolioID, body)

	mu.Lock()
	if want := "PAXGTRY from " + fmt.Sprint(date.Unix()); len(requests) != 1 || requests[0] != want {
		mu.Unlock()
		t.Fatalf("btcturk requests = %v, want one: %s", requests, want)
	}
	mu.Unlock()
	var count int
	var first string
	if err := env.db.QueryRow(`SELECT COUNT(*), MIN(date) FROM benchmark_values WHERE code = 'GOLD'`).Scan(&count, &first); err != nil {
		t.Fatalf("count GOLD benchmark rows: %v", err)
	}
	if count != 10 || first != date.Format(time.DateOnly) {
		t.Fatalf("GOLD benchmark rows = %d from %s, want 10 from %s", count, first, date.Format(time.DateOnly))
	}

	if _, err := env.db.Exec(`DELETE FROM prices`); err != nil {
		t.Fatalf("delete prices: %v", err)
	}
	if err := settings.NewRepo(env.db, nil).Set(context.Background(), scheduler.EnabledKey("gold"), "false"); err != nil {
		t.Fatalf("disable gold: %v", err)
	}
	env.createTransaction(session, portfolioID, body)
	backfillWorkers.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("btcturk requests = %v, want no new one with gold disabled", requests)
	}
}

// TestOnDemandFetchErrorDoesNotBlockCreate covers the failure path: a fetch
// error (a 500 here) is logged and ignored, and the transaction's 201 still
// goes out with no price row written.
func TestOnDemandFetchErrorDoesNotBlockCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: srv.URL + "/"}, us.NewFetcher(nil), &tcmb.Fetcher{BaseURL: srv.URL + "/"}, btcturk.NewFetcher())
	instrumentID := env.instrumentID
	session := env.login("carol@example.com")
	portfolioID := env.createPortfolio(session, "Test")

	rec := env.do(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID), map[string]any{
		"type": "buy", "date": "2024-01-01", "instrument_id": instrumentID, "quantity": "10", "price": "5",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if got := priceRowCount(t, env, instrumentID, ""); got != 0 {
		t.Fatalf("price rows = %d, want 0 after a fetch error", got)
	}
}

// bulletinZipDated is bulletinZip with a one-row bulletin for date, built
// without *testing.T so it can run inside a stub server's handler.
func bulletinZipDated(date string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("thb.csv")
	w.Write([]byte("TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n" +
		"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n" +
		date + ";TEST.E;TEST INSTRUMENT;EQT;100.50\n"))
	zw.Close()
	return buf.Bytes()
}

// TestOnDemandBackfillBIST checks that a buy dated three weekdays ago
// backfills one bist_bulletin row per weekday from that date through
// yesterday in the background, on top of today's from the synchronous
// fetch. A second buy posted while that backfill runs must not start a
// second walk, so the stub sees exactly one request per date.
func TestOnDemandBackfillBIST(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// Path ends in thbYYYYMMDD1.zip; answer a bulletin dated that day.
		digits := strings.TrimSuffix(strings.TrimPrefix(path.Base(r.URL.Path), "thb"), "1.zip")
		d, err := time.Parse("20060102", digits)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Write(bulletinZipDated(d.Format(time.DateOnly)))
	}))
	defer srv.Close()

	today := price.Midnight(time.Now().In(price.Istanbul))
	var want []string // weekdays from three weekdays ago through yesterday
	start := today
	for len(want) < 3 {
		start = start.AddDate(0, 0, -1)
		if start.Weekday() != time.Saturday && start.Weekday() != time.Sunday {
			want = append([]string{start.Format(time.DateOnly)}, want...)
		}
	}
	want = append(want, today.Format(time.DateOnly))

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: srv.URL + "/"}, us.NewFetcher(nil), tcmbStub(t, nil), btcturk.NewFetcher())
	instrumentID := env.instrumentID
	session := env.login("dave@example.com")
	portfolioID := env.createPortfolio(session, "Test")

	body := map[string]any{"type": "buy", "date": want[0], "instrument_id": instrumentID, "quantity": "10", "price": "5"}
	env.createTransaction(session, portfolioID, body)
	// The first walk pauses bist.RequestPause between dates, so it is
	// still running when this second save lands.
	env.createTransaction(session, portfolioID, body)
	backfillWorkers.Wait()

	rows, err := env.db.Query(`SELECT date FROM prices WHERE instrument_id = ? AND source = 'bist_bulletin' ORDER BY date`, instrumentID)
	if err != nil {
		t.Fatalf("query prices: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan price date: %v", err)
		}
		got = append(got, d)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bist_bulletin dates = %v, want %v", got, want)
	}
	if n := requests.Load(); int(n) != len(want) {
		t.Fatalf("stub requests = %d, want %d (one per date, no second walk)", n, len(want))
	}
}

// tcmbBulletinXML is a minimal TCMB daily bulletin: the rate date comes
// from the requested URL, not the body, so one body serves every date.
const tcmbBulletinXML = `<?xml version="1.0" encoding="UTF-8"?>
<Tarih_Date>
<Currency CurrencyCode="USD"><Unit>1</Unit><ForexBuying>40.1234</ForexBuying></Currency>
<Currency CurrencyCode="EUR"><Unit>1</Unit><ForexBuying>45.6789</ForexBuying></Currency>
</Tarih_Date>`

// tcmbStub returns a tcmb.Fetcher pointed at a stub server that answers
// tcmbBulletinXML for every date, counting requests in requests when it is
// not nil.
func tcmbStub(t *testing.T, requests *atomic.Int32) *tcmb.Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			requests.Add(1)
		}
		w.Write([]byte(tcmbBulletinXML))
	}))
	t.Cleanup(srv.Close)
	return &tcmb.Fetcher{BaseURL: srv.URL + "/"}
}

// weekdaysBack returns the weekday dates from the nth weekday before today
// (Europe/Istanbul) through yesterday, which is the range the on-demand
// backfill walks for a transaction dated on the first of them.
func weekdaysBack(n int) []string {
	today := price.Midnight(time.Now().In(price.Istanbul))
	start := today
	for left := n; left > 0; {
		start = start.AddDate(0, 0, -1)
		if start.Weekday() != time.Saturday && start.Weekday() != time.Sunday {
			left--
		}
	}
	var dates []string
	for d := start; d.Before(today); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d.Format(time.DateOnly))
		}
	}
	return dates
}

// TestOnDemandBackfillFX checks that a buy dated three weekdays ago
// backfills one TCMB rate set per weekday from that date through
// yesterday, for a market with no price source of its own (TEFAS), and
// downloads no BIST bulletin doing it.
func TestOnDemandBackfillFX(t *testing.T) {
	var bulletinRequests atomic.Int32
	bulletinSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bulletinRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer bulletinSrv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: bulletinSrv.URL + "/"}, us.NewFetcher(nil), tcmbStub(t, nil), btcturk.NewFetcher())

	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AFT', 'TEFAS', 'Test Fonu', 'TRY')`)
	if err != nil {
		t.Fatalf("insert tefas instrument: %v", err)
	}
	fundID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("tefas instrument id: %v", err)
	}

	want := weekdaysBack(3)
	session := env.login("erin@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": want[0], "instrument_id": fundID, "quantity": "10", "price": "5",
	})
	backfillWorkers.Wait()

	if got := fxDates(t, env); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tcmb fx dates = %v, want %v", got, want)
	}
	if n := bulletinRequests.Load(); n != 0 {
		t.Fatalf("bulletin requests = %d, want 0 for a TEFAS transaction", n)
	}
}

// TestOnDemandBackfillFXCashOnly covers a USD deposit, which carries no
// instrument and so used to skip the on-demand callback entirely. It must
// still backfill one TCMB rate set per weekday from its date through
// yesterday, and download no BIST bulletin doing it.
func TestOnDemandBackfillFXCashOnly(t *testing.T) {
	var bulletinRequests atomic.Int32
	bulletinSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bulletinRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer bulletinSrv.Close()

	env := newOnDemandTestEnv(t, &bist.Fetcher{BaseURL: bulletinSrv.URL + "/"}, us.NewFetcher(nil), tcmbStub(t, nil), btcturk.NewFetcher())

	want := weekdaysBack(3)
	session := env.login("frank@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	env.createTransaction(session, portfolioID, map[string]any{
		"type": "deposit", "date": want[0], "amount": "1000", "currency": "USD",
	})
	backfillWorkers.Wait()

	if got := fxDates(t, env); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tcmb fx dates = %v, want %v", got, want)
	}
	if n := bulletinRequests.Load(); n != 0 {
		t.Fatalf("bulletin requests = %d, want 0 for a cash transaction", n)
	}
}

// fxDates returns the distinct dates with a tcmb-sourced fx_rates row,
// ascending.
func fxDates(t *testing.T, env *transactionTestEnv) []string {
	t.Helper()
	rows, err := env.db.Query(`SELECT DISTINCT date FROM fx_rates WHERE source = 'tcmb' ORDER BY date`)
	if err != nil {
		t.Fatalf("query fx_rates: %v", err)
	}
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan fx date: %v", err)
		}
		dates = append(dates, d)
	}
	return dates
}

// TestBackfillQueueRunsSkippedRequest covers a second backfill
// requested while one is running, which used to be dropped. Both ranges must end
// up filled, including the earlier start of the second request.
func TestBackfillQueueRunsSkippedRequest(t *testing.T) {
	var q backfillQueue
	var mu sync.Mutex
	synced := map[string]bool{}
	release := make(chan struct{})
	var once sync.Once

	src := bist.BackfillSource{
		Name: "test",
		HasSource: func(ctx context.Context, date string) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			return synced[date], nil
		},
		Sync: func(ctx context.Context, d time.Time) (int, error) {
			// Hold the first walk open so the second request is queued
			// while it runs, which is the case that used to be skipped.
			once.Do(func() { <-release })
			mu.Lock()
			defer mu.Unlock()
			synced[d.Format(time.DateOnly)] = true
			return 1, nil
		},
	}

	near, far := weekdaysBack(2), weekdaysBack(5)
	in := instrument.Instrument{ID: 1, Symbol: "TEST", Market: "BIST"}
	startBackfill(&q, in, near[0], []bist.BackfillSource{src})
	startBackfill(&q, in, far[0], []bist.BackfillSource{src})
	close(release)
	backfillWorkers.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, d := range far {
		if !synced[d] {
			t.Fatalf("date %s not backfilled; synced = %v, want all of %v", d, synced, far)
		}
	}
}

// TestBackfillQueueSkipsNoopOnDemandRun checks that an on-demand walk
// whose source already has every date adds no source_runs row, so it can't
// hide the daily fetch as the last run. An admin walk is still logged.
func TestBackfillQueueSkipsNoopOnDemandRun(t *testing.T) {
	conn := openBackfillTestDB(t)
	q := &backfillQueue{runs: scheduler.NewRunLog(conn)}
	count := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow("SELECT COUNT(*) FROM source_runs WHERE source = 'tcmb'").Scan(&n); err != nil {
			t.Fatalf("count source_runs: %v", err)
		}
		return n
	}

	src := bist.BackfillSource{
		Name:      "tcmb",
		HasSource: func(ctx context.Context, date string) (bool, error) { return true, nil },
		Sync: func(ctx context.Context, d time.Time) (int, error) {
			t.Errorf("Sync(%s) called for a date the source already has", d.Format(time.DateOnly))
			return 0, nil
		},
	}
	startBackfill(q, instrument.Instrument{ID: 1, Symbol: "USD", Market: "CASH"}, weekdaysBack(3)[0], []bist.BackfillSource{src})
	backfillWorkers.Wait()
	if n := count(); n != 0 {
		t.Fatalf("source_runs rows after a no-op on-demand walk = %d, want 0", n)
	}

	q.addWalk("tcmb", func(ctx context.Context) (int, error) { return 0, nil })
	backfillWorkers.Wait()
	if n := count(); n != 1 {
		t.Fatalf("source_runs rows after an admin walk = %d, want 1", n)
	}
}

// Every market an instrument can have maps to a scheduler
// source, so the on-demand fetch never reads an unknown enabled key.
// Markets are read from the seed, so a new seeded market fails here
// until marketSource lists it.
func TestMarketSourceCoversSeededMarkets(t *testing.T) {
	conn := openBackfillTestDB(t)
	if err := instrument.Seed(context.Background(), conn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err := conn.Query(`SELECT DISTINCT market FROM instruments`)
	if err != nil {
		t.Fatalf("query markets: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatalf("scan: %v", err)
		}
		n++
		if marketSource[m] == "" {
			t.Errorf("market %q has no scheduler source in marketSource", m)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if n == 0 {
		t.Fatal("seed produced no markets")
	}
}

// TestOnDemandFetchCoveredNeverQueues checks that a save whose fund history
// is already stored does not wait for the fetch slot while another fetch
// holds it, and makes no request.
func TestOnDemandFetchCoveredNeverQueues(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(`{"resultList":[]}`))
	}))
	defer srv.Close()
	conn := openBackfillTestDB(t)
	repo := settings.NewRepo(conn, nil)
	ctx := context.Background()
	for name, on := range map[string]string{"gold": "false", "tcmb": "false", "tefas": "true"} {
		if err := repo.Set(ctx, scheduler.EnabledKey(name), on); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AFT', 'TEFAS', 'Test Fonu', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, _ := res.LastInsertId()
	date := weekdaysBack(3)[0]
	if err := price.NewRepo(conn).Upsert(ctx, id, date, "1.5", "tefas"); err != nil {
		t.Fatalf("upsert price: %v", err)
	}
	fetch := newOnDemandFetch(&deps{cfg: repo, instruments: instrument.NewRepo(conn), prices: price.NewRepo(conn), fx: fx.NewRepo(conn), tefasFetcher: &tefas.Fetcher{BaseURL: srv.URL}}, &backfillQueue{})

	_, release, err := transaction.AcquireFetchSlot(ctx)
	if err != nil {
		t.Fatalf("hold the slot: %v", err)
	}
	defer release()

	start := time.Now()
	in := instrument.Instrument{ID: id, Symbol: "AFT", Market: "TEFAS", Name: "Test Fonu", Currency: "TRY"}
	transaction.EnsurePrice(ctx, fetch, in, transaction.Transaction{InstrumentID: &id, Date: date, Currency: "TRY"})
	backfillWorkers.Wait()
	if took := time.Since(start); took > time.Second {
		t.Errorf("covered save took %v while the slot was held, want no wait", took)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("tefas requests = %d, want 0 for stored history", n)
	}
}
