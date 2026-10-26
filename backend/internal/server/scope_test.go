package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
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

// TestScopeTransactionsResolvesTheID checks the id rules scopeTransactions
// shares with scopePortfolioID: a non-number, another user's portfolio and a
// missing portfolio are portfolio.ErrNotFound, "all" is one list per
// portfolio of the user, a number is one list.
func TestScopeTransactionsResolvesTheID(t *testing.T) {
	e := newTransactionTestEnv(t)
	owner := e.login("scope-owner@example.com")
	other := e.login("scope-other@example.com")
	mine := e.createPortfolio(owner, "Mine")
	e.createPortfolio(owner, "Second")
	foreign := e.createPortfolio(other, "Theirs")
	portfolios, transactions := portfolio.NewRepo(e.db), transaction.NewRepo(e.db)
	ctx := context.Background()
	userID, err := e.sessions.GetSession(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"abc", "", fmt.Sprint(foreign), "999999"} {
		if _, err := scopeTransactions(ctx, portfolios, transactions, userID, id); !errors.Is(err, portfolio.ErrNotFound) {
			t.Errorf("id %q: err = %v, want portfolio.ErrNotFound", id, err)
		}
	}
	for id, want := range map[string]int{"all": 2, fmt.Sprint(mine): 1} {
		lists, err := scopeTransactions(ctx, portfolios, transactions, userID, id)
		if err != nil || len(lists) != want {
			t.Errorf("id %q: %d lists, err %v, want %d lists", id, len(lists), err, want)
		}
	}
}
