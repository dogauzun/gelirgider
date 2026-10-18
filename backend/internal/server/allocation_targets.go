package server

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/dogauzun/gelirgider/backend/internal/allocation"
	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerAllocationTargetRoutes wires GET and PUT
// /api/portfolios/{id}/allocation-targets (ADR 0028).
func registerAllocationTargetRoutes(mux *http.ServeMux, d *deps) {
	h := &allocationTargetHandler{deps: d}
	mux.HandleFunc("GET /api/portfolios/{id}/allocation-targets", h.get)
	mux.HandleFunc("PUT /api/portfolios/{id}/allocation-targets", h.put)
}

type allocationTargetHandler struct{ *deps }

// allocationTarget is one class's weight in percent, a decimal string.
type allocationTarget struct {
	AssetClass string `json:"asset_class"`
	Weight     string `json:"weight"`
}

// allocationTargetsBody is the PUT body and the response of both routes.
type allocationTargetsBody struct {
	Targets []allocationTarget `json:"targets"`
}

// scopePortfolioID resolves the {id} path value into the set's portfolio: nil
// for "all", else the id of a portfolio userID owns. A non-number, or another
// user's portfolio, is portfolio.ErrNotFound.
func (h *allocationTargetHandler) scopePortfolioID(r *http.Request, userID int64) (*int64, error) {
	id := r.PathValue("id")
	if id == "all" {
		return nil, nil
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, portfolio.ErrNotFound
	}
	if _, err := h.portfolios.Get(r.Context(), userID, n); err != nil {
		return nil, err
	}
	return &n, nil
}

// parseAllocationTargets validates req's set: known classes, each once, weights
// from 0 to 100, summing to 100. An empty set is valid. Errors are
// "<field>: <message>", as the other routes write them.
func parseAllocationTargets(req allocationTargetsBody) ([]allocation.Target, error) {
	hundred := decimal.NewFromInt(100)
	out := make([]allocation.Target, 0, len(req.Targets))
	sum := decimal.Zero
	for _, t := range req.Targets {
		if !slices.Contains(allocation.Classes, t.AssetClass) {
			return nil, errors.New("asset_class: unknown class")
		}
		if slices.ContainsFunc(out, func(o allocation.Target) bool { return o.AssetClass == t.AssetClass }) {
			return nil, errors.New("asset_class: duplicate")
		}
		w, err := transaction.ParseNonNegative(t.Weight)
		if err != nil || w.GreaterThan(hundred) {
			return nil, errors.New("weight: must be between 0 and 100")
		}
		out = append(out, allocation.Target{AssetClass: t.AssetClass, Weight: w})
		sum = sum.Add(w)
	}
	if len(out) > 0 && !sum.Equal(hundred) {
		return nil, errors.New("weight: must sum to 100")
	}
	return out, nil
}

func toAllocationTargetsBody(ts []allocation.Target) allocationTargetsBody {
	out := allocationTargetsBody{Targets: make([]allocationTarget, 0, len(ts))}
	for _, t := range ts {
		out.Targets = append(out.Targets, allocationTarget{AssetClass: t.AssetClass, Weight: t.Weight.String()})
	}
	return out
}

// get answers the stored set of the scope.
//
//	@Summary	Read a portfolio's allocation targets
//	@Description	The target weight of each asset class (equity, fund, commodity, crypto, deposit, cash), in percent, ordered by that class order. targets is an empty array when none are set. `all` is the set of the all-portfolios view and is stored apart from every portfolio's set.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{object}	server.allocationTargetsBody
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/allocation-targets [get]
func (h *allocationTargetHandler) get(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	portfolioID, err := h.scopePortfolioID(r, userID)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	ts, err := h.allocationTargets.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toAllocationTargetsBody(ts))
}

// put replaces the stored set of the scope.
//
//	@Summary	Replace a portfolio's allocation targets
//	@Description	Replaces the whole set in one SQL transaction and answers the stored set. An empty targets array clears it. A set holds each class at most once, and its weights sum to exactly 100.
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string							true	"numeric id or `all`"
//	@Param		body	body		server.allocationTargetsBody	true	"asset_class one of equity, fund, commodity, crypto, deposit, cash; weight a decimal from 0 to 100"
//	@Success	200		{object}	server.allocationTargetsBody
//	@Failure	400		{object}	object{error=string}	"invalid request body; asset_class: unknown class; asset_class: duplicate; weight: must be between 0 and 100; weight: must sum to 100"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/allocation-targets [put]
func (h *allocationTargetHandler) put(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	portfolioID, err := h.scopePortfolioID(r, userID)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	var req allocationTargetsBody
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	ts, err := parseAllocationTargets(req)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = h.allocationTargets.Replace(r.Context(), userID, portfolioID, ts)
	// A portfolio deleted since the lookup trips the foreign key.
	if db.IsForeignKeyViolation(err) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err == nil {
		ts, err = h.allocationTargets.List(r.Context(), userID, portfolioID)
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toAllocationTargetsBody(ts))
}
