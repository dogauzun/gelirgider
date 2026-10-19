package bist

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

const (
	sectorTableStart = `id="summaryBasicData"`
	sectorBodyStart  = `id="temelTBody_Ozet"`
)

var (
	headerCell = regexp.MustCompile(`(?s)<th\b[^>]*>(.*?)</th>`)
	bodyRow    = regexp.MustCompile(`(?s)<tr\b[^>]*>(.*?)</tr>`)
	bodyCell   = regexp.MustCompile(`(?s)<td\b[^>]*>(.*?)</td>`)
)

// cellText strips the tags of a cell, decodes its HTML entities and trims it.
func cellText(s string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(s, "")))
}

// parseSectorTable reads the summary table of the page into symbol -> raw
// sector. The two cells have no class, so the columns are found by the header
// texts "Kod" and "Sektör". A missing table, header, row or cell is an error,
// so a page redesign is noticed instead of silently storing nothing.
func parseSectorTable(page string) (map[string]string, error) {
	start := strings.Index(page, sectorTableStart)
	if start < 0 {
		return nil, fmt.Errorf("sector table %q not found", sectorTableStart)
	}
	table := page[start:]
	bodyAt := strings.Index(table, sectorBodyStart)
	if bodyAt < 0 {
		return nil, fmt.Errorf("sector table body %q not found", sectorBodyStart)
	}
	body := table[bodyAt:]
	if end := strings.Index(body, "</tbody>"); end >= 0 {
		body = body[:end]
	}

	codeCol, sectorCol := -1, -1
	for i, m := range headerCell.FindAllStringSubmatch(table[:bodyAt], -1) {
		switch cellText(m[1]) {
		case "Kod":
			codeCol = i
		case "Sektör":
			sectorCol = i
		}
	}
	if codeCol < 0 || sectorCol < 0 {
		return nil, fmt.Errorf(`sector table header "Kod" or "Sektör" not found`)
	}

	rows := bodyRow.FindAllStringSubmatch(body, -1)
	if len(rows) == 0 {
		return nil, fmt.Errorf("sector table has no rows")
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		cells := bodyCell.FindAllStringSubmatch(r[1], -1)
		if len(cells) <= max(codeCol, sectorCol) {
			return nil, fmt.Errorf("sector row missing a cell (got %d): %.200s", len(cells), r[1])
		}
		symbol := cellText(cells[codeCol][1])
		if symbol == "" {
			return nil, fmt.Errorf("sector row without a symbol: %.200s", r[1])
		}
		out[symbol] = cellText(cells[sectorCol][1])
	}
	return out, nil
}

// SyncSectors reads the summary table of page (FetchPage's body) and stores
// each BIST instrument's raw sector when it differs from the stored one. An
// instrument with no row on the page (an ETF, ALTIN) is left untouched and a
// page symbol with no instrument is skipped, never inserted. The admin's
// sector_override is never written (ADR 0026). Returns the number of sectors
// written.
func SyncSectors(ctx context.Context, page string, instrumentRepo *instrument.Repo) (int, error) {
	sectors, err := parseSectorTable(page)
	if err != nil {
		return 0, err
	}
	instruments, err := instrumentRepo.All(ctx, instrument.MarketBIST)
	if err != nil {
		return 0, fmt.Errorf("list instruments: %w", err)
	}
	written, matched := 0, 0
	for _, in := range instruments {
		raw, ok := sectors[in.Symbol]
		if !ok {
			continue
		}
		matched++
		if in.Sector.Valid && in.Sector.String == raw {
			continue
		}
		if err := instrumentRepo.SetSector(ctx, in.ID, raw); err != nil {
			return written, err
		}
		written++
	}
	slog.Default().Info("bist sector sync complete",
		"rows", len(sectors), "matched", matched, "written", written, "unknown_symbols", len(sectors)-matched)
	return written, nil
}
