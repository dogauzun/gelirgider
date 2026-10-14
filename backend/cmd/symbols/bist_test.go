package main

import (
	"bytes"
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
	got := dedupe([]bist.Close{
		{Symbol: "OPT25", Name: "EQUITY NAME"},
		{Symbol: "OPT25", Name: "FUND NAME"},
		{Symbol: "THYAO", Name: "TURK HAVA YOLLARI"},
	}, &warn)

	if len(got) != 2 {
		t.Fatalf("got %d symbols, want 2: %+v", len(got), got)
	}
	if got["OPT25"] != "EQUITY NAME" {
		t.Fatalf("OPT25 = %q, want the first-seen %q", got["OPT25"], "EQUITY NAME")
	}
	if !strings.Contains(warn.String(), "duplicate symbol OPT25") {
		t.Fatalf("warn output = %q, want it to report the duplicate", warn.String())
	}
}
