package price_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// newAuthedRequest builds a PUT/GET request carrying a valid session
// cookie for userID, the same way a real client reaches these endpoints
// (they sit behind auth.RequireUser).
func newAuthedRequest(t *testing.T, sessions *auth.Repo, userID int64, method, path, body string) *http.Request {
	t.Helper()
	session, err := sessions.CreateSession(context.Background(), userID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	return r
}

// setUp wires a mux equivalent to the real /api mount: price routes behind
// auth.RequireUser, plus the repos and a seeded instrument tests need.
func setUp(t *testing.T) (mux http.Handler, sessions *auth.Repo, users *user.Repo, prices *price.Repo, manual *price.ManualRepo) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`INSERT INTO instruments (id, symbol, market, name, currency)
		VALUES (1, 'THYAO', 'BIST', 'Turk Hava Yollari', 'TRY')`); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}

	sessions = auth.NewRepo(conn)
	users = user.NewRepo(conn)
	prices = price.NewRepo(conn)
	manual = price.NewManualRepo(conn)

	protected := http.NewServeMux()
	price.RegisterRoutes(protected, prices, manual, instrument.NewRepo(conn))
	mux = sessions.RequireUser(protected)
	return mux, sessions, users, prices, manual
}

// TestPutRejectsOutOfRangeDate covers the PUT handler's date bounds: a date
// after today (Europe/Istanbul) or before MinDate (GLG-354) is rejected with
// 400; today and MinDate themselves are accepted.
func TestPutRejectsOutOfRangeDate(t *testing.T) {
	mux, sessions, users, _, _ := setUp(t)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}

	put := func(date string) int {
		r := newAuthedRequest(t, sessions, usr.ID, http.MethodPut, "/api/instruments/1/prices/"+date, `{"close":"5"}`)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code
	}

	if got := put("2030-01-01"); got != http.StatusBadRequest {
		t.Fatalf("PUT future date: status = %d, want 400", got)
	}
	if got := put(price.Today()); got != http.StatusOK {
		t.Fatalf("PUT today: status = %d, want 200", got)
	}
	if got := put("1989-12-31"); got != http.StatusBadRequest {
		t.Fatalf("PUT 1989-12-31: status = %d, want 400", got)
	}
	if got := put(price.MinDate); got != http.StatusOK {
		t.Fatalf("PUT %s: status = %d, want 200", price.MinDate, got)
	}
}

// TestPutIsPerUserAndDoesNotTouchSharedPrices covers the GLG-103 fix: two
// users' manual prices for the same instrument/date don't clobber each
// other, and neither write touches the shared prices table (which fetched
// sources own).
func TestPutIsPerUserAndDoesNotTouchSharedPrices(t *testing.T) {
	mux, sessions, users, prices, manual := setUp(t)
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create bob: %v", err)
	}

	date := price.Today()
	put := func(userID int64, close string) *httptest.ResponseRecorder {
		r := newAuthedRequest(t, sessions, userID, http.MethodPut, "/api/instruments/1/prices/"+date, `{"close":"`+close+`"}`)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}

	if rec := put(alice.ID, "10.00"); rec.Code != http.StatusOK {
		t.Fatalf("alice PUT: status = %d, body %s", rec.Code, rec.Body.String())
	}
	recBob := put(bob.ID, "20.00")
	if recBob.Code != http.StatusOK {
		t.Fatalf("bob PUT: status = %d, body %s", recBob.Code, recBob.Body.String())
	}
	var bobResp struct {
		Close string `json:"close"`
	}
	if err := json.NewDecoder(recBob.Body).Decode(&bobResp); err != nil {
		t.Fatalf("decode bob response: %v", err)
	}
	if bobResp.Close != "20" {
		t.Fatalf("bob response close = %s, want 20", bobResp.Close)
	}

	aliceLatest, err := manual.Latest(t.Context(), alice.ID, []int64{1}, date)
	if err != nil {
		t.Fatalf("manual.Latest alice: %v", err)
	}
	if got := aliceLatest[1].Close.String(); got != "10" {
		t.Fatalf("alice's manual price = %s, want 10 (bob's write must not overwrite it)", got)
	}
	bobLatest, err := manual.Latest(t.Context(), bob.ID, []int64{1}, date)
	if err != nil {
		t.Fatalf("manual.Latest bob: %v", err)
	}
	if got := bobLatest[1].Close.String(); got != "20" {
		t.Fatalf("bob's manual price = %s, want 20", got)
	}

	sharedLatest, err := prices.Latest(t.Context(), []int64{1}, date)
	if err != nil {
		t.Fatalf("prices.Latest: %v", err)
	}
	if _, ok := sharedLatest[1]; ok {
		t.Fatalf("shared prices table got a row from a manual PUT, want it untouched: %+v", sharedLatest[1])
	}
}

// TestDeleteManualPriceIsPerUser covers the GLG-134 fix: DELETE removes the
// caller's own manual row, leaves another user's row for the same
// instrument and date alone, and 404s when the caller has no such row.
func TestDeleteManualPriceIsPerUser(t *testing.T) {
	mux, sessions, users, _, manual := setUp(t)
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create bob: %v", err)
	}

	date := price.Today()
	for _, u := range []struct {
		id    int64
		close string
	}{{alice.ID, "10.00"}, {bob.ID, "20.00"}} {
		if err := manual.Upsert(t.Context(), u.id, 1, date, u.close); err != nil {
			t.Fatalf("manual.Upsert: %v", err)
		}
	}

	del := func(userID int64, date string) int {
		r := newAuthedRequest(t, sessions, userID, http.MethodDelete, "/api/instruments/1/prices/"+date, "")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec.Code
	}

	if got := del(alice.ID, date); got != http.StatusNoContent {
		t.Fatalf("alice DELETE: status = %d, want 204", got)
	}
	aliceLatest, err := manual.Latest(t.Context(), alice.ID, []int64{1}, date)
	if err != nil {
		t.Fatalf("manual.Latest alice: %v", err)
	}
	if _, ok := aliceLatest[1]; ok {
		t.Fatalf("alice's manual price survived her DELETE: %+v", aliceLatest[1])
	}
	bobLatest, err := manual.Latest(t.Context(), bob.ID, []int64{1}, date)
	if err != nil {
		t.Fatalf("manual.Latest bob: %v", err)
	}
	if got := bobLatest[1].Close.String(); got != "20" {
		t.Fatalf("bob's manual price = %q, want 20 (alice's DELETE must not touch it)", got)
	}

	// Already gone for alice, and never hers on the other date to begin with.
	if got := del(alice.ID, date); got != http.StatusNotFound {
		t.Fatalf("alice DELETE again: status = %d, want 404", got)
	}
	if got := del(alice.ID, "2026-01-02"); got != http.StatusNotFound {
		t.Fatalf("alice DELETE unknown date: status = %d, want 404", got)
	}
}

// historyRow is the subset of the history response these tests assert on.
type historyRow struct {
	Date   string `json:"date"`
	Close  string `json:"close"`
	Source string `json:"source"`
}

// TestHistoryIncludesOnlyCallersManualRows covers the other half of
// GLG-134: the caller's manual entries appear in the history tagged
// source "manual", another user's do not, and a manual row on the same date
// as a fetched one replaces it (the rule valuation applies).
func TestHistoryIncludesOnlyCallersManualRows(t *testing.T) {
	mux, sessions, users, prices, manual := setUp(t)
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create bob: %v", err)
	}

	if err := prices.Upsert(t.Context(), 1, "2026-09-11", "58.00", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := prices.Upsert(t.Context(), 1, "2026-09-10", "57.00", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	// The ticket's repro: a later-dated manual row shadowing the fetched
	// closes, plus one on a date a fetched row already covers.
	if err := manual.Upsert(t.Context(), alice.ID, 1, "2026-09-12", "60.00"); err != nil {
		t.Fatalf("manual.Upsert alice: %v", err)
	}
	if err := manual.Upsert(t.Context(), alice.ID, 1, "2026-09-10", "59.00"); err != nil {
		t.Fatalf("manual.Upsert alice: %v", err)
	}
	if err := manual.Upsert(t.Context(), bob.ID, 1, "2026-09-09", "99.00"); err != nil {
		t.Fatalf("manual.Upsert bob: %v", err)
	}

	r := newAuthedRequest(t, sessions, alice.ID, http.MethodGet, "/api/instruments/1/prices", "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET history: status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []historyRow
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode history: %v", err)
	}

	want := []historyRow{
		{"2026-09-12", "60.00", "manual"},
		{"2026-09-11", "58.00", "bist"},
		{"2026-09-10", "59.00", "manual"},
	}
	if len(got) != len(want) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestHistoryLimitCountsMergedRows guards the merge's limit handling: each
// table is queried for up to limit rows, so the merged result must still be
// truncated to limit -- and to the newest rows across both.
func TestHistoryLimitCountsMergedRows(t *testing.T) {
	mux, sessions, users, prices, manual := setUp(t)
	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create alice: %v", err)
	}

	if err := prices.Upsert(t.Context(), 1, "2026-09-09", "57.00", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := manual.Upsert(t.Context(), alice.ID, 1, "2026-09-10", "59.00"); err != nil {
		t.Fatalf("manual.Upsert: %v", err)
	}

	r := newAuthedRequest(t, sessions, alice.ID, http.MethodGet, "/api/instruments/1/prices?limit=1", "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET history: status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got []historyRow
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	want := []historyRow{{"2026-09-10", "59.00", "manual"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("history with limit=1 = %+v, want %+v", got, want)
	}
}

// TestPutNamesDigitLimit covers GLG-456: a positive close past the digit
// limit is reported with the limit, not as "must be a positive decimal".
func TestPutNamesDigitLimit(t *testing.T) {
	mux, sessions, users, _, _ := setUp(t)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	r := newAuthedRequest(t, sessions, usr.ID, http.MethodPut, "/api/instruments/1/prices/"+price.MinDate, `{"close":"1.123456789"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := "close: must have at most 15 integer and 8 fraction digits"
	if rec.Code != http.StatusBadRequest || body.Error != want {
		t.Fatalf("PUT close 1.123456789: %d %q, want 400 %q", rec.Code, body.Error, want)
	}
}

// TestPutNormalizesClose: a close with leading and trailing zeros is stored
// and returned in decimal.String() form, like transaction amounts (GLG-684).
func TestPutNormalizesClose(t *testing.T) {
	mux, sessions, users, _, _ := setUp(t)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	r := newAuthedRequest(t, sessions, usr.ID, http.MethodPut, "/api/instruments/1/prices/"+price.MinDate, `{"close":"0010.50"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"close":"10.5"`) {
		t.Fatalf("PUT close 0010.50: %d %s, want 200 with close 10.5", rec.Code, rec.Body)
	}

	r = newAuthedRequest(t, sessions, usr.ID, http.MethodGet, "/api/instruments/1/prices", "")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	var rows []struct {
		Close string `json:"close"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Close != "10.5" {
		t.Fatalf("GET prices = %+v, want one row with close 10.5", rows)
	}
}
