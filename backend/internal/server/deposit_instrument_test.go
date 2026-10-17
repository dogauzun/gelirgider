package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDepositInstrumentHiddenFromGenericRoutes checks that a DEPOSIT
// instrument answers as an unknown one on search, transaction create and
// update, the import commit and an import alias, for every user, while a
// seeded instrument still works.
func TestDepositInstrumentHiddenFromGenericRoutes(t *testing.T) {
	e := newTransactionTestEnv(t)
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-ZZ1', 'DEPOSIT', 'Deposit ZZ1', 'TRY')`)
	if err != nil {
		t.Fatalf("insert deposit instrument: %v", err)
	}
	depositID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("deposit instrument id: %v", err)
	}
	const unknown = "instrument_id: unknown instrument"

	for _, email := range []string{"a@example.com", "b@example.com"} {
		session := e.login(email)
		portfolioID := e.createPortfolio(session, "P")
		txPath := fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID)
		buy := func(instrumentID int64) map[string]any {
			return map[string]any{"type": "buy", "date": "2025-01-02", "instrument_id": instrumentID, "quantity": "1", "price": "10"}
		}
		search := func(q string) []string {
			rec := e.do(session, http.MethodGet, "/api/instruments?q="+q, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: search %q status = %d; body: %s", email, q, rec.Code, rec.Body.String())
			}
			var found []struct {
				Symbol string `json:"symbol"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&found); err != nil {
				t.Fatalf("decode search: %v", err)
			}
			symbols := []string{}
			for _, f := range found {
				symbols = append(symbols, f.Symbol)
			}
			return symbols
		}
		wantRefused := func(label string, rec *httptest.ResponseRecorder, msg string) {
			t.Helper()
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), msg) {
				t.Errorf("%s: %s = %d %s, want 400 containing %q", email, label, rec.Code, rec.Body.String(), msg)
			}
		}

		if got := search("DEP-ZZ1"); len(got) != 0 {
			t.Errorf("%s: search for the deposit symbol = %v, want none", email, got)
		}
		if got := search("TEST"); len(got) != 1 || got[0] != "TEST" {
			t.Errorf("%s: search for the seeded symbol = %v, want [TEST]", email, got)
		}

		wantRefused("POST", e.do(session, http.MethodPost, txPath, buy(depositID)), unknown)
		id := e.createTransaction(session, portfolioID, buy(e.instrumentID))
		wantRefused("PUT", e.do(session, http.MethodPut, fmt.Sprintf("/api/transactions/%d", id), buy(depositID)), unknown)
		if rec := e.do(session, http.MethodPut, fmt.Sprintf("/api/transactions/%d", id), buy(e.instrumentID)); rec.Code != http.StatusOK {
			t.Errorf("%s: PUT of the seeded instrument = %d %s, want 200", email, rec.Code, rec.Body.String())
		}

		body := importBody(portfolioID, fakeSHA("dep-"+email),
			map[string]any{"type": "buy", "date": importPeriodStart, "instrument_id": depositID, "quantity": "1", "price": "10", "fee": "0", "currency": "TRY"})
		wantRefused("commit row", e.do(session, http.MethodPost, "/api/imports", body), "rows[0]."+unknown)

		body = importBody(portfolioID, fakeSHA("alias-"+email))
		body["aliases"] = []map[string]any{{"symbol": "XYZ", "instrument_id": depositID}}
		wantRefused("alias", e.do(session, http.MethodPost, "/api/imports", body), "aliases[0].instrument_id: unknown instrument")
	}
}
