package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/plan"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerPlanRoutes wires POST and GET /api/portfolios/{id}/plans, GET
// .../plans/due, PUT and DELETE /api/plans/{id}, and POST /api/plans/{id}/book
// and /skip. onDemandFetch is the transaction routes' price and FX hook.
func registerPlanRoutes(mux *http.ServeMux, d *deps, onDemandFetch transaction.OnDemandFetch) {
	h := &planHandler{deps: d, onDemandFetch: onDemandFetch}
	mux.HandleFunc("POST /api/portfolios/{id}/plans", h.create)
	mux.HandleFunc("GET /api/portfolios/{id}/plans", h.list)
	mux.HandleFunc("GET /api/portfolios/{id}/plans/due", h.due)
	mux.HandleFunc("PUT /api/plans/{id}", h.update)
	mux.HandleFunc("DELETE /api/plans/{id}", h.delete)
	mux.HandleFunc("POST /api/plans/{id}/book", h.book)
	mux.HandleFunc("POST /api/plans/{id}/skip", h.skip)
}

type planHandler struct {
	*deps
	onDemandFetch transaction.OnDemandFetch
}

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
//	@Failure	400		{object}	object{error=string}	"invalid request body; instrument_id: unknown instrument; currency: must match instrument currency (<currency>); currency: must be one of TRY, USD, EUR; amount: required; amount: must have at most 15 integer and 9 fraction digits; amount: must be a non-negative number with at most 15 integer and 9 fraction digits; amount: must be a positive number; day_of_month: must be between 1 and 31; start_date: must be YYYY-MM-DD; start_date: must be on or after 1990-01-01; start_date: must be at most 1 year after today; end_date: must be YYYY-MM-DD; end_date: must be on or after start_date; note: must be at most 500 characters; note: must not contain control characters"
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

// planEditRequest is the PUT body: the editable fields of a plan, every one
// set from the body. An omitted or empty end_date clears the stored one.
type planEditRequest struct {
	Amount     string `json:"amount"`
	DayOfMonth int    `json:"day_of_month"`
	EndDate    string `json:"end_date"`
	Fund       bool   `json:"fund"`
	Note       string `json:"note"`
}

// planID returns the {id} path value, ok false when it is not a number.
func planID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// update edits a saving plan.
//
//	@Summary	Edit a saving plan
//	@Description	Sets amount, day_of_month, end_date, fund and note from the body; an omitted or empty end_date clears the stored one. The instrument, currency and start_date are fixed: delete the plan and create it again to change them. next_date becomes the first occurrence of the new day on or after the old next_date.
//	@Tags		plans
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"plan id"
//	@Param		body	body		server.planEditRequest	true	"amount > 0; day_of_month 1 to 31; end_date on or after the plan's start_date or omitted"
//	@Success	200		{object}	server.planResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; amount: required; amount: must have at most 15 integer and 9 fraction digits; amount: must be a non-negative number with at most 15 integer and 9 fraction digits; amount: must be a positive number; day_of_month: must be between 1 and 31; end_date: must be YYYY-MM-DD; end_date: must be on or after start_date; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"plan not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/plans/{id} [put]
func (h *planHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return
	}
	var req planEditRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	userID := auth.MustUserID(r.Context())
	old, err := h.plans.Get(r.Context(), userID, id)
	if errors.Is(err, plan.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	p, err := parsePlanRequest(planRequest{
		InstrumentID: old.InstrumentID, Currency: old.Currency, Amount: req.Amount, DayOfMonth: req.DayOfMonth,
		StartDate: old.StartDate, EndDate: req.EndDate, Fund: req.Fund, Note: req.Note,
	}, nil)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID, p.UserID, p.PortfolioID, p.CreatedAt = old.ID, userID, old.PortfolioID, old.CreatedAt
	p.NextDate = plan.FirstOnOrAfter(old.NextDate, p.DayOfMonth)
	err = h.plans.Update(r.Context(), p)
	if errors.Is(err, plan.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	var in *instrument.Instrument
	if p.InstrumentID != nil {
		got, err := h.instruments.Get(r.Context(), *p.InstrumentID)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		in = &got
	}
	auth.WriteJSON(w, http.StatusOK, toPlanResponse(p, in, price.Today()))
}

// delete removes a saving plan. The transactions it booked stay.
//
//	@Summary	Delete a saving plan
//	@Description	Removes the plan. The transactions and deposits it booked are left as they are.
//	@Tags		plans
//	@Param		id	path	int	true	"plan id"
//	@Success	204
//	@Failure	404	{object}	object{error=string}	"plan not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/plans/{id} [delete]
func (h *planHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return
	}
	err := h.plans.Delete(r.Context(), auth.MustUserID(r.Context()), id)
	if errors.Is(err, plan.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// planConflict is the 409 message of a plan that cannot be booked or skipped now.
type planConflict string

func (c planConflict) Error() string { return string(c) }

// dueConflict returns why p cannot be booked or skipped on today, "" when it can.
func dueConflict(p plan.Plan, today string) string {
	switch {
	case p.Ended():
		return "plan has ended"
	case p.NextDate > today:
		return "plan is not due yet"
	}
	return ""
}

// planClose returns the close on or before the next_date of p, which has an
// instrument; false when there is none.
func (h *planHandler) planClose(ctx context.Context, p plan.Plan) (price.Price, bool, error) {
	closes, err := holdings.LatestCloses(ctx, h.priceSources(), p.UserID, []int64{*p.InstrumentID}, p.NextDate)
	c, ok := closes[*p.InstrumentID]
	return c, ok, err
}

// duePlanResponse is a due plan with the booking it suggests. instrument_id,
// symbol, market, price, price_date and quantity are null for a deposit plan;
// price, price_date, quantity and booked_amount are null for a buy plan with
// no close.
type duePlanResponse struct {
	PlanID       int64   `json:"plan_id"`
	InstrumentID *int64  `json:"instrument_id"`
	Symbol       *string `json:"symbol"`
	Market       *string `json:"market"`
	Currency     string  `json:"currency"`
	Amount       string  `json:"amount"`
	Date         string  `json:"date"`
	Missed       int     `json:"missed"`
	Price        *string `json:"price"`
	PriceDate    *string `json:"price_date"`
	Quantity     *string `json:"quantity"`
	BookedAmount *string `json:"booked_amount"`
}

// due suggests the plans to book: it writes nothing.
//
//	@Summary	List due saving plans
//	@Description	The portfolio's plans whose next_date is today or past and that have not ended, oldest next_date first. date is the next_date and missed the number of occurrences from it through today, or through end_date when that is earlier (1 when it is the only one). For a buy plan price is the close on or before date (null when there is none), quantity the amount divided by price rounded down to the market's scale (BIST 0, US 4, TEFAS 6, CRYPTO 8, GOLD 4) and booked_amount quantity times price to 2 decimals; quantity and booked_amount are null without a close. For a deposit plan instrument_id, symbol, market, price, price_date and quantity are null and booked_amount is the amount. Nothing is booked: POST /plans/{id}/book books one occurrence and POST /plans/{id}/skip skips it. The all scope is not supported.
//	@Tags		plans
//	@Produce	json
//	@Param		id	path		int	true	"portfolio id"
//	@Success	200	{array}		server.duePlanResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/plans/due [get]
func (h *planHandler) due(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	ps, err := h.plans.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	today := price.Today()
	ps = slices.DeleteFunc(ps, func(p plan.Plan) bool { return !p.Due(today) })
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
	out := make([]duePlanResponse, 0, len(ps))
	for _, p := range ps {
		through := today
		if p.EndDate != nil {
			through = min(today, *p.EndDate)
		}
		d := duePlanResponse{
			PlanID: p.ID, InstrumentID: p.InstrumentID, Currency: p.Currency, Amount: p.Amount.String(),
			Date: p.NextDate, Missed: plan.Missed(p.NextDate, through, p.DayOfMonth),
		}
		if p.InstrumentID == nil {
			booked := p.Amount.String()
			d.BookedAmount = &booked
			out = append(out, d)
			continue
		}
		in := ins[*p.InstrumentID]
		d.Symbol, d.Market = &in.Symbol, &in.Market
		c, found, err := h.planClose(r.Context(), p)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		if found {
			qty, booked := plan.Booking(p.Amount, c.Close, in.Market)
			closeStr, quantity, bookedAmount := c.Close.String(), qty.String(), booked.StringFixed(2)
			d.Price, d.PriceDate, d.Quantity, d.BookedAmount = &closeStr, &c.Date, &quantity, &bookedAmount
		}
		out = append(out, d)
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// duePlan returns the plan named by the {id} path value, and its instrument
// (nil for a deposit plan), when it is the caller's and can be booked or
// skipped on today. Otherwise it writes the 404, 409 or 500 and returns false.
func (h *planHandler) duePlan(w http.ResponseWriter, r *http.Request, today string) (plan.Plan, *instrument.Instrument, bool) {
	id, ok := planID(r)
	if !ok {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return plan.Plan{}, nil, false
	}
	p, err := h.plans.Get(r.Context(), auth.MustUserID(r.Context()), id)
	if errors.Is(err, plan.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "plan not found")
		return p, nil, false
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return p, nil, false
	}
	if msg := dueConflict(p, today); msg != "" {
		auth.WriteError(w, http.StatusConflict, msg)
		return p, nil, false
	}
	if p.InstrumentID == nil {
		return p, nil, true
	}
	in, err := h.instruments.Get(r.Context(), *p.InstrumentID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return p, nil, false
	}
	return p, &in, true
}

// advance moves p's next_date to the next occurrence inside one SQL
// transaction. It first re-reads the plan, repeats the due check and calls
// write (nil for a skip) with the plan it read, so the rows write inserts and
// the move commit together or not at all. BeginTx takes the write lock, so
// two calls cannot both pass the check. It returns the advanced plan.
func (h *planHandler) advance(ctx context.Context, p plan.Plan, today string, write func(tx *sql.Tx, cur plan.Plan) error) (plan.Plan, error) {
	tx, err := h.conn.BeginTx(ctx, nil)
	if err != nil {
		return p, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	cur, err := h.plans.GetTx(ctx, tx, p.UserID, p.ID)
	if err != nil {
		return p, err
	}
	if msg := dueConflict(cur, today); msg != "" {
		return p, planConflict(msg)
	}
	// The occurrence and the amount behind a suggested quantity must still be
	// the stored ones, else a stale call books the next occurrence at the
	// previous date's close.
	if cur.NextDate != p.NextDate || !cur.Amount.Equal(p.Amount) {
		return p, planConflict("plan changed, try again")
	}
	if write != nil {
		if err := write(tx, cur); err != nil {
			return p, err
		}
	}
	next := plan.After(cur.NextDate, cur.DayOfMonth)
	if err := h.plans.AdvanceTx(ctx, tx, cur, next); err != nil {
		return p, err
	}
	if err := tx.Commit(); err != nil {
		return p, fmt.Errorf("commit: %w", err)
	}
	cur.NextDate = next
	return cur, nil
}

// writeAdvanceError answers an error of advance.
func writeAdvanceError(w http.ResponseWriter, r *http.Request, err error) {
	var conflict planConflict
	switch {
	case errors.As(err, &conflict):
		auth.WriteError(w, http.StatusConflict, string(conflict))
	case errors.Is(err, plan.ErrNotFound), errors.Is(err, portfolio.ErrNotFound):
		auth.WriteError(w, http.StatusNotFound, "plan not found")
	default:
		auth.WriteInternalError(w, r, err)
	}
}

// book books the next occurrence of a due plan.
//
//	@Summary	Book a saving plan's next occurrence
//	@Description	Books the occurrence dated next_date and moves next_date to the next one. A buy plan books a buy of quantity (the amount divided by the close on or before next_date, rounded down to the market's scale) at that close, amount quantity times close to 2 decimals, fee 0, in the instrument's currency; with fund set a deposit of the booked amount goes in before it. A deposit plan books a deposit of its amount. Every row carries the plan's note and is dated next_date. The rows and the move are written in one SQL transaction. Answers with the plan, so the caller sees the new next_date and status.
//	@Tags		plans
//	@Produce	json
//	@Param		id	path		int	true	"plan id"
//	@Success	200	{object}	server.planResponse
//	@Failure	404	{object}	object{error=string}	"plan not found"
//	@Failure	409	{object}	object{error=string}	"plan has ended; plan is not due yet; plan changed, try again; no close on or before <date>; amount buys no unit at <close>"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/plans/{id}/book [post]
func (h *planHandler) book(w http.ResponseWriter, r *http.Request) {
	today := price.Today()
	p, in, ok := h.duePlan(w, r, today)
	if !ok {
		return
	}
	booked, quantity, closePrice := p.Amount, decimal.Zero, decimal.Zero
	if in != nil {
		c, found, err := h.planClose(r.Context(), p)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		if !found {
			auth.WriteError(w, http.StatusConflict, "no close on or before "+p.NextDate)
			return
		}
		closePrice = c.Close
		if quantity, booked = plan.Booking(p.Amount, closePrice, in.Market); !quantity.IsPositive() {
			auth.WriteError(w, http.StatusConflict, "amount buys no unit at "+closePrice.String())
			return
		}
	}
	// last is the row EnsurePrice runs for: the buy, else the deposit.
	var last transaction.Transaction
	next, err := h.advance(r.Context(), p, today, func(tx *sql.Tx, cur plan.Plan) (err error) {
		// The cash goes in before the buy so it gets the lower id.
		if cur.Fund || in == nil {
			last, err = h.transactions.CreateTx(r.Context(), tx, cur.UserID, transaction.Transaction{
				PortfolioID: cur.PortfolioID, Type: transaction.TypeDeposit, Date: cur.NextDate,
				Amount: booked, Fee: decimal.Zero, Currency: cur.Currency, Note: cur.Note,
			})
			if err != nil || in == nil {
				return err
			}
		}
		last, err = h.transactions.CreateTx(r.Context(), tx, cur.UserID, transaction.Transaction{
			PortfolioID: cur.PortfolioID, InstrumentID: cur.InstrumentID, Type: transaction.TypeBuy, Date: cur.NextDate,
			Quantity: quantity, Price: closePrice, Amount: booked, Fee: decimal.Zero, Currency: cur.Currency, Note: cur.Note,
		})
		return err
	})
	if err != nil {
		writeAdvanceError(w, r, err)
		return
	}
	var ensure instrument.Instrument
	if in != nil {
		ensure = *in
	}
	transaction.EnsurePrice(r.Context(), h.onDemandFetch, ensure, last)
	auth.WriteJSON(w, http.StatusOK, toPlanResponse(next, in, today))
}

// skip moves a due plan past its next occurrence without booking it.
//
//	@Summary	Skip a saving plan's next occurrence
//	@Description	Moves next_date to the next occurrence and books nothing. Answers with the plan, so the caller sees the new next_date and status.
//	@Tags		plans
//	@Produce	json
//	@Param		id	path		int	true	"plan id"
//	@Success	200	{object}	server.planResponse
//	@Failure	404	{object}	object{error=string}	"plan not found"
//	@Failure	409	{object}	object{error=string}	"plan has ended; plan is not due yet; plan changed, try again"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/plans/{id}/skip [post]
func (h *planHandler) skip(w http.ResponseWriter, r *http.Request) {
	today := price.Today()
	p, in, ok := h.duePlan(w, r, today)
	if !ok {
		return
	}
	next, err := h.advance(r.Context(), p, today, nil)
	if err != nil {
		writeAdvanceError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toPlanResponse(next, in, today))
}
