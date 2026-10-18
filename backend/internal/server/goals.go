package server

import (
	"errors"
	"net/http"
	"slices"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/goal"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// registerGoalRoutes wires GET, PUT and DELETE /api/portfolios/{id}/goal.
func registerGoalRoutes(mux *http.ServeMux, d *deps) {
	h := &goalHandler{deps: d}
	mux.HandleFunc("GET /api/portfolios/{id}/goal", h.get)
	mux.HandleFunc("PUT /api/portfolios/{id}/goal", h.put)
	mux.HandleFunc("DELETE /api/portfolios/{id}/goal", h.delete)
}

type goalHandler struct{ *deps }

// goalBody is the PUT body and the response of GET and PUT.
type goalBody struct {
	Amount     string `json:"amount"`
	Unit       string `json:"unit"`
	TargetDate string `json:"target_date"`
}

// scope resolves the portfolio of the request. It writes the error and is
// false when it cannot.
func (h *goalHandler) scope(w http.ResponseWriter, r *http.Request) (userID int64, portfolioID *int64, ok bool) {
	userID = auth.MustUserID(r.Context())
	portfolioID, err := h.scopePortfolioID(r, userID)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return 0, nil, false
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return 0, nil, false
	}
	return userID, portfolioID, true
}

// writeGoalError answers the error of a goal lookup or removal: 404 for a
// scope without a goal, else 500.
func writeGoalError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, goal.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "goal not found")
		return
	}
	auth.WriteInternalError(w, r, err)
}

func toGoalBody(g goal.Goal) goalBody {
	return goalBody{Amount: g.Amount.String(), Unit: g.Unit, TargetDate: g.TargetDate}
}

// parseGoal validates req against the user's reporting currency: real needs
// TRY, usd needs a currency other than USD. Errors are "<field>: <message>",
// as the other routes write them.
func parseGoal(req goalBody, reportingCurrency string) (goal.Goal, error) {
	amount, err := transaction.ParseNonNegative(req.Amount)
	if err != nil || !amount.IsPositive() {
		return goal.Goal{}, errors.New("amount: must be a positive number")
	}
	if !slices.Contains(goal.Units, req.Unit) {
		return goal.Goal{}, errors.New("unit: must be one of nominal, real, usd")
	}
	if req.Unit == "real" && reportingCurrency != "TRY" {
		return goal.Goal{}, errors.New("unit: real needs a TRY reporting currency")
	}
	if req.Unit == "usd" && reportingCurrency == "USD" {
		return goal.Goal{}, errors.New("unit: usd needs a non-USD reporting currency")
	}
	// YYYY-MM-DD sorts lexically, so a string compare is a date compare.
	if !price.ValidDate(req.TargetDate) || req.TargetDate <= price.Today() {
		return goal.Goal{}, errors.New("target_date: must be after today")
	}
	return goal.Goal{Amount: amount, Unit: req.Unit, TargetDate: req.TargetDate}, nil
}

// get answers the goal of the scope.
//
//	@Summary	Read a portfolio's goal
//	@Description	The savings goal of the portfolio: amount to reach in unit by target_date. `all` is the goal of the all-portfolios view and is stored apart from every portfolio's goal.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{object}	server.goalBody
//	@Failure	404	{object}	object{error=string}	"portfolio not found; goal not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/goal [get]
func (h *goalHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := h.scope(w, r)
	if !ok {
		return
	}
	g, err := h.goals.Get(r.Context(), userID, portfolioID)
	if err != nil {
		writeGoalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toGoalBody(g))
}

// put replaces the goal of the scope.
//
//	@Summary	Set a portfolio's goal
//	@Description	Replaces the goal of the scope and answers the stored goal. unit is nominal (the reporting currency), real (inflation-adjusted, only with a TRY reporting currency) or usd (only with a non-USD reporting currency). target_date is YYYY-MM-DD and after today (Europe/Istanbul).
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string			true	"numeric id or `all`"
//	@Param		body	body		server.goalBody	true	"amount a positive decimal; unit one of nominal, real, usd"
//	@Success	200		{object}	server.goalBody
//	@Failure	400		{object}	object{error=string}	"invalid request body; amount: must be a positive number; unit: must be one of nominal, real, usd; unit: real needs a TRY reporting currency; unit: usd needs a non-USD reporting currency; target_date: must be after today"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/goal [put]
func (h *goalHandler) put(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := h.scope(w, r)
	if !ok {
		return
	}
	var req goalBody
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	usr, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	g, err := parseGoal(req, usr.ReportingCurrency)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	g, err = h.goals.Set(r.Context(), userID, portfolioID, g)
	// A portfolio deleted since the lookup trips the foreign key.
	if db.IsForeignKeyViolation(err) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toGoalBody(g))
}

// delete removes the goal of the scope.
//
//	@Summary	Delete a portfolio's goal
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path	string	true	"numeric id or `all`"
//	@Success	204	"deleted"
//	@Failure	404	{object}	object{error=string}	"portfolio not found; goal not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/goal [delete]
func (h *goalHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := h.scope(w, r)
	if !ok {
		return
	}
	if err := h.goals.Delete(r.Context(), userID, portfolioID); err != nil {
		writeGoalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
