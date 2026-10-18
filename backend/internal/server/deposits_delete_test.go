package server

import (
	"fmt"
	"net/http"
	"testing"
)

func countRows(t *testing.T, e *transactionTestEnv, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestDeleteDeposit checks that A's deposit rows (buy, deposit, instrument)
// refuse the generic transaction routes, that B cannot delete A's deposit, and
// that A's delete removes the buy, the deposit row, the instrument and, for a
// funded deposit, the funding cash row, leaving the cash as before. A matured
// deposit answers 409, and the interest row of a maturity stays a plain
// transaction.
func TestDeleteDeposit(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	portfolioID := e.createPortfolio(a, "P")
	path := fmt.Sprintf("/api/portfolios/%d", portfolioID)
	e.createTransaction(a, portfolioID, map[string]any{"type": "deposit", "date": "2025-12-31", "amount": "100000"})
	cashBefore := decodeJSON[struct {
		Cash string `json:"cash"`
	}](t, e.do(a, http.MethodGet, path+"/summary", nil).Body.Bytes()).Cash

	plain := decodeJSON[depositResponse](t, e.do(a, http.MethodPost, path+"/deposits", depositBody("2026-01-01", "2026-02-02")).Body.Bytes())
	funded := depositBody("2026-01-01", "2026-02-02")
	funded["fund"] = true
	fundedID := decodeJSON[depositResponse](t, e.do(a, http.MethodPost, path+"/deposits", funded).Body.Bytes()).ID

	var buyID int64
	if err := e.db.QueryRow(`SELECT id FROM transactions WHERE type = 'buy' AND instrument_id = ?`, plain.InstrumentID).Scan(&buyID); err != nil {
		t.Fatalf("find buy: %v", err)
	}
	txPath := fmt.Sprintf("/api/transactions/%d", buyID)
	body := map[string]any{"type": "buy", "instrument_id": plain.InstrumentID, "date": "2026-01-01", "quantity": "1", "price": "1"}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := e.do(a, method, txPath, body)
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "{\"error\":\"deposit rows are changed through the deposit routes\"}\n" {
			t.Errorf("A %s of the buy = %d %s, want 400 and the deposit message", method, rec.Code, rec.Body.String())
		}
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM transactions WHERE id = ? AND quantity = '100000'`, buyID); n != 1 {
		t.Errorf("buy rows after the refused PUT and DELETE = %d, want it unchanged", n)
	}

	plainPath := fmt.Sprintf("/api/deposits/%d", plain.ID)
	if rec := e.do(b, http.MethodDelete, plainPath, nil); rec.Code != http.StatusNotFound {
		t.Errorf("B's delete = %d, want 404", rec.Code)
	}
	if rec := e.do(a, http.MethodDelete, "/api/deposits/x", nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete of a bad id = %d, want 404", rec.Code)
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM term_deposits`); n != 2 {
		t.Fatalf("deposits after B's delete = %d, want 2", n)
	}

	if rec := e.do(a, http.MethodDelete, plainPath, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("A's delete = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM transactions WHERE instrument_id = ?`, plain.InstrumentID) +
		countRows(t, e, `SELECT COUNT(*) FROM term_deposits WHERE id = ?`, plain.ID) +
		countRows(t, e, `SELECT COUNT(*) FROM instruments WHERE id = ?`, plain.InstrumentID); n != 0 {
		t.Errorf("rows left of the deleted deposit = %d, want 0", n)
	}
	if rec := e.do(a, http.MethodDelete, plainPath, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}

	// The funded one takes its cash deposit with it, so the cash is as before.
	var fundingID int64
	if err := e.db.QueryRow(`SELECT funding_transaction_id FROM term_deposits WHERE id = ?`, fundedID).Scan(&fundingID); err != nil {
		t.Fatalf("funding id: %v", err)
	}
	if rec := e.do(a, http.MethodDelete, fmt.Sprintf("/api/deposits/%d", fundedID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("funded delete = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM transactions WHERE id = ?`, fundingID); n != 0 {
		t.Errorf("funding rows left = %d, want 0", n)
	}
	summary := decodeJSON[struct {
		Cash string `json:"cash"`
	}](t, e.do(a, http.MethodGet, path+"/summary", nil).Body.Bytes())
	if summary.Cash != cashBefore {
		t.Errorf("cash after the deletes = %s, want %s", summary.Cash, cashBefore)
	}
}

// TestDeleteDepositFundingRow checks that a funding row the user deleted first
// does not fail the delete, and that a funding_transaction_id naming another
// user's transaction leaves that transaction in place.
func TestDeleteDepositFundingRow(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	portfolioA, portfolioB := e.createPortfolio(a, "A"), e.createPortfolio(b, "B")
	path := fmt.Sprintf("/api/portfolios/%d/deposits", portfolioA)
	funded := depositBody("2026-01-01", "2026-02-02")
	funded["fund"] = true
	first := decodeJSON[depositResponse](t, e.do(a, http.MethodPost, path, funded).Body.Bytes())
	second := decodeJSON[depositResponse](t, e.do(a, http.MethodPost, path, funded).Body.Bytes())

	var fundingID int64
	if err := e.db.QueryRow(`SELECT funding_transaction_id FROM term_deposits WHERE id = ?`, first.ID).Scan(&fundingID); err != nil {
		t.Fatalf("funding id: %v", err)
	}
	if rec := e.do(a, http.MethodDelete, fmt.Sprintf("/api/transactions/%d", fundingID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete of the funding row = %d %s, want 204 (a plain deposit)", rec.Code, rec.Body.String())
	}
	if rec := e.do(a, http.MethodDelete, fmt.Sprintf("/api/deposits/%d", first.ID), nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete after the funding row is gone = %d %s, want 204", rec.Code, rec.Body.String())
	}

	others := e.createTransaction(b, portfolioB, map[string]any{"type": "deposit", "date": "2025-12-31", "amount": "5"})
	if _, err := e.db.Exec(`UPDATE term_deposits SET funding_transaction_id = ? WHERE id = ?`, others, second.ID); err != nil {
		t.Fatalf("point the funding id at B's row: %v", err)
	}
	if rec := e.do(a, http.MethodDelete, fmt.Sprintf("/api/deposits/%d", second.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM transactions WHERE id = ?`, others); n != 1 {
		t.Errorf("B's transaction rows = %d, want it still there", n)
	}
}

// TestDeleteMaturedDeposit checks that a deposit with a sell answers 409 and
// keeps every row, and that the maturity's interest row (a dividend with no
// instrument) can still be edited and deleted.
func TestDeleteMaturedDeposit(t *testing.T) {
	e := newTransactionTestEnv(t)
	a := e.login("a@example.com")
	portfolioID := e.createPortfolio(a, "P")
	path := fmt.Sprintf("/api/portfolios/%d", portfolioID)
	e.createTransaction(a, portfolioID, map[string]any{"type": "deposit", "date": "2025-12-31", "amount": "100000"})
	dep := decodeJSON[depositResponse](t, e.do(a, http.MethodPost, path+"/deposits", depositBody("2026-01-01", "2026-02-02")).Body.Bytes())
	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", dep.ID), nil); rec.Code != http.StatusOK {
		t.Fatalf("mature = %d %s", rec.Code, rec.Body.String())
	}

	if rec := e.do(a, http.MethodDelete, fmt.Sprintf("/api/deposits/%d", dep.ID), nil); rec.Code != http.StatusConflict {
		t.Errorf("delete of a matured deposit = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if n := countRows(t, e, `SELECT COUNT(*) FROM transactions WHERE instrument_id = ?`, dep.InstrumentID); n != 2 {
		t.Errorf("deposit rows after the refused delete = %d, want buy and sell", n)
	}

	var interestID int64
	if err := e.db.QueryRow(`SELECT id FROM transactions WHERE type = 'dividend' AND instrument_id IS NULL`).Scan(&interestID); err != nil {
		t.Fatalf("find interest row: %v", err)
	}
	interestPath := fmt.Sprintf("/api/transactions/%d", interestID)
	edit := map[string]any{"type": "dividend", "date": "2026-02-02", "amount": "10", "currency": "TRY"}
	if rec := e.do(a, http.MethodPut, interestPath, edit); rec.Code != http.StatusOK {
		t.Errorf("edit of the interest row = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if rec := e.do(a, http.MethodDelete, interestPath, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete of the interest row = %d %s, want 204", rec.Code, rec.Body.String())
	}
}
