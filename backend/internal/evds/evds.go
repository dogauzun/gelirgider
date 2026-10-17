// Package evds talks to TCMB's EVDS API, the BIST 100 index close source
// chosen in docs/adr/0012-benchmarks.md ("BIST 100 source"). EVDS needs the
// API key of a free account, sent in the "key" header, never in the URL.
package evds

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
)

// KeySetting is the secret setting holding the EVDS API key, entered in the
// admin area (ADR 0010).
const KeySetting = "provider.evds.api_key"

// defaultBaseURL is the production EVDS API base path.
const defaultBaseURL = "https://evds3.tcmb.gov.tr/igmevdsms-dis"

// maxBodyBytes caps a response read, far above any real one, so a broken
// or hostile upstream cannot exhaust memory; a larger body fails the fetch.
const maxBodyBytes = 8 << 20

// userAgent is the project's own, the same as tefas sends (ADR 0012).
const userAgent = "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)"

// sourceEVDS is the benchmark.Repo source value written by SyncXU100.
const sourceEVDS = "evds"

// maxItems is EVDS's per-call observation cap. A longer range is cut to the
// newest maxItems observations with status 200 (ADR 0012), so a response
// that reaches it may be missing its start.
const maxItems = 1000

// syncWindowDays is how far back SyncXU100 re-reads: a close an earlier run
// missed heals on the next run, and it spans the longest holiday run.
const syncWindowDays = 14

// requestPause is the polite delay BackfillXU100 sleeps between requests.
const requestPause = 200 * time.Millisecond

// dateLayout is EVDS's DD-MM-YYYY date format, in the path and in Tarih.
const dateLayout = "02-01-2006"

// Fetcher retrieves series from the EVDS API over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to httpbody's 30s client when nil.
	HTTPClient *http.Client
	// BaseURL is the API base path. Overridable so tests can point at an
	// httptest.Server. Defaults to defaultBaseURL when empty.
	BaseURL string
	// APIKey authenticates requests. Used when Settings is nil (tests).
	// Empty means not configured.
	APIKey string
	// Settings, when set, supplies the key on every fetch instead of
	// APIKey, so a key saved in the admin area is used by the next run
	// without a restart.
	Settings *settings.Repo
}

// NewFetcher returns a Fetcher pointed at the production EVDS API, with the
// key read from s on every fetch.
func NewFetcher(s *settings.Repo) *Fetcher {
	return &Fetcher{Settings: s}
}

// key returns the stored API key, or APIKey when Settings is nil. Empty
// means not configured. The XU100 fetcher calls it.
func (f *Fetcher) key(ctx context.Context) (string, error) {
	if f.Settings == nil {
		return f.APIKey, nil
	}
	k, _, err := f.Settings.GetSecret(ctx, KeySetting)
	return k, err
}

// HasKey reports whether an API key is configured, so the startup backfill
// can skip XU100 without one instead of failing.
func (f *Fetcher) HasKey(ctx context.Context) (bool, error) {
	k, err := f.key(ctx)
	return k != "", err
}

// keyTestTimeout bounds TestKey's one request.
const keyTestTimeout = 10 * time.Second

// TestKey makes one request with key for one day of the BIST 100 series and
// returns "" when EVDS accepts the key, or a short reason: "invalid key"
// (401 for a wrong key, 403 for none), "rate limited" (429) or
// "unreachable" (anything else), the same reasons as us.Fetcher.TestKey.
// The key and the response body are never returned or logged.
func (f *Fetcher) TestKey(ctx context.Context, key string) string {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	ctx, cancel := context.WithTimeout(ctx, keyTestTimeout)
	defer cancel()
	// EVDS takes its parameters in the path, not in a query string.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/series=TP.MK.F.BILESIK&startDate=25-09-2026&endDate=25-09-2026&type=json", nil)
	if err != nil {
		return "unreachable"
	}
	req.Header.Set("key", key)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpbody.Client(f.HTTPClient).Do(req)
	if err != nil {
		// The error names the URL, never the header carrying the key.
		slog.Default().Warn("provider key test failed", "setting", KeySetting, "error", err)
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
	slog.Default().Warn("provider key test failed", "setting", KeySetting, "status", resp.StatusCode)
	return "unreachable"
}

// Close is one BIST 100 daily close. Date is YYYY-MM-DD.
type Close struct {
	Date  string
	Value decimal.Decimal
}

// FetchXU100 returns the BIST 100 closes (series TP.MK.F.BILESIK) from from
// to to inclusive, in one request. Weekday holidays (null) are skipped. An
// empty key makes no request and returns an error that says where to set
// it. A response of maxItems or more is an error: EVDS cut it silently.
func (f *Fetcher) FetchXU100(ctx context.Context, from, to time.Time) ([]Close, error) {
	key, err := f.key(ctx)
	if err != nil {
		return nil, fmt.Errorf("xu100: read EVDS API key: %w", err)
	}
	if key == "" {
		return nil, errors.New("xu100: EVDS API key not configured, set it in the admin area")
	}
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	// EVDS takes its parameters in the path, not in a query string.
	url := base + "/series=TP.MK.F.BILESIK&startDate=" + from.Format(dateLayout) + "&endDate=" + to.Format(dateLayout) + "&type=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build evds request: %w", err)
	}
	req.Header.Set("key", key)
	req.Header.Set("User-Agent", userAgent)
	respBody, err := httpbody.Fetch(f.HTTPClient, req, maxBodyBytes)
	if err != nil {
		// The error names the URL and the status, never the body or the
		// header carrying the key.
		return nil, fmt.Errorf("fetch evds xu100: %w", err)
	}
	var body struct {
		Items []struct {
			Tarih string  `json:"Tarih"`
			Value *string `json:"TP_MK_F_BILESIK"`
		} `json:"items"`
	}
	if err := json.Unmarshal(respBody, &body); err != nil {
		return nil, fmt.Errorf("decode evds xu100: %w", err)
	}
	if len(body.Items) >= maxItems {
		return nil, fmt.Errorf("evds xu100: %d items for %s..%s, response truncated", len(body.Items), from.Format(dateLayout), to.Format(dateLayout))
	}
	var out []Close
	for _, it := range body.Items {
		if it.Value == nil {
			continue
		}
		d, err := time.Parse(dateLayout, it.Tarih)
		if err != nil {
			return nil, fmt.Errorf("evds xu100: bad date %q: %w", it.Tarih, err)
		}
		v, err := decimal.NewFromString(*it.Value)
		if err != nil {
			return nil, fmt.Errorf("evds xu100 %s: bad value %q: %w", it.Tarih, *it.Value, err)
		}
		out = append(out, Close{Date: d.Format(time.DateOnly), Value: v})
	}
	return out, nil
}

// SyncXU100 re-reads the closes of [date - syncWindowDays, date] and
// upserts them as benchmark XU100, source "evds". Returns the rows upserted.
func SyncXU100(ctx context.Context, f *Fetcher, repo *benchmark.Repo, date time.Time) (int, error) {
	return syncRange(ctx, f, repo, date.AddDate(0, 0, -syncWindowDays), date)
}

// BackfillXU100 upserts the closes of [from, to] that are not stored yet:
// every run of more than syncWindowDays calendar days without a
// stored close, the longest holiday run, is fetched; a shorter one is a
// holiday or left to SyncXU100's window, as EVDS has no trading calendar.
// The range starts syncWindowDays before from, so a from on a holiday is
// covered by a stored close before it; a stored range makes no request.
// It stops at the first error and returns the rows written.
func BackfillXU100(ctx context.Context, f *Fetcher, repo *benchmark.Repo, from, to time.Time) (int, error) {
	prev := from.AddDate(0, 0, -syncWindowDays) // covered up to here
	stored, err := repo.Range(ctx, benchmark.CodeXU100, prev.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return 0, err
	}
	total, fetched := 0, false
	walk := func(start, end time.Time) error {
		n, err := backfillRange(ctx, f, repo, start, end, fetched)
		total += n
		fetched = true
		return err
	}
	for _, v := range stored {
		d, err := time.ParseInLocation(time.DateOnly, v.Date, from.Location())
		if err != nil {
			return total, fmt.Errorf("xu100: bad stored date %q: %w", v.Date, err)
		}
		if d.After(prev.AddDate(0, 0, syncWindowDays)) {
			if err := walk(prev.AddDate(0, 0, 1), d.AddDate(0, 0, -1)); err != nil {
				return total, err
			}
		}
		prev = d
	}
	if len(stored) == 0 || to.After(prev.AddDate(0, 0, syncWindowDays)) {
		if err := walk(prev.AddDate(0, 0, 1), to); err != nil {
			return total, err
		}
	}
	return total, nil
}

// backfillRange upserts the closes of [from, to] one year per request, at
// most 262 observations, far under maxItems. It pauses requestPause
// between requests, and before the first one too when pause is set.
func backfillRange(ctx context.Context, f *Fetcher, repo *benchmark.Repo, from, to time.Time, pause bool) (int, error) {
	total := 0
	for start := from; !start.After(to); {
		if pause || start != from {
			select {
			case <-ctx.Done():
				return total, ctx.Err()
			case <-time.After(requestPause):
			}
		}
		end := start.AddDate(1, 0, -1)
		if end.After(to) {
			end = to
		}
		n, err := syncRange(ctx, f, repo, start, end)
		total += n
		if err != nil {
			return total, err
		}
		start = end.AddDate(0, 0, 1)
	}
	return total, nil
}

// syncRange fetches [from, to] in one request and upserts every close as
// exact decimal text without trailing zeros (12899.35000000 → 12899.35).
func syncRange(ctx context.Context, f *Fetcher, repo *benchmark.Repo, from, to time.Time) (int, error) {
	closes, err := f.FetchXU100(ctx, from, to)
	if err != nil {
		return 0, err
	}
	rows := make([]benchmark.Row, len(closes))
	for i, c := range closes {
		rows[i] = benchmark.Row{Code: benchmark.CodeXU100, Date: c.Date, Value: c.Value.String(), Source: sourceEVDS}
	}
	if err := repo.UpsertMany(ctx, rows); err != nil {
		return 0, fmt.Errorf("upsert xu100 %s to %s: %w", from.Format(time.DateOnly), to.Format(time.DateOnly), err)
	}
	return len(closes), nil
}
