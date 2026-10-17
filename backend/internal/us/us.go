// Package us fetches the latest US session close for tracked US
// instruments from Finnhub's quote endpoint, and their daily-close history
// from Tiingo, and stores them via price.Repo. Mirrors internal/tcmb's shape (Fetcher + Sync(ctx, date)),
// the shape scheduler.Source expects; source, endpoint, and quirks are
// documented in docs/adr/0003-multi-currency-and-market-data-sources.md.
// Provider details stay internal, the same shape ADR 0001 already chose
// for internal/bist.
package us

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
)

// defaultBaseURL is the production Finnhub REST API base path.
const defaultBaseURL = "https://finnhub.io/api/v1"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// FinnhubKeySetting and TiingoKeySetting are the secret settings holding
// the provider API keys, entered in the admin area (ADR 0010).
const (
	FinnhubKeySetting = "provider.finnhub.api_key"
	TiingoKeySetting  = "provider.tiingo.api_key"
)

// SourceFinnhub is the price.Repo source value written by Sync.
const SourceFinnhub = "finnhub"

// usMarket is the instruments.market value for US equities/ETFs.
const usMarket = "US"

// RequestPause is the delay Sync sleeps between successive symbol lookups.
// Free tier allows 60 requests/minute, far more than a once-daily,
// one-lookup-per-tracked-symbol run needs; the pause is just polite.
const RequestPause = 200 * time.Millisecond

// Close is one instrument's latest session close.
type Close struct {
	Symbol string
	Date   string // YYYY-MM-DD, the New York trading date the close is for
	Close  decimal.Decimal
}

// quoteResponse is Finnhub's GET /quote response, the fields needed to
// date the close.
type quoteResponse struct {
	C  json.Number `json:"c"`  // current price; the session's close once it has ended
	PC json.Number `json:"pc"` // close of the session before c's
	T  int64       `json:"t"`  // time of c, unix seconds
}

// Fetcher retrieves quotes from Finnhub's REST API over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to httpbody's client with a
	// 30s timeout when nil.
	HTTPClient *http.Client
	// BaseURL is the API base path, e.g. defaultBaseURL. Overridable so
	// tests can point at an httptest.Server. Defaults to defaultBaseURL
	// when empty.
	BaseURL string
	// APIKey authenticates requests, sent as the X-Finnhub-Token header.
	// Used when Settings is nil (tests). Empty means "not configured":
	// FetchClose returns a clear error instead of making a request or
	// panicking.
	APIKey string
	// TiingoBaseURL is Tiingo's API base, for daily-close history.
	// Defaults to defaultTiingoBaseURL when empty.
	TiingoBaseURL string
	// TiingoKey authenticates history requests. Used when Settings is nil.
	// Empty means "not configured": SyncHistory logs once and skips.
	TiingoKey string
	// Settings, when set, supplies both keys on every fetch instead of
	// APIKey and TiingoKey, so a key saved in the admin area is used by
	// the next run without a restart.
	Settings *settings.Repo
}

// NewFetcher returns a Fetcher pointed at the production Finnhub and Tiingo
// APIs, with keys read from s on every fetch.
func NewFetcher(s *settings.Repo) *Fetcher {
	return &Fetcher{Settings: s}
}

// key returns the secret setting's value from Settings, or static when
// Settings is nil. Empty means not configured.
func (f *Fetcher) key(ctx context.Context, setting, static string) (string, error) {
	if f.Settings == nil {
		return static, nil
	}
	k, _, err := f.Settings.GetSecret(ctx, setting)
	return k, err
}

// keyTestTimeout bounds TestKey's one request.
const keyTestTimeout = 10 * time.Second

// TestKey makes one cheap request with key to the provider whose secret
// setting is setting and returns "" when the provider accepts the key, or a
// short reason: "invalid key" (401, 403), "rate limited" (429) or
// "unreachable" (anything else). The key and the provider's body are never
// returned or logged.
//
// Finnhub gets GET /quote?symbol=AAPL. Tiingo's documented /api/test
// answers 200 even for a wrong token, so Tiingo gets one day of AAPL daily
// prices instead, which answers 403 for one.
func (f *Fetcher) TestKey(ctx context.Context, setting, key string) string {
	var u, header, value string
	switch setting {
	case FinnhubKeySetting:
		base := cmp.Or(f.BaseURL, defaultBaseURL)
		u, header, value = base+"/quote?symbol=AAPL", "X-Finnhub-Token", key
	case TiingoKeySetting:
		base := cmp.Or(f.TiingoBaseURL, defaultTiingoBaseURL)
		u, header, value = base+"/tiingo/daily/AAPL/prices?startDate=2024-01-02&endDate=2024-01-02", "Authorization", "Token "+key
	default:
		return "unknown provider"
	}
	ctx, cancel := context.WithTimeout(ctx, keyTestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "unreachable"
	}
	req.Header.Set(header, value)
	resp, err := httpbody.Client(f.HTTPClient).Do(req)
	if err != nil {
		// The error names the URL, never the header carrying the key.
		slog.Default().Warn("provider key test failed", "setting", setting, "error", err)
		return "unreachable"
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return "invalid key"
	case http.StatusTooManyRequests:
		return "rate limited"
	}
	slog.Default().Warn("provider key test failed", "setting", setting, "status", resp.StatusCode)
	return "unreachable"
}

// FetchClose fetches symbol's latest close as of now, dated by the quote's
// own timestamp t, not by now: once t's session has closed (now
// is past 16:00 New York on t's date, which covers a pre-open run where t
// is still the last session) c is that session's close; while it is still
// open c is a live price, so pc is returned under the session before.
// Finnhub answers a symbol it has no data for with HTTP 200 and every
// quote field zeroed rather than a 404; that shape (t or the price == 0)
// is treated as "skip, don't fail the run" here, the same non-fatal signal a 404 is for bist/tcmb.
// Any transport error or other non-200 status (this includes 401
// unauthorized and 429 rate-limited) is a hard error.
func (f *Fetcher) FetchClose(ctx context.Context, symbol string, now time.Time) (Close, bool, error) {
	key, err := f.key(ctx, FinnhubKeySetting, f.APIKey)
	if err != nil {
		return Close{}, false, err
	}
	if key == "" {
		return Close{}, false, fmt.Errorf("finnhub: API key not configured, set it in the admin area")
	}

	base := cmp.Or(f.BaseURL, defaultBaseURL)
	u, err := url.Parse(base + "/quote")
	if err != nil {
		return Close{}, false, fmt.Errorf("build finnhub url: %w", err)
	}
	q := u.Query()
	q.Set("symbol", symbol)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Close{}, false, fmt.Errorf("build finnhub request: %w", err)
	}
	req.Header.Set("X-Finnhub-Token", key)

	body, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if err != nil {
		return Close{}, false, fmt.Errorf("fetch finnhub quote for %s: %w", symbol, err)
	}

	var qr quoteResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return Close{}, false, fmt.Errorf("parse finnhub quote for %s: %w", symbol, err)
	}
	if qr.T == 0 {
		return Close{}, false, nil
	}
	t := time.Unix(qr.T, 0).In(newYork())
	v, date := qr.C, t.Format(time.DateOnly)
	if now.Before(time.Date(t.Year(), t.Month(), t.Day(), marketCloseHour, 0, 0, 0, t.Location())) {
		v, date = qr.PC, LastSession(t)
	}
	if v == "" { // null or absent
		return Close{}, false, nil
	}
	// Parsed from the JSON text as-is, never via float64.
	c, err := decimal.NewFromString(v.String())
	if err != nil {
		return Close{}, false, fmt.Errorf("parse finnhub close %q for %s: %w", v, symbol, err)
	}
	if c.IsZero() {
		return Close{}, false, nil
	}
	return Close{Symbol: symbol, Date: date, Close: c}, true, nil
}

// SyncOne fetches and stores the latest close as of now for a single
// instrument in, under the date FetchClose gives it, source = "finnhub". Extracted from Sync's per-symbol body
// so an on-demand fetch can fetch just the one instrument a new
// transaction references instead of every tracked symbol. wrote is false,
// with a nil error, when Finnhub has no quote for the symbol (see
// FetchClose).
func SyncOne(ctx context.Context, fetcher *Fetcher, priceRepo *price.Repo, in instrument.Instrument, now time.Time) (wrote bool, err error) {
	c, ok, err := fetcher.FetchClose(ctx, in.Symbol, now)
	if err != nil {
		return false, fmt.Errorf("fetch close for %s: %w", in.Symbol, err)
	}
	if !ok {
		return false, nil
	}
	if err := priceRepo.Upsert(ctx, in.ID, c.Date, c.Close.String(), SourceFinnhub); err != nil {
		return false, fmt.Errorf("upsert price for %s: %w", in.Symbol, err)
	}
	return true, nil
}

// Sync fetches the latest close as of now for every tracked US instrument
// (one referenced by at least one transaction, any user) and upserts it
// into priceRepo with source = "finnhub", each under its own quote's date
// (see FetchClose). The loop is instrument.EachTracked: a failing symbol (a
// 5xx, a 429) is logged and the rest continue, every symbol failing is an
// error, and a cancelled ctx stops it quietly. A symbol Finnhub
// has no quote for is counted as skipped, neither written nor failed.
// Returns the number of prices written.
func Sync(ctx context.Context, fetcher *Fetcher, instrumentRepo *instrument.Repo, priceRepo *price.Repo, now time.Time) (int, error) {
	tracked, err := instrumentRepo.Tracked(ctx, usMarket)
	if err != nil {
		return 0, fmt.Errorf("list tracked us instruments: %w", err)
	}
	if len(tracked) == 0 {
		return 0, nil
	}

	skipped := 0
	written, failed, err := instrument.EachTracked(ctx, tracked, RequestPause, "us instruments", func(in instrument.Instrument) (int, bool, error) {
		wrote, err := SyncOne(ctx, fetcher, priceRepo, in, now)
		if wrote {
			return 1, false, nil
		}
		if err == nil {
			skipped++ // no quote, but a request was made: EachTracked still pauses
		}
		return 0, false, err
	})
	if ctx.Err() == nil {
		slog.Default().Info("finnhub us sync complete", "written", written, "skipped", skipped, "failed", failed)
	}
	return written, err
}

// marketCloseHour is the regular US session's close, New York time.
const marketCloseHour = 16

func newYork() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC // tzdata is embedded in cmd/server; should never happen
	}
	return loc
}

// LastSession returns the New York date (YYYY-MM-DD) of the latest session
// that has closed by now, skipping weekends and NYSE holidays: the date a
// run at now stores a close under, for a symbol that traded in that
// session (see FetchClose).
func LastSession(now time.Time) string {
	d := now.In(newYork())
	day := price.Midnight(d)
	if d.Hour() < marketCloseHour {
		day = day.AddDate(0, 0, -1)
	}
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || isHoliday(day) {
		day = day.AddDate(0, 0, -1)
	}
	return day.Format(time.DateOnly)
}

// isHoliday reports whether the NYSE is closed all day on day, a New York
// midnight.
//
// shortcut: the standing holidays only. One-off closures (a state funeral,
// a hurricane) and Juneteenth before 2022 are not known; add the date here
// if one matters.
func isHoliday(day time.Time) bool {
	y, loc := day.Year(), day.Location()
	at := func(m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, loc) }
	// nth occurrence of wd in m; n < 0 counts back from the month's end.
	nth := func(m time.Month, wd time.Weekday, n int) time.Time {
		if n < 0 {
			last := at(m+1, 0)
			return last.AddDate(0, 0, -((int(last.Weekday()) - int(wd) + 7) % 7))
		}
		first := at(m, 1)
		return first.AddDate(0, 0, (int(wd)-int(first.Weekday())+7)%7+7*(n-1))
	}
	// A Saturday holiday is observed the Friday before, a Sunday one the Monday after.
	observed := func(t time.Time) time.Time {
		switch t.Weekday() {
		case time.Saturday:
			return t.AddDate(0, 0, -1)
		case time.Sunday:
			return t.AddDate(0, 0, 1)
		}
		return t
	}
	// Easter Sunday, Anonymous Gregorian algorithm.
	a, b, c := y%19, y/100, y%100
	g := (b - (b+8)/25 + 1) / 3
	h := (19*a + b - b/4 - g + 15) % 30
	l := (32 + 2*(b%4) + 2*(c/4) - h - c%4) % 7
	m := (a + 11*h + 22*l) / 451
	easter := at(time.Month((h+l-7*m+114)/31), (h+l-7*m+114)%31+1)

	holidays := []time.Time{
		nth(time.January, time.Monday, 3),
		nth(time.February, time.Monday, 3),
		easter.AddDate(0, 0, -2),
		nth(time.May, time.Monday, -1),
		observed(at(time.July, 4)),
		nth(time.September, time.Monday, 1),
		nth(time.November, time.Thursday, 4),
		observed(at(time.December, 25)),
	}
	if y >= 2022 {
		holidays = append(holidays, observed(at(time.June, 19)))
	}
	if at(time.January, 1).Weekday() != time.Saturday { // not observed on the Friday before
		holidays = append(holidays, observed(at(time.January, 1)))
	}
	for _, hd := range holidays {
		if hd.Equal(day) {
			return true
		}
	}
	return false
}

// defaultTiingoBaseURL is the production Tiingo REST API base.
const defaultTiingoBaseURL = "https://api.tiingo.com"

// sourceTiingo is the price.Repo source value written by SyncHistory.
const sourceTiingo = "tiingo"

// historyPad is how far before the requested start SyncHistory fetches, so
// a start date on a weekend or holiday still gets the close before it, and
// the "already covered" check in SyncHistory holds on the next call.
const historyPad = 7 * 24 * time.Hour

// DailyClose is one trading day's unadjusted close.
type DailyClose struct {
	Date  string // YYYY-MM-DD, US trading date
	Close decimal.Decimal
	// SplitFactor is Tiingo's splitFactor: new shares per old share on an
	// ex-date, 1 on other days. Zero when the row has none or a
	// non-positive one, which FetchHistory logs and drops.
	SplitFactor decimal.Decimal
	// DivCash is Tiingo's divCash: gross cash dividend per share in USD on
	// an ex-date, 0 on other days. Zero when the row has none or a negative
	// or malformed one, which FetchHistory logs and drops.
	DivCash decimal.Decimal
}

// FetchHistory returns symbol's daily unadjusted closes from from to
// Tiingo's latest. Unadjusted, like Finnhub's previous close and like the
// prices users enter. Tiingo spells share classes with a dash (BRK-B)
// where the Nasdaq directory uses a dot (BRK.B). An unknown ticker is a
// 404, returned as no rows, not an error.
func (f *Fetcher) FetchHistory(ctx context.Context, symbol string, from time.Time) ([]DailyClose, error) {
	key, err := f.key(ctx, TiingoKeySetting, f.TiingoKey)
	if err != nil {
		return nil, err
	}
	base := cmp.Or(f.TiingoBaseURL, defaultTiingoBaseURL)
	u, err := url.Parse(base + "/tiingo/daily/" + url.PathEscape(strings.ReplaceAll(symbol, ".", "-")) + "/prices")
	if err != nil {
		return nil, fmt.Errorf("build tiingo url: %w", err)
	}
	q := u.Query()
	q.Set("startDate", from.Format(time.DateOnly))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build tiingo request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+key)
	body, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if se := (*httpbody.StatusError)(nil); errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch tiingo history for %s: %w", symbol, err)
	}

	var rows []struct {
		Date        string      `json:"date"`
		Close       json.Number `json:"close"`
		SplitFactor json.Number `json:"splitFactor"`
		DivCash     json.Number `json:"divCash"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("parse tiingo history for %s: %w", symbol, err)
	}
	out := make([]DailyClose, 0, len(rows))
	for _, r := range rows {
		if len(r.Date) < 10 {
			return nil, fmt.Errorf("tiingo history for %s: bad date %q", symbol, r.Date)
		}
		c, err := decimal.NewFromString(r.Close.String())
		if err != nil {
			return nil, fmt.Errorf("tiingo history for %s: bad close %q: %w", symbol, r.Close, err)
		}
		if !c.IsPositive() {
			continue
		}
		var sf decimal.Decimal
		if r.SplitFactor != "" {
			if sf, err = decimal.NewFromString(r.SplitFactor.String()); err != nil {
				return nil, fmt.Errorf("tiingo history for %s: bad splitFactor %q: %w", symbol, r.SplitFactor, err)
			}
			if !sf.IsPositive() {
				// A bad row: keep the close, drop the factor.
				slog.Default().Warn("tiingo: skipping non-positive splitFactor", "symbol", symbol, "date", r.Date[:10], "split_factor", r.SplitFactor.String())
				sf = decimal.Decimal{}
			}
		}
		var dc decimal.Decimal
		if r.DivCash != "" {
			if dc, err = decimal.NewFromString(r.DivCash.String()); err != nil || dc.IsNegative() {
				// A bad row: keep the close, drop the dividend.
				slog.Default().Warn("tiingo: skipping bad divCash", "symbol", symbol, "date", r.Date[:10], "div_cash", r.DivCash.String())
				dc = decimal.Decimal{}
			}
		}
		out = append(out, DailyClose{Date: r.Date[:10], Close: c, SplitFactor: sf, DivCash: dc})
	}
	return out, nil
}

// noTiingoKeyOnce makes a missing Tiingo key one log line per process,
// not one per transaction save.
var noTiingoKeyOnce sync.Once

// tiingoConfigured reports whether f has a Tiingo key, logging once per
// process when it has none.
func (f *Fetcher) tiingoConfigured(ctx context.Context) (bool, error) {
	key, err := f.key(ctx, TiingoKeySetting, f.TiingoKey)
	if err != nil || key != "" {
		return key != "", err
	}
	noTiingoKeyOnce.Do(func() {
		slog.Default().Warn("us: Tiingo API key not configured, set it in the admin area; skipping US price history")
	})
	return false, nil
}

// Syncer is the Tiingo fetcher and the repos SyncHistory and SyncRecent
// read and write, built once at startup.
type Syncer struct {
	Fetcher     *Fetcher
	Instruments *instrument.Repo
	Prices      *price.Repo
	Splits      *split.Repo
	Dividends   *dividend.Repo
}

// SyncHistory stores in's daily closes from from (YYYY-MM-DD, padded back
// by historyPad) to Tiingo's latest, source = "tiingo", in one request.
// It does nothing when in already has a price on or before
// from: an earlier SyncHistory already covered that range. Without a
// Tiingo key it logs once and returns 0, nil, so US history degrades to
// Finnhub's latest close only.
//
// Every row whose splitFactor is present and not 1 is also stored in splits,
// source = "tiingo" (ADR 0008). A factor <= 0 is dropped by
// FetchHistory; that row's close is still stored. A split after an already
// covered range is not seen here (see the early return).
//
// Every row with divCash > 0 is also stored in dividends, currency USD,
// source = "tiingo" (ADR 0009).
//
// shortcut: a ticker Tiingo does not know (404) has no row on or before
// from, so it is re-requested on every call. Ceiling is one wasted
// request per such symbol per save/startup; remember misses if the free
// tier's hourly limit ever bites.
func (s *Syncer) SyncHistory(ctx context.Context, in instrument.Instrument, from string) (int, error) {
	if ok, err := s.Fetcher.tiingoConfigured(ctx); !ok {
		return 0, err
	}
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return 0, fmt.Errorf("us history for %s: bad start date %q: %w", in.Symbol, from, err)
	}
	have, err := s.Prices.Latest(ctx, []int64{in.ID}, from)
	if err != nil {
		return 0, fmt.Errorf("check us history for %s: %w", in.Symbol, err)
	}
	if _, ok := have[in.ID]; ok {
		return 0, nil
	}
	closes, err := s.Fetcher.FetchHistory(ctx, in.Symbol, start.Add(-historyPad))
	if err != nil {
		return 0, err
	}
	return s.storeHistory(ctx, in, closes)
}

// storeHistory upserts closes into Prices in one transaction, every splitFactor that is
// present and not 1 into Splits, and every divCash > 0 into Dividends in
// USD, all source = "tiingo". Returns the number of closes written, also when a
// split or dividend upsert then fails: the closes are already committed.
func (s *Syncer) storeHistory(ctx context.Context, in instrument.Instrument, closes []DailyClose) (int, error) {
	rows := make([]price.Row, len(closes))
	for i, c := range closes {
		rows[i] = price.Row{InstrumentID: in.ID, Date: c.Date, Close: c.Close.String(), Source: sourceTiingo}
	}
	if err := s.Prices.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert prices for %s: %w", in.Symbol, err)
	}
	for _, c := range closes {
		if !c.SplitFactor.IsZero() && !c.SplitFactor.Equal(decimal.NewFromInt(1)) {
			if err := s.Splits.Upsert(ctx, in.ID, c.Date, c.SplitFactor, sourceTiingo); err != nil {
				return len(closes), fmt.Errorf("upsert split for %s: %w", in.Symbol, err)
			}
		}
		if c.DivCash.IsPositive() {
			if err := s.Dividends.Upsert(ctx, in.ID, c.Date, c.DivCash, "USD", sourceTiingo); err != nil {
				return len(closes), fmt.Errorf("upsert dividend for %s: %w", in.Symbol, err)
			}
		}
	}
	return len(closes), nil
}

// SyncRecent re-reads the last historyPad of Tiingo daily history for every
// tracked US instrument and stores the closes, splits and dividends.
// SyncHistory stops once a range is covered and Finnhub carries no split
// data, so this is the daily pass that catches a split after an
// instrument's history was first fetched. The loop is
// instrument.EachTracked: one instrument failing is logged and the rest
// continue, every instrument failing is an error, and a cancelled ctx stops
// it quietly.
// Without a Tiingo key it logs once and returns 0, nil. Returns the number
// of closes written.
//
// shortcut: one Tiingo request per tracked symbol per run, against the free
// tier's 50 requests/hour. Past ~50 tracked US symbols the tail of the run
// gets 429s (logged, retried the next day); spread the run over the hour or
// move to a paid tier if that ever happens.
func (s *Syncer) SyncRecent(ctx context.Context, now time.Time) (int, error) {
	if ok, err := s.Fetcher.tiingoConfigured(ctx); !ok {
		return 0, err
	}
	tracked, err := s.Instruments.Tracked(ctx, usMarket)
	if err != nil {
		return 0, fmt.Errorf("list tracked us instruments: %w", err)
	}
	written, failed, err := instrument.EachTracked(ctx, tracked, RequestPause, "us instruments", func(in instrument.Instrument) (int, bool, error) {
		closes, err := s.Fetcher.FetchHistory(ctx, in.Symbol, now.Add(-historyPad))
		if err != nil {
			return 0, false, err
		}
		n, err := s.storeHistory(ctx, in, closes)
		return n, false, err
	})
	if ctx.Err() == nil {
		slog.Default().Info("tiingo us recent sync complete", "written", written, "failed", failed)
	}
	return written, err
}
