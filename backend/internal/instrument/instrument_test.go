package instrument_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

// TestSeedAndSearch covers Seed's idempotency (running it twice on a fresh
// DB must not duplicate rows) and Search's prefix/substring matching.
func TestSeedAndSearch(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()

	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("first Seed: %v", err)
	}
	count := rowCount(t, conn)
	if count == 0 {
		t.Fatalf("expected instruments to be seeded, got 0 rows")
	}

	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	if got := rowCount(t, conn); got != count {
		t.Fatalf("row count changed after re-seeding: got %d, want %d", got, count)
	}

	repo := instrument.NewRepo(conn)
	results, err := repo.Search(ctx, "THY", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, in := range results {
		if in.Symbol == "THYAO" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Search(%q) = %+v, want THYAO among results", "THY", results)
	}

	usResults, err := repo.Search(ctx, "AAPL", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var aapl *instrument.Instrument
	for i := range usResults {
		if usResults[i].Symbol == "AAPL" {
			aapl = &usResults[i]
		}
	}
	if aapl == nil {
		t.Fatalf("Search(%q) = %+v, want AAPL among results", "AAPL", usResults)
	}
	if aapl.Market != "US" || aapl.Currency != "USD" {
		t.Fatalf("AAPL = %+v, want market US, currency USD", aapl)
	}

	yacResults, err := repo.Search(ctx, "YAC", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var yac *instrument.Instrument
	for i := range yacResults {
		if yacResults[i].Symbol == "YAC" {
			yac = &yacResults[i]
		}
	}
	if yac == nil {
		t.Fatalf("Search(%q) = %+v, want YAC among results", "YAC", yacResults)
	}
	if yac.Market != "TEFAS" || yac.Currency != "TRY" {
		t.Fatalf("YAC = %+v, want market TEFAS, currency TRY", yac)
	}

	btcResults, err := repo.Search(ctx, "btc", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	btc := map[string]string{}
	for _, in := range btcResults {
		if in.Market == "CRYPTO" {
			btc[in.Symbol] = in.Currency
		}
	}
	if btc["BTC-TRY"] != "TRY" || btc["BTC-USD"] != "USD" {
		t.Fatalf("Search(%q) = %+v, want CRYPTO BTC-TRY (TRY) and BTC-USD (USD)", "btc", btcResults)
	}

	gram, err := repo.BySymbol(ctx, "GOLD", "GRAM")
	if err != nil {
		t.Fatalf("BySymbol(GOLD, GRAM): %v", err)
	}
	if gram.Name != "Gram Altın" || gram.Currency != "TRY" {
		t.Fatalf("GRAM = %+v, want name Gram Altın, currency TRY", gram)
	}
	gramResults, err := repo.Search(ctx, "gram", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(gramResults) == 0 || gramResults[0].ID != gram.ID {
		t.Fatalf("Search(%q) = %+v, want GOLD GRAM first", "gram", gramResults)
	}

	coins := map[string]string{"CEYREK": "Çeyrek Altın", "YARIM": "Yarım Altın", "TAM": "Tam Altın", "ATA": "Ata Altın"}
	for symbol, name := range coins {
		coin, err := repo.BySymbol(ctx, "GOLD", symbol)
		if err != nil {
			t.Fatalf("BySymbol(GOLD, %s): %v", symbol, err)
		}
		if coin.Name != name || coin.Currency != "TRY" {
			t.Fatalf("%s = %+v, want name %s, currency TRY", symbol, coin, name)
		}
	}
}

// TestSearchFoldsTurkishDiacritics covers querying with real Turkish
// characters against ASCII-seeded names (e.g. "Turk Hava Yollari").
func TestSearchFoldsTurkishDiacritics(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	repo := instrument.NewRepo(conn)
	results, err := repo.Search(ctx, "türk", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, in := range results {
		if in.Symbol == "THYAO" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Search(%q) = %+v, want THYAO among results", "türk", results)
	}
}

// TestSearchTrimsQuery checks that a pasted "THYAO " still finds
// THYAO, and a blank query matches nothing rather than everything. So does a
// query holding a NUL.
func TestSearchTrimsQuery(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	repo := instrument.NewRepo(conn)
	for _, q := range []string{"THYAO ", " thy"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		found := false
		for _, in := range results {
			if in.Symbol == "THYAO" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Search(%q) = %+v, want THYAO among results", q, results)
		}
	}

	// A NUL cuts SQLite's LIKE pattern, so it must not act as a wildcard.
	for _, q := range []string{" ", "\x00", "THY\x00ZZZ"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(results) != 0 {
			t.Fatalf("Search(%q) = %+v, want no results", q, results)
		}
	}
}

// TestSearchIgnoresOverlongQuery checks that a query past SQLite's LIKE
// pattern limit (50,000 bytes) matches nothing instead of failing.
func TestSearchIgnoresOverlongQuery(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	results, err := instrument.NewRepo(conn).Search(ctx, strings.Repeat("a", 60000), 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search = %d results, want none", len(results))
	}

}

// TestSearchMatchesNamesWithTurkishDiacritics checks that a name query
// matches TEFAS names, which are seeded with their Turkish characters ("ATA
// PORTFÖY PARA PİYASASI"), whether it is typed with diacritics or without.
func TestSearchMatchesNamesWithTurkishDiacritics(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	repo := instrument.NewRepo(conn)
	for _, q := range []string{"piyasası", "piyasasi"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		found := false
		for _, in := range results {
			if in.Market == "TEFAS" && strings.Contains(in.Name, "PİYASASI") {
				found = true
			}
		}
		if !found {
			t.Fatalf("Search(%q) = %+v, want a TEFAS fund whose name contains PİYASASI", q, results)
		}
	}
}

// TestSearchMatchesEveryWordAnywhere checks that a query of several
// words matches a name holding each of them, in any order and with other
// words between them, so "garanti para piyasası" finds GARANTİ PORTFÖY
// BİRİNCİ PARA PİYASASI (TL) FONU. A word may also be the start of the
// symbol ("gtl para"), and every word must match: "garanti zzzz" finds
// nothing.
func TestSearchMatchesEveryWordAnywhere(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	repo := instrument.NewRepo(conn)
	if _, err := repo.Insert(ctx, "TEFAS", "GTL", "GARANTİ PORTFÖY BİRİNCİ PARA PİYASASI (TL) FONU", "TRY"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	for _, q := range []string{"garanti para piyasası", "piyasasi  GARANTİ birinci", "gtl para"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		found := false
		for _, in := range results {
			if in.Symbol == "GTL" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Search(%q) = %+v, want GTL among results", q, results)
		}
	}

	results, err := repo.Search(ctx, "garanti zzzz", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search(%q) = %+v, want no results", "garanti zzzz", results)
	}
}

// TestSearchNameMatchesAreNotFilledByOneMarket checks that one market does not
// fill the name matches: more BIST ETFs are named "... PORTFOY ..." than the
// API's limit of 20, so a BIST-first tie-break inside the name-match bucket hid
// all ~1000 TEFAS funds carrying the same word. Run against the real seeded
// CSVs so the regression cannot come back when a market gains instruments.
func TestSearchNameMatchesAreNotFilledByOneMarket(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	var bistPortfoy int
	if err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM instruments WHERE market = 'BIST' AND name LIKE '%PORTFOY%'",
	).Scan(&bistPortfoy); err != nil {
		t.Fatalf("count BIST PORTFOY instruments: %v", err)
	}
	if bistPortfoy <= 20 {
		t.Fatalf("expected more than 20 BIST instruments named PORTFOY to make this a real crowding test, got %d", bistPortfoy)
	}

	repo := instrument.NewRepo(conn)
	for _, q := range []string{"portföy", "portfoy"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		found := false
		for _, in := range results {
			if in.Market == "TEFAS" {
				found = true
			}
		}
		if !found {
			t.Fatalf("Search(%q) = %+v, want at least one TEFAS fund", q, results)
		}
	}
}

// TestSearchFindsGoldByMarketWord checks that "altın" (tr) and "gold"
// (en), the GOLD market's own label, list all five GOLD instruments first
// instead of letting gold funds and stocks fill the limit. An exact symbol
// match (GOLD on US, ALTIN on BIST) still ranks above them.
func TestSearchFindsGoldByMarketWord(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	repo := instrument.NewRepo(conn)

	for _, q := range []string{"altın", "ALTIN", "gold"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		// An exact symbol hit ranks ahead of the market word: US GOLD for
		// "gold", the BIST gold certificate ALTIN for "altın" and "ALTIN".
		first := instrument.Instrument{Symbol: "ALTIN", Market: "BIST"}
		if q == "gold" {
			first = instrument.Instrument{Symbol: "GOLD", Market: "US"}
		}
		if len(results) == 0 || results[0].Symbol != first.Symbol || results[0].Market != first.Market {
			t.Fatalf("Search(%q) = %+v, want %s %s first", q, results, first.Market, first.Symbol)
		}
		results = results[1:]
		if len(results) < 5 {
			t.Fatalf("Search(%q) = %+v, want the five GOLD instruments first", q, results)
		}
		got := map[string]bool{}
		for _, in := range results[:5] {
			if in.Market == "GOLD" {
				got[in.Symbol] = true
			}
		}
		for _, symbol := range []string{"GRAM", "CEYREK", "YARIM", "TAM", "ATA"} {
			if !got[symbol] {
				t.Fatalf("Search(%q) = %+v, want GOLD %s among the first results", q, results, symbol)
			}
		}
	}

	// The market word also counts as one word of a multi-word
	// query, in any order, so the en "gram gold" finds Gram Altın as the tr
	// "gram altın" does.
	for _, q := range []string{"gram gold", "gold gram"} {
		results, err := repo.Search(ctx, q, 20)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(results) == 0 || results[0].Symbol != "GRAM" || results[0].Market != "GOLD" {
			t.Fatalf("Search(%q) = %+v, want GOLD GRAM first", q, results)
		}
	}
}

// TestSearchRanksExactAndPrefixMatchesFirst checks that an exact symbol
// match ranks above other prefix matches, prefix matches rank above
// name-substring matches, and BIST breaks ties ahead of US. Crypto pairs
// (BTC-TRY for "btc") rank right after the exact match.
func TestSearchRanksExactAndPrefixMatchesFirst(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPB', 'US', 'AAP Bond ETF', 'USD'),
		       (2, 'AAPD', 'US', 'AAP Daily ETF', 'USD'),
		       (3, 'AAPL', 'US', 'Apple Inc', 'USD'),
		       (4, 'GARA', 'US', 'Gara Corp', 'USD'),
		       (5, 'GARAN', 'BIST', 'Garanti Bankasi', 'TRY'),
		       (6, 'BTC', 'US', 'Grayscale Bitcoin Mini Trust', 'USD'),
		       (7, 'BTCIM', 'BIST', 'Baticim', 'TRY'),
		       (8, 'BTC-TRY', 'CRYPTO', 'Bitcoin', 'TRY'),
		       (9, 'BTC-USD', 'CRYPTO', 'Bitcoin', 'USD')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	repo := instrument.NewRepo(conn)

	aaplResults, err := repo.Search(ctx, "aapl", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(aaplResults) == 0 || aaplResults[0].Symbol != "AAPL" {
		t.Fatalf("Search(%q) = %+v, want AAPL first", "aapl", aaplResults)
	}

	garResults, err := repo.Search(ctx, "gar", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(garResults) == 0 || garResults[0].Symbol != "GARAN" {
		t.Fatalf("Search(%q) = %+v, want GARAN first", "gar", garResults)
	}

	// A crypto pair whose base is the query ranks right after the
	// exact symbol match, ahead of BIST prefix matches.
	btcResults, err := repo.Search(ctx, "btc", 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var btcSymbols []string
	for _, in := range btcResults {
		btcSymbols = append(btcSymbols, in.Symbol)
	}
	if got, want := strings.Join(btcSymbols, ","), "BTC,BTC-TRY,BTC-USD,BTCIM"; got != want {
		t.Fatalf("Search(%q) symbols = %s, want %s", "btc", got, want)
	}
}

// TestTrackedReturnsOnlyInstrumentsWithATransaction covers Tracked's join
// against transactions: an instrument with a transaction row is included,
// one with none is not, and market filters correctly across BIST and US.
func TestTrackedReturnsOnlyInstrumentsWithATransaction(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple Inc', 'USD'),
		       (2, 'MSFT', 'US', 'Microsoft Corp', 'USD'),
		       (3, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, created_at)
		VALUES (1, 'u@example.com', 'x', '2026-09-11T00:00:00Z')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO portfolios (id, user_id, name, created_at)
		VALUES (1, 1, 'Main', '2026-09-11T00:00:00Z')`); err != nil {
		t.Fatalf("seed portfolio: %v", err)
	}
	// Only AAPL (US) and THYAO (BIST) are referenced by a transaction; MSFT
	// is a known instrument nobody holds.
	if _, err := conn.ExecContext(ctx, `INSERT INTO transactions
		(id, user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		VALUES (1, 1, 1, 1, 'buy', '2026-09-10', '10', '200.00', '2000.00', '0', 'USD', '2026-09-10T00:00:00Z'),
		       (2, 1, 1, 3, 'buy', '2026-09-10', '5', '300.00', '1500.00', '0', 'TRY', '2026-09-10T00:00:00Z'),
		       (3, 1, 1, NULL, 'deposit', '2026-09-10', NULL, NULL, '1000.00', '0', 'TRY', '2026-09-10T00:00:00Z')`); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}

	repo := instrument.NewRepo(conn)
	tracked, err := repo.Tracked(ctx, "US")
	if err != nil {
		t.Fatalf("Tracked: %v", err)
	}
	if len(tracked) != 1 || tracked[0].Symbol != "AAPL" {
		t.Fatalf("Tracked(US) = %+v, want only AAPL", tracked)
	}

	trackedBist, err := repo.Tracked(ctx, "BIST")
	if err != nil {
		t.Fatalf("Tracked: %v", err)
	}
	if len(trackedBist) != 1 || trackedBist[0].Symbol != "THYAO" {
		t.Fatalf("Tracked(BIST) = %+v, want only THYAO", trackedBist)
	}
}

// TestEachTrackedContinuesPastFailure checks the shared loop: a
// failing instrument is logged once and the rest still run; every attempted
// one failing is an error, with a skipped one beside them too; a pause
// follows each request except the last, none follows a skipped one; a
// cancel during the pause returns at once with the rows so far and logs
// nothing.
func TestEachTrackedContinuesPastFailure(t *testing.T) {
	var logs bytes.Buffer
	prevLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prevLog)
	pauses, onPause := 0, func() {}
	prevPause := *instrument.PauseAfter
	*instrument.PauseAfter = func(d time.Duration) <-chan time.Time {
		pauses++
		onPause()
		return prevPause(d)
	}
	defer func() { *instrument.PauseAfter = prevPause }()

	tracked := []instrument.Instrument{{Symbol: "A"}, {Symbol: "B"}, {Symbol: "C"}}
	boom := errors.New("boom")
	calls := 0
	run := func(ctx context.Context, pause time.Duration, fn func(instrument.Instrument) (int, bool, error)) (int, int, error) {
		logs.Reset()
		pauses, calls = 0, 0
		return instrument.EachTracked(ctx, tracked, pause, "funds", func(in instrument.Instrument) (int, bool, error) {
			calls++
			return fn(in)
		})
	}
	ctx := context.Background()

	written, failed, err := run(ctx, time.Millisecond, func(in instrument.Instrument) (int, bool, error) {
		if in.Symbol == "B" {
			return 0, false, boom
		}
		return 2, false, nil
	})
	if calls != 3 || written != 4 || failed != 1 || err != nil || pauses != 2 {
		t.Fatalf("second failing: calls %d, EachTracked = %d, %d, %v, pauses %d; want 3 calls, 4, 1, nil, 2 pauses", calls, written, failed, err, pauses)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("second failing logged %d WARN lines, want 1:\n%s", n, logs.String())
	}

	_, failed, err = run(ctx, time.Millisecond, func(instrument.Instrument) (int, bool, error) { return 0, false, boom })
	if failed != 3 || err == nil || !strings.Contains(err.Error(), "all 3 funds failed") || !errors.Is(err, boom) {
		t.Fatalf("all failing: failed %d, err %v; want 3 and all 3 funds failed wrapping boom", failed, err)
	}

	_, _, err = run(ctx, time.Millisecond, func(in instrument.Instrument) (int, bool, error) {
		if in.Symbol == "A" {
			return 0, true, nil
		}
		return 0, false, boom
	})
	if err == nil || !strings.Contains(err.Error(), "all 2 funds failed") || pauses != 1 {
		t.Fatalf("skipped then failing: err %v, pauses %d; want all 2 funds failed and 1 pause", err, pauses)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancelled time.Time
	onPause = func() {
		cancelled = time.Now()
		cancel() // the shutdown arrives during the pause after the first instrument
	}
	const pause = time.Second
	written, _, err = run(ctx, pause, func(instrument.Instrument) (int, bool, error) { return 2, false, nil })
	if elapsed := time.Since(cancelled); elapsed >= pause/2 {
		t.Fatalf("EachTracked returned %v after the cancel, want well under the %v pause", elapsed, pause)
	}
	if !errors.Is(err, context.Canceled) || written != 2 || calls != 1 {
		t.Fatalf("cancelled: calls %d, EachTracked = %d, %v; want 1 call, 2 rows and context.Canceled", calls, written, err)
	}
	if logs.Len() > 0 {
		t.Fatalf("logged after cancel:\n%s", logs.String())
	}
}

func rowCount(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM instruments").Scan(&n); err != nil {
		t.Fatalf("count instruments: %v", err)
	}
	return n
}

// TestRenameSurvivesSeed checks that Rename changes a fund's name, and
// a later Seed keeps it, since Seed only inserts missing rows.
func TestRenameSurvivesSeed(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	repo := instrument.NewRepo(conn)
	in, err := repo.BySymbol(ctx, "TEFAS", "IIE")
	if err != nil {
		t.Fatalf("BySymbol: %v", err)
	}
	if err := repo.Rename(ctx, in.ID, "RENAMED FUND"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := instrument.Seed(ctx, conn); err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	got, err := repo.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "RENAMED FUND" {
		t.Fatalf("name after Rename and Seed = %q, want the new name", got.Name)
	}
}

// TestByIDs covers the one-query lookup: every requested id resolves, a
// missing id is ErrNotFound like Get, and no ids is an empty map.
func TestByIDs(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'AAPL', 'US', 'Apple Inc', 'USD'),
		       (2, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	repo := instrument.NewRepo(conn)

	got, err := repo.ByIDs(ctx, []int64{1, 2})
	if err != nil {
		t.Fatalf("ByIDs: %v", err)
	}
	if len(got) != 2 || got[1].Symbol != "AAPL" || got[2].Currency != "TRY" {
		t.Fatalf("ByIDs = %+v, want AAPL and THYAO", got)
	}

	if _, err := repo.ByIDs(ctx, []int64{1, 99}); !errors.Is(err, instrument.ErrNotFound) {
		t.Fatalf("ByIDs with a missing id: err = %v, want ErrNotFound", err)
	}

	empty, err := repo.ByIDs(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("ByIDs(nil) = %v, %v; want empty map, nil", empty, err)
	}
}
