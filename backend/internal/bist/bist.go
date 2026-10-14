// Package bist fetches Borsa Istanbul daily equity bulletin closing prices
// and stores them via price.Repo. Source, URL pattern, CSV layout, and
// quirks are documented in docs/adr/0001-bist-price-source.md; this package
// keeps every provider-specific detail internal. It also fetches BIST bonus
// issues and cash dividends from İş Yatırım's capital increase table
// (splits.go, ADR 0008).
package bist

import (
	"archive/zip"
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// defaultBaseURL is the production bulletin base path.
const defaultBaseURL = "https://www.borsaistanbul.com/data/thb/"

// maxBulletinBytes and maxPageBytes cap a response read, far above any real
// bulletin zip or capital increase page, so a broken or hostile upstream
// cannot exhaust memory; a larger body fails the fetch.
const (
	maxBulletinBytes = 32 << 20
	maxPageBytes     = 8 << 20
)

// SourceBistBulletin is the price.Repo source value written by Sync.
const SourceBistBulletin = "bist_bulletin"

// RequestPause is a polite delay callers should sleep between successive
// Sync calls when fetching many dates in a row (e.g. a historical backfill
// walking years of bulletins), so as not to hammer the bulletin server.
const RequestPause = 200 * time.Millisecond

// Turkish bulletin header names (row 1) the parser matches columns by, not
// index, so an upstream layout change fails loudly instead of silently.
const (
	headerTradeDate = "TARIH"
	headerSymbol    = "ISLEM KODU"
	headerName      = "BULTEN ADI"
	headerGroup     = "ENSTRUMAN GRUBU"
	headerClose     = "KAPANIS FIYATI"
)

// wantedGroups maps each ENSTRUMAN GRUBU we track (equities,
// exchange-traded funds and the Darphane gold certificate ALTIN.S1, the
// only EMS instrument) to the ISLEM KODU suffix stripped from it to get
// instruments.symbol. Rows in any other group are skipped.
var wantedGroups = map[string]string{
	"EQT": ".E",
	"ETF": ".F",
	"EMS": ".S1",
}

// Close is one instrument's closing price for a trading date. Close is zero
// when the bulletin prints an empty or zero close (a suspended instrument);
// only FetchBulletin returns such rows.
type Close struct {
	Symbol string
	Name   string // BULTEN ADI, used when Sync inserts a new instrument
	Date   string // YYYY-MM-DD
	Close  decimal.Decimal
}

// Fetcher retrieves and parses daily bulletin zips over HTTP.
type Fetcher struct {
	// HTTPClient is used for requests. Defaults to httpbody's client with a
	// 30s timeout when nil.
	HTTPClient *http.Client
	// BaseURL is the bulletin base path, e.g. defaultBaseURL. Overridable
	// so tests can point at an httptest.Server. Defaults to defaultBaseURL
	// when empty.
	BaseURL string
	// CapitalURL is the capital increase page read by FetchCapitalEvents.
	// Overridable for tests. Defaults to defaultCapitalURL when empty.
	CapitalURL string
}

// NewFetcher returns a Fetcher pointed at the production bulletin URL.
func NewFetcher() *Fetcher {
	return &Fetcher{}
}

// FetchCloses returns FetchBulletin's rows for date without the rows whose
// close is empty or zero (suspended instruments), which are never stored
// as a price.
func (f *Fetcher) FetchCloses(ctx context.Context, date time.Time) ([]Close, error) {
	rows, _, err := f.FetchBulletin(ctx, date)
	if err != nil {
		return nil, err
	}
	var closes []Close
	for _, r := range rows {
		if !r.Close.IsZero() {
			closes = append(closes, r)
		}
	}
	return closes, nil
}

// FetchBulletin downloads and parses the daily equity bulletin for date. A
// 404 response (non-trading day, or not yet published) returns a nil slice
// and nil error. Any other non-200 status, or a zip/CSV parse failure, is
// an error. Rows outside the EQT, ETF and EMS instrument groups are
// skipped, as are rows whose series code does not end in the suffix its
// group maps to; the int result counts the latter. Rows with an empty or
// zero close (suspended instruments) are kept with a zero Close: cmd/symbols
// seeds them, since a suspended stock is still holdable.
func (f *Fetcher) FetchBulletin(ctx context.Context, date time.Time) ([]Close, int, error) {
	base := cmp.Or(f.BaseURL, defaultBaseURL)
	url := fmt.Sprintf("%s%04d/%02d/thb%04d%02d%02d1.zip",
		base, date.Year(), date.Month(), date.Year(), date.Month(), date.Day())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build bulletin request: %w", err)
	}
	body, err := httpbody.Fetch(f.HTTPClient, req, maxBulletinBytes)
	if se := (*httpbody.StatusError)(nil); errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("fetch bulletin: %w", err)
	}

	return parseBulletinZip(body)
}

// parseBulletinZip unzips the bulletin in memory and parses its one CSV
// member.
func parseBulletinZip(body []byte) ([]Close, int, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, 0, fmt.Errorf("open bulletin zip: %w", err)
	}
	if len(zr.File) == 0 {
		return nil, 0, fmt.Errorf("bulletin zip has no files")
	}

	rc, err := zr.File[0].Open()
	if err != nil {
		return nil, 0, fmt.Errorf("open bulletin csv: %w", err)
	}
	defer rc.Close()

	return parseBulletinCSV(rc)
}

// parseBulletinCSV parses the semicolon-separated bulletin CSV: two header
// rows (Turkish then English), then one data row per instrument.
func parseBulletinCSV(r io.Reader) ([]Close, int, error) {
	// Some bulletins (2025-05-02) start with a UTF-8 BOM, which would
	// otherwise stick to the first header name.
	br := bufio.NewReader(r)
	if bom, _ := br.Peek(3); string(bom) == "\xef\xbb\xbf" {
		br.Discard(3)
	}
	reader := csv.NewReader(br)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1 // header/data row widths differ

	header, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("read bulletin turkish header row: %w", err)
	}
	if _, err := reader.Read(); err != nil {
		return nil, 0, fmt.Errorf("read bulletin english header row: %w", err)
	}

	col := func(name string) (int, error) {
		for i, h := range header {
			// Collapse internal whitespace: the real header has "ISLEM  KODU".
			if strings.Join(strings.Fields(h), " ") == name {
				return i, nil
			}
		}
		return -1, fmt.Errorf("bulletin header missing column %q", name)
	}
	dateIdx, err := col(headerTradeDate)
	if err != nil {
		return nil, 0, err
	}
	symbolIdx, err := col(headerSymbol)
	if err != nil {
		return nil, 0, err
	}
	nameIdx, err := col(headerName)
	if err != nil {
		return nil, 0, err
	}
	groupIdx, err := col(headerGroup)
	if err != nil {
		return nil, 0, err
	}
	closeIdx, err := col(headerClose)
	if err != nil {
		return nil, 0, err
	}
	lastNeededIdx := max(dateIdx, symbolIdx, nameIdx, groupIdx, closeIdx)

	var closes []Close
	skipped := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read bulletin data row: %w", err)
		}
		if lastNeededIdx >= len(record) {
			continue // short/blank trailing row
		}
		suffix, ok := wantedGroups[strings.TrimSpace(record[groupIdx])]
		if !ok {
			continue
		}
		code := strings.TrimSpace(record[symbolIdx])
		if !strings.HasSuffix(code, suffix) {
			// A series code in a tracked group that does not carry the
			// group's suffix is a different series of the instrument, e.g.
			// the default-settlement (temerrut) "OYAKC.TE" line inside EQT.
			// It is not a tradable symbol of its own, so it is dropped.
			skipped++
			continue
		}
		symbol := strings.TrimSuffix(code, suffix)
		if symbol == "" {
			continue
		}
		var c decimal.Decimal // an empty close (suspended instrument) stays zero
		if closeStr := strings.TrimSpace(record[closeIdx]); closeStr != "" {
			if c, err = decimal.NewFromString(closeStr); err != nil {
				return nil, 0, fmt.Errorf("parse close %q: %w", closeStr, err)
			}
		}
		date, err := parseBulletinDate(strings.TrimSpace(record[dateIdx]))
		if err != nil {
			return nil, 0, err
		}
		closes = append(closes, Close{
			Symbol: symbol,
			Name:   strings.TrimSpace(record[nameIdx]),
			Date:   date,
			Close:  c,
		})
	}
	return closes, skipped, nil
}

// parseBulletinDate returns the bulletin's trade date cell as YYYY-MM-DD.
// Most bulletins print it that way; those of 21 May to 9 June 2020 print
// day.month.year with an unpadded day, e.g. "1.06.2020". Any
// other form is an error, so it is never stored as raw text.
func parseBulletinDate(s string) (string, error) {
	for _, layout := range []string{time.DateOnly, "2.1.2006"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d.Format(time.DateOnly), nil
		}
	}
	return "", fmt.Errorf("parse bulletin date %q", s)
}

// maxLoggedAdded caps the symbol list in Sync's log line: the first
// bulletin after a fresh seed can add a handful, and a backfill walking
// years of bulletins can add every company delisted since, which is not
// worth a log line hundreds of symbols wide.
const maxLoggedAdded = 20

// Sync fetches closes for date and upserts them into priceRepo with
// source = "bist_bulletin", overwriting any existing prices row for the
// same instrument and date. It never touches manual_prices: a user's
// manual price lives there and wins over the fetched close when the two
// are merged. A bulletin symbol with no BIST instrument row gets
// one inserted (name from the bulletin, currency TRY) so a company listed
// after bist_symbols.csv was generated is searchable and priceable the day
// it first trades; the added symbols are named in the log line. Returns the
// number of prices written.
func Sync(ctx context.Context, fetcher *Fetcher, instrumentRepo *instrument.Repo, priceRepo *price.Repo, date time.Time) (int, error) {
	closes, err := fetcher.FetchCloses(ctx, date)
	if err != nil {
		return 0, err
	}
	if len(closes) == 0 {
		return 0, nil
	}

	instruments, err := instrumentRepo.All(ctx, "BIST")
	if err != nil {
		return 0, fmt.Errorf("list instruments: %w", err)
	}
	idBySymbol := make(map[string]int64, len(instruments))
	for _, in := range instruments {
		idBySymbol[in.Symbol] = in.ID
	}

	written := 0
	var added []string
	for _, c := range closes {
		id, ok := idBySymbol[c.Symbol]
		if !ok {
			id, err = instrumentRepo.Insert(ctx, "BIST", c.Symbol, c.Name, "TRY")
			if err != nil {
				return written, err
			}
			idBySymbol[c.Symbol] = id
			added = append(added, c.Symbol)
		}
		if err := priceRepo.Upsert(ctx, id, c.Date, c.Close.String(), SourceBistBulletin); err != nil {
			return written, fmt.Errorf("upsert price for %s: %w", c.Symbol, err)
		}
		written++
	}
	logged := added
	if len(logged) > maxLoggedAdded {
		logged = logged[:maxLoggedAdded]
	}
	slog.Default().Info("bist bulletin sync complete",
		"date", date.Format(time.DateOnly), "written", written,
		"added_instruments", len(added), "added_symbols", logged)
	return written, nil
}
