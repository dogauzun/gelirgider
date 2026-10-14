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
}

type positionHistoryResponse struct {
	Currency string                 `json:"currency"`
	Points   []positionHistoryPoint `json:"points"`
}

// get answers one holding's daily series over a date range.
//
//	@Summary	Position history
//	@Description	One point per day from `from` to `to` for one instrument of the portfolio, in the instrument's own currency (no fx). A `from` before the instrument's first transaction moves to it, unless that transaction is after `to`. Without `from` the range starts at the instrument's first transaction (at most 3660 days back); a `to` after today is clamped to today.
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
//
//nolint:dupl // the from/to rules are copied from historyHandler.get on purpose, see the ponytail comment below
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

	// ponytail: the from/to rules below are copied from historyHandler.get
	// rather than shared, to keep history.go untouched; extract a helper if
	// a third copy appears.
	today := istanbulToday()
	to := today
	if q := r.URL.Query().Get("to"); q != "" {
		var err error
		to, err = time.Parse(time.DateOnly, q)
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "to must be YYYY-MM-DD")
			return
		}
		if to.After(today) {
			to = today
		}
	}

	var from time.Time
	explicitFrom := r.URL.Query().Get("from") != ""
	if explicitFrom {
		from, err = time.Parse(time.DateOnly, r.URL.Query().Get("from"))
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "from must be YYYY-MM-DD")
			return
		}
	}
	var first time.Time
	if len(txs) > 0 {
		first, err = time.Parse(time.DateOnly, txs[0].Date)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
	}
	switch {
	case explicitFrom:
		if len(txs) > 0 && first.After(from) && !first.After(to) {
			from = first
		}
	case len(txs) > 0:
		from = first
		if earliest := to.AddDate(0, 0, -maxHistoryDays); from.Before(earliest) {
			from = earliest
		}
	default:
		from = to
	}

	if from.After(to) {
		auth.WriteError(w, http.StatusBadRequest, "from must not be after to")
		return
	}
	days := int(to.Sub(from).Hours()/24) + 1
	if explicitFrom && days > maxHistoryDays {
		auth.WriteError(w, http.StatusBadRequest, "range exceeds 3660 days")
		return
	}

	// Same series as history.go builds per instrument (holdings.MergedSeries).
	merged, err := holdings.MergedSeries(r.Context(), holdings.PriceSources{Prices: h.prices, Manual: h.manualPrices}, userID, []int64{instrumentID}, from, to)
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
	state := holdings.NewState(map[int64]string{instrumentID: instr.Currency})
	latest := map[int64]decimal.Decimal{}
	txIdx, priceIdx := 0, 0
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		dayStr := day.Format(time.DateOnly)
		for txIdx < len(txs) && txs[txIdx].Date <= dayStr {
			state.Apply(txs[txIdx])
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
		for _, p := range state.Summarize(valued).Positions {
			point.Quantity = p.Quantity.String()
			point.AvgCost = p.AvgCost.Round(9).String()
			point.CostBasis = p.CostBasis.StringFixed(2)
			point.MarketValue = p.MarketValue.StringFixed(2)
		}
		points = append(points, point)
	}

	auth.WriteJSON(w, http.StatusOK, positionHistoryResponse{Currency: instr.Currency, Points: points})
}
