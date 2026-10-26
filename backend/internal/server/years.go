package server

import (
	"maps"
	"net/http"
	"slices"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/shopspring/decimal"
)

// yearRow is one calendar year of the years response. Its span and returns
// are the periods route's year bucket (ADR 0011, 0012).
type yearRow struct {
	Year    string `json:"year"`    // "2026"
	From    string `json:"from"`    // first day, Jan 1 or the first transaction
	To      string `json:"to"`      // last day, Dec 31 or today
	Partial bool   `json:"partial"` // the first and the current year
	Opening string `json:"opening"` // total value on from
	Closing string `json:"closing"` // total value on to
	NetFlow string `json:"net_flow"`
	// Gain is closing - opening - net_flow, exact on the rounded figures.
	Gain string `json:"gain"`
	// The four buckets are the attribution route's total over the year.
	attributionBuckets
	Returns   returnsResponse  `json:"returns"`
	Benchmark *returnsResponse `json:"benchmark"`
	// Inflation is TÜFE's change over from..to, as the history route
	// computes it; null unless the reporting currency is TRY.
	Inflation *string `json:"inflation"`
	// Best and worst are the instruments with the largest and the smallest
	// price + income + fx over the year, null when none has every rate.
	Best  *yearInstrument `json:"best"`
	Worst *yearInstrument `json:"worst"`
}

// yearInstrument names an instrument and its total over a year, in the
// reporting currency; Currency is the instrument's own.
type yearInstrument struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
	Market       string `json:"market"`
	Currency     string `json:"currency"`
	Total        string `json:"total"`
}

// yearsResponse is every calendar year from the first transaction to today,
// oldest first, with no gaps.
type yearsResponse struct {
	Currency     string    `json:"currency"`
	Years        []yearRow `json:"years"`
	MissingRates []string  `json:"missing_rates"`
	// InflationEstimatedFrom is the first TÜFE day that is extrapolated, null
	// when none is or when inflation is not served.
	InflationEstimatedFrom *string `json:"inflation_estimated_from"`
}

// years answers the yearly statement of the scope: per calendar year, what
// it opened and closed at, the flows, the gain and its split, the returns,
// the benchmark and the inflation.
//
//	@Summary	Yearly statement
//	@Description	One row per calendar year, from the first transaction (at most 3660 days back) to today, oldest first. A row's from, to, partial, returns and benchmark equal the periods route's year bucket. opening and closing are the total value on the row's first and last day; net_flow is the history route's net_flow for from..to; gain is closing - opening - net_flow, exactly, on those figures. price, income, fx and cash_fx are the attribution route's total over from..to, with the same rounding (each bucket is rounded on its own; gain is computed on the rounded opening, closing and net_flow, so the four can differ from gain by up to 0.03 plus 0.01 per flow day, ADR 0038) and the same null rule: a bucket is null when a day of the year had no rate to convert it, and missing_rates lists those currencies. inflation is TÜFE's change over from..to as the history route returns it, null unless the reporting currency is TRY, and null when TÜFE has no positive level on from; inflation_estimated_from is the first extrapolated TÜFE day. A row's benchmark is null without the parameter, or when the benchmark has no level on some day of the year. best and worst are the instruments with the largest and the smallest price + income + fx over from..to, among those with a rate on every day (total is that sum in the reporting currency; instrument_id, symbol, market and currency are the instrument's); a tie keeps the lower instrument id; both are null when the year has no such instrument, and both name it when it has one.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id			path		string	true	"numeric id or `all`"
//	@Param		benchmark	query		string	false	"USD, EUR, XU100, TUFE, GOLD, or instrument:<id> for an instrument with a transaction in the scope"
//	@Success	200	{object}	server.yearsResponse
//	@Failure	400	{object}	object{error=string}	"unknown benchmark"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/years [get]
func (h *historyHandler) years(w http.ResponseWriter, r *http.Request) {
	userID, txs, bm, ok := h.scopeWithBenchmark(w, r)
	if !ok {
		return
	}

	s, from, currency, err := h.sweepToToday(r.Context(), userID, txs, sweepOpts{benchmark: bm, attribution: true})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	resp := yearsResponse{Currency: currency, Years: []yearRow{}, MissingRates: []string{}}
	if s != nil {
		var levels []float64
		if currency == "TRY" {
			// The sweep's last day is its today, so levels[k] is s.entries[k]'s day.
			to := from.AddDate(0, 0, len(s.entries)-1)
			var estimatedFrom string
			levels, estimatedFrom, err = benchmarkLevels(r.Context(), h.benchmarks, benchmark.CodeTUFE, from, to)
			if err != nil {
				auth.WriteInternalError(w, r, err)
				return
			}
			if estimatedFrom != "" {
				resp.InflationEstimatedFrom = &estimatedFrom
			}
		}
		resp.Years, resp.MissingRates = s.yearRows(levels)
	}
	auth.WriteJSON(w, http.StatusOK, resp)
}

// yearRows builds a row for each calendar year of the sweep and the union of
// the sweep's and the rows' missing rates, sorted. levels is the TÜFE level
// of each sweep day, nil when inflation is not served.
func (s *historySweep) yearRows(levels []float64) ([]yearRow, []string) {
	missing := map[string]bool{}
	for _, c := range s.missingRates {
		missing[c] = true
	}
	rows := []yearRow{}
	for _, span := range s.periodSpans(len("2006")) {
		i, j := span[0], span[1]
		values, flows, external, negativeCash := s.rangeInputs(i, j)
		attr := s.rangeAttribution(i, j)
		for _, c := range attr.missingRates {
			missing[c] = true
		}
		opening, closing, netFlow := s.totals[i].Round(2), s.totals[j].Round(2), s.netFlow(i, external)
		row := yearRow{
			Year: s.entries[i].Date[:len("2006")], From: s.entries[i].Date, To: s.entries[j].Date,
			Opening: opening.StringFixed(2), Closing: closing.StringFixed(2), NetFlow: netFlow.StringFixed(2),
			Gain:               closing.Sub(opening).Sub(netFlow).StringFixed(2),
			attributionBuckets: attr.totals(),
			Returns:            periodReturns(values, flows, negativeCash),
		}
		row.Best, row.Worst = s.extremes(attr)
		if _, returns, ok := s.rangeBenchmark(i, j, flows, negativeCash); ok {
			row.Benchmark = &returns
		}
		if len(levels) == len(s.entries) && levels[i] > 0 {
			row.Inflation = formatReturn(levels[j]/levels[i]-1, true)
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		rows[0].Partial = true
		rows[len(rows)-1].Partial = true
	}
	rates := slices.AppendSeq([]string{}, maps.Keys(missing))
	slices.Sort(rates)
	return rows, rates
}

// extremes names the instruments of r with the largest and the smallest
// price + income + fx, skipping those without every rate. Ascending ids and
// a strict comparison make a tie keep the lower id.
func (s *historySweep) extremes(r rangeReturn) (best, worst *yearInstrument) {
	var bestSum, worstSum decimal.Decimal
	for _, id := range slices.Sorted(maps.Keys(r.instruments)) {
		t := r.instruments[id]
		if t.missing {
			continue
		}
		sum := t.price.Add(t.income).Add(t.fx)
		pick := func() *yearInstrument {
			return &yearInstrument{InstrumentID: id, Symbol: s.symbolOf[id], Market: s.marketOf[id], Currency: s.currencyOf[id], Total: sum.StringFixed(2)}
		}
		if best == nil || sum.GreaterThan(bestSum) {
			best, bestSum = pick(), sum
		}
		if worst == nil || sum.LessThan(worstSum) {
			worst, worstSum = pick(), sum
		}
	}
	return best, worst
}
