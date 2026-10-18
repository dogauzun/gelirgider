package server

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
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
