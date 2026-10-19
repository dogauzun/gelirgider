package server

import (
	"fmt"
	"net/http"
	"testing"
)

// TestPortfolioLookupErrorIsInternal checks that a failing portfolio lookup
// answers 500 on a route per way of resolving the path value (scopeLists,
// scopeID, Owned, the statement preview), not the 404 of a portfolio that is
// gone.
func TestPortfolioLookupErrorIsInternal(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("lookup@example.com")
	id := e.createPortfolio(session, "Lookup")
	if _, err := e.db.Exec(`DROP TABLE portfolios`); err != nil {
		t.Fatalf("drop portfolios: %v", err)
	}

	for _, path := range []string{"summary", "allocation-targets", "transactions"} {
		wantError(t, e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/%s", id, path), nil), http.StatusInternalServerError, "internal error")
	}
	wantError(t, upload(e, session, "auto", id, []byte("x")), http.StatusInternalServerError, "internal error")
}
