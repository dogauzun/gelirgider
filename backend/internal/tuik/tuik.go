// Package tuik fetches the monthly Turkish CPI level (TÜFE) from the TÜİK
// data portal and stores it in benchmark_values. Source, format and quirks
// are documented in docs/adr/0012-benchmarks.md, "Storage and sources".
package tuik

import (
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
)

// defaultBaseURL is the production TÜİK data portal host. robots.txt
// allows it (ADR 0012).
const defaultBaseURL = "https://veriportali.tuik.gov.tr"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// tufePath is the CSV download of dataflow DF_TUFE_SDMX_TT10, English file
// (`.` decimals). The version is part of the id: a new one 404s.
const tufePath = "/api/en/dataflows/DF_TUFE_SDMX_TT10+V1.0/file/csv"

// userAgent is the project's own, the same as tefas sends. curl's default
// User-Agent gets 403 on this host.
const userAgent = "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)"

// sourceTUIK is the benchmark.Repo source value written by SyncTUFE.
const sourceTUIK = "tuik"

// columns is the field count of every row. Columns are read by position:
// the header text embeds sample values that change every month.
const (
	columns   = 15
	colSeries = 3 // "Index" or a rate of change
	colBase   = 4 // base year
	colMonth  = 7 // YYYY-MM
	colValue  = 8
)

// Fetcher downloads the TÜFE file over HTTP.
type Fetcher struct {
	// HTTPClient defaults to a client with a 30s timeout.
	HTTPClient *http.Client
	// BaseURL defaults to defaultBaseURL; tests point it at httptest.
	BaseURL string
}

// NewFetcher returns a Fetcher pointed at the production TÜİK portal.
func NewFetcher() *Fetcher {
	return &Fetcher{}
}

// Level is one month's published CPI level. Month is the first day of the
// month (YYYY-MM-01), Value the exact decimal text from the file.
type Level struct {
	Month string
	Value string
}

// FetchTUFE downloads the whole CPI history and returns its Index rows.
func (f *Fetcher) FetchTUFE(ctx context.Context) ([]Level, error) {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+tufePath, nil)
	if err != nil {
		return nil, fmt.Errorf("build tuik request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	body, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch tuik tufe: %w", err)
	}
	return parseTUFE(body)
}

// parseTUFE returns the Index rows of the file. Any malformed row, or Index
// rows on more than one base year, fails the whole file.
func parseTUFE(body []byte) ([]Level, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))))
	r.Comma = ';'
	r.FieldsPerRecord = columns
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse tuik tufe csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("tuik tufe: no data rows")
	}
	var levels []Level
	baseYear := ""
	for _, rec := range records[1:] {
		if rec[colSeries] != "Index" {
			continue
		}
		if baseYear == "" {
			baseYear = rec[colBase]
		} else if rec[colBase] != baseYear {
			return nil, fmt.Errorf("tuik tufe: Index rows on base years %s and %s", baseYear, rec[colBase])
		}
		month, err := time.Parse("2006-01", rec[colMonth])
		if err != nil {
			return nil, fmt.Errorf("tuik tufe: bad month %q: %w", rec[colMonth], err)
		}
		v, err := decimal.NewFromString(rec[colValue])
		if err != nil {
			return nil, fmt.Errorf("tuik tufe %s: bad level %q: %w", rec[colMonth], rec[colValue], err)
		}
		if !v.IsPositive() {
			return nil, fmt.Errorf("tuik tufe %s: non-positive level %q", rec[colMonth], rec[colValue])
		}
		levels = append(levels, Level{Month: month.Format(time.DateOnly), Value: rec[colValue]})
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("tuik tufe: no Index rows")
	}
	return levels, nil
}

// SyncTUFE downloads the whole history and upserts every Index row as
// benchmark TUFE, source "tuik". One run is both the daily sync and the
// backfill: a rebase republished by TÜİK rewrites the history in one run.
// Nothing is written unless the whole file parses. Returns the rows
// upserted.
func SyncTUFE(ctx context.Context, f *Fetcher, repo *benchmark.Repo) (int, error) {
	levels, err := f.FetchTUFE(ctx)
	if err != nil {
		return 0, err
	}
	rows := make([]benchmark.Row, len(levels))
	for i, l := range levels {
		rows[i] = benchmark.Row{Code: benchmark.CodeTUFE, Date: l.Month, Value: l.Value, Source: sourceTUIK}
	}
	if err := repo.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert tufe: %w", err)
	}
	written := len(rows)
	slog.Default().Info("tuik tufe sync complete", "written", written)
	return written, nil
}
