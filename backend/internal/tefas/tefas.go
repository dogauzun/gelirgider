// Package tefas fetches daily NAVs for tracked TEFAS funds from
// tefas.gov.tr's fonFiyatBilgiGetir endpoint and stores them via price.Repo
// with source = "tefas". Same shape as internal/btcturk (Fetcher + Sync).
// Lookup checks whether a fund code exists on TEFAS.
// Endpoint, payload and the robots.txt decision are in
// docs/adr/0004-tefas-data-source.md.
package tefas

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// defaultBaseURL is the production TEFAS host.
const defaultBaseURL = "https://www.tefas.gov.tr"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// SourceTEFAS is the price.Repo source value written by this package.
const SourceTEFAS = "tefas"

// tefasMarket is the instruments.market value for TEFAS funds.
const tefasMarket = "TEFAS"

// userAgent identifies the fetcher honestly (ADR 0004).
const userAgent = "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)"

// RequestPause is the polite delay Sync sleeps between funds.
const RequestPause = 500 * time.Millisecond

// periods are the lookback windows, in months, that TEFAS accepts.
var periods = []int{1, 3, 6, 12, 36, 60}

// backfillSlackDays is how far a fund's first price may trail the start of
// its history, two consecutive prices may be apart, and its last price may
// trail now, before SyncHistory counts the series as having a hole. Wider
// than the longest Turkish holiday stretch (a 9-day bayram), so a fund bought
// on a closed day is not refetched on every run.
const backfillSlackDays = 14

// Fetcher retrieves fund NAVs from TEFAS over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to a client with a 30s
	// timeout.
	HTTPClient *http.Client
	// BaseURL defaults to defaultBaseURL; tests point it at httptest.
	BaseURL string
}

// NewFetcher returns a Fetcher pointed at the production TEFAS API.
func NewFetcher() *Fetcher {
	return &Fetcher{}
}

// NAV is one published daily price. Date is YYYY-MM-DD. Name is the fund's
// title (fonUnvan) as TEFAS printed it on that row; it changes over time
// while the code stays.
type NAV struct {
	Date  string
	Close decimal.Decimal
	Name  string
}

type navResponse struct {
	ErrorMessage string `json:"errorMessage"`
	ResultList   []struct {
		Tarih    string      `json:"tarih"`
		Fiyat    json.Number `json:"fiyat"` // never float64
		FonUnvan string      `json:"fonUnvan"`
	} `json:"resultList"`
}

// errNoResult marks FetchNAVs' error for a null resultList, the API's
// answer for a code it does not know, so Lookup can tell it from a failure.
var errNoResult = errors.New("error")

// FetchNAVs POSTs one fund with a periyod-month window and returns its
// published NAVs. Unpublished rows (fiyat 0, today's placeholder) are
// dropped. A null resultList is the API's error shape and is an error.
func (f *Fetcher) FetchNAVs(ctx context.Context, symbol string, periyod int) ([]NAV, error) {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	body, _ := json.Marshal(map[string]any{"fonKodu": symbol, "dil": "TR", "periyod": periyod})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/funds/fonFiyatBilgiGetir", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build tefas request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", userAgent)
	respBody, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch tefas navs for %s: %w", symbol, err)
	}

	var r navResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("parse tefas navs for %s: %w", symbol, err)
	}
	if r.ResultList == nil {
		return nil, fmt.Errorf("tefas navs for %s: %w %q", symbol, errNoResult, r.ErrorMessage)
	}
	out := make([]NAV, 0, len(r.ResultList))
	for _, row := range r.ResultList {
		if row.Fiyat == "" {
			continue
		}
		c, err := decimal.NewFromString(row.Fiyat.String())
		if err != nil {
			return nil, fmt.Errorf("tefas navs for %s: bad fiyat %q: %w", symbol, row.Fiyat, err)
		}
		if !c.IsPositive() {
			continue
		}
		if _, err := time.Parse(time.DateOnly, row.Tarih); err != nil {
			return nil, fmt.Errorf("tefas navs for %s: bad tarih %q: %w", symbol, row.Tarih, err)
		}
		out = append(out, NAV{Date: row.Tarih, Close: c, Name: row.FonUnvan})
	}
	return out, nil
}

// Lookup asks TEFAS whether code is a fund it publishes NAVs for, with one
// one-month FetchNAVs. found is true when at least one NAV comes back, and
// name is then the newest row's fonUnvan. A null or empty resultList is not
// found, not an error; a transport or parse failure is an error. The
// statement import creates a fund TEFAS confirms this way.
func (f *Fetcher) Lookup(ctx context.Context, code string) (name string, found bool, err error) {
	navs, err := f.FetchNAVs(ctx, code, 1)
	if errors.Is(err, errNoResult) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var newest NAV
	for _, n := range navs {
		if n.Date > newest.Date {
			newest = n
		}
	}
	return newest.Name, len(navs) > 0, nil
}

// Syncer is the TEFAS fetcher and the repos SyncFund and SyncHistory read
// and write, built once at startup.
type Syncer struct {
	Fetcher     *Fetcher
	Instruments *instrument.Repo
	Prices      *price.Repo
}

// SyncFund fetches in's NAVs over a periyod-month window and upserts them
// in one transaction. Then, when the newest row's fonUnvan is non-empty and differs from
// in.Name, it renames the instrument: TEFAS names change while the code
// stays. Returns rows written.
func (s *Syncer) SyncFund(ctx context.Context, in instrument.Instrument, periyod int) (int, error) {
	navs, err := s.Fetcher.FetchNAVs(ctx, in.Symbol, periyod)
	if err != nil {
		return 0, err
	}
	rows := make([]price.Row, len(navs))
	for i, n := range navs {
		rows[i] = price.Row{InstrumentID: in.ID, Date: n.Date, Close: n.Close.String(), Source: SourceTEFAS}
	}
	if err := s.Prices.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert prices for %s: %w", in.Symbol, err)
	}
	var newest NAV
	for _, n := range navs {
		if n.Date > newest.Date {
			newest = n
		}
	}
	if newest.Name != "" && newest.Name != in.Name {
		if err := s.Instruments.Rename(ctx, in.ID, newest.Name); err != nil {
			return len(navs), err
		}
		slog.Default().Info("tefas fund renamed", "symbol", in.Symbol, "from", in.Name, "to", newest.Name)
	}
	return len(navs), nil
}

// Sync runs SyncFund for every tracked TEFAS fund (one referenced by at
// least one transaction), with the smallest window that reaches date. The
// daily run asks for today, so it re-reads the last month and fills any gap
// left by missed runs. The loop is instrument.EachTracked: a failing fund
// is logged and the rest continue, every fund failing is an error, and a
// cancelled ctx stops it quietly. Returns rows written.
func Sync(ctx context.Context, f *Fetcher, instruments *instrument.Repo, prices *price.Repo, date time.Time) (int, error) {
	tracked, err := instruments.Tracked(ctx, tefasMarket)
	if err != nil {
		return 0, fmt.Errorf("list tracked tefas instruments: %w", err)
	}
	periyod := coveringPeriod(date.Format(time.DateOnly), time.Now())
	s := &Syncer{Fetcher: f, Instruments: instruments, Prices: prices}
	written, failed, err := instrument.EachTracked(ctx, tracked, RequestPause, "funds", func(in instrument.Instrument) (int, bool, error) {
		n, err := s.SyncFund(ctx, in, periyod)
		return n, false, err
	})
	if ctx.Err() == nil {
		slog.Default().Info("tefas sync complete",
			"date", date.Format(time.DateOnly), "periyod", periyod, "written", written, "failed", failed)
	}
	return written, err
}

// SyncHistory fetches in's NAVs back to from (YYYY-MM-DD) in one request
// (renaming in as SyncFund does),
// unless its stored prices already span the range: no two consecutive dates
// among from, the prices stored in [from, now] and now are more than
// backfillSlackDays apart (with none stored in it, the newest price before
// from stands in for from, and a fund with no price at all is fetched), so a series that stops early or skips
// months is fetched again. from is clamped to the 60-month
// window TEFAS serves, so a very old transaction does not refetch forever.
// Returns rows written.
//
// shortcut: a transaction dated before the fund existed (a typo), and a
// delisted fund whose last NAV is older than backfillSlackDays, refetch the
// fund on every call, one request each. Upgrade path: remember the attempt
// in empty_fetches if that ever shows up in the logs.
func (s *Syncer) SyncHistory(ctx context.Context, in instrument.Instrument, from string, now time.Time) (int, error) {
	start, covered, err := s.historyStart(ctx, in, from, now)
	if err != nil || covered {
		return 0, err
	}
	return s.SyncFund(ctx, in, coveringPeriod(start, now))
}

// HistoryCovered reports whether SyncHistory has nothing to fetch for in
// from from, without any request.
func (s *Syncer) HistoryCovered(ctx context.Context, in instrument.Instrument, from string, now time.Time) (bool, error) {
	_, covered, err := s.historyStart(ctx, in, from, now)
	return covered, err
}

// historyStart returns the date SyncHistory fetches back to, and whether in's
// stored prices already span the range from there to now without a hole.
func (s *Syncer) historyStart(ctx context.Context, in instrument.Instrument, from string, now time.Time) (start string, covered bool, err error) {
	today := now.In(price.Istanbul)
	start = max(from, today.AddDate(0, -periods[len(periods)-1], 0).Format(time.DateOnly))
	if _, err := time.Parse(time.DateOnly, start); err != nil {
		return "", false, fmt.Errorf("tefas history for %s: bad start date %q: %w", in.Symbol, from, err)
	}
	stored, err := s.Prices.Range(ctx, []int64{in.ID}, start, today.Format(time.DateOnly))
	if err != nil {
		return "", false, fmt.Errorf("check tefas history for %s: %w", in.Symbol, err)
	}
	hole := false
	prev := start
	if len(stored) == 0 {
		// A transaction newer than the last NAV is covered by the price before it.
		last, err := s.Prices.Latest(ctx, []int64{in.ID}, start)
		if err != nil {
			return "", false, fmt.Errorf("check tefas history for %s: %w", in.Symbol, err)
		}
		p, ok := last[in.ID]
		hole, prev = !ok, cmp.Or(p.Date, start)
	}
	for _, p := range append(stored, price.Price{Date: today.Format(time.DateOnly)}) {
		hole = hole || daysBetween(prev, p.Date) > backfillSlackDays
		prev = p.Date
	}
	return start, !hole, nil
}

// daysBetween returns the days from a to b, both YYYY-MM-DD already validated.
func daysBetween(a, b string) int {
	ta, _ := time.Parse(time.DateOnly, a)
	tb, _ := time.Parse(time.DateOnly, b)
	return int(tb.Sub(ta).Hours() / 24)
}

// coveringPeriod returns the smallest TEFAS period whose window starts on or
// before date (YYYY-MM-DD), or the largest one when none reaches it.
func coveringPeriod(date string, now time.Time) int {
	for _, p := range periods {
		if now.In(price.Istanbul).AddDate(0, -p, 0).Format(time.DateOnly) <= date {
			return p
		}
	}
	return periods[len(periods)-1]
}
