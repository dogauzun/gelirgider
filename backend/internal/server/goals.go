package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/goal"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerGoalRoutes wires GET, PUT and DELETE /api/portfolios/{id}/goal and
// GET /api/portfolios/{id}/projection.
func registerGoalRoutes(mux *http.ServeMux, d *deps) {
	h := &goalHandler{deps: d}
	mux.HandleFunc("GET /api/portfolios/{id}/goal", h.get)
	mux.HandleFunc("PUT /api/portfolios/{id}/goal", h.put)
	mux.HandleFunc("DELETE /api/portfolios/{id}/goal", h.delete)
	mux.HandleFunc("GET /api/portfolios/{id}/projection", h.projection)
}

type goalHandler struct{ *deps }

// goalBody is the PUT body and the response of GET and PUT.
type goalBody struct {
	Amount     string `json:"amount"`
	Unit       string `json:"unit"`
	TargetDate string `json:"target_date"`
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
	// YYYY-MM-DD sorts lexically, so comparing the YYYY-MM prefixes compares the months.
	today := price.Today()
	if !price.ValidDate(req.TargetDate) || req.TargetDate[:7] <= today[:7] {
		return goal.Goal{}, errors.New("target_date: must be in a later month than today")
	}
	// The same calendar month goal.MaxYears ahead is the latest: the fan has one entry per month and compounds, so a far target answers megabytes or overflows float64.
	year, _ := strconv.Atoi(today[:4])
	if req.TargetDate[:7] > strconv.Itoa(year+goal.MaxYears)+today[4:7] {
		return goal.Goal{}, fmt.Errorf("target_date: must be at most %d years after today", goal.MaxYears)
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
	userID, portfolioID, ok := h.scopeID(w, r)
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
//	@Description	Replaces the goal of the scope and answers the stored goal. unit is nominal (the reporting currency), real (the amount at the prices of the day the goal is set, only with a TRY reporting currency) or usd (only with a non-USD reporting currency). target_date is YYYY-MM-DD, in a later calendar month than today and at most 50 years ahead (Europe/Istanbul).
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string			true	"numeric id or `all`"
//	@Param		body	body		server.goalBody	true	"amount a positive decimal; unit one of nominal, real, usd"
//	@Success	200		{object}	server.goalBody
//	@Failure	400		{object}	object{error=string}	"invalid request body; amount: must be a positive number; unit: must be one of nominal, real, usd; unit: real needs a TRY reporting currency; unit: usd needs a non-USD reporting currency; target_date: must be in a later month than today; target_date: must be at most 50 years after today"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/goal [put]
func (h *goalHandler) put(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := h.scopeID(w, r)
	if !ok {
		return
	}
	var req goalBody
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	reporting, err := h.users.ReportingCurrency(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	g, err := parseGoal(req, reporting)
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
	userID, portfolioID, ok := h.scopeID(w, r)
	if !ok {
		return
	}
	if err := h.goals.Delete(r.Context(), userID, portfolioID); err != nil {
		writeGoalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const (
	projectionReturnMonths = 36
	projectionFlowMonths   = 12
)

type projectionMonth struct {
	Month string  `json:"month"`
	Low   float64 `json:"low"`
	Mid   float64 `json:"mid"`
	High  float64 `json:"high"`
}

// projectionResponse is the fan of the goal's scope: the 25th, 50th and 75th
// percentile paths per month up to the target month, in the goal's unit.
// Floats, because a projection is an estimate, not money.
type projectionResponse struct {
	Unit       string            `json:"unit"`
	Currency   string            `json:"currency"`
	Amount     string            `json:"amount"`
	TargetDate string            `json:"target_date"`
	Months     []projectionMonth `json:"months"`
	// ReachedAt is the first month of the median path at or above the
	// amount, the current month when today's value already is at the amount,
	// null when none is.
	ReachedAt *string `json:"reached_at"`
	// MonthlyGap is the smallest extra monthly flow, in the reporting
	// currency and rounded up, that lets the median path reach the amount by
	// the target month, null when it reaches it anyway.
	MonthlyGap    *float64 `json:"monthly_gap"`
	Progress      float64  `json:"progress"`
	HistoryMonths int      `json:"history_months"`
}

// projection answers the projection fan of the scope's goal.
//
//	@Summary	Projection of a portfolio's goal
//	@Description	Projects the scope's value month by month from the next month through the target month, with the 25th, 50th and 75th percentile of the monthly time-weighted returns of the last 36 full calendar months (nearest rank) and the median net external flow of the last 12 full months. Month values are in the goal's unit: real is the reporting currency at the prices of the day the goal was set: a nominal value is multiplied by the TUFE level of that day over today's level and divided by the TUFE rate of the last 12 months compounded for each month ahead, usd by today's USD rate. months is empty, with history_months counting the usable months, when fewer than 6 full months have a return, and empty with history_months 0 when the scope has no history or the unit lacks its TUFE level or USD rate. reached_at is the first month of the median path at or above the amount, the current month when today's value already is at the amount; monthly_gap is the extra monthly flow in the reporting currency, rounded up, that the median path needs to reach the amount by the target month, null when reached_at is set. progress is today's value in the unit divided by the amount.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{object}	server.projectionResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found; goal not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/projection [get]
func (h *goalHandler) projection(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := h.scopeID(w, r)
	if !ok {
		return
	}
	g, err := h.goals.Get(r.Context(), userID, portfolioID)
	if err != nil {
		writeGoalError(w, r, err)
		return
	}
	reporting, err := h.users.ReportingCurrency(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	resp := projectionResponse{Unit: g.Unit, Currency: reporting, Amount: g.Amount.String(), TargetDate: g.TargetDate, Months: []projectionMonth{}}
	in, found, err := h.projectionInput(r.Context(), userID, r.PathValue("id"), g, reporting)
	if err != nil {
		portfolio.WriteLookupError(w, r, err)
		return
	}
	if found {
		p := goal.Project(in)
		for _, m := range p.Months {
			resp.Months = append(resp.Months, projectionMonth(m))
		}
		resp.ReachedAt, resp.MonthlyGap, resp.Progress, resp.HistoryMonths = p.ReachedAt, p.MonthlyGap, p.Progress, p.HistoryMonths
	}
	auth.WriteJSON(w, http.StatusOK, resp)
}

// projectionInput builds the input of goal.Project from the history sweep
// of the scope. found is false when there is nothing to project from: no
// history, or a unit whose TUFE level or USD rate is missing.
func (h *goalHandler) projectionInput(ctx context.Context, userID int64, id string, g goal.Goal, reporting string) (in goal.ProjectionInput, found bool, err error) {
	lists, err := scopeTransactions(ctx, h.portfolios, h.transactions, userID, id)
	if err != nil {
		return in, false, err
	}
	txs := flatByDate(lists)
	if len(txs) == 0 {
		return in, false, nil
	}
	today := istanbulToday()
	from, err := defaultHistoryFrom(txs, today)
	if err != nil || from.After(today) {
		return in, false, err
	}
	c := price.Yesterday(g.CreatedAt).AddDate(0, 0, 1) // the Istanbul date of created_at
	in = goal.ProjectionInput{Amount: g.Amount.InexactFloat64(), Unit: g.Unit}
	if found, err = h.unitRate(ctx, &in, reporting, today, time.Date(c.Year(), c.Month(), c.Day(), 0, 0, 0, 0, time.UTC)); !found || err != nil {
		return in, false, err
	}
	s, err := (&historyHandler{h.deps}).sweep(ctx, userID, txs, from, today, sweepOpts{})
	if err != nil {
		return in, false, err
	}
	var flows []float64
	var twrs []*float64
	for _, b := range s.buckets(len("2006-01")) {
		if b.Partial {
			continue
		}
		flows = append(flows, b.flow)
		var twr *float64
		if b.Returns.TWR != nil {
			v, err := strconv.ParseFloat(*b.Returns.TWR, 64)
			if err != nil {
				return in, false, err
			}
			twr = &v
		}
		twrs = append(twrs, twr)
	}
	for _, twr := range lastN(twrs, projectionReturnMonths) {
		if twr != nil {
			in.MonthlyReturns = append(in.MonthlyReturns, *twr)
		}
	}
	in.MonthlyFlows = lastN(flows, projectionFlowMonths)
	in.Value = s.values[len(s.values)-1]
	thisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	in.FirstMonth = thisMonth.AddDate(0, 1, 0).Format("2006-01")
	target, err := time.Parse(time.DateOnly, g.TargetDate)
	if err != nil {
		return in, false, err
	}
	// shortcut: a stored goal past the cap projects only to the cap; re-saving the goal fixes the date
	in.Months = min((target.Year()-today.Year())*12+int(target.Month()-today.Month()), goal.MaxYears*12)
	return in, true, nil
}

// unitRate sets the CPI rate or the USD rate the goal's unit divides by.
// found is false when the level or rate is missing, or the unit does not fit
// the reporting currency. The 409 on a currency change keeps a stored goal in
// step with the currency, but this does not rely on it: a row stored before
// that rule, or racing a currency change, can still be out of step.
func (h *goalHandler) unitRate(ctx context.Context, in *goal.ProjectionInput, reporting string, today, created time.Time) (found bool, err error) {
	switch in.Unit {
	case "real":
		if reporting != "TRY" {
			return false, nil
		}
		levels, _, err := benchmarkLevels(ctx, h.benchmarks, benchmark.CodeTUFE, today.AddDate(-1, 0, 0), today)
		if err != nil || len(levels) == 0 || levels[0] <= 0 || levels[len(levels)-1] <= 0 {
			return false, err
		}
		in.MonthlyCPIRate = math.Pow(levels[len(levels)-1]/levels[0], 1.0/12) - 1
		anchors, _, err := benchmarkLevels(ctx, h.benchmarks, benchmark.CodeTUFE, created, today)
		first := slices.IndexFunc(anchors, func(l float64) bool { return l > 0 })
		if err != nil || first < 0 {
			return false, err
		}
		in.RealBase = anchors[first] / anchors[len(anchors)-1]
	case "usd":
		rates, err := h.fx.Latest(ctx, []string{"USD", reporting}, today.Format(time.DateOnly))
		if err != nil {
			return false, err
		}
		byCurrency := make(map[string]decimal.Decimal, len(rates))
		for c, r := range rates {
			byCurrency[c] = r.Rate
		}
		factor, ok := holdings.Factor(byCurrency, "USD", reporting)
		if !ok || !factor.IsPositive() {
			return false, nil
		}
		in.USDRate = factor.InexactFloat64()
	}
	return true, nil
}

func lastN[T any](xs []T, n int) []T {
	return xs[max(0, len(xs)-n):]
}
