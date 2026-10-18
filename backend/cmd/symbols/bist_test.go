package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
)

// The bulletin parse itself (group filter, suffix match, kept suspended
// rows, BOM strip) is tested on bist.Fetcher.FetchBulletin in
// internal/bist.

// TestDedupeKeepsFirstSymbolAndWarns covers the one case where a bare
// symbol could collide: the same code listed in both the EQT and ETF
// groups. First seen wins, and the loser is reported rather than silently
// overwriting the winner's name.
func TestDedupeKeepsFirstSymbolAndWarns(t *testing.T) {
	var warn bytes.Buffer
	got, classes := dedupe([]bist.Close{
		{Symbol: "OPT25", Name: "EQUITY NAME", Group: "EQT"},
		{Symbol: "OPT25", Name: "FUND NAME", Group: "ETF"},
		{Symbol: "THYAO", Name: "TURK HAVA YOLLARI", Group: "EQT"},
	}, &warn)

	if len(got) != 2 {
		t.Fatalf("got %d symbols, want 2: %+v", len(got), got)
	}
	if got["OPT25"] != "EQUITY NAME" {
		t.Fatalf("OPT25 = %q, want the first-seen %q", got["OPT25"], "EQUITY NAME")
	}
	if classes["OPT25"] != "equity" {
		t.Fatalf("OPT25 class = %q, want the first-seen %q", classes["OPT25"], "equity")
	}
	if !strings.Contains(warn.String(), "duplicate symbol OPT25") {
		t.Fatalf("warn output = %q, want it to report the duplicate", warn.String())
	}
}

// TestWriteCSVWritesAssetClassFromBulletinGroup runs one EQT, one ETF and the
// ALTIN.S1 EMS row through dedupe and writeCSV, plus a row of an unmapped
// group, which keeps an empty class.
func TestWriteCSVWritesAssetClassFromBulletinGroup(t *testing.T) {
	names, classes := dedupe([]bist.Close{
		{Symbol: "THYAO", Name: "TURK HAVA YOLLARI", Group: "EQT"},
		{Symbol: "OPT25", Name: "OPTIMA PORTFOY", Group: "ETF"},
		{Symbol: "ALTIN", Name: "DARPHANE ALTIN SERTIFIKASI", Group: "EMS"},
		{Symbol: "XXX", Name: "OTHER", Group: "WNT"},
	}, io.Discard)

	path := filepath.Join(t.TempDir(), "bist_symbols.csv")
	if err := writeCSV(path, names, classes, nil); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	want := "symbol,name,asset_class\n" +
		"ALTIN,DARPHANE ALTIN SERTIFIKASI,commodity\n" +
		"OPT25,OPTIMA PORTFOY,fund\n" +
		"THYAO,TURK HAVA YOLLARI,equity\n" +
		"XXX,OTHER,\n"
	if string(got) != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}
}

// TestWriteCSVWritesSector writes the TEFAS layout: a sector column, empty for
// a symbol the map lacks. With a class map too, the sector comes after it.
func TestWriteCSVWritesSector(t *testing.T) {
	names := map[string]string{"BBB": "FUND B", "AAA": "FUND A"}
	path := filepath.Join(t.TempDir(), "tefas_symbols.csv")
	if err := writeCSV(path, names, nil, map[string]string{"AAA": "Karma Şemsiye Fonu"}); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	want := "symbol,name,sector\nAAA,FUND A,Karma Şemsiye Fonu\nBBB,FUND B,\n"
	if string(got) != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}

	if err := writeCSV(path, names, map[string]string{"AAA": "fund"}, map[string]string{"BBB": "x"}); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	got, _ = os.ReadFile(path)
	want = "symbol,name,asset_class,sector\nAAA,FUND A,fund,\nBBB,FUND B,,x\n"
	if string(got) != want {
		t.Fatalf("csv = %q, want %q", got, want)
	}
}
