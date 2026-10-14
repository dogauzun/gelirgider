package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// registerCashGapRoutes wires GET and POST /api/portfolios/{id}/cash-gaps:
// the days a portfolio's cash goes below zero for lack of
// recorded deposits, and writing those deposits on the user's confirm.
func registerCashGapRoutes(mux *http.ServeMux, d *deps) {
	h := &cashGapHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/cash-gaps", h.list)
	mux.HandleFunc("POST /api/portfolios/{id}/cash-gaps", h.fill)
}

type cashGapHandler struct{ *deps }

// cashGap is one day a portfolio's cash in currency reached a new low below
// zero; amount is the deposit that keeps it at the previous low, rounded up
// to the 9 fraction digits a deposit takes.
type cashGap struct {
	PortfolioID int64  `json:"portfolio_id"`
	Date        string `json:"date"`
	Currency    string `json:"currency"`
	Amount      string `json:"amount"`
}

// cashGapsRequest is the POST body: the note every deposit gets.
type cashGapsRequest struct {
	Note string `json:"note"`
}

// gaps runs the gap walk once per portfolio in scope, so cash never moves
// between portfolios. Ordered by portfolio, date, currency, never nil. A
// portfolio with gaps has transactions, which carry its id.
func (h *cashGapHandler) gaps(r *http.Request, userID int64) ([]cashGap, error) {
	lists, err := scopeTransactions(r.Context(), h.portfolios, h.transactions, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	out := []cashGap{}
	for _, txs := range lists {
		for _, g := range holdings.CashGaps(txs) {
			out = append(out, cashGap{PortfolioID: txs[0].PortfolioID, Date: g.Date, Currency: g.Currency, Amount: g.Amount.String()})
		}
	}
	return out, nil
}

// list returns the cash gaps of the scope.
//
//	@Summary	Cash gaps of a portfolio
//	@Description	Per portfolio and currency, every day the cash balance reaches a new low below zero, with the extra shortfall that day: the deposits that, recorded, keep the cash from going negative (ADR 0011). Ordered by portfolio, date, currency; empty when the cash never goes below zero.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{array}		server.cashGap
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/cash-gaps [get]
func (h *cashGapHandler) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	gaps, err := h.gaps(r, userID)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, gaps)
}

// fill recomputes the scope's cash gaps and records each as a deposit in
// its own portfolio, all in one database transaction. The client sends no
// amounts, so a stale list cannot be written.
//
// ponytail: two concurrent POSTs both see the gaps and both write them;
// the client sends one per confirm. Recompute inside the write transaction
// if that ever matters.
//
//	@Summary	Record the cash gaps as deposits
//	@Description	Recomputes the cash gaps of the scope and inserts one deposit per gap, dated the gap's day, in its currency and portfolio, carrying the note. Returns the new transactions' ids; empty, with nothing inserted, when there are no gaps.
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string					true	"numeric id or `all`"
//	@Param		body	body		server.cashGapsRequest	true	"note for every deposit, validated like a transaction's note when there is a gap to fill"
//	@Success	200		{array}		integer					"created transaction ids"
//	@Failure	400		{object}	object{error=string}	"invalid request body; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/cash-gaps [post]
func (h *cashGapHandler) fill(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	var req cashGapsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	gaps, err := h.gaps(r, userID)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	// Each deposit goes through the transaction form's validation, the
	// note included, as a statement import row does.
	deposits := make([]transaction.Transaction, 0, len(gaps))
	for _, g := range gaps {
		t, _, err := transaction.FromRequest(r.Context(), h.instruments, transaction.Request{Type: transaction.TypeDeposit, Date: g.Date, Amount: &g.Amount, Currency: &g.Currency, Note: &req.Note})
		if errors.Is(err, transaction.ErrLookup) {
			auth.WriteInternalError(w, r, err)
			return
		}
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		t.PortfolioID = g.PortfolioID
		deposits = append(deposits, t)
	}
	ids, err := h.transactions.CreateAll(r.Context(), userID, deposits)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, ids)
}
