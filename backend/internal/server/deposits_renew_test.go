package server

import (
	"fmt"
	"net/http"
	"testing"
)

// TestRenewDeposit checks a funded 100000 TRY deposit at 0.45 with stopaj
// 0.175 from 2026-01-01 to 2026-02-02, renewed at 0.40 with stopaj 0.15 until
// 2026-03-04: the new deposit holds 103254.8 from 2026-02-02, the old one is
// matured with its interest booked and the cash is back to where it was before
// the deposit. A second renew is 409, another user's is 404, a bad body is 400
// and a deposit that is not due yet is 409 and books nothing.
func TestRenewDeposit(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	portfolioID := e.createPortfolio(a, "P")
	path := fmt.Sprintf("/api/portfolios/%d", portfolioID)
	body := depositBody("2026-01-01", "2026-02-02")
	body["fund"] = true
	rec := e.do(a, http.MethodPost, path+"/deposits", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	old := decodeJSON[depositResponse](t, rec.Body.Bytes())
	renewPath := fmt.Sprintf("/api/deposits/%d/renew", old.ID)
	terms := map[string]any{"rate": "0.40", "stopaj_rate": "0.15", "maturity_date": "2026-03-04"}
	var n int
	count := func() int {
		t.Helper()
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&n); err != nil {
			t.Fatalf("count transactions: %v", err)
		}
		return n
	}

	// Not due yet.
	activeBody := depositBody(daysFromToday(t, -1), daysFromToday(t, 30))
	activeBody["fund"] = true
	future := e.do(a, http.MethodPost, path+"/deposits", activeBody)
	futureID := decodeJSON[depositResponse](t, future.Body.Bytes()).ID
	before := count()
	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/renew", futureID), map[string]any{"rate": "0.40", "stopaj_rate": "0.15", "maturity_date": daysFromToday(t, 60)}); rec.Code != http.StatusConflict {
		t.Errorf("renew before the maturity date = %d, want 409", rec.Code)
	}
	if count() != before {
		t.Errorf("rows after the early renew = %d, want still %d", n, before)
	}

	if rec := e.do(b, http.MethodPost, renewPath, terms); rec.Code != http.StatusNotFound {
		t.Errorf("other user's renew = %d, want 404", rec.Code)
	}
	if rec := e.do(a, http.MethodPost, "/api/deposits/x/renew", terms); rec.Code != http.StatusNotFound {
		t.Errorf("renew of a bad id = %d, want 404", rec.Code)
	}
	if rec := e.do(a, http.MethodPost, renewPath, map[string]any{"rate": "0.40", "stopaj_rate": "0.15", "maturity_date": "2026-02-02"}); rec.Code != http.StatusBadRequest {
		t.Errorf("renew with a maturity not after the old one = %d, want 400", rec.Code)
	}
	if count() != before {
		t.Errorf("rows after the rejected renews = %d, want still %d", n, before)
	}

	rec = e.do(a, http.MethodPost, renewPath, terms)
	if rec.Code != http.StatusCreated {
		t.Fatalf("renew = %d %s, want 201", rec.Code, rec.Body.String())
	}
	next := decodeJSON[depositResponse](t, rec.Body.Bytes())
	if next.Principal != "103254.8" || next.StartDate != "2026-02-02" || next.MaturityDate != "2026-03-04" ||
		next.Rate != "0.4" || next.StopajRate != "0.15" || next.Currency != "TRY" ||
		next.RenewedFrom == nil || *next.RenewedFrom != old.ID {
		t.Errorf("renewed deposit = %+v, want 103254.8 from 2026-02-02 to 2026-03-04 renewing %d", next, old.ID)
	}
	if rec := e.do(a, http.MethodPost, renewPath, terms); rec.Code != http.StatusConflict {
		t.Errorf("second renew = %d, want 409", rec.Code)
	}

	list := decodeJSON[[]depositResponse](t, e.do(a, http.MethodGet, path+"/deposits", nil).Body.Bytes())
	status := map[int64]depositResponse{}
	for _, d := range list {
		status[d.ID] = d
	}
	if status[old.ID].Status != "matured" || status[old.ID].RenewedFrom != nil || status[next.ID].RenewedFrom == nil || *status[next.ID].RenewedFrom != old.ID {
		t.Errorf("list = %+v, want the old deposit matured and the new one renewing it", list)
	}
	var amount, fee string
	if err := e.db.QueryRow(`SELECT amount, fee FROM transactions WHERE type = 'dividend' AND instrument_id IS NULL`).Scan(&amount, &fee); err != nil ||
		amount != "3945.21" || fee != "690.41" {
		t.Errorf("interest row = %s %s (err %v), want 3945.21 and 690.41", amount, fee, err)
	}
	summary := decodeJSON[struct {
		Cash string `json:"cash"`
	}](t, e.do(a, http.MethodGet, path+"/summary", nil).Body.Bytes())
	// Both deposits were funded, so the cash is where it was before them.
	if summary.Cash != "0.00" {
		t.Errorf("cash = %s, want 0.00: the renewal used exactly the cash the maturity returned", summary.Cash)
	}

	// A second row renewing the same deposit is rejected by the index.
	if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TD-DUP', 'DEPOSIT', 'dup', 'TRY')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	if _, err := e.db.Exec(`INSERT INTO term_deposits (user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate, start_date, maturity_date, renewed_from, created_at)
		SELECT user_id, portfolio_id, (SELECT id FROM instruments WHERE symbol = 'TD-DUP'), currency, principal, rate, stopaj_rate, start_date, maturity_date, renewed_from, created_at
		FROM term_deposits WHERE id = ?`, next.ID); err == nil {
		t.Error("a second deposit with the same renewed_from was accepted, want a unique violation")
	}

	// The old deposit is matured and the new one open: deleting the portfolio
	// removes both rows in one statement.
	if rec := e.do(a, http.MethodDelete, path+"?force=true", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete portfolio = %d %s, want 204", rec.Code, rec.Body.String())
	}
}
