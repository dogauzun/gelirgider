package bist_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// bulletinCSV is a small fixture in the real bulletin's shape: two header
// rows (Turkish with its double-spaced "ISLEM  KODU", which the parser
// matches on, then English), three equity rows, one ETF row (group ETF,
// .F suffix), one warrant row and one equity-warrant row (groups we do not
// track), one default-settlement (temerrut) equity row whose code ends in
// .TE rather than .E, and two equity rows with an empty and a zero close
// (suspended instruments).
const bulletinCSV = "TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n" +
	"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n" +
	"2026-09-11;THYAO.E;TURK HAVA YOLLARI;EQT;299.25\n" +
	"2026-09-11;ASELS.E;ASELSAN;EQT;393.75\n" +
	"2026-09-11;KUYAS.E;KUYAS GAYRIMENKUL;EQT;72.65\n" +
	"2026-09-11;OPT25.F;OPTIMA PORTFOY;ETF;52.74\n" +
	"2026-09-11;WARANT1.W;WARRANT ONE;WNT;10.00\n" +
	"2026-09-11;TKFEN122612C36.E;TEKFEN CALL;EPW;1.25\n" +
	"2026-09-11;OYAKC.TE;OYAK CIMENTO;EQT;92.00\n" +
	"2026-09-11;UMPAS.E;UMPAS HOLDING;EQT;\n" +
	"2026-09-11;ISKUR.E;ISKUR INSAAT;EQT;0\n"

// bulletinZip builds an in-memory zip containing one CSV member, matching
// how the real bulletin ships.
func bulletinZip(t *testing.T, csv string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("thb202609111.csv")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(csv)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestFetchClosesParsesEquityAndETFRowsOnly(t *testing.T) {
	zipBytes := bulletinZip(t, bulletinCSV)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer srv.Close()

	f := &bist.Fetcher{BaseURL: srv.URL + "/"}
	closes, err := f.FetchCloses(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchCloses: %v", err)
	}
	if len(closes) != 4 {
		t.Fatalf("got %d closes, want 4: %+v", len(closes), closes)
	}

	want := map[string]string{
		"THYAO": "299.25",
		"ASELS": "393.75",
		"KUYAS": "72.65",
		"OPT25": "52.74", // ETF group, .F suffix stripped like an equity's .E
		// OYAKC.TE is an EQT row whose code does not end in .E (the
		// default-settlement series), so it is skipped, not emitted as a
		// junk "OYAKC.TE" symbol.
	}
	for _, c := range closes {
		wantClose, ok := want[c.Symbol]
		if !ok {
			t.Fatalf("unexpected symbol in result: %s", c.Symbol)
		}
		if !c.Close.Equal(decimal.RequireFromString(wantClose)) {
			t.Fatalf("close for %s = %s, want %s", c.Symbol, c.Close, wantClose)
		}
		if c.Date != "2026-09-11" {
			t.Fatalf("date for %s = %s, want 2026-09-11", c.Symbol, c.Date)
		}
		wantGroup := "EQT"
		if c.Symbol == "OPT25" {
			wantGroup = "ETF"
		}
		if c.Group != wantGroup {
			t.Fatalf("group for %s = %q, want %q", c.Symbol, c.Group, wantGroup)
		}
		delete(want, c.Symbol)
	}
	if len(want) != 0 {
		t.Fatalf("missing symbols in result: %v", want)
	}
}

// TestFetchClosesKeepsGoldCertificate covers ALTIN.S1, the Darphane gold
// certificate, the only instrument in bulletin group EMS.
func TestFetchClosesKeepsGoldCertificate(t *testing.T) {
	zipBytes := bulletinZip(t, "TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n"+
		"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n"+
		"2026-10-07;ALTIN.S1;DARPHANE ALTIN SERTIFIKASI;EMS;70.49\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer srv.Close()

	f := &bist.Fetcher{BaseURL: srv.URL + "/"}
	closes, err := f.FetchCloses(context.Background(), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchCloses: %v", err)
	}
	if len(closes) != 1 || closes[0].Symbol != "ALTIN" || closes[0].Group != "EMS" || !closes[0].Close.Equal(decimal.RequireFromString("70.49")) {
		t.Fatalf("closes = %+v, want one EMS ALTIN close of 70.49", closes)
	}
}

// TestFetchBulletinKeepsSuspendedRowsAndStripsUTF8BOM covers the rows
// cmd/symbols seeds: every tracked row in bulletin order, the suspended
// UMPAS (empty close) and ISKUR (zero close) included with a zero Close,
// and the one suffix mismatch (OYAKC.TE) counted. The CSV starts with a
// UTF-8 BOM, as the 2025-05-02 bulletin does, which hid the first header
// column.
func TestFetchBulletinKeepsSuspendedRowsAndStripsUTF8BOM(t *testing.T) {
	zipBytes := bulletinZip(t, "\xef\xbb\xbf"+bulletinCSV)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer srv.Close()

	f := &bist.Fetcher{BaseURL: srv.URL + "/"}
	rows, skipped, err := f.FetchBulletin(context.Background(), time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchBulletin: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (OYAKC.TE)", skipped)
	}
	want := []struct{ symbol, name, close string }{
		{"THYAO", "TURK HAVA YOLLARI", "299.25"},
		{"ASELS", "ASELSAN", "393.75"},
		{"KUYAS", "KUYAS GAYRIMENKUL", "72.65"},
		{"OPT25", "OPTIMA PORTFOY", "52.74"},
		{"UMPAS", "UMPAS HOLDING", "0"},
		{"ISKUR", "ISKUR INSAAT", "0"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Symbol != w.symbol || r.Name != w.name || r.Date != "2026-09-11" || !r.Close.Equal(decimal.RequireFromString(w.close)) {
			t.Fatalf("row %d = %+v, want %s %q close %s on 2026-09-11", i, r, w.symbol, w.name, w.close)
		}
	}
}

func TestFetchCloses404IsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := &bist.Fetcher{BaseURL: srv.URL + "/"}
	closes, err := f.FetchCloses(context.Background(), time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchCloses: %v", err)
	}
	if len(closes) != 0 {
		t.Fatalf("got %d closes, want 0", len(closes))
	}
}

// TestSyncAddsUnknownSymbolsAndOverwritesExistingPrice covers the two pieces
// of non-trivial logic in Sync: a bulletin symbol with no instrument row
// gets one inserted from the bulletin's name column, and an
// existing prices row for the same instrument/date is overwritten by the
// bulletin.
func TestSyncAddsUnknownSymbolsAndOverwritesExistingPrice(t *testing.T) {
	zipBytes := bulletinZip(t, bulletinCSV)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer srv.Close()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	// THYAO and ASELS are known instruments; KUYAS and OPT25 are not, so
	// Sync must insert them from the bulletin.
	if _, err := conn.ExecContext(ctx, `INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY'),
		       (2, 'ASELS', 'BIST', 'Aselsan', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	instruments := instrument.NewRepo(conn)
	prices := price.NewRepo(conn)

	// A prices row already exists for THYAO on the bulletin's date; the
	// bulletin must overwrite it.
	if err := prices.Upsert(ctx, 1, "2026-09-11", "1.00", "test"); err != nil {
		t.Fatalf("seed prices row: %v", err)
	}

	f := &bist.Fetcher{BaseURL: srv.URL + "/"}
	n, err := bist.Sync(ctx, f, instruments, prices, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 4 {
		t.Fatalf("Sync wrote %d prices, want 4 (KUYAS and OPT25 are unknown and must be added)", n)
	}

	// The unknown symbols now have an instrument row carrying the
	// bulletin's name, so they are searchable the day they first trade.
	kuyas, err := instruments.BySymbol(ctx, "BIST", "KUYAS")
	if err != nil {
		t.Fatalf("BySymbol KUYAS: %v", err)
	}
	if kuyas.Name != "KUYAS GAYRIMENKUL" || kuyas.Currency != "TRY" {
		t.Fatalf("KUYAS instrument = %+v, want name=KUYAS GAYRIMENKUL currency=TRY", kuyas)
	}
	// The suffix filter still holds: the default-settlement
	// OYAKC.TE row must not become an instrument of its own.
	for _, junk := range []string{"OYAKC.TE", "OYAKC", "WARANT1", "TKFEN122612C36"} {
		if _, err := instruments.BySymbol(ctx, "BIST", junk); !errors.Is(err, instrument.ErrNotFound) {
			t.Fatalf("BySymbol %s = %v, want ErrNotFound (row is not a tracked series)", junk, err)
		}
	}

	latest, err := prices.Latest(ctx, []int64{1, 2}, "2026-09-11")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	thyao, ok := latest[1]
	if !ok {
		t.Fatalf("no price stored for THYAO")
	}
	if thyao.Close.String() != "299.25" || thyao.Source != "bist_bulletin" {
		t.Fatalf("THYAO price = %+v, want close=299.25 source=bist_bulletin (existing prices row should be overwritten)", thyao)
	}
	asels, ok := latest[2]
	if !ok {
		t.Fatalf("no price stored for ASELS")
	}
	if asels.Close.String() != "393.75" || asels.Source != "bist_bulletin" {
		t.Fatalf("ASELS price = %+v, want close=393.75 source=bist_bulletin", asels)
	}
}

// TestSyncStoresDayMonthYearBulletinDateAsISO covers the 21 May to 9 June
// 2020 bulletins, whose date cell reads "21.05.2020": the price is
// stored under 2020-05-21, and a bulletin with an unreadable date cell
// stores nothing and fails.
func TestSyncStoresDayMonthYearBulletinDateAsISO(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		cell    string
		wantErr bool
	}{
		{"21.05.2020", false},
		{"1.06.2020", false},
		{"2020/05/21", true},
	} {
		t.Run(tc.cell, func(t *testing.T) {
			zipBytes := bulletinZip(t, "TARIH;ISLEM  KODU;BULTEN ADI;ENSTRUMAN GRUBU;KAPANIS FIYATI\n"+
				"TRADE DATE;INSTRUMENT SERIES CODE;INSTRUMENT NAME;INSTRUMENT GROUP;CLOSING PRICE\n"+
				tc.cell+";THYAO.E;TURK HAVA YOLLARI;EQT;5.10\n")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(zipBytes)
			}))
			defer srv.Close()

			conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("db.Open: %v", err)
			}
			defer conn.Close()

			f := &bist.Fetcher{BaseURL: srv.URL + "/"}
			_, err = bist.Sync(ctx, f, instrument.NewRepo(conn), price.NewRepo(conn), time.Date(2020, 5, 21, 0, 0, 0, 0, time.UTC))
			if tc.wantErr != (err != nil) {
				t.Fatalf("Sync error = %v, want error %v", err, tc.wantErr)
			}

			var dates []string
			rows, err := conn.QueryContext(ctx, "SELECT date FROM prices")
			if err != nil {
				t.Fatalf("query prices: %v", err)
			}
			defer rows.Close()
			for rows.Next() {
				var d string
				if err := rows.Scan(&d); err != nil {
					t.Fatalf("scan: %v", err)
				}
				dates = append(dates, d)
			}
			want := map[string]string{"21.05.2020": "2020-05-21", "1.06.2020": "2020-06-01"}[tc.cell]
			if tc.wantErr && len(dates) != 0 || !tc.wantErr && (len(dates) != 1 || dates[0] != want) {
				t.Fatalf("stored dates = %v, want %q", dates, want)
			}
		})
	}
}

func TestTrimBoardSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"THYAO.E":  "THYAO",
		"OPT25.F":  "OPT25",
		"ALTIN.S1": "ALTIN",
		"VTI":      "VTI",
		"":         "",
		"A.E.E":    "A.E",
		"A.E.F":    "A.E",
		"A.F.E":    "A.F",
	} {
		if got := bist.TrimBoardSuffix(in); got != want {
			t.Errorf("TrimBoardSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
