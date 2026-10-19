package server

import (
	"context"
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
	// BenchmarkTwr is the same for the benchmark's shadow series, null without
	// a benchmark, when the shadow has no value, or when benchmark.returns.twr
	// is null (ADR 0012).
	BenchmarkTwr *string `json:"benchmark_twr"`
}

type positionHistoryResponse struct {
	Currency    string                 `json:"currency"`
	Points      []positionHistoryPoint `json:"points"`
	Returns     returnsResponse        `json:"returns"`
	ReturnsFrom *string                `json:"returns_from"` // first day with a close, null when there is none
	Benchmark   *benchmarkResponse     `json:"benchmark"`    // the ?benchmark= shadow position, null when none was requested
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

// positionSeries is the series the returns run on: from the first point with
// a close, whose value is the base (the flows of the days before and on it are
// inside it), to the last point with a value or a flow, and the values and
// flows between. The days after a close-out have value 0 and no flow, and the
// period rate would compound over them (ADR 0033). start is len(points) when
// no point has a close; the series is start..start+len(values)-1.
func positionSeries(points []positionHistoryPoint, value, flow []decimal.Decimal) (start int, values, flows []float64) {
	for start < len(points) && points[start].Close == nil {
		start++
	}
	end := len(points)
	for end > start+1 && value[end-1].IsZero() && flow[end-1].IsZero() {
		end--
	}
	values = make([]float64, end-start)
	flows = make([]float64, len(values))
	for k := range values {
		values[k] = value[start+k].InexactFloat64()
		if k > 0 {
			flows[k] = flow[start+k].InexactFloat64()
		}
	}
	return start, values, flows
}

// positionReturns fills the twr of points, which begin at the series' start,
// and answers the range's returns.
func positionReturns(points []positionHistoryPoint, values, flows []float64) returnsResponse {
	returns := periodReturns(values, flows, false)
	for k, twr := range twrPoints(values, flows, returns.TWR) {
		points[k].Twr = twr
	}
	return returns
}

// positionBenchmark is the shadow position of ADR 0012 on the position's own
// flows, in the instrument's currency: the benchmark's level converted at
// each day's rate is P_i. points, values and flows begin at the series' start;
// prices holds P_i from there. Its returns are null when a day has no usable
// price (shadowValues); benchmark_twr is filled on points otherwise.
func positionBenchmark(code string, points []positionHistoryPoint, values, flows, prices []float64, estimatedFrom string) *benchmarkResponse {
	bench := &benchmarkResponse{Code: code}
	if len(values) == 0 {
		return bench
	}
	shadow, ok := shadowValues(values[0], prices, flows)
	if !ok {
		return bench
	}
	bench.Returns = periodReturns(shadow, flows, false)
	for k, twr := range twrPoints(shadow, flows, bench.Returns.TWR) {
		points[k].BenchmarkTwr = twr
	}
	if estimatedFrom != "" {
		bench.EstimatedFrom = &estimatedFrom
	}
	return bench
}

// benchmarkPrices is P_i for every day of from..to, one per point: the level
// of benchmark code in the instrument's currency, 0 on a day without a rate
// (ADR 0012). estimatedFrom is the first extrapolated TUFE day, "" when none.
func (h *positionHistoryHandler) benchmarkPrices(ctx context.Context, code, currency string, from, to time.Time, points []positionHistoryPoint) (prices []float64, estimatedFrom string, err error) {
	levels, estimatedFrom, err := benchmarkLevels(ctx, h.benchmarks, code, from, to)
	if err != nil {
		return nil, "", err
	}
	levelCurrency := benchmarkCurrencies[code]
	rates := map[string]decimal.Decimal{}
	var walk *fxWalk
	if levelCurrency != currency {
		walk, err = loadFXWalk(ctx, h.fx, map[string]bool{levelCurrency: true}, currency, from.Format(time.DateOnly), to.Format(time.DateOnly))
		if err != nil {
			return nil, "", err
		}
		rates = walk.rates
	}
	prices = make([]float64, len(points))
	for i, p := range points {
		if walk != nil {
			walk.advance(p.Date)
		}
		level := 1.0 // USD and EUR
		if levels != nil {
			level = levels[i]
		}
		if f, ok := holdings.Factor(rates, levelCurrency, currency); ok {
			prices[i] = level * f.InexactFloat64()
		}
	}
	return prices, estimatedFrom, nil
}

// get answers one holding's daily series over a date range.
//
//	@Summary	Position history
//	@Description	One point per day from `from` to `to` for one instrument of the portfolio, in the instrument's own currency (no fx). `returns` holds the range's simple, time-weighted and money-weighted returns of the position, in that currency, as 6-decimal fractions or null when undefined (a non-positive value on the first day, or a time-weighted factor below zero); buys and their fees are flows in, sells and dividends (net of stopaj and fee) flows out. They start at `returns_from`, the first day with a close (null when there is none), whose value is the base, and end at the last day with a value or a flow, so a closed position's end is its last sell; each point's `twr` is the running time-weighted return from that day, null before it and after the end. A `from` before the instrument's first transaction moves to it, unless that transaction is after `to`. Without `from` the range starts at the instrument's first transaction (at most 3660 days back); a `to` after today is clamped to today. `benchmark` lays a shadow position over it: the benchmark's level converted into the instrument's currency, bought with the position's value on `returns_from` and fed with its flows (ADR 0012); `benchmark` answers its code, its three returns (null when a day from `returns_from` has no level or no rate) and `estimated_from`, the first extrapolated TUFE day, and each point's `benchmark_twr` is its running time-weighted return, null when the returns are or after the end. `benchmark` is null without the parameter.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id				path		int		true	"portfolio id"
//	@Param		instrument_id	path		int		true	"instrument id"
//	@Param		from			query		string	false	"first day, YYYY-MM-DD"
//	@Param		to				query		string	false	"last day, YYYY-MM-DD, default today"
//	@Param		benchmark		query		string	false	"USD, EUR, XU100, TUFE or GOLD"
//	@Success	200	{object}	server.positionHistoryResponse
//	@Failure	400	{object}	object{error=string}	"unknown benchmark; to must be YYYY-MM-DD; from must be YYYY-MM-DD; from must not be after to; range exceeds 3660 days"
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

	benchmarkCode := r.URL.Query().Get("benchmark")
	if _, known := benchmarkCurrencies[benchmarkCode]; benchmarkCode != "" && !known {
		auth.WriteError(w, http.StatusBadRequest, "unknown benchmark")
		return
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

	resp := positionHistoryResponse{Currency: instr.Currency, Points: points}
	start, values, flows := positionSeries(points, value, flow)
	if start < len(points) {
		resp.ReturnsFrom = &points[start].Date
		resp.Returns = positionReturns(points[start:start+len(values)], values, flows)
	}
	if benchmarkCode != "" {
		prices, estimatedFrom, err := h.benchmarkPrices(r.Context(), benchmarkCode, instr.Currency, from, to, points)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		resp.Benchmark = positionBenchmark(benchmarkCode, points[start:start+len(values)], values, flows, prices[start:start+len(values)], estimatedFrom)
	}
	auth.WriteJSON(w, http.StatusOK, resp)
}
