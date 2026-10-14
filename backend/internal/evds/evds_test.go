package evds

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestTestKey checks that the key goes in the "key" header, and EVDS's
// status maps to the same reasons as us.Fetcher.TestKey. It sends
// the project User-Agent, like FetchXU100.
func TestTestKey(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusOK, ""},
		{http.StatusUnauthorized, "invalid key"},
		{http.StatusForbidden, "invalid key"},
		{http.StatusTooManyRequests, "rate limited"},
		{http.StatusInternalServerError, "unreachable"},
	} {
		var gotKey, gotPath, gotUA string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey, gotPath, gotUA = r.Header.Get("key"), r.URL.Path, r.UserAgent()
			w.WriteHeader(tc.status)
		}))
		got := (&Fetcher{BaseURL: srv.URL}).TestKey(context.Background(), "test-key")
		srv.Close()
		if got != tc.want {
			t.Errorf("status %d: TestKey = %q, want %q", tc.status, got, tc.want)
		}
		if gotKey != "test-key" {
			t.Errorf("status %d: key header = %q, want test-key", tc.status, gotKey)
		}
		if gotUA != userAgent {
			t.Errorf("status %d: User-Agent = %q, want %q", tc.status, gotUA, userAgent)
		}
		if want := "/series=TP.MK.F.BILESIK&startDate=25-09-2026&endDate=25-09-2026&type=json"; gotPath != want {
			t.Errorf("status %d: path = %q, want %q", tc.status, gotPath, want)
		}
	}
}

// testRepo returns a benchmark.Repo on a fresh database.
func testRepo(t *testing.T) *benchmark.Repo {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return benchmark.NewRepo(conn)
}

// TestSyncXU100 checks Sync against the real EVDS payloads of
// 2026-09-28: closes land as exact text without trailing zeros, a null
// holiday is skipped, the request carries the key header, the project
// User-Agent and DD-MM-YYYY dates of the 14-day window, an empty key makes
// no request, and a 1000-item (truncated) response writes nothing.
func TestSyncXU100(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		return b
	}
	var full strings.Builder
	full.WriteString(`{"totalCount":1000,"items":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			full.WriteString(",")
		}
		fmt.Fprintf(&full, `{"Tarih":"%s","TP_MK_F_BILESIK":"10000.00000000"}`, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("02-01-2006"))
	}
	full.WriteString("]}")

	for _, tc := range []struct {
		name, key string
		date      time.Time
		body      []byte
		wantPath  string
		want      map[string]string // date -> stored value; nil for an error
	}{
		{
			name: "sample", key: "test-key", date: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), body: read("xu100.json"),
			wantPath: "/series=TP.MK.F.BILESIK&startDate=11-09-2026&endDate=25-09-2026&type=json",
			want:     map[string]string{"2026-09-21": "13337.69", "2026-09-22": "13198.84", "2026-09-23": "13251.85", "2026-09-24": "12888.33", "2026-09-25": "12899.35"},
		},
		{
			name: "holiday", key: "test-key", date: time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC), body: read("xu100_holiday.json"),
			wantPath: "/series=TP.MK.F.BILESIK&startDate=10-04-2026&endDate=24-04-2026&type=json",
			want:     map[string]string{"2026-04-22": "14335.49", "2026-04-24": "14409.07"},
		},
		{name: "empty key", date: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), body: read("xu100.json")},
		{
			name: "truncated", key: "test-key", date: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), body: []byte(full.String()),
			wantPath: "/series=TP.MK.F.BILESIK&startDate=11-09-2026&endDate=25-09-2026&type=json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("key") != "test-key" || r.UserAgent() != userAgent || r.URL.Path != tc.wantPath {
					t.Errorf("request key %q, UA %q, path %q", r.Header.Get("key"), r.UserAgent(), r.URL.Path)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Write(tc.body)
			}))
			defer srv.Close()
			repo := testRepo(t)
			ctx := context.Background()

			n, err := SyncXU100(ctx, &Fetcher{BaseURL: srv.URL, APIKey: tc.key}, repo, tc.date)
			rows, rerr := repo.Range(ctx, benchmark.CodeXU100, "0000-01-01", "9999-12-31")
			if rerr != nil {
				t.Fatalf("Range: %v", rerr)
			}
			if tc.want == nil {
				if err == nil || n != 0 || len(rows) != 0 {
					t.Fatalf("SyncXU100 = %d, %v with %d rows, want an error and none", n, err, len(rows))
				}
				if tc.key == "" && (requests != 0 || !strings.Contains(err.Error(), "EVDS API key not configured")) {
					t.Fatalf("empty key: %d requests, error %q; want none and the not-configured error", requests, err)
				}
				return
			}
			if err != nil || n != len(tc.want) || len(rows) != len(tc.want) {
				t.Fatalf("SyncXU100 = %d, %v with %d rows, want %d", n, err, len(rows), len(tc.want))
			}
			for _, r := range rows {
				if r.Value.String() != tc.want[r.Date] || r.Source != "evds" {
					t.Errorf("%s = %q from %q, want %q from evds", r.Date, r.Value, r.Source, tc.want[r.Date])
				}
			}
		})
	}
}

// TestBackfillXU100 checks the backfill asks for one year per request, from
// syncWindowDays before from, the last one ending at to, so no
// request can reach the 1000-item cap.
func TestBackfillXU100(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Write([]byte(`{"totalCount":0,"items":[]}`))
	}))
	defer srv.Close()

	from := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if _, err := BackfillXU100(context.Background(), &Fetcher{BaseURL: srv.URL, APIKey: "test-key"}, testRepo(t), from, to); err != nil {
		t.Fatalf("BackfillXU100: %v", err)
	}
	want := []string{
		"/series=TP.MK.F.BILESIK&startDate=17-02-2024&endDate=16-02-2025&type=json",
		"/series=TP.MK.F.BILESIK&startDate=17-02-2025&endDate=16-02-2026&type=json",
		"/series=TP.MK.F.BILESIK&startDate=17-02-2026&endDate=25-09-2026&type=json",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}
