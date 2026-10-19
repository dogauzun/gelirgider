// Package btcturk fetches daily closes for tracked crypto pairs from
// BtcTurk's public klines endpoint and stores them via price.Repo with
// source = "btcturk". Same shape as internal/us (Fetcher + Sync); source,
// endpoint and the USDT-as-USD simplification are documented in
// ADR 0007. SyncGold stores the PAXG-TRY gram gold
// price (ADR 0017).
package btcturk

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// defaultBaseURL is the production BtcTurk graph API base path.
const defaultBaseURL = "https://graph-api.btcturk.com"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// SourceBtcTurk is the price.Repo source value written by this package.
const SourceBtcTurk = "btcturk"

// SourcePAXG is the price.Repo and benchmark.Repo source value written by
// SyncGold.
const SourcePAXG = "btcturk_paxg"

// GoldHistoryStart is the date of BtcTurk's first PAXG-TRY daily candle,
// where the gold history starts (ADR 0017).
const GoldHistoryStart = "2022-06-30"

// gramsPerTroyOunce turns the PAXG-TRY close (TRY per fine troy ounce) into
// TRY per gram (ADR 0017, "Price").
var gramsPerTroyOunce = decimal.RequireFromString("31.1034768")

// syncDays is how many days before date Sync re-requests, so any daily
// run fills a short gap left by missed runs.
const syncDays = 7

// RequestPause is the polite delay Sync sleeps between pairs.
const RequestPause = 200 * time.Millisecond

// Fetcher retrieves daily candles from BtcTurk over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to httpbody's 30s client.
	HTTPClient *http.Client
	// BaseURL defaults to defaultBaseURL; tests point it at httptest.
	BaseURL string
}

// NewFetcher returns a Fetcher pointed at the production BtcTurk API.
func NewFetcher() *Fetcher {
	return &Fetcher{}
}

// Candle is one day's close. Date is the candle's UTC date, YYYY-MM-DD.
type Candle struct {
	Date  string
	Close decimal.Decimal
}

type klinesResponse struct {
	S string        `json:"s"`
	T []json.Number `json:"t"`
	C []json.Number `json:"c"`
}

// Pair maps an instrument symbol BASE-QUOTE to a BtcTurk pair: BTC-TRY is
// BTCTRY, BTC-USD is BTCUSDT (1 USDT treated as 1 USD, ADR 0007). Any
// other quote currency is an error.
func Pair(symbol string) (string, error) {
	base, quote, ok := strings.Cut(symbol, "-")
	if !ok || base == "" {
		return "", fmt.Errorf("btcturk: symbol %q is not BASE-QUOTE", symbol)
	}
	switch quote {
	case "TRY":
		return base + "TRY", nil
	case "USD":
		return base + "USDT", nil
	}
	return "", fmt.Errorf("btcturk: unsupported quote currency %q in %s", quote, symbol)
}

// FetchCandles returns the finished daily candles (t + 24h <= now) of pair
// between from and to. The newest candle BtcTurk returns is the current,
// unfinished day and is dropped. Zero or missing closes are skipped.
// Numbers are parsed as json.Number straight into decimal, never float64.
func (f *Fetcher) FetchCandles(ctx context.Context, pair string, from, to time.Time) ([]Candle, error) {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	u, err := url.Parse(base + "/v1/klines/history")
	if err != nil {
		return nil, fmt.Errorf("build btcturk url: %w", err)
	}
	q := u.Query()
	q.Set("symbol", pair)
	q.Set("resolution", "1D")
	q.Set("from", strconv.FormatInt(from.Unix(), 10))
	q.Set("to", strconv.FormatInt(to.Unix(), 10))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build btcturk request: %w", err)
	}
	body, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch btcturk klines for %s: %w", pair, err)
	}

	var kr klinesResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&kr); err != nil {
		return nil, fmt.Errorf("parse btcturk klines for %s: %w", pair, err)
	}
	if kr.S != "ok" {
		return nil, fmt.Errorf("btcturk klines for %s: status %q", pair, kr.S)
	}
	if len(kr.T) != len(kr.C) {
		return nil, fmt.Errorf("btcturk klines for %s: %d times but %d closes", pair, len(kr.T), len(kr.C))
	}

	now := time.Now()
	out := make([]Candle, 0, len(kr.T))
	for i, tn := range kr.T {
		sec, err := tn.Int64()
		if err != nil {
			return nil, fmt.Errorf("btcturk klines for %s: bad time %q: %w", pair, tn, err)
		}
		start := time.Unix(sec, 0).UTC()
		if start.Add(24 * time.Hour).After(now) {
			continue // current, unfinished day
		}
		if kr.C[i] == "" {
			continue // null close
		}
		c, err := decimal.NewFromString(kr.C[i].String())
		if err != nil {
			return nil, fmt.Errorf("btcturk klines for %s: bad close %q: %w", pair, kr.C[i], err)
		}
		if !c.IsPositive() {
			continue
		}
		out = append(out, Candle{Date: start.Format(time.DateOnly), Close: c})
	}
	return out, nil
}

// SyncRange fetches in's daily candles between from and to in one request
// and upserts every finished one under its UTC date. Returns rows written.
func SyncRange(ctx context.Context, f *Fetcher, prices *price.Repo, in instrument.Instrument, from, to time.Time) (int, error) {
	pair, err := Pair(in.Symbol)
	if err != nil {
		return 0, err
	}
	candles, err := f.FetchCandles(ctx, pair, from, to)
	if err != nil {
		return 0, err
	}
	rows := make([]price.Row, len(candles))
	for i, c := range candles {
		rows[i] = price.Row{InstrumentID: in.ID, Date: c.Date, Close: c.Close.String(), Source: SourceBtcTurk}
	}
	if err := prices.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert prices for %s: %w", in.Symbol, err)
	}
	return len(candles), nil
}

// Sync runs SyncRange over the syncDays days up to date for every tracked
// CRYPTO instrument (one referenced by at least one transaction), in
// instrument.EachTracked: a failing pair is logged and the rest continue,
// every pair failing is an error, and a cancelled ctx stops it quietly.
// Returns rows written.
func Sync(ctx context.Context, f *Fetcher, instruments *instrument.Repo, prices *price.Repo, date time.Time) (int, error) {
	tracked, err := instruments.Tracked(ctx, instrument.MarketCrypto)
	if err != nil {
		return 0, fmt.Errorf("list tracked crypto instruments: %w", err)
	}
	from, to := date.AddDate(0, 0, -syncDays), date.Add(24*time.Hour)
	written, failed, err := instrument.EachTracked(ctx, tracked, RequestPause, "pairs", func(in instrument.Instrument) (int, bool, error) {
		n, err := SyncRange(ctx, f, prices, in, from, to)
		return n, false, err
	})
	if ctx.Err() == nil {
		slog.Default().Info("btcturk crypto sync complete",
			"date", date.Format(time.DateOnly), "written", written, "failed", failed)
	}
	return written, err
}

// GoldSyncer is the BtcTurk fetcher and the repos SyncGold and
// SyncGoldHistory read and write, built once at startup.
type GoldSyncer struct {
	Fetcher     *Fetcher
	Instruments *instrument.Repo
	Prices      *price.Repo
	Benchmarks  *benchmark.Repo
}

// SyncGold fetches the PAXGTRY daily candles between from and to in one
// request and stores each finished candle's close / 31.1034768, rounded to
// 4 decimals, under its UTC date: as the GOLD/GRAM price and as the GOLD
// benchmark value, both with source btcturk_paxg (ADR 0017). There is no
// tracked-instrument check: the benchmark needs the series even when nobody
// holds gold. A missing GOLD/GRAM instrument is an error and nothing is
// written; the instrument seed creates it, never SyncGold. Returns the
// number of days stored as prices, also when the benchmark write then fails.
//
// shortcut: PAXG, a token for one fine troy ounce, is the gold proxy, closed
// at 24:00 UTC with no bank spread. To change the source, change the Source
// section of docs/adr/0017-gold.md first.
func (s *GoldSyncer) SyncGold(ctx context.Context, from, to time.Time) (int, error) {
	gram, err := s.Instruments.BySymbol(ctx, instrument.MarketGold, "GRAM")
	if err != nil {
		return 0, fmt.Errorf("look up GOLD/GRAM: %w", err)
	}
	candles, err := s.Fetcher.FetchCandles(ctx, "PAXGTRY", from, to)
	if err != nil {
		return 0, err
	}
	priceRows := make([]price.Row, len(candles))
	benchRows := make([]benchmark.Row, len(candles))
	for i, c := range candles {
		g := c.Close.DivRound(gramsPerTroyOunce, 4).StringFixed(4)
		priceRows[i] = price.Row{InstrumentID: gram.ID, Date: c.Date, Close: g, Source: SourcePAXG}
		benchRows[i] = benchmark.Row{Code: benchmark.CodeGOLD, Date: c.Date, Value: g, Source: SourcePAXG}
	}
	if err := s.Prices.UpsertMany(ctx, priceRows); err != nil {
		return 0, fmt.Errorf("upsert GOLD/GRAM prices: %w", err)
	}
	if err := s.Benchmarks.UpsertMany(ctx, benchRows); err != nil {
		return len(candles), fmt.Errorf("upsert GOLD benchmark: %w", err)
	}
	return len(candles), nil
}

// LastCandleDay returns the date of the newest finished daily candle at
// now: yesterday in UTC, as a UTC midnight. Crypto and gold closes are
// stored under the UTC candle date (ADR 0007, ADR 0017), so this can be a
// day before price.Yesterday.
func LastCandleDay(now time.Time) time.Time {
	return price.Midnight(now.UTC()).AddDate(0, 0, -1)
}

// SyncGoldHistory runs SyncGold from from (YYYY-MM-DD) up to now in one
// request. from is moved back to at most six days before yesterday (UTC),
// so a gold buy dated today still gets a last price, and forward to
// GoldHistoryStart. A GOLD/GRAM price on every day from there to yesterday
// is nothing to fetch. Returns the number of days stored.
func (s *GoldSyncer) SyncGoldHistory(ctx context.Context, from string, now time.Time) (int, error) {
	day, stored, err := s.historyStart(ctx, from, now)
	if err != nil || stored {
		return 0, err
	}
	return s.SyncGold(ctx, day, now)
}

// HistoryCovered reports whether SyncGoldHistory has nothing to fetch from
// from, without any request.
func (s *GoldSyncer) HistoryCovered(ctx context.Context, from string, now time.Time) (bool, error) {
	_, stored, err := s.historyStart(ctx, from, now)
	return stored, err
}

// historyStart returns the first day SyncGoldHistory fetches, and whether
// GOLD/GRAM is already priced on every day from there to yesterday.
func (s *GoldSyncer) historyStart(ctx context.Context, from string, now time.Time) (day time.Time, stored bool, err error) {
	last := LastCandleDay(now)
	from = min(from, last.AddDate(0, 0, -6).Format(time.DateOnly))
	day, err = time.Parse(time.DateOnly, max(from, GoldHistoryStart))
	if err != nil {
		return day, false, fmt.Errorf("gold history start date: %w", err)
	}
	gram, err := s.Instruments.BySymbol(ctx, instrument.MarketGold, "GRAM")
	if err != nil {
		return day, false, fmt.Errorf("look up GOLD/GRAM: %w", err)
	}
	stored, err = PricedEveryDay(ctx, s.Prices, gram.ID, day, last)
	return day, stored, err
}

// PricedEveryDay reports whether instrumentID has a price row on every day
// from start to last, both UTC midnights. An empty range (start
// after last) counts as stored: there is no finished candle to fetch.
func PricedEveryDay(ctx context.Context, prices *price.Repo, instrumentID int64, start, last time.Time) (bool, error) {
	have, err := prices.Range(ctx, []int64{instrumentID}, start.Format(time.DateOnly), last.Format(time.DateOnly))
	if err != nil {
		return false, err
	}
	return len(have) >= int(last.Sub(start).Hours()/24)+1, nil
}
