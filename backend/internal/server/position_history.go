package server

import (
	"net/http"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerPositionHistoryRoutes wires
// GET /api/portfolios/{id}/instruments/{instrument_id}/history: one holding's
// daily quantity, cost, value and close, in the instrument's own currency
// (ADR 0016).
func registerPositionHistoryRoutes(mux *http.ServeMux, d *deps) {
	h := &positionHistoryHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/instruments/{instrument_id}/history", h.get)
}

type positionHistoryHandler struct{ *deps }

// positionHistoryPoint is one day of a holding, in the instrument's
// currency. All but close are zero on a day the position is closed; before
// its first price the position is valued at cost.
type positionHistoryPoint struct {
	Date        string  `json:"date"`
	Close       *string `json:"close"` // latest close on or before date, null before the first price
	Quantity    string  `json:"quantity"`
	AvgCost     string  `json:"avg_cost"`
	CostBasis   string  `json:"cost_basis"`
	MarketValue string  `json:"market_value"`
	Twr         *string `json:"twr"` // running time-weighted return from returns_from, null before it or when the range's is null
}

type positionHistoryResponse struct {
	Currency    string                 `json:"currency"`
	Points      []positionHistoryPoint `json:"points"`
	Returns     returnsResponse        `json:"returns"`
	ReturnsFrom *string                `json:"returns_from"` // first day with a close, null when there is none
}

// positionFlow is the money a row moves into (positive) or out of (negative)
// the position: a buy and its fee in, a sell or a dividend net of its fee out
// (ADR 0009). The caller passes only this instrument's rows.
func positionFlow(t transaction.Transaction) decimal.Decimal {
	switch t.Type {
	case transaction.TypeBuy:
		return t.Amount.Add(t.Fee)
	case transaction.TypeSell, transaction.TypeDividend:
		return t.Amount.Sub(t.Fee).Neg()
	}
	return decimal.Zero
}

// positionReturns fills the points' twr and answers the range's returns and
// its first day. The series starts at the first point with a close, whose
// value is the base: the flows of the days before and on it are inside it.
func positionReturns(points []positionHistoryPoint, value, flow []decimal.Decimal) (returnsResponse, *string) {
	start := 0
	for start < len(points) && points[start].Close == nil {
		start++
	}
	if start == len(points) {
		return returnsResponse{}, nil
	}
	values := make([]float64, len(points)-start)
	flows := make([]float64, len(values))
	for k := range values {
		values[k] = value[start+k].InexactFloat64()
		if k > 0 {
			flows[k] = flow[start+k].InexactFloat64()
		}
	}
	returns := periodReturns(values, flows, false)
	for k, twr := range twrPoints(values, flows, returns.TWR) {
		points[start+k].Twr = twr
	}
	return returns, &points[start].Date
}

// get answers one holding's daily series over a date range.
//
//	@Summary	Position history
//	@Description	One point per day from `from` to `to` for one instrument of the portfolio, in the instrument's own currency (no fx). `returns` holds the range's simple, time-weighted and money-weighted returns of the position, in that currency, as 6-decimal fractions or null when undefined (a non-positive value on the first day, or a time-weighted factor below zero); buys and their fees are flows in, sells and dividends (net of stopaj and fee) flows out. They start at `returns_from`, the first day with a close (null when there is none), whose value is the base; each point's `twr` is the running time-weighted return from that day, null before it. A `from` before the instrument's first transaction moves to it, unless that transaction is after `to`. Without `from` the range starts at the instrument's first transaction (at most 3660 days back); a `to` after today is clamped to today.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id				path		int		true	"portfolio id"
//	@Param		instrument_id	path		int		true	"instrument id"
//	@Param		from			query		string	false	"first day, YYYY-MM-DD"
//	@Param		to				query		string	false	"last day, YYYY-MM-DD, default today"
//	@Success	200	{object}	server.positionHistoryResponse
//	@Failure	400	{object}	object{error=string}	"to must be YYYY-MM-DD; from must be YYYY-MM-DD; from must not be after to; range exceeds 3660 days"
//	@Failure	404	{object}	object{error=string}	"portfolio not found; instrument not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/instruments/{instrument_id}/history [get]
func (h *positionHistoryHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	instr, ok := instrument.FromPath(w, r, h.instruments, "instrument_id")
	if !ok {
		return
	}
	instrumentID := instr.ID

	all, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// List is already in apply order; filtering keeps it, and the split rows
	// of this instrument come along with its trades.
	var txs []transaction.Transaction
	for _, t := range all {
		if t.InstrumentID != nil && *t.InstrumentID == instrumentID {
			txs = append(txs, t)
		}
	}

	from, to, ok := historyRange(w, r, txs)
	if !ok {
		return
	}
	days := int(to.Sub(from).Hours()/24) + 1

	// Same series as history.go builds per instrument (holdings.MergedSeries).
	merged, err := holdings.MergedSeries(r.Context(), h.priceSources(), userID, []int64{instrumentID}, from, to)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	series := merged[instrumentID]
	instrumentSplits, err := h.splits.List(r.Context(), instrumentID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	points := make([]positionHistoryPoint, 0, days)
	value := make([]decimal.Decimal, 0, days) // unrounded market value per day
	flow := make([]decimal.Decimal, 0, days)  // positionFlow summed per day
	state := holdings.NewState(map[int64]string{instrumentID: instr.Currency})
	latest := map[int64]decimal.Decimal{}
	txIdx, priceIdx := 0, 0
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		dayStr := day.Format(time.DateOnly)
		dayFlow := decimal.Zero
		for txIdx < len(txs) && txs[txIdx].Date <= dayStr {
			state.Apply(txs[txIdx])
			dayFlow = dayFlow.Add(positionFlow(txs[txIdx]))
			txIdx++
		}
		for priceIdx < len(series) && series[priceIdx].Date <= dayStr {
			latest[instrumentID] = series[priceIdx].Close
			priceIdx++
		}

		point := positionHistoryPoint{Date: dayStr, Quantity: "0", AvgCost: "0", CostBasis: "0.00", MarketValue: "0.00"}
		// close stays the unadjusted price; the value uses it in the
		// day's post-split terms.
		valued := map[int64]decimal.Decimal{}
		if close, ok := latest[instrumentID]; ok {
			s := close.String()
			point.Close = &s
			valued[instrumentID] = splitScaled(close, instrumentSplits, series[priceIdx-1].Date, dayStr)
		}
		// Summarize lists an open position only, so a closed one keeps the
		// zeros above.
		dayValue := decimal.Zero
		for _, p := range state.Summarize(valued).Positions {
			point.Quantity = p.Quantity.String()
			point.AvgCost = p.AvgCost.Round(9).String()
			point.CostBasis = p.CostBasis.StringFixed(2)
			point.MarketValue = p.MarketValue.StringFixed(2)
			dayValue = p.MarketValue
		}
		points = append(points, point)
		value = append(value, dayValue)
		flow = append(flow, dayFlow)
	}

	returns, returnsFrom := positionReturns(points, value, flow)
	auth.WriteJSON(w, http.StatusOK, positionHistoryResponse{Currency: instr.Currency, Points: points, Returns: returns, ReturnsFrom: returnsFrom})
}
