// Command symbols refreshes one of the symbol lists embedded by
// backend/internal/instrument: it downloads a market's instrument list and
// writes internal/instrument/<market>_symbols.csv with a "symbol,name"
// header ("symbol,name,asset_class" for bist), sorted by symbol and deduplicated. Each market's source and
// parsing rules are documented at the top of its file (bist.go, crypto.go,
// tefas.go, us.go).
//
// Run from backend/:
//
//	go run ./cmd/symbols -market bist
//	go run ./cmd/symbols -market bist -date 2026-09-11
//	go run ./cmd/symbols -market crypto
//	go run ./cmd/symbols -market tefas
//	go run ./cmd/symbols -market us
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

const userAgent = "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)"

// maxBodyBytes caps a response read, far above any real one, so a broken or
// hostile upstream cannot exhaust memory; a larger body fails the run. Same
// cap as the internal fetchers.
const maxBodyBytes = 8 << 20

// symbolRow is one instrument row of a market's source list.
type symbolRow struct {
	Symbol string
	Name   string
}

func main() {
	market := flag.String("market", "", "bist, crypto, tefas or us (required)")
	dateFlag := flag.String("date", "", "bist only: bulletin date YYYY-MM-DD (default: most recent weekday)")
	flag.Parse()

	var err error
	switch *market {
	case "bist":
		var date time.Time
		if date, err = resolveDate(*dateFlag, time.Now()); err == nil {
			err = runBIST(date)
		}
	case "crypto":
		err = runCrypto()
	case "tefas":
		err = runTEFAS()
	case "us":
		err = runUS()
	default:
		fmt.Fprintln(os.Stderr, "symbols: -market must be one of bist, crypto, tefas, us")
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "symbols:", err)
		os.Exit(1)
	}
}

// writeCSV writes bySymbol to path, sorted by symbol. A non-nil classBySymbol
// adds an asset_class column and a non-nil sectorBySymbol a sector column after
// it (empty for a symbol the map lacks); nil keeps them out.
func writeCSV(path string, bySymbol, classBySymbol, sectorBySymbol map[string]string) error {
	symbols := make([]string, 0, len(bySymbol))
	for symbol := range bySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	header := []string{"symbol", "name"}
	if classBySymbol != nil {
		header = append(header, "asset_class")
	}
	if sectorBySymbol != nil {
		header = append(header, "sector")
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, symbol := range symbols {
		record := []string{symbol, bySymbol[symbol]}
		if classBySymbol != nil {
			record = append(record, classBySymbol[symbol])
		}
		if sectorBySymbol != nil {
			record = append(record, sectorBySymbol[symbol])
		}
		if err := w.Write(record); err != nil {
			return fmt.Errorf("write row %s: %w", symbol, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush %s: %w", path, err)
	}
	fmt.Printf("wrote %d instruments to %s\n", len(symbols), path)
	return nil
}
