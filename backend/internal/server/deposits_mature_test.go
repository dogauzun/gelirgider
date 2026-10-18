package server

import (
	"fmt"
	"net/http"
	"testing"
)

// TestMatureDeposit checks a 100000 TRY deposit at 0.45 with stopaj 0.175
// from 2026-01-01 to 2026-02-02: it is listed as due with gross 3945.21,
// stopaj 690.41 and net 3254.80, and maturing it returns the cash to its
// pre-deposit 100000 plus 3254.80, books the interest, closes the position and
// marks the deposit matured. A second mature is 409, another user's is 404 and
// a deposit that matures in the future is 409 and books nothing.
func TestMatureDeposit(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	portfolioID := e.createPortfolio(a, "P")
	path := fmt.Sprintf("/api/portfolios/%d", portfolioID)
	e.createTransaction(a, portfolioID, map[string]any{"type": "deposit", "date": "2025-12-31", "amount": "100000"})

	// The second deposit pays for itself, so only the first one moves the cash.
	active := depositBody(daysFromToday(t, -1), daysFromToday(t, 30))
	active["fund"] = true
	var dueID int64
	for _, body := range []map[string]any{depositBody("2026-01-01", "2026-02-02"), active} {
		rec := e.do(a, http.MethodPost, path+"/deposits", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
		}
		if dueID == 0 {
			dueID = decodeJSON[depositResponse](t, rec.Body.Bytes()).ID
		}
	}

	due := decodeJSON[[]dueDepositResponse](t, e.do(a, http.MethodGet, path+"/deposits/due", nil).Body.Bytes())
	want := dueDepositResponse{ID: dueID, Currency: "TRY", Principal: "100000", MaturityDate: "2026-02-02", Gross: "3945.21", Stopaj: "690.41", Net: "3254.80"}
	if len(due) != 1 || due[0] != want {
		t.Fatalf("due = %+v, want just %+v", due, want)
	}

	if rec := e.do(b, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", dueID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("other user's mature = %d, want 404", rec.Code)
	}
	if rec := e.do(a, http.MethodPost, "/api/deposits/x/mature", nil); rec.Code != http.StatusNotFound {
		t.Errorf("mature of a bad id = %d, want 404", rec.Code)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE type IN ('sell', 'dividend')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sell and dividend rows before mature = %d (err %v), want 0", n, err)
	}

	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", dueID), nil); rec.Code != http.StatusOK {
		t.Fatalf("mature = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", dueID), nil); rec.Code != http.StatusConflict {
		t.Errorf("second mature = %d, want 409", rec.Code)
	}
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE type IN ('sell', 'dividend')`).Scan(&n); err != nil || n != 2 {
		t.Errorf("sell and dividend rows after two matures = %d (err %v), want 2", n, err)
	}
	var note, amount, fee string
	if err := e.db.QueryRow(`SELECT note, amount, fee FROM transactions WHERE type = 'dividend' AND instrument_id IS NULL`).Scan(&note, &amount, &fee); err != nil ||
		note != "Vadeli mevduat TRY 2026-02-02" || amount != "3945.21" || fee != "690.41" {
		t.Errorf("interest row = %q %s %s (err %v), want the deposit's name, 3945.21, 690.41", note, amount, fee, err)
	}

	if due = decodeJSON[[]dueDepositResponse](t, e.do(a, http.MethodGet, path+"/deposits/due", nil).Body.Bytes()); len(due) != 0 {
		t.Errorf("due after mature = %+v, want none", due)
	}
	list := decodeJSON[[]depositResponse](t, e.do(a, http.MethodGet, path+"/deposits", nil).Body.Bytes())
	if len(list) != 2 || list[1].ID != dueID || list[1].Status != "matured" || list[0].Status != "active" {
		t.Errorf("list = %+v, want the first deposit matured and the second active", list)
	}

	type summaryBody struct {
		Cash      string `json:"cash"`
		Positions []struct {
			Quantity string `json:"quantity"`
		} `json:"positions"`
		Stats struct {
			ByCurrency map[string]struct {
				Interest string `json:"interest"`
			} `json:"by_currency"`
		} `json:"stats"`
	}
	summary := decodeJSON[summaryBody](t, e.do(a, http.MethodGet, path+"/summary", nil).Body.Bytes())
	if summary.Cash != "103254.80" || summary.Stats.ByCurrency["TRY"].Interest != "3254.80" {
		t.Errorf("cash = %s, interest = %s, want 103254.80 and 3254.80", summary.Cash, summary.Stats.ByCurrency["TRY"].Interest)
	}
	// The matured deposit is closed; the active one still holds its principal.
	open := 0
	for _, p := range summary.Positions {
		if p.Quantity != "0" {
			open++
		}
	}
	if open != 1 {
		t.Errorf("positions = %+v, want only the active deposit open", summary.Positions)
	}

	future := decodeJSON[[]depositResponse](t, e.do(a, http.MethodGet, path+"/deposits", nil).Body.Bytes())[0]
	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", future.ID), nil); rec.Code != http.StatusConflict {
		t.Errorf("mature before the maturity date = %d, want 409", rec.Code)
	}
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE type IN ('sell', 'dividend')`).Scan(&n); err != nil || n != 2 {
		t.Errorf("rows after the early mature = %d (err %v), want still 2", n, err)
	}
}
