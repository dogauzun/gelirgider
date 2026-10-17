// -market us downloads the Nasdaq Trader symbol directory (nasdaqlisted.txt
// and otherlisted.txt) and writes internal/instrument/us_symbols.csv.
// Source, format, and the decision to check the generated CSV in rather
// than fetch it at runtime are documented in
// docs/adr/0003-multi-currency-and-market-data-sources.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/httpbody"
)

const (
	nasdaqListedURL = "https://www.nasdaqtrader.com/dynamic/symdir/nasdaqlisted.txt"
	otherListedURL  = "https://www.nasdaqtrader.com/dynamic/symdir/otherlisted.txt"
	usPath          = "internal/instrument/us_symbols.csv"
)

func runUS() error {
	nasdaq, err := fetchSymbols(nasdaqListedURL, "Symbol")
	if err != nil {
		return fmt.Errorf("nasdaqlisted.txt: %w", err)
	}
	other, err := fetchSymbols(otherListedURL, "ACT Symbol")
	if err != nil {
		return fmt.Errorf("otherlisted.txt: %w", err)
	}

	byNameSymbol := make(map[string]string, len(nasdaq)+len(other))
	for _, s := range nasdaq {
		byNameSymbol[s.Symbol] = s.Name
	}
	for _, s := range other {
		byNameSymbol[s.Symbol] = s.Name
	}
	return writeCSV(usPath, byNameSymbol, nil)
}

// fetchSymbols downloads a pipe-delimited Nasdaq Trader symbol directory
// file and returns its non-test rows. symbolCol is the header name of the
// symbol column: the two files disagree ("Symbol" vs "ACT Symbol").
// Columns are matched by header name, not position, so an upstream layout
// change fails loudly instead of silently — the same rule internal/bist
// uses for bulletin columns.
//
// The files are plain pipe-delimited text with no quoting, so lines are
// split directly rather than parsed with encoding/csv: the last line is a
// "File Creation Time: ..." footer with a different field count than the
// header, which a csv.Reader would reject outright.
func fetchSymbols(url, symbolCol string) ([]symbolRow, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	body, err := httpbody.Fetch(nil, req, maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return nil, fmt.Errorf("empty response")
	}
	col := make(map[string]int)
	for i, name := range strings.Split(scanner.Text(), "|") {
		col[name] = i
	}
	symbolIdx, ok := col[symbolCol]
	if !ok {
		return nil, fmt.Errorf("missing %q column in header", symbolCol)
	}
	nameIdx, ok := col["Security Name"]
	if !ok {
		return nil, fmt.Errorf("missing %q column in header", "Security Name")
	}
	testIdx, ok := col["Test Issue"]
	if !ok {
		return nil, fmt.Errorf("missing %q column in header", "Test Issue")
	}
	lastIdx := symbolIdx
	for _, i := range []int{nameIdx, testIdx} {
		if i > lastIdx {
			lastIdx = i
		}
	}

	var out []symbolRow
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "File Creation Time") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) <= lastIdx {
			continue // malformed/short line; skip rather than fail the whole import
		}
		if fields[testIdx] == "Y" {
			continue // test issue
		}
		symbol := strings.TrimSpace(fields[symbolIdx])
		if symbol == "" {
			continue
		}
		out = append(out, symbolRow{Symbol: symbol, Name: cleanName(fields[nameIdx])})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return out, nil
}

// cleanName trims whitespace and strips a trailing "Common Stock" suffix
// (with or without a leading " - ", the two styles the two files use)
// since it adds no information once the market is known to be US equities.
// Any other suffix (ADS, class shares, preferred, ...) is left as is:
// stripping those isn't trivial, so the ticket calls for keeping them.
func cleanName(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{" - Common Stock", " Common Stock"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSpace(strings.TrimSuffix(name, suffix))
		}
	}
	return name
}
