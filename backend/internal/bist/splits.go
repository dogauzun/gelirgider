package bist

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/split"
)

// defaultCapitalURL is İş Yatırım's "Temel Değerler ve Oranlar" page, whose
// server-rendered capital increase table lists the current calendar year's
// events (ADR 0008, "BIST source"). robots.txt disallows /_layouts/, so the
// page's year-filter AJAX call is never made: older years go through
// -add-split.
const defaultCapitalURL = "https://www.isyatirim.com.tr/tr-tr/analiz/hisse/Sayfalar/Temel-Degerler-Ve-Oranlar.aspx"

// sourceIsYatirim is the split.Repo and dividend.Repo source value written
// by SyncCapitalEvents.
const sourceIsYatirim = "isyatirim"

// CapitalEvent is one row of the capital increase table on ex-date Date
// (YYYY-MM-DD). Ratio is the bonus issue (bedelsiz) split ratio, new shares
// per old share, zero when the row has no bonus part. Dividend is the gross
// TRY cash dividend per share, zero when the row pays none (ADR 0009).
type CapitalEvent struct {
	Symbol   string
	Date     string
	Ratio    decimal.Decimal
	Dividend decimal.Decimal
}

const capitalTableStart = `<tbody id="SermayeRows">`

// capitalCell matches one cell of the capitalColumns columns read, by class
// name, not position, so a reordered table still parses and a renamed one
// fails.
var capitalCell = regexp.MustCompile(`<td class="[^"]*\b(HISSE_KODU|SHHE_TARIH|SHHE_BDSZ_IK_ORAN|SHHE_BDSZ_TM_ORAN|SHHE_NAKIT_TM_ORAN)\b[^"]*"([^>]*)>(.*?)</td>`)

const capitalColumns = 5

var (
	dataOrder = regexp.MustCompile(`data-order="(\d{8})`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
)

// FetchCapitalEvents downloads the capital increase table and returns its
// bonus issues and cash dividends. Rows with neither (rights issues) are
// dropped; rows dated in the future are kept for the caller to skip.
func (f *Fetcher) FetchCapitalEvents(ctx context.Context) ([]CapitalEvent, error) {
	url := cmp.Or(f.CapitalURL, defaultCapitalURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build capital increase request: %w", err)
	}
	body, err := httpbody.Fetch(f.HTTPClient, req, maxPageBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch capital increases: %w", err)
	}
	return parseCapitalTable(string(body))
}

// parseCapitalTable reads the SermayeRow rows of the capital increase table.
// A missing table or a row missing one of the read cells is an error, so a
// page redesign is noticed instead of silently storing nothing.
func parseCapitalTable(page string) ([]CapitalEvent, error) {
	start := strings.Index(page, capitalTableStart)
	if start < 0 {
		return nil, fmt.Errorf("capital increase table %q not found", capitalTableStart)
	}
	table := page[start:]
	if end := strings.Index(table, "</tbody>"); end >= 0 {
		table = table[:end]
	}

	var out []CapitalEvent
	for _, row := range strings.Split(table, `<tr class="SermayeRow">`)[1:] {
		cells := map[string][2]string{} // class -> {attributes, inner html}
		for _, m := range capitalCell.FindAllStringSubmatch(row, -1) {
			cells[m[1]] = [2]string{m[2], m[3]}
		}
		if len(cells) != capitalColumns {
			return nil, fmt.Errorf("capital increase row missing a column (got %d of %d): %.200s", len(cells), capitalColumns, row)
		}
		ik, err := parseTurkishDecimal(cells["SHHE_BDSZ_IK_ORAN"][1])
		if err != nil {
			return nil, err
		}
		tm, err := parseTurkishDecimal(cells["SHHE_BDSZ_TM_ORAN"][1])
		if err != nil {
			return nil, err
		}
		// Percent of the 1 TL nominal, so gross TRY per share = percent / 100
		// (ADR 0009).
		cash, err := parseTurkishDecimal(cells["SHHE_NAKIT_TM_ORAN"][1])
		if err != nil {
			return nil, err
		}
		bonus := ik.Add(tm)
		if bonus.IsZero() && cash.IsZero() {
			continue // rights issue: neither a split (ADR 0008) nor a dividend
		}
		ev := CapitalEvent{Dividend: cash.Shift(-2)}
		if !bonus.IsZero() {
			ev.Ratio = decimal.NewFromInt(1).Add(bonus.Shift(-2))
		}
		symbol := strings.TrimSpace(htmlTag.ReplaceAllString(cells["HISSE_KODU"][1], ""))
		dm := dataOrder.FindStringSubmatch(cells["SHHE_TARIH"][0])
		if dm == nil {
			return nil, fmt.Errorf("capital increase row for %s: no data-order date", symbol)
		}
		d, err := time.Parse("20060102", dm[1])
		if err != nil {
			return nil, fmt.Errorf("capital increase row for %s: date %q: %w", symbol, dm[1], err)
		}
		ev.Symbol, ev.Date = symbol, d.Format(time.DateOnly)
		out = append(out, ev)
	}
	return out, nil
}

// parseTurkishDecimal parses "1.799,90" ("." thousands, "," decimal).
func parseTurkishDecimal(s string) (decimal.Decimal, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ".", "")
	d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("capital increase percent %q: %w", s, err)
	}
	return d, nil
}

// SyncCapitalEvents stores every bonus issue and cash dividend dated on or
// before today's date (the caller passes an Europe/Istanbul time): bonus
// issues in splitRepo, dividends in dividendRepo in TRY, both with source
// "isyatirim". Planned rows dated after today are skipped; the page is
// re-read daily. Symbols with no BIST instrument are skipped, never
// inserted. Returns the number of splits plus dividends written.
func SyncCapitalEvents(ctx context.Context, fetcher *Fetcher, instrumentRepo *instrument.Repo, splitRepo *split.Repo, dividendRepo *dividend.Repo, today time.Time) (int, error) {
	events, err := fetcher.FetchCapitalEvents(ctx)
	if err != nil {
		return 0, err
	}
	instruments, err := instrumentRepo.All(ctx, "BIST")
	if err != nil {
		return 0, fmt.Errorf("list instruments: %w", err)
	}
	idBySymbol := make(map[string]int64, len(instruments))
	for _, in := range instruments {
		idBySymbol[in.Symbol] = in.ID
	}

	todayStr := today.Format(time.DateOnly)
	splits, dividends, unknown, planned := 0, 0, 0, 0
	for _, ev := range events {
		if ev.Date > todayStr {
			planned++
			continue
		}
		id, ok := idBySymbol[ev.Symbol]
		if !ok {
			unknown++
			continue
		}
		if !ev.Ratio.IsZero() {
			if err := splitRepo.Upsert(ctx, id, ev.Date, ev.Ratio, sourceIsYatirim); err != nil {
				return splits + dividends, fmt.Errorf("upsert split for %s: %w", ev.Symbol, err)
			}
			splits++
		}
		if ev.Dividend.IsPositive() {
			if err := dividendRepo.Upsert(ctx, id, ev.Date, ev.Dividend, "TRY", sourceIsYatirim); err != nil {
				return splits + dividends, fmt.Errorf("upsert dividend for %s: %w", ev.Symbol, err)
			}
			dividends++
		}
	}
	slog.Default().Info("bist capital event sync complete",
		"rows", len(events), "splits", splits, "dividends", dividends,
		"unknown_symbols", unknown, "planned", planned)
	return splits + dividends, nil
}
