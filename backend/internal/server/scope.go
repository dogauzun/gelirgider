package server

import (
	"context"
	"sort"
	"strconv"

	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// scopeTransactions resolves the {id} path value of a portfolio route into
// one transaction list per portfolio in scope: the one portfolio
// for a numeric id, every portfolio of userID for "all". An id that is not
// a number, or names another user's portfolio, is portfolio.ErrNotFound, as
// it is on the single-portfolio routes today.
func scopeTransactions(ctx context.Context, portfolios *portfolio.Repo, transactions *transaction.Repo, userID int64, id string) ([][]transaction.Transaction, error) {
	var ids []int64
	if id == "all" {
		ps, err := portfolios.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
	} else {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, portfolio.ErrNotFound
		}
		if _, err := portfolios.Get(ctx, userID, n); err != nil {
			return nil, err
		}
		ids = []int64{n}
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
