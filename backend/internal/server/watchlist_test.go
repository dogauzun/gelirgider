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

// TestWatchlistTarget walks PUT: the side fixed by the latest close (110), the
// reached flag on PUT and GET, a new close, clearing, the refusals and
// another user's id.
func TestWatchlistTarget(t *testing.T) {
	e := newTransactionTestEnv(t)
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('NOPX', 'BIST', 'No price', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	noPrice, _ := res.LastInsertId()
	seedWatchCloses(t, e)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	add := func(instrumentID int64) string {
		rec := e.do(a, http.MethodPost, "/api/watchlist", map[string]any{"instrument_id": instrumentID, "note": "keep"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
		}
		return "/api/watchlist/" + strconv.FormatInt(decodeJSON[watchItemResponse](t, rec.Body.Bytes()).ID, 10)
	}
	path, noPricePath := add(e.instrumentID), add(noPrice)
	put := func(session, path string, body any) watchItemResponse {
		t.Helper()
		rec := e.do(session, http.MethodPut, path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %v = %d %s, want 200", body, rec.Code, rec.Body.String())
		}
		return decodeJSON[watchItemResponse](t, rec.Body.Bytes())
	}
	state := func(r watchItemResponse) string {
		return val(r.TargetPrice) + " " + val(r.TargetSide) + " " + strconv.FormatBool(r.TargetReached) + " " + r.Note
	}
	check := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", what, got, want)
		}
	}

	check("target 150", state(put(a, path, map[string]any{"target_price": "150"})), "150 above false keep")
	check("target 110", state(put(a, path, map[string]any{"target_price": "110", "note": " new "})), "110 below true new")
	check("target 150 again", state(put(a, path, map[string]any{"target_price": "150", "note": ""})), "150 above false ")

	today := price.Today()
	if err := price.NewRepo(e.db).Upsert(context.Background(), e.instrumentID, today, "160", "test"); err != nil {
		t.Fatalf("seed price: %v", err)
	}
	list := decodeJSON[[]watchItemResponse](t, e.do(a, http.MethodGet, "/api/watchlist", nil).Body.Bytes())
	check("GET after close 160", state(list[1]), "150 above true ")

	check("cleared by null", state(put(a, path, map[string]any{"target_price": nil})), "<null> <null> false ")
	put(a, path, map[string]any{"target_price": "150"})
	check("cleared by omitting", state(put(a, path, map[string]any{})), "<null> <null> false ")

	for name, c := range map[string]struct {
		path, want string
		body       map[string]any
	}{
		"no close":    {noPricePath, "target_price: no close to compare with", map[string]any{"target_price": "5"}},
		"zero":        {path, "target_price: must be a positive decimal", map[string]any{"target_price": "0"}},
		"digits":      {path, "target_price: must have at most 15 integer and 8 fraction digits", map[string]any{"target_price": "12345678901234567890"}},
		"long note":   {path, "note: must be at most 500 characters", map[string]any{"note": strings.Repeat("x", 501)}},
		"number body": {path, "invalid request body", map[string]any{"target_price": 5}},
	} {
		rec := e.do(a, http.MethodPut, c.path, c.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("PUT %s = %d %s, want 400 %s", name, rec.Code, rec.Body.String(), c.want)
		}
	}
	// A refused PUT leaves the row (NOPX, sorted first) alone.
	check("row after refusals", state(decodeJSON[[]watchItemResponse](t, e.do(a, http.MethodGet, "/api/watchlist", nil).Body.Bytes())[0]), "<null> <null> false keep")

	for name, p := range map[string]string{"another user's id": path, "unknown id": "/api/watchlist/9999", "malformed id": "/api/watchlist/x"} {
		who := b
		if name != "another user's id" {
			who = a
		}
		rec := e.do(who, http.MethodPut, p, map[string]any{"target_price": "5"})
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "watchlist item not found") {
			t.Errorf("PUT %s = %d %s, want 404 watchlist item not found", name, rec.Code, rec.Body.String())
		}
	}
}
