package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// scopeTransactions resolves the {id} path value of a portfolio route into
// one transaction list per portfolio in scope: the one portfolio
// for a numeric id, every portfolio of userID for "all". An id that is not
// a number, or names another user's portfolio, is portfolio.ErrNotFound, as
// it is on the single-portfolio routes today.
func scopeTransactions(ctx context.Context, portfolios *portfolio.Repo, transactions *transaction.Repo, userID int64, id string) ([][]transaction.Transaction, error) {
	pid, err := scopePortfolioID(ctx, portfolios, userID, id)
	if err != nil {
		return nil, err
	}
	var ids []int64
	if pid != nil {
		ids = []int64{*pid}
	} else {
		ps, err := portfolios.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
	}

	// shortcut: one List query per portfolio. Fine for a handful of
	// portfolios; one UNION query if portfolio counts grow.
	lists := make([][]transaction.Transaction, 0, len(ids))
	for _, pid := range ids {
		txs, err := transactions.List(ctx, userID, pid)
		if err != nil {
			return nil, err
		}
		lists = append(lists, txs)
	}
	return lists, nil
}

// scopePortfolioID resolves the {id} path value into the portfolio a stored
// per-scope row belongs to: nil for "all", else the id of a portfolio userID
// owns. A non-number, or another user's portfolio, is portfolio.ErrNotFound.
func scopePortfolioID(ctx context.Context, portfolios *portfolio.Repo, userID int64, id string) (*int64, error) {
	if id == "all" {
		return nil, nil
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, portfolio.ErrNotFound
	}
	if _, err := portfolios.Get(ctx, userID, n); err != nil {
		return nil, err
	}
	return &n, nil
}

// scopeLists is scopeTransactions for the signed-in user and the request's
// {id}. It writes the error and is false when it cannot.
func (d *deps) scopeLists(w http.ResponseWriter, r *http.Request) (userID int64, lists [][]transaction.Transaction, ok bool) {
	userID = auth.MustUserID(r.Context())
	lists, err := scopeTransactions(r.Context(), d.portfolios, d.transactions, userID, r.PathValue("id"))
	if err != nil {
		portfolio.WriteLookupError(w, r, err)
		return 0, nil, false
	}
	return userID, lists, true
}

// scopeID is scopePortfolioID for the signed-in user. It writes the error
// and is false when it cannot.
func (d *deps) scopeID(w http.ResponseWriter, r *http.Request) (userID int64, portfolioID *int64, ok bool) {
	userID = auth.MustUserID(r.Context())
	portfolioID, err := scopePortfolioID(r.Context(), d.portfolios, userID, r.PathValue("id"))
	if err != nil {
		portfolio.WriteLookupError(w, r, err)
		return 0, nil, false
	}
	return userID, portfolioID, true
}

// flatByDate concatenates per-portfolio lists into one, ordered by date. The
// sort is stable, so each portfolio's SortForApply order holds within a day.
func flatByDate(lists [][]transaction.Transaction) []transaction.Transaction {
	var out []transaction.Transaction
	for _, l := range lists {
		out = append(out, l...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}
