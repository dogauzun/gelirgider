package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/shopspring/decimal"
)

// TestWatchlist walks the routes: add, duplicate, list, refused instruments,
// another user's view and delete.
func TestWatchlist(t *testing.T) {
	e := newTransactionTestEnv(t)
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-ZZ1', 'DEPOSIT', 'Deposit ZZ1', 'TRY')`)
	if err != nil {
		t.Fatalf("insert deposit instrument: %v", err)
	}
	depositID, _ := res.LastInsertId()
	a, b := e.login("a@example.com"), e.login("b@example.com")

	rec := e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": e.instrumentID, "note": " wait "})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", rec.Code, rec.Body.String())
	}
	created := decodeJSON[watchItemResponse](t, rec.Body.Bytes())
	if created.Symbol != "TEST" || created.Market != "BIST" || created.Currency != "TRY" || created.Note != "wait" ||
		created.TargetPrice != nil || created.TargetSide != nil {
		t.Errorf("created = %+v, want TEST, BIST, TRY, note wait, null target fields", created)
	}

	rec = e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": e.instrumentID})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already on the watchlist") {
		t.Errorf("second POST = %d %s, want 409 already on the watchlist", rec.Code, rec.Body.String())
	}
	for name, id := range map[string]int64{"DEPOSIT instrument": depositID, "unknown instrument": 9999} {
		rec = e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": id})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "instrument_id: unknown instrument") {
			t.Errorf("POST %s = %d %s, want 400 instrument_id: unknown instrument", name, rec.Code, rec.Body.String())
		}
	}

	list := decodeJSON[[]watchItemResponse](t, e.do(a, http.MethodGet, "/api/watchlist", nil).Body.Bytes())
	if len(list) != 1 || list[0] != created {
		t.Errorf("A's list = %+v, want just %+v", list, created)
	}
	if list := decodeJSON[[]watchItemResponse](t, e.do(b, http.MethodGet, "/api/watchlist", nil).Body.Bytes()); len(list) != 0 {
		t.Errorf("B's list = %+v, want empty", list)
	}

	path := "/api/watchlist/" + strconv.FormatInt(created.ID, 10)
	if rec = e.do(b, http.MethodDelete, path, nil); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "watchlist item not found") {
		t.Errorf("B's DELETE = %d %s, want 404 watchlist item not found", rec.Code, rec.Body.String())
	}
	if rec = e.do(a, http.MethodDelete, "/api/watchlist/x", nil); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE of a malformed id = %d, want 404", rec.Code)
	}
	if rec = e.do(a, http.MethodDelete, path, nil); rec.Code != http.StatusNoContent {
		t.Errorf("A's DELETE = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if list := decodeJSON[[]watchItemResponse](t, e.do(a, http.MethodGet, "/api/watchlist", nil).Body.Bytes()); len(list) != 0 {
		t.Errorf("A's list after DELETE = %+v, want empty", list)
	}
}

// watchRow is the fields of a watchlist row the price tests look at, nil
// pointers dereferenced to "" so a missing value reads as null.
type watchRow struct{ last, source, date, prev, prevDate, change string }

func val(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

func watchRows(t *testing.T, e *transactionTestEnv, session string) []watchRow {
	t.Helper()
	var out []watchRow
	for _, r := range decodeJSON[[]watchItemResponse](t, e.do(session, http.MethodGet, "/api/watchlist", nil).Body.Bytes()) {
		out = append(out, watchRow{val(r.LastPrice), val(r.PriceSource), val(r.PriceDate), val(r.PrevClose), val(r.PrevPriceDate), val(r.DayChange)})
	}
	return out
}

// seedWatchCloses stores fetched closes of 100 two days ago and 110 yesterday
// for the test instrument and returns both dates.
func seedWatchCloses(t *testing.T, e *transactionTestEnv) (dayBefore, yesterday string) {
	t.Helper()
	today, _ := time.Parse(time.DateOnly, price.Today())
	dayBefore, yesterday = today.AddDate(0, 0, -2).Format(time.DateOnly), today.AddDate(0, 0, -1).Format(time.DateOnly)
	repo := price.NewRepo(e.db)
	for date, close := range map[string]string{dayBefore: "100", yesterday: "110"} {
		if err := repo.Upsert(context.Background(), e.instrumentID, date, close, "test"); err != nil {
			t.Fatalf("seed price: %v", err)
		}
	}
	return dayBefore, yesterday
}

// TestWatchlistPrices covers the close fields: fetched closes, a user's own
// manual price, an instrument without a price and a split after the previous
// close.
func TestWatchlistPrices(t *testing.T) {
	e := newTransactionTestEnv(t)
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('NOPX', 'BIST', 'No price', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	noPrice, _ := res.LastInsertId()
	dayBefore, yesterday := seedWatchCloses(t, e)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	for _, s := range []string{a, b} {
		if rec := e.do(s, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": e.instrumentID}); rec.Code != http.StatusCreated {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": noPrice}); rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}

	null := watchRow{"<null>", "<null>", "<null>", "<null>", "<null>", "<null>"}
	fetched := watchRow{"110", "test", yesterday, "100", dayBefore, "10.00"}
	if got := watchRows(t, e, a); len(got) != 2 || got[0] != null || got[1] != fetched {
		t.Errorf("A's rows = %+v, want NOPX null %+v, TEST %+v", got, null, fetched)
	}

	if err := price.NewManualRepo(e.db).Upsert(context.Background(), e.userID(a), e.instrumentID, yesterday, "120"); err != nil {
		t.Fatalf("seed manual price: %v", err)
	}
	manual := watchRow{"120", "manual", yesterday, "100", dayBefore, "20.00"}
	if got := watchRows(t, e, a); len(got) != 2 || got[1] != manual {
		t.Errorf("A's TEST row = %+v, want %+v", got, manual)
	}
	if got := watchRows(t, e, b); len(got) != 1 || got[0] != fetched {
		t.Errorf("B's rows = %+v, want just %+v", got, fetched)
	}
}

// TestWatchlistPricesSplit shows the previous close in today's terms: a 2-for-1
// split dated yesterday halves the close of the day before.
func TestWatchlistPricesSplit(t *testing.T) {
	e := newTransactionTestEnv(t)
	dayBefore, yesterday := seedWatchCloses(t, e)
	if err := split.NewRepo(e.db).Upsert(context.Background(), e.instrumentID, yesterday, decimal.NewFromInt(2), "manual"); err != nil {
		t.Fatalf("seed split: %v", err)
	}
	a := e.login("a@example.com")
	rec := e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": e.instrumentID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}
	want := watchRow{"110", "test", yesterday, "50", dayBefore, "60.00"}
	if got := watchRows(t, e, a); len(got) != 1 || got[0] != want {
		t.Errorf("rows = %+v, want just %+v", got, want)
	}
	created := decodeJSON[watchItemResponse](t, rec.Body.Bytes())
	if val(created.PrevClose) != "50" || val(created.DayChange) != "60.00" {
		t.Errorf("POST response = prev_close %s, day_change %s, want 50, 60.00", val(created.PrevClose), val(created.DayChange))
	}
}
