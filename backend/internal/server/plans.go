package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/plan"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// registerPlanRoutes wires POST and GET /api/portfolios/{id}/plans.
func registerPlanRoutes(mux *http.ServeMux, d *deps) {
	h := &planHandler{deps: d}
	mux.HandleFunc("POST /api/portfolios/{id}/plans", h.create)
	mux.HandleFunc("GET /api/portfolios/{id}/plans", h.list)
}

type planHandler struct{ *deps }

// planRequest is the POST body. amount is a decimal string. instrument_id,
// currency, end_date, fund and note may be omitted; a plan with no instrument
// is a cash deposit plan and takes its currency from the body.
type planRequest struct {
	InstrumentID *int64 `json:"instrument_id"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	DayOfMonth   int    `json:"day_of_month"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	// Fund books the amount as a cash deposit before each buy.
	Fund bool   `json:"fund"`
	Note string `json:"note"`
}

type planResponse struct {
	ID           int64   `json:"id"`
	PortfolioID  int64   `json:"portfolio_id"`
	InstrumentID *int64  `json:"instrument_id"`
	Symbol       *string `json:"symbol"`
	Market       *string `json:"market"`
	Currency     string  `json:"currency"`
	Amount       string  `json:"amount"`
	DayOfMonth   int     `json:"day_of_month"`
	StartDate    string  `json:"start_date"`
	EndDate      *string `json:"end_date"`
	NextDate     string  `json:"next_date"`
	Fund         bool    `json:"fund"`
	Note         string  `json:"note"`
	// Status is ended (the next occurrence is past the end date), else due
	// (the next occurrence is today or past), else active.
	Status string `json:"status"`
}

// toPlanResponse builds the response of p on today. in is p's instrument, nil
// for a cash deposit plan.
func toPlanResponse(p plan.Plan, in *instrument.Instrument, today string) planResponse {
	status := "active"
	switch {
	case p.Ended():
		status = "ended"
	case p.Due(today):
		status = "due"
	}
	out := planResponse{
		ID: p.ID, PortfolioID: p.PortfolioID, InstrumentID: p.InstrumentID, Currency: p.Currency,
		Amount: p.Amount.String(), DayOfMonth: p.DayOfMonth, StartDate: p.StartDate, EndDate: p.EndDate,
		NextDate: p.NextDate, Fund: p.Fund, Note: p.Note, Status: status,
	}
	if in != nil {
		out.Symbol, out.Market = &in.Symbol, &in.Market
	}
	return out
}

// parsePlanRequest validates req in the documented order and returns the plan
// to store, minus its owner. in is the instrument req names, nil when it names
// none. Errors are "<field>: <message>", as the transaction routes write them.
func parsePlanRequest(req planRequest, in *instrument.Instrument) (plan.Plan, error) {
	p := plan.Plan{InstrumentID: req.InstrumentID, Currency: req.Currency, DayOfMonth: req.DayOfMonth, Fund: req.Fund}
	switch {
	case in != nil && req.Currency != "" && req.Currency != in.Currency:
		return p, fmt.Errorf("currency: must match instrument currency (%s)", in.Currency)
	case in != nil:
		p.Currency = in.Currency
	case !currency.IsValid(req.Currency):
		return p, fmt.Errorf("currency: must be one of %s", strings.Join(currency.Valid, ", "))
	}
	var err error
	if p.Amount, err = transaction.ParseNonNegative(req.Amount); err != nil {
		return p, fmt.Errorf("amount: %w", err)
	}
	if !p.Amount.IsPositive() {
		return p, errors.New("amount: must be a positive number")
	}
	if req.DayOfMonth < 1 || req.DayOfMonth > 31 {
		return p, errors.New("day_of_month: must be between 1 and 31")
	}
	if err = checkPlanStart(req.StartDate); err != nil {
		return p, err
	}
	p.StartDate = req.StartDate
	if req.EndDate != "" {
		if !price.ValidDate(req.EndDate) {
			return p, errors.New("end_date: must be YYYY-MM-DD")
		}
		if req.EndDate < req.StartDate {
			return p, errors.New("end_date: must be on or after start_date")
		}
		p.EndDate = &req.EndDate
	}
	if p.Note, err = transaction.ParseNote(req.Note); err != nil {
		return p, err
	}
	p.NextDate = plan.FirstOnOrAfter(p.StartDate, p.DayOfMonth)
	return p, nil
}

// checkPlanStart checks a plan's start date: a real date, not before MinDate,
// at most a year after today.
func checkPlanStart(start string) error {
	if !price.ValidDate(start) {
		return errors.New("start_date: must be YYYY-MM-DD")
	}
	// A start in the future is fine, so only the lower bound is checked.
	today := price.Today()
	if err := price.CheckDate(min(start, today)); err != nil {
		return fmt.Errorf("start_date: %w", err)
	}
	t, _ := time.Parse(time.DateOnly, today)
	if start > t.AddDate(1, 0, 0).Format(time.DateOnly) {
		return errors.New("start_date: must be at most 1 year after today")
	}
	return nil
}

// create stores a saving plan.
//
//	@Summary	Create a saving plan
//	@Description	A monthly plan: a buy of the instrument, or with no instrument a cash deposit, of amount on day_of_month from start_date to end_date (open-ended when omitted). A day past the month's end falls on its last day. next_date, the first occurrence on or after start_date, is where the plan stands. A buy plan's amount is in the instrument's currency; a deposit plan takes currency from the body. With fund true the amount is booked as a cash deposit before each buy. Decimals are strings with at most 15 integer and 9 fraction digits.
//	@Tags		plans
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"portfolio id"
//	@Param		body	body		server.planRequest	true	"instrument_id of a non-DEPOSIT instrument or none; currency required without an instrument; amount > 0; day_of_month 1 to 31; start_date from 1990-01-01 to one year after today; end_date on or after start_date or omitted"
//	@Success	201		{object}	server.planResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; instrument_id: unknown instrument; currency: must match instrument currency (<currency>); currency: must be one of TRY, USD, EUR; amount: required; amount: must be a non-negative number with at most 15 integer and 9 fraction digits; amount: must be a positive number; day_of_month: must be between 1 and 31; start_date: must be YYYY-MM-DD; start_date: must be on or after 1990-01-01; start_date: must be at most 1 year after today; end_date: must be YYYY-MM-DD; end_date: must be on or after start_date; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/plans [post]
func (h *planHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	var req planRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	var in *instrument.Instrument
	if req.InstrumentID != nil {
		got, err := h.instruments.Get(r.Context(), *req.InstrumentID)
		if errors.Is(err, instrument.ErrNotFound) || (err == nil && got.Market == instrument.MarketDeposit) {
			auth.WriteError(w, http.StatusBadRequest, "instrument_id: unknown instrument")
			return
		}
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		in = &got
	}
	p, err := parsePlanRequest(req, in)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.UserID, p.PortfolioID = userID, portfolioID
	p, err = h.plans.Insert(r.Context(), p)
	if db.IsForeignKeyViolation(err) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusCreated, toPlanResponse(p, in, price.Today()))
}

// list returns the portfolio's saving plans.
//
//	@Summary	List a portfolio's saving plans
//	@Description	Soonest next_date first. status is ended when the next occurrence is past the end date, else due from the next occurrence's date, else active. symbol and market are null for a cash deposit plan.
//	@Tags		plans
//	@Produce	json
//	@Param		id	path		int	true	"portfolio id"
//	@Success	200	{array}		server.planResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/plans [get]
func (h *planHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	ps, err := h.plans.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	var ids []int64
	for _, p := range ps {
		if p.InstrumentID != nil {
			ids = append(ids, *p.InstrumentID)
		}
	}
	ins, err := h.instruments.ByIDs(r.Context(), ids)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	today := price.Today()
	out := make([]planResponse, 0, len(ps))
	for _, p := range ps {
		var in *instrument.Instrument
		if p.InstrumentID != nil {
			got := ins[*p.InstrumentID]
			in = &got
		}
		out = append(out, toPlanResponse(p, in, today))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}
