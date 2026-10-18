package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/plan"
	"github.com/dogauzun/gelirgider/backend/internal/price"
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
