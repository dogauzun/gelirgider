package tuik_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/tuik"
)

// TestSyncTUFE runs SyncTUFE against a trimmed copy of the real file of
// 2026-09-28 (BOM kept): the Index rows land with their exact text, the
// rate-of-change rows are skipped, and the ratio across the 2025=100
// rebase is TÜİK's published January 2026 change. A file whose Index rows
// carry two base years fails and writes nothing.
func TestSyncTUFE(t *testing.T) {
	sample, err := os.ReadFile(filepath.Join("testdata", "tufe.csv"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	twoBases := bytes.Replace(sample, []byte(`"Index";"2025";"CPI";"Consumer Price Index";"2005-01"`),
		[]byte(`"Index";"2003";"CPI";"Consumer Price Index";"2005-01"`), 1)
	if bytes.Equal(twoBases, sample) {
		t.Fatal("two-base fixture unchanged")
	}

	cases := []struct {
		name    string
		body    []byte
		want    map[string]string // month -> level
		wantErr bool
	}{
		{name: "sample", body: sample, want: map[string]string{
			"2026-08-01": "134.75",
			"2026-01-01": "115.73",
			"2025-12-01": "110.386963",
			"2005-01-01": "3.596523",
		}},
		{name: "two base years", body: twoBases, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/en/dataflows/DF_TUFE_SDMX_TT10+V1.0/file/csv" || r.UserAgent() != "gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)" {
					http.Error(w, "Access denied", http.StatusForbidden)
					return
				}
				w.Write(tc.body)
			}))
			defer srv.Close()

			conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("db.Open: %v", err)
			}
			defer conn.Close()
			repo := benchmark.NewRepo(conn)
			ctx := context.Background()

			n, err := tuik.SyncTUFE(ctx, &tuik.Fetcher{BaseURL: srv.URL}, repo)
			rows, rerr := repo.Range(ctx, benchmark.CodeTUFE, "0000-01-01", "9999-12-31")
			if rerr != nil {
				t.Fatalf("Range: %v", rerr)
			}
			if tc.wantErr {
				if err == nil || n != 0 || len(rows) != 0 {
					t.Fatalf("SyncTUFE = %d, %v with %d rows stored, want an error and none", n, err, len(rows))
				}
				return
			}
			if err != nil {
				t.Fatalf("SyncTUFE: %v", err)
			}
			if n != len(tc.want) || len(rows) != len(tc.want) {
				t.Fatalf("SyncTUFE wrote %d, stored %d rows, want %d: %+v", n, len(rows), len(tc.want), rows)
			}
			got := map[string]string{}
			for _, r := range rows {
				if r.Source != "tuik" {
					t.Fatalf("source %q, want tuik", r.Source)
				}
				got[r.Date] = r.Value.String()
			}
			for month, level := range tc.want {
				if got[month] != level {
					t.Fatalf("%s = %q, want %q", month, got[month], level)
				}
			}
			jan := decimal.RequireFromString(got["2026-01-01"])
			dec := decimal.RequireFromString(got["2025-12-01"])
			if change := jan.Div(dec).Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100)).Round(2).String(); change != "4.84" {
				t.Fatalf("January 2026 change %s%%, want 4.84%%", change)
			}
		})
	}
}
