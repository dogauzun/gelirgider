package bist

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

func readSektor(t *testing.T) string {
	t.Helper()
	page, err := os.ReadFile("testdata/sektor.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(page)
}

func TestParseSectorTable(t *testing.T) {
	got, err := parseSectorTable(readSektor(t))
	if err != nil {
		t.Fatalf("parseSectorTable: %v", err)
	}
	want := map[string]string{
		"A1CAP": "Aracı Kurumlar",
		"AAGYO": "GYO",
		"ACSEL": "Kimyasal Ürün",
		"MESRU": "Meşrubat / İçecek",
		"NOSYM": "Petrol",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d", len(got), got, len(want))
	}
	for sym, w := range want {
		if got[sym] != w {
			t.Errorf("%s: got %q, want %q", sym, got[sym], w)
		}
	}
}

// A redesign is noticed: a missing table, header, row or cell is an error.
func TestParseSectorTableFailsOnRedesign(t *testing.T) {
	page := readSektor(t)
	tests := map[string]string{
		"no table":        "<html><body>redesigned</body></html>",
		"no Sektör":       strings.Replace(page, "<th>Sektör</th>", "<th>Sector</th>", 1),
		"no Kod":          strings.Replace(page, "<th>Kod</th>", "<th>Code</th>", 1),
		"short row":       strings.Replace(page, `<td>Petrol</td><td class="text-right">1,00</td>`, "", 1),
		"no rows":         page[:strings.Index(page, "<tr><td>")] + "</tbody></table>",
		"row without key": strings.Replace(page, `>A1CAP</a>`, "></a>", 1),
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSectorTable(p); err == nil {
				t.Fatal("parseSectorTable: want an error")
			}
		})
	}
}

func TestSyncSectors(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	// A1CAP: old value, changed. AAGYO: already current. ACSEL: never fetched,
	// with an admin override. ETFX: no row on the page, keeps its sector.
	// NOSYM has no instrument.
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency, sector, sector_override) VALUES
		(1, 'A1CAP', 'BIST', 'A1', 'TRY', 'Eski', NULL),
		(2, 'AAGYO', 'BIST', 'Agaoglu', 'TRY', 'GYO', NULL),
		(3, 'ACSEL', 'BIST', 'Acisel', 'TRY', NULL, 'materials'),
		(4, 'ETFX', 'BIST', 'Etf', 'TRY', 'Elle', NULL)`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	repo := instrument.NewRepo(conn)

	n, err := SyncSectors(ctx, readSektor(t), repo)
	if err != nil {
		t.Fatalf("SyncSectors: %v", err)
	}
	if n != 2 {
		t.Errorf("SyncSectors wrote %d, want 2 (A1CAP changed, ACSEL fetched)", n)
	}

	type sectors struct{ sector, override string }
	want := map[int64]sectors{
		1: {"Aracı Kurumlar", ""},
		2: {"GYO", ""},
		3: {"Kimyasal Ürün", "materials"},
		4: {"Elle", ""},
	}
	for id, w := range want {
		in, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %d: %v", id, err)
		}
		if in.Sector.String != w.sector || in.SectorOverride.String != w.override {
			t.Errorf("instrument %d: sector %q override %q, want %q %q", id, in.Sector.String, in.SectorOverride.String, w.sector, w.override)
		}
	}
	all, err := repo.All(ctx, "BIST")
	if err != nil || len(all) != len(want) {
		t.Errorf("instruments after sync: %d (err %v), want %d: NOSYM must not be inserted", len(all), err, len(want))
	}

	if n, err := SyncSectors(ctx, readSektor(t), repo); err != nil || n != 0 {
		t.Errorf("second SyncSectors = %d, %v, want 0, nil", n, err)
	}
}
