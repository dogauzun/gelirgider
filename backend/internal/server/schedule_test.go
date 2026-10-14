package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestAdminScheduleSettings checks that GET lists the scheduler's
// sources in order with defaults applied and can_backfill set only for a
// source with a Backfill; PUT then GET round-trips a source
// and the global schedule; a bad at or backfill_from is 400, an unknown
// source 404 and a non-admin 403.
func TestAdminScheduleSettings(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	noop := func(context.Context, string) (int, error) { return 0, nil }
	sources := []scheduler.Source{{Name: "bist", Backfill: noop}, {Name: "tefas", DefaultAt: "14:00"}}
	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil, sources...)

	users := user.NewRepo(conn)
	login := func(email string) string {
		t.Helper()
		u, err := users.Create(context.Background(), email, "hash")
		if err != nil {
			t.Fatalf("users.Create: %v", err)
		}
		s, err := auth.NewRepo(conn).CreateSession(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		return s.ID
	}
	admin, bob := login("admin@example.com"), login("bob@example.com")
	if _, err := conn.Exec("UPDATE users SET is_admin = 1 WHERE email = 'admin@example.com'"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	expect := func(session, method, path, body string, wantCode int, wantBody string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "session", Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != wantCode || got != wantBody {
			t.Errorf("%s %s %s = %d %s, want %d %s", method, path, body, rec.Code, got, wantCode, wantBody)
		}
	}

	expect(admin, "GET", "/api/admin/sources", "", 200,
		`[{"name":"bist","enabled":true,"at":"19:00","can_backfill":true,"last_run":null,"last_success_at":null},{"name":"tefas","enabled":false,"at":"14:00","can_backfill":false,"last_run":null,"last_success_at":null}]`)
	expect(admin, "PUT", "/api/admin/sources/tefas", `{"enabled":true,"at":"15:30"}`, 204, "")
	expect(admin, "GET", "/api/admin/sources", "", 200,
		`[{"name":"bist","enabled":true,"at":"19:00","can_backfill":true,"last_run":null,"last_success_at":null},{"name":"tefas","enabled":true,"at":"15:30","can_backfill":false,"last_run":null,"last_success_at":null}]`)
	for _, at := range []string{"9:30", "24:00", "12:60", ""} {
		expect(admin, "PUT", "/api/admin/sources/bist", `{"enabled":true,"at":"`+at+`"}`, 400, `{"error":"at: must be HH:MM"}`)
	}
	expect(admin, "PUT", "/api/admin/sources/nope", `{"enabled":true,"at":"10:00"}`, 404, `{"error":"unknown source"}`)

	expect(admin, "GET", "/api/admin/schedule", "", 200, `{"catchup_enabled":true,"backfill_enabled":true,"backfill_from":""}`)
	put := `{"catchup_enabled":false,"backfill_enabled":true,"backfill_from":"2024-01-02"}`
	expect(admin, "PUT", "/api/admin/schedule", put, 204, "")
	expect(admin, "GET", "/api/admin/schedule", "", 200, put)
	expect(admin, "PUT", "/api/admin/schedule", `{"catchup_enabled":true,"backfill_enabled":true,"backfill_from":"2999-01-01"}`, 400,
		`{"error":"backfill_from: must not be in the future"}`)
	expect(admin, "PUT", "/api/admin/schedule", `{"catchup_enabled":true,"backfill_enabled":true,"backfill_from":"2024-02-30"}`, 400,
		`{"error":"backfill_from: must be YYYY-MM-DD"}`)
	expect(admin, "PUT", "/api/admin/schedule", `{"catchup_enabled":true,"backfill_enabled":true,"backfill_from":"0001-01-01"}`, 400,
		`{"error":"backfill_from: must be on or after 1990-01-01"}`)

	expect(bob, "GET", "/api/admin/sources", "", 403, `{"error":"admin only"}`)
	expect(bob, "PUT", "/api/admin/schedule", put, 403, `{"error":"admin only"}`)
}

// TestAdminSourcesRunStatus checks that a run that errors, then one that
// succeeds, leaves last_run without an error and last_success_at set; a
// source that never ran has both null. ErrNothingToFetch is a success.
func TestAdminSourcesRunStatus(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	sources := []scheduler.Source{{Name: "bist"}, {Name: "tcmb"}, {Name: "tefas"}}
	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil, sources...)
	u, err := user.NewRepo(conn).Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if _, err := conn.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", u.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	s, err := auth.NewRepo(conn).CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	get := func() map[string]sourceSettings {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/sources", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: s.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var items []sourceSettings
		if err := json.NewDecoder(rec.Body).Decode(&items); rec.Code != 200 || err != nil {
			t.Fatalf("GET /api/admin/sources = %d, decode error %v", rec.Code, err)
		}
		out := map[string]sourceSettings{}
		for _, it := range items {
			out[it.Name] = it
		}
		return out
	}

	ctx := context.Background()
	runs := scheduler.NewRunLog(conn)
	record := func(source, kind string, rows int, err error) {
		t.Helper()
		if gotRows, gotErr := runs.Record(ctx, source, kind, func() (int, error) { return rows, err }); gotRows != rows || gotErr != err {
			t.Fatalf("Record = %d, %v; want fn's result %d, %v", gotRows, gotErr, rows, err)
		}
	}

	record("bist", scheduler.KindScheduled, 0, errors.New("bulletin down"))
	bist := get()["bist"]
	if bist.LastRun == nil || bist.LastRun.Error == nil || *bist.LastRun.Error != "bulletin down" || bist.LastSuccessAt != nil {
		t.Fatalf("after a failed run: bist = %+v, want last_run.error set and last_success_at null", bist)
	}

	record("bist", scheduler.KindCatchUp, 612, nil)
	record("tcmb", scheduler.KindScheduled, 0, scheduler.ErrNothingToFetch)
	got := get()
	bist = got["bist"]
	if r := bist.LastRun; r == nil || r.Error != nil || r.Kind != scheduler.KindCatchUp || r.Rows != 612 || r.StartedAt == "" || r.FinishedAt == nil {
		t.Fatalf("bist last_run = %+v, want the successful catchup run with 612 rows", r)
	}
	if bist.LastSuccessAt == nil || *bist.LastSuccessAt != *bist.LastRun.FinishedAt {
		t.Fatalf("bist last_success_at = %v, want the last run's finished_at", bist.LastSuccessAt)
	}
	if tcmb := got["tcmb"]; tcmb.LastRun == nil || tcmb.LastRun.Error != nil || tcmb.LastSuccessAt == nil {
		t.Fatalf("tcmb = %+v, want ErrNothingToFetch stored as a success", tcmb)
	}
	if tefas := got["tefas"]; tefas.LastRun != nil || tefas.LastSuccessAt != nil {
		t.Fatalf("tefas = %+v, want nulls for a source that never ran", tefas)
	}
}

// TestAdminRunNowAndBackfill checks that run answers 202 and runs the
// source once for today in Europe/Istanbul, logged as manual; a second run
// while the first is going is 409. Backfill queues every enabled source
// with a history, or the one named even if disabled.
func TestAdminRunNowAndBackfill(t *testing.T) {
	conn := openBackfillTestDB(t)
	started, release := make(chan time.Time, 2), make(chan struct{})
	var calls atomic.Int32
	backfill := func(ctx context.Context, from string) (int, error) { return 1, nil }
	sources := []scheduler.Source{
		{Name: "tcmb", Sync: func(ctx context.Context, date time.Time) (int, error) {
			calls.Add(1)
			started <- date
			<-release
			return 3, nil
		}},
		{Name: "bist-splits"},
		{Name: "us", Backfill: backfill},
		{Name: "crypto", Backfill: backfill},
	}
	cfg := settings.NewRepo(conn, nil)
	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, cfg, nil, sources...)
	u, err := user.NewRepo(conn).Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if _, err := conn.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", u.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	s, err := auth.NewRepo(conn).CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	do := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "session", Value: s.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	// finished waits for source's last run to end and returns it. The
	// deadline only stops a hung run; a loaded machine can take seconds.
	finished := func(source string) *scheduler.SourceRun {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
			_, body := do("GET", "/api/admin/sources", "")
			var items []sourceSettings
			if err := json.Unmarshal([]byte(body), &items); err != nil {
				t.Fatalf("decode sources: %v", err)
			}
			for _, it := range items {
				if it.Name == source && it.LastRun != nil && it.LastRun.FinishedAt != nil {
					return it.LastRun
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s: no finished run", source)
		return nil
	}

	if code, body := do("POST", "/api/admin/sources/tcmb/run", ""); code != 202 {
		t.Fatalf("run = %d %s, want 202", code, body)
	}
	if date := <-started; date.Location().String() != "Europe/Istanbul" {
		t.Errorf("Sync date location = %s, want Europe/Istanbul", date.Location())
	}
	if code, body := do("POST", "/api/admin/sources/tcmb/run", ""); code != 409 || body != `{"error":"already running"}` {
		t.Errorf("second run = %d %s, want 409 already running", code, body)
	}
	close(release)
	if r := finished("tcmb"); r.Kind != scheduler.KindManual || r.Rows != 3 || r.Error != nil {
		t.Errorf("tcmb last_run = %+v, want a successful manual run with 3 rows", r)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("Sync ran %d times, want 1", n)
	}
	if code, body := do("POST", "/api/admin/sources/nope/run", ""); code != 404 {
		t.Errorf("unknown source run = %d %s, want 404", code, body)
	}

	if err := cfg.Set(context.Background(), scheduler.EnabledKey("crypto"), "false"); err != nil {
		t.Fatalf("disable crypto: %v", err)
	}
	for _, c := range []struct{ body, want string }{
		{"", `{"queued":["us"]}`},
		{`{"source":"crypto"}`, `{"queued":["crypto"]}`},
	} {
		if code, got := do("POST", "/api/admin/backfill", c.body); code != 202 || got != c.want {
			t.Errorf("backfill %s = %d %s, want 202 %s", c.body, code, got, c.want)
		}
	}
	backfillWorkers.Wait()
	for _, name := range []string{"us", "crypto"} {
		if r := finished(name); r.Kind != scheduler.KindBackfill {
			t.Errorf("%s last_run kind = %s, want backfill", name, r.Kind)
		}
	}
	if code, body := do("POST", "/api/admin/backfill", `{"source":"bist-splits"}`); code != 400 {
		t.Errorf("backfill of a source with no history = %d %s, want 400", code, body)
	}
}

// lineWriter sends each write, one slog line, to its channel.
type lineWriter chan string

func (w lineWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

// TestAdminRunNowLogsFailure checks that a failed run now writes a log
// line. It once wrote none, so the operator saw the error only in the admin
// page's last run.
func TestAdminRunNowLogsFailure(t *testing.T) {
	lines := make(lineWriter, 100)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(lines, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	conn := openBackfillTestDB(t)
	sources := []scheduler.Source{{Name: "us", Sync: func(ctx context.Context, date time.Time) (int, error) {
		return 0, errors.New("dial tcp: blocked")
	}}}
	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil, sources...)
	u, err := user.NewRepo(conn).Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if _, err := conn.Exec("UPDATE users SET is_admin = 1 WHERE id = ?", u.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	s, err := auth.NewRepo(conn).CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/sources/us/run", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: s.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run = %d %s, want 202", rec.Code, rec.Body.String())
	}

	timeout := time.After(30 * time.Second)
	for {
		select {
		case line := <-lines:
			var rec struct{ Level, Msg, Source, Kind, Error string }
			if json.Unmarshal([]byte(line), &rec) == nil && rec.Level == "ERROR" && rec.Source == "us" {
				if rec.Msg != "fetch failed" || rec.Kind != scheduler.KindManual || rec.Error != "dial tcp: blocked" {
					t.Fatalf("log = %s, want fetch failed for the manual run with the error", line)
				}
				return
			}
		case <-timeout:
			t.Fatal("no ERROR log line for the failed run now")
		}
	}
}
