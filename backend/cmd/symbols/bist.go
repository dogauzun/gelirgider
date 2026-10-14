// -market bist downloads one Borsa Istanbul daily equity bulletin zip and
// writes internal/instrument/bist_symbols.csv: BIST equity, ETF and gold
// certificate instruments. Bulletin URL pattern and CSV layout are documented in
// docs/adr/0001-bist-price-source.md; the fetch and parse, group filter and
// suffix map are internal/bist's FetchBulletin, the same code that reads the
// bulletin for daily closing prices.
//
// The bulletin lists ~630 equities (ENSTRUMAN GRUBU == EQT, code suffix
// .E), ~35 exchange-traded funds (ETF, suffix .F) and one gold
// certificate (EMS, ALTIN.S1) a day; all are seeded as market BIST, currency TRY. Rows with an empty or zero close
// are suspended instruments, not delistings, and are kept: a suspended
// stock is still holdable.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
)

const bistPath = "internal/instrument/bist_symbols.csv"

// resolveDate parses -date if given, otherwise returns the most recent
// weekday on or before now (the bulletin is only published for trading
// days).
func resolveDate(flagValue string, now time.Time) (time.Time, error) {
	if flagValue != "" {
		return time.Parse(time.DateOnly, flagValue)
	}
	d := now
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d, nil
}

func runBIST(date time.Time) error {
	rows, skipped, err := bist.NewFetcher().FetchBulletin(context.Background(), date)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no bulletin rows for %s (not published yet, or not a trading day)", date.Format(time.DateOnly))
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "symbols: skipped %d row(s) whose series code did not end in its group's suffix\n", skipped)
	}

	return writeCSV(bistPath, dedupe(rows, os.Stderr))
}

// dedupe collapses rows to one name per bare symbol. A bare symbol could
// in principle appear in two groups (X.E and X.F); first seen wins and the
// collision is reported to warn. Not expected in practice, so there is no
// tie-break rule beyond bulletin order.
func dedupe(rows []bist.Close, warn io.Writer) map[string]string {
	byNameSymbol := make(map[string]string, len(rows))
	for _, row := range rows {
		if kept, dup := byNameSymbol[row.Symbol]; dup {
			fmt.Fprintf(warn, "symbols: duplicate symbol %s, keeping %q, ignoring %q\n",
				row.Symbol, kept, row.Name)
			continue
		}
		byNameSymbol[row.Symbol] = row.Name
	}
	return byNameSymbol
}
