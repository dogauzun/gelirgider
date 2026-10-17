// Package tcmb fetches the Central Bank of Turkey (TCMB) daily exchange
// rate bulletin for one date and stores USD and EUR rates via fx.Repo.
// Source, URL pattern, XML shape, and quirks are documented in
// docs/adr/0003-multi-currency-and-market-data-sources.md; this package
// keeps every provider-specific detail internal, the same shape ADR 0001
// already chose for internal/bist.
package tcmb

import (
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
)

// defaultBaseURL is the production bulletin archive base path.
const defaultBaseURL = "https://www.tcmb.gov.tr/kurlar/"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// SourceTCMB is the fx.Repo source value written by Sync.
const SourceTCMB = "tcmb"

// trackedCurrencies limits stored rates to the currencies the app actually
// needs (ADR 0003: TRY, USD, EUR are the only transaction currencies, and
// TRY is implicit with no row). TCMB publishes many more; storing the rest
// would be dead data.
var trackedCurrencies = map[string]bool{"USD": true, "EUR": true}

// Rate is one currency's exchange rate for a date.
type Rate struct {
	Currency string
	Date     string // YYYY-MM-DD
	Rate     decimal.Decimal
}

// xmlBulletin is the root <Tarih_Date> element; only the currency list is
// needed.
type xmlBulletin struct {
	Currencies []xmlCurrency `xml:"Currency"`
}

// xmlCurrency is one <Currency> element. Matched by attribute/element name,
// not position, so an upstream column reorder doesn't silently break
// parsing.
type xmlCurrency struct {
	CurrencyCode string `xml:"CurrencyCode,attr"`
	Unit         string `xml:"Unit"`
	ForexBuying  string `xml:"ForexBuying"`
}

// Fetcher retrieves and parses the daily bulletin XML over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to httpbody's client with a
	// 30s timeout when nil.
	HTTPClient *http.Client
	// BaseURL is the bulletin archive base path, e.g. defaultBaseURL.
	// Overridable so tests can point at an httptest.Server. Defaults to
	// defaultBaseURL when empty.
	BaseURL string
}

// NewFetcher returns a Fetcher pointed at the production TCMB URL.
func NewFetcher() *Fetcher {
	return &Fetcher{}
}

// FetchRates downloads and parses the daily exchange rate bulletin for
// date, using the archive URL (which also serves today's bulletin once
// published, so there is no need to special-case today.xml). A 404
// response (weekend, holiday, or not yet published) returns a nil slice
// and nil error, the same "not available" signal bist.Fetcher.FetchCloses
// returns for the same situation, so scheduler.Run (which retries
// on a zero-row, nil-error Sync result) treats it identically without
// needing a distinct sentinel. Any other non-200 status, or an XML parse
// failure, is an error. Only currencies in trackedCurrencies are returned;
// a currency with an empty ForexBuying is skipped.
func (f *Fetcher) FetchRates(ctx context.Context, date time.Time) ([]Rate, error) {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	url := fmt.Sprintf("%s%04d%02d/%02d%02d%04d.xml",
		base, date.Year(), date.Month(), date.Day(), date.Month(), date.Year())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build tcmb request: %w", err)
	}
	body, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if se := (*httpbody.StatusError)(nil); errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch tcmb bulletin: %w", err)
	}

	return parseBulletin(body, date.Format(time.DateOnly))
}

// parseBulletin parses the bulletin XML and returns one Rate per tracked
// currency present with a non-empty ForexBuying. A non-positive ForexBuying
// or Unit fails the whole bulletin, like any other malformed value.
func parseBulletin(body []byte, date string) ([]Rate, error) {
	var b xmlBulletin
	if err := xml.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse tcmb bulletin xml: %w", err)
	}

	var rates []Rate
	for _, c := range b.Currencies {
		if !trackedCurrencies[c.CurrencyCode] {
			continue
		}
		if c.ForexBuying == "" {
			continue
		}
		buying, err := decimal.NewFromString(c.ForexBuying)
		if err != nil {
			return nil, fmt.Errorf("parse %s ForexBuying %q: %w", c.CurrencyCode, c.ForexBuying, err)
		}
		unit := decimal.NewFromInt(1)
		if c.Unit != "" {
			unit, err = decimal.NewFromString(c.Unit)
			if err != nil {
				return nil, fmt.Errorf("parse %s Unit %q: %w", c.CurrencyCode, c.Unit, err)
			}
		}
		// A zero Unit would panic in Div; a non-positive rate would zero or
		// flip every converted value.
		if !buying.IsPositive() || !unit.IsPositive() {
			return nil, fmt.Errorf("tcmb %s: non-positive ForexBuying %q or Unit %q", c.CurrencyCode, c.ForexBuying, c.Unit)
		}
		rates = append(rates, Rate{
			Currency: c.CurrencyCode,
			Date:     date,
			Rate:     buying.Div(unit),
		})
	}
	return rates, nil
}

// Sync fetches rates for date and upserts them into fxRepo with
// source = "tcmb", overwriting any existing row for the same (currency,
// date). Returns the number of rows written.
func Sync(ctx context.Context, fetcher *Fetcher, fxRepo *fx.Repo, date time.Time) (int, error) {
	rates, err := fetcher.FetchRates(ctx, date)
	if err != nil {
		return 0, err
	}
	if len(rates) == 0 {
		return 0, nil
	}

	dateStr := date.Format(time.DateOnly)
	rows := make([]fx.Row, len(rates))
	for i, r := range rates {
		rows[i] = fx.Row{Currency: r.Currency, Date: dateStr, Rate: r.Rate.String(), Source: SourceTCMB}
	}
	if err := fxRepo.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert fx rates for %s: %w", dateStr, err)
	}
	written := len(rows)
	slog.Default().Info("tcmb fx sync complete", "date", dateStr, "written", written)
	return written, nil
}

// BackfillSource is TCMB's FX rates as a bist.BackfillSource, walked one
// weekday at a time, for the startup backfill and the on-demand one alike.
// Its Name is scheduler.NameTCMB, the scheduler source name the on-demand
// backfill queue logs the walk under in source_runs; the fx rows it checks
// and marks are SourceTCMB's.
func BackfillSource(f *Fetcher, fxRepo *fx.Repo) bist.BackfillSource {
	return bist.BackfillSource{
		Name: scheduler.NameTCMB,
		HasSource: func(ctx context.Context, date string) (bool, error) {
			return fxRepo.HasSource(ctx, date, SourceTCMB)
		},
		Sync: func(ctx context.Context, d time.Time) (int, error) {
			return Sync(ctx, f, fxRepo, d)
		},
		MarkEmpty: func(ctx context.Context, date string) error {
			return fxRepo.MarkEmpty(ctx, date, SourceTCMB)
		},
	}
}
