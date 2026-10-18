package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/plan"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// twoMonthsAgo returns the first day of the month two months before today.
func twoMonthsAgo(t *testing.T) string {
	t.Helper()
	today, err := time.Parse(time.DateOnly, price.Today())
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}
	return time.Date(today.Year(), today.Month()-2, 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

func TestCreateAndListPlans(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(session, "P"))

	start := twoMonthsAgo(t)
	rec := e.do(session, http.MethodPost, path, map[string]any{
		"instrument_id": e.instrumentID, "amount": "1000", "day_of_month": 5, "start_date": start,
		"end_date": "2099-12-31", "fund": true, "note": " Düzenli alım ",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create buy plan = %d %s, want 201", rec.Code, rec.Body.String())
	}
	buy := decodeJSON[planResponse](t, rec.Body.Bytes())
	wantNext := plan.FirstOnOrAfter(start, 5)
	if buy.NextDate != wantNext || buy.Status != "due" || buy.Currency != "TRY" || buy.Amount != "1000" || !buy.Fund || buy.Note != "Düzenli alım" {
		t.Errorf("buy plan = %+v, want next_date %s, due, TRY, 1000, fund, trimmed note", buy, wantNext)
	}
	if buy.InstrumentID == nil || *buy.InstrumentID != e.instrumentID || buy.Symbol == nil || *buy.Symbol != "TEST" || buy.Market == nil || *buy.Market != "BIST" {
		t.Errorf("buy plan instrument = %v %v %v, want the env's TEST BIST", buy.InstrumentID, buy.Symbol, buy.Market)
	}

	rec = e.do(session, http.MethodPost, path, map[string]any{
		"currency": "USD", "amount": "50.5", "day_of_month": 31, "start_date": daysFromToday(t, 30),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deposit plan = %d %s, want 201", rec.Code, rec.Body.String())
	}
	dep := decodeJSON[planResponse](t, rec.Body.Bytes())
	if dep.Currency != "USD" || dep.Status != "active" || dep.InstrumentID != nil || dep.Symbol != nil || dep.Market != nil || dep.EndDate != nil {
		t.Errorf("deposit plan = %+v, want active USD with null instrument, symbol, market and end_date", dep)
	}
	if !strings.Contains(rec.Body.String(), `"instrument_id":null`) {
		t.Errorf("deposit plan body = %s, want instrument_id null", rec.Body.String())
	}

	list := decodeJSON[[]planResponse](t, e.do(session, http.MethodGet, path, nil).Body.Bytes())
	if len(list) != 2 || list[0].ID != buy.ID || list[1].ID != dep.ID {
		t.Errorf("list = %+v, want the buy plan then the deposit plan", list)
	}
	if list[0].NextDate > list[1].NextDate {
		t.Errorf("list next_dates = %s, %s, want ascending", list[0].NextDate, list[1].NextDate)
	}
}

func TestCreatePlanValidation(t *testing.T) {
	e := newTransactionTestEnv(t)
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-ZZ1', 'DEPOSIT', 'Deposit ZZ1', 'TRY')`)
	if err != nil {
		t.Fatalf("insert deposit instrument: %v", err)
	}
	depositID, _ := res.LastInsertId()
	session := e.login("a@example.com")
	path := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(session, "P"))
	body := func(over map[string]any) map[string]any {
		b := map[string]any{"currency": "TRY", "amount": "100", "day_of_month": 5, "start_date": "2026-11-01"}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{"unknown instrument", body(map[string]any{"instrument_id": 999999}), "instrument_id: unknown instrument"},
		{"deposit instrument", body(map[string]any{"instrument_id": depositID}), "instrument_id: unknown instrument"},
		{"currency mismatch", body(map[string]any{"instrument_id": e.instrumentID, "currency": "USD"}), "currency: must match instrument currency (TRY)"},
		{"currency", body(map[string]any{"currency": "GBP"}), "currency: must be one of TRY, USD, EUR"},
		{"no currency", body(map[string]any{"currency": ""}), "currency: must be one of TRY, USD, EUR"},
		{"missing amount", body(map[string]any{"amount": ""}), "amount: required"},
		{"too many digits", body(map[string]any{"amount": "1234567890123456"}), "amount: must have at most 15 integer"},
		{"zero amount", body(map[string]any{"amount": "0"}), "amount: must be a positive number"},
		{"day 0", body(map[string]any{"day_of_month": 0}), "day_of_month: must be between 1 and 31"},
		{"day 32", body(map[string]any{"day_of_month": 32}), "day_of_month: must be between 1 and 31"},
		{"bad start", body(map[string]any{"start_date": "2026-13-01"}), "start_date: must be YYYY-MM-DD"},
		{"old start", body(map[string]any{"start_date": "1989-12-31"}), "start_date: must be on or after 1990-01-01"},
		{"far start", body(map[string]any{"start_date": daysFromToday(t, 367)}), "start_date: must be at most 1 year after today"},
		{"bad end", body(map[string]any{"end_date": "soon"}), "end_date: must be YYYY-MM-DD"},
		{"end before start", body(map[string]any{"end_date": "2026-10-31"}), "end_date: must be on or after start_date"},
		{"note", body(map[string]any{"note": "a\nb"}), "note: must not contain control characters"},
	}
	for _, tt := range tests {
		rec := e.do(session, http.MethodPost, path, tt.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %s, want 400 containing %q", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}
	// The edges the messages bound are accepted.
	for _, ok := range []map[string]any{
		body(map[string]any{"start_date": daysFromToday(t, 365), "end_date": daysFromToday(t, 365)}),
		body(map[string]any{"instrument_id": e.instrumentID, "currency": "TRY", "day_of_month": 31, "start_date": "1990-01-01"}),
	} {
		if rec := e.do(session, http.MethodPost, path, ok); rec.Code != http.StatusCreated {
			t.Errorf("edge %v = %d %s, want 201", ok, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM saving_plans`).Scan(&n); err != nil || n != 2 {
		t.Errorf("plans stored = %d (err %v), want 2 from the accepted requests", n, err)
	}
}

func TestPlansAreScopedToTheirUser(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	aPath := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(a, "A"))
	bPath := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(b, "B"))
	body := map[string]any{"currency": "TRY", "amount": "100", "day_of_month": 5, "start_date": "2026-11-01"}
	if rec := e.do(a, http.MethodPost, aPath, body); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", rec.Code, rec.Body.String())
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := e.do(b, method, aPath, body)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "portfolio not found") {
			t.Errorf("B %s on A's portfolio = %d %s, want 404 portfolio not found", method, rec.Code, rec.Body.String())
		}
	}
	if list := decodeJSON[[]planResponse](t, e.do(b, http.MethodGet, bPath, nil).Body.Bytes()); len(list) != 0 {
		t.Errorf("B's plans = %+v, want none", list)
	}
	if list := decodeJSON[[]planResponse](t, e.do(a, http.MethodGet, aPath, nil).Body.Bytes()); len(list) != 1 {
		t.Errorf("A's plans = %+v, want 1", list)
	}
}

func TestEditPlan(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(session, "P"))
	create := func(day int, start, end string) planResponse {
		t.Helper()
		b := map[string]any{
			"instrument_id": e.instrumentID, "amount": "1000", "day_of_month": day, "start_date": start,
			"end_date": end, "note": "old",
		}
		rec := e.do(session, http.MethodPost, path, b)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s, want 201", rec.Code, rec.Body.String())
		}
		return decodeJSON[planResponse](t, rec.Body.Bytes())
	}
	put := func(p planResponse, over map[string]any) *httptest.ResponseRecorder {
		b := map[string]any{"amount": "1500", "day_of_month": p.DayOfMonth, "end_date": "2027-12-31", "fund": true, "note": " Düzenli alım "}
		for k, v := range over {
			b[k] = v
		}
		return e.do(session, http.MethodPut, fmt.Sprintf("/api/plans/%d", p.ID), b)
	}

	later := create(20, "2026-11-20", "2027-06-30")
	if later.NextDate != "2026-11-20" {
		t.Fatalf("seeded next_date = %s, want 2026-11-20", later.NextDate)
	}
	rec := put(later, map[string]any{"day_of_month": 5})
	if rec.Code != http.StatusOK {
		t.Fatalf("edit = %d %s, want 200", rec.Code, rec.Body.String())
	}
	got := decodeJSON[planResponse](t, rec.Body.Bytes())
	if got.NextDate != "2026-12-05" || got.DayOfMonth != 5 || got.Amount != "1500" || !got.Fund || got.Note != "Düzenli alım" ||
		got.EndDate == nil || *got.EndDate != "2027-12-31" || got.StartDate != "2026-11-20" || got.Symbol == nil || *got.Symbol != "TEST" {
		t.Errorf("edited plan = %+v, want day 5, next 2026-12-05, 1500, fund, new note and end, start and instrument kept", got)
	}
	list := decodeJSON[[]planResponse](t, e.do(session, http.MethodGet, path, nil).Body.Bytes())
	if len(list) != 1 || list[0].NextDate != "2026-12-05" || list[0].Amount != "1500" {
		t.Errorf("list after edit = %+v, want the stored edit", list)
	}

	earlier := create(5, "2026-11-05", "")
	if earlier.NextDate != "2026-11-05" {
		t.Fatalf("seeded next_date = %s, want 2026-11-05", earlier.NextDate)
	}
	got = decodeJSON[planResponse](t, put(earlier, map[string]any{"day_of_month": 20}).Body.Bytes())
	if got.NextDate != "2026-11-20" {
		t.Errorf("next_date after day 5 to 20 = %s, want 2026-11-20", got.NextDate)
	}

	withEnd := create(5, "2026-11-05", "2027-01-31")
	got = decodeJSON[planResponse](t, put(withEnd, map[string]any{"end_date": ""}).Body.Bytes())
	if got.EndDate != nil {
		t.Errorf("end_date after an empty end_date = %v, want cleared", *got.EndDate)
	}
	var stored *string
	if err := e.db.QueryRow(`SELECT end_date FROM saving_plans WHERE id = ?`, withEnd.ID).Scan(&stored); err != nil || stored != nil {
		t.Errorf("stored end_date = %v (err %v), want NULL", stored, err)
	}

	for name, over := range map[string]map[string]any{
		"end before start": {"end_date": "2026-11-04"},
		"amount":           {"amount": "0"},
		"day":              {"day_of_month": 32},
		"note":             {"note": "a\nb"},
	} {
		rec := put(withEnd, over)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body.String())
		}
	}
	if rec := put(withEnd, map[string]any{"end_date": "2026-11-04"}); !strings.Contains(rec.Body.String(), "end_date: must be on or after start_date") {
		t.Errorf("end before start body = %s, want end_date: must be on or after start_date", rec.Body.String())
	}
}

func TestEditAndDeletePlan(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	path := fmt.Sprintf("/api/portfolios/%d/plans", e.createPortfolio(a, "A"))
	rec := e.do(a, http.MethodPost, path, map[string]any{"currency": "TRY", "amount": "100", "day_of_month": 5, "start_date": "2026-11-01"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", rec.Code, rec.Body.String())
	}
	own := fmt.Sprintf("/api/plans/%d", decodeJSON[planResponse](t, rec.Body.Bytes()).ID)
	body := map[string]any{"amount": "200", "day_of_month": 6}

	for _, tt := range []struct {
		name, method, path string
		session            string
	}{
		{"B edits A's plan", http.MethodPut, own, b},
		{"B deletes A's plan", http.MethodDelete, own, b},
		{"edit a missing plan", http.MethodPut, "/api/plans/999999", a},
		{"delete a missing plan", http.MethodDelete, "/api/plans/999999", a},
		{"edit a non-number", http.MethodPut, "/api/plans/x", a},
		{"delete a non-number", http.MethodDelete, "/api/plans/x", a},
	} {
		rec := e.do(tt.session, tt.method, tt.path, body)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "plan not found") {
			t.Errorf("%s = %d %s, want 404 plan not found", tt.name, rec.Code, rec.Body.String())
		}
	}
	if list := decodeJSON[[]planResponse](t, e.do(a, http.MethodGet, path, nil).Body.Bytes()); len(list) != 1 || list[0].Amount != "100" {
		t.Fatalf("A's plans after B's attempts = %+v, want the plan untouched", list)
	}

	if rec := e.do(a, http.MethodDelete, own, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if list := decodeJSON[[]planResponse](t, e.do(a, http.MethodGet, path, nil).Body.Bytes()); len(list) != 0 {
		t.Errorf("plans after delete = %+v, want none", list)
	}
	if rec := e.do(a, http.MethodDelete, own, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
}

// bookEnv is a portfolio with a close of 123.45 for the env's instrument, dated
// the day before next, the next_date of a day-5 plan started two months ago.
type bookEnv struct {
	*transactionTestEnv
	session   string
	portfolio int64
	next      string
}

func newBookEnv(t *testing.T) *bookEnv {
	t.Helper()
	e := newTransactionTestEnv(t)
	next := twoMonthsAgo(t)[:8] + "05"
	d, err := time.Parse(time.DateOnly, next)
	if err != nil {
		t.Fatalf("parse %s: %v", next, err)
	}
	if _, err := e.db.Exec(`INSERT INTO prices (instrument_id, date, close, source) VALUES (?, ?, '123.45', 'test')`,
		e.instrumentID, d.AddDate(0, 0, -1).Format(time.DateOnly)); err != nil {
		t.Fatalf("insert price: %v", err)
	}
	session := e.login("a@example.com")
	return &bookEnv{transactionTestEnv: e, session: session, portfolio: e.createPortfolio(session, "P"), next: next}
}

// newPlan creates a day-5 plan of 1000 TRY started two months ago on the env's
// instrument, with over applied to the body.
func (b *bookEnv) newPlan(over map[string]any) planResponse {
	b.t.Helper()
	body := map[string]any{
		"instrument_id": b.instrumentID, "amount": "1000", "day_of_month": 5, "start_date": twoMonthsAgo(b.t), "note": "Düzenli alım",
	}
	for k, v := range over {
		body[k] = v
	}
	rec := b.do(b.session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/plans", b.portfolio), body)
	if rec.Code != http.StatusCreated {
		b.t.Fatalf("create plan = %d %s, want 201", rec.Code, rec.Body.String())
	}
	return decodeJSON[planResponse](b.t, rec.Body.Bytes())
}

// act posts to /api/plans/{id}/{action} and returns the recorder.
func (b *bookEnv) act(session string, id int64, action string) *httptest.ResponseRecorder {
	return b.do(session, http.MethodPost, fmt.Sprintf("/api/plans/%d/%s", id, action), nil)
}

func TestDuePlansAndBook(t *testing.T) {
	b := newBookEnv(t)
	buy := b.newPlan(nil)

	rec := b.do(b.session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/plans/due", b.portfolio), nil)
	due := decodeJSON[[]duePlanResponse](t, rec.Body.Bytes())
	// The occurrences of the 5th from two months ago through today.
	wantMissed := 2
	if price.Today()[8:] >= "05" {
		wantMissed = 3
	}
	if len(due) != 1 {
		t.Fatalf("due = %s, want the one plan", rec.Body.String())
	}
	d := due[0]
	if d.PlanID != buy.ID || d.Date != b.next || d.Missed != wantMissed || d.Symbol == nil || *d.Symbol != "TEST" || d.Market == nil || *d.Market != "BIST" ||
		d.Price == nil || *d.Price != "123.45" || d.PriceDate == nil || d.Quantity == nil || *d.Quantity != "8" || d.BookedAmount == nil || *d.BookedAmount != "987.60" {
		t.Errorf("due plan = %s, want the buy plan dated %s, missed %d, price 123.45, quantity 8, booked 987.60", rec.Body.String(), b.next, wantMissed)
	}

	rec = b.act(b.session, buy.ID, "book")
	if rec.Code != http.StatusOK {
		t.Fatalf("book = %d %s, want 200", rec.Code, rec.Body.String())
	}
	d0, _ := time.Parse(time.DateOnly, b.next)
	if got := decodeJSON[planResponse](t, rec.Body.Bytes()); got.NextDate != d0.AddDate(0, 1, 0).Format(time.DateOnly) {
		t.Errorf("next_date after book = %s, want one month after %s", got.NextDate, b.next)
	}
	txs := listTransactions(b.transactionTestEnv, b.session, b.portfolio)
	if len(txs) != 1 || txs[0].Type != "buy" || txs[0].Date != b.next || txs[0].Quantity == nil || *txs[0].Quantity != "8" ||
		txs[0].Price == nil || *txs[0].Price != "123.45" || txs[0].Amount != "987.6" || txs[0].Fee != "0" || txs[0].Note == nil || *txs[0].Note != "Düzenli alım" {
		t.Errorf("transactions after book = %+v, want one buy of 8 at 123.45 dated %s with the plan's note", txs, b.next)
	}
}

func TestBookPlanWithFundAndDepositPlan(t *testing.T) {
	b := newBookEnv(t)
	funded := b.newPlan(map[string]any{"fund": true})
	if rec := b.act(b.session, funded.ID, "book"); rec.Code != http.StatusOK {
		t.Fatalf("book funded = %d %s, want 200", rec.Code, rec.Body.String())
	}
	txs := listTransactions(b.transactionTestEnv, b.session, b.portfolio)
	if len(txs) != 2 {
		t.Fatalf("transactions after a funded book = %+v, want a deposit and a buy", txs)
	}
	buy, cash := txs[0], txs[1] // newest first
	if buy.Type != "buy" || cash.Type != "deposit" || cash.Amount != "987.6" || cash.Date != b.next || cash.ID >= buy.ID {
		t.Errorf("transactions = %+v, want a deposit of 987.60 dated %s with a lower id than the buy", txs, b.next)
	}

	// A deposit plan books a deposit of its amount; a skip books nothing.
	dep := b.newPlan(map[string]any{"instrument_id": nil, "currency": "TRY", "amount": "250"})
	rec := b.do(b.session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/plans/due", b.portfolio), nil)
	for _, d := range decodeJSON[[]duePlanResponse](t, rec.Body.Bytes()) {
		if d.PlanID == dep.ID && (d.InstrumentID != nil || d.Symbol != nil || d.Price != nil || d.Quantity != nil || d.BookedAmount == nil || *d.BookedAmount != "250") {
			t.Errorf("due deposit plan = %+v, want null instrument, price and quantity and booked_amount 250", d)
		}
	}
	if rec := b.act(b.session, dep.ID, "book"); rec.Code != http.StatusOK {
		t.Fatalf("book deposit plan = %d %s, want 200", rec.Code, rec.Body.String())
	}
	txs = listTransactions(b.transactionTestEnv, b.session, b.portfolio)
	if len(txs) != 3 || txs[0].Type != "deposit" || txs[0].Amount != "250" || txs[0].Date != b.next {
		t.Errorf("transactions after a deposit plan = %+v, want a third row, a deposit of 250 dated %s", txs, b.next)
	}

	skipped := b.newPlan(nil)
	rec = b.act(b.session, skipped.ID, "skip")
	got := decodeJSON[planResponse](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || got.NextDate <= skipped.NextDate {
		t.Errorf("skip = %d %s, want 200 with next_date after %s", rec.Code, rec.Body.String(), skipped.NextDate)
	}
	if n := len(listTransactions(b.transactionTestEnv, b.session, b.portfolio)); n != 3 {
		t.Errorf("transactions after a skip = %d, want still 3", n)
	}
}

func TestBookAndSkipPlanConflicts(t *testing.T) {
	b := newBookEnv(t)
	res, err := b.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('NOPX', 'BIST', 'No Price', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	noPrice, _ := res.LastInsertId()
	future := daysFromToday(t, 10)
	futureDay, _ := strconv.Atoi(future[8:])
	tests := []struct {
		name  string
		over  map[string]any
		books bool // only book is refused, skip goes through
		want  string
	}{
		{"not due yet", map[string]any{"start_date": future, "day_of_month": futureDay}, false, "plan is not due yet"},
		{"ended", map[string]any{"end_date": twoMonthsAgo(t)}, false, "plan has ended"},
		{"no close", map[string]any{"instrument_id": noPrice}, true, "no close on or before " + b.next},
		{"amount buys no unit", map[string]any{"amount": "10"}, true, "amount buys no unit at 123.45"},
	}
	for _, tt := range tests {
		p := b.newPlan(tt.over)
		rec := b.act(b.session, p.ID, "book")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: book = %d %s, want 409 containing %q", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
		wantSkip := http.StatusConflict
		if tt.books {
			wantSkip = http.StatusOK
		}
		if rec := b.act(b.session, p.ID, "skip"); rec.Code != wantSkip {
			t.Errorf("%s: skip = %d %s, want %d", tt.name, rec.Code, rec.Body.String(), wantSkip)
		}
	}
	if n := len(listTransactions(b.transactionTestEnv, b.session, b.portfolio)); n != 0 {
		t.Errorf("transactions after the refused bookings = %d, want none", n)
	}

	// A buy plan with no close is listed with null price, quantity and booked_amount.
	var noClose *duePlanResponse
	rec := b.do(b.session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/plans/due", b.portfolio), nil)
	for _, d := range decodeJSON[[]duePlanResponse](t, rec.Body.Bytes()) {
		if d.InstrumentID != nil && *d.InstrumentID == noPrice {
			noClose = &d
		}
	}
	if noClose == nil || noClose.Price != nil || noClose.PriceDate != nil || noClose.Quantity != nil || noClose.BookedAmount != nil {
		t.Errorf("due plan with no close = %+v, want null price, price_date, quantity and booked_amount", noClose)
	}
}

func TestBookAndSkipPlanAreScopedToTheirUser(t *testing.T) {
	b := newBookEnv(t)
	other := b.login("b@example.com")
	p := b.newPlan(nil)
	for _, id := range []string{fmt.Sprint(p.ID), "999999", "x"} {
		for _, action := range []string{"book", "skip"} {
			session := other
			if id != fmt.Sprint(p.ID) {
				session = b.session
			}
			rec := b.do(session, http.MethodPost, fmt.Sprintf("/api/plans/%s/%s", id, action), nil)
			if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "plan not found") {
				t.Errorf("%s of plan %s = %d %s, want 404 plan not found", action, id, rec.Code, rec.Body.String())
			}
		}
	}
	rec := b.do(other, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/plans/due", b.portfolio), nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("B's due on A's portfolio = %d, want 404", rec.Code)
	}
	if got := decodeJSON[[]planResponse](t, b.do(b.session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/plans", b.portfolio), nil).Body.Bytes()); got[0].NextDate != b.next {
		t.Errorf("A's plan next_date = %s, want %s untouched", got[0].NextDate, b.next)
	}
}

// A stale call (a double-clicked Book on a plan with two missed occurrences)
// must not book the occurrence after the one it priced.
func TestAdvanceRefusesAStalePlan(t *testing.T) {
	b := newBookEnv(t)
	p := b.newPlan(nil)
	ctx := context.Background()
	var userID int64
	if err := b.db.QueryRow(`SELECT user_id FROM saving_plans WHERE id = ?`, p.ID).Scan(&userID); err != nil {
		t.Fatalf("plan owner: %v", err)
	}
	h := &planHandler{deps: &deps{conn: b.db, plans: plan.NewRepo(b.db), transactions: transaction.NewRepo(b.db)}}
	stale, err := h.plans.Get(ctx, userID, p.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if rec := b.act(b.session, p.ID, "book"); rec.Code != http.StatusOK {
		t.Fatalf("first book = %d %s, want 200", rec.Code, rec.Body.String())
	}

	var conflict planConflict
	_, err = h.advance(ctx, stale, price.Today(), func(tx *sql.Tx, cur plan.Plan) error {
		t.Error("write ran for a stale plan")
		return nil
	})
	if !errors.As(err, &conflict) || string(conflict) != "plan changed, try again" {
		t.Fatalf("advance on a stale plan = %v, want the conflict %q", err, "plan changed, try again")
	}
	if n := len(listTransactions(b.transactionTestEnv, b.session, b.portfolio)); n != 1 {
		t.Errorf("transactions after the stale call = %d, want the one buy", n)
	}
}
