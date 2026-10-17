package server

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

const maxHistoryDays = 3660

// registerHistoryRoutes wires GET /api/portfolios/{id}/history. Lives in
// package server for the same reason registerSummaryRoutes does: it needs
// both transaction and portfolio repos, and portfolio can't import
// transaction without an import cycle.
func registerHistoryRoutes(mux *http.ServeMux, d *deps) {
	h := &historyHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/history", h.get)
	mux.HandleFunc("GET /api/portfolios/{id}/returns/periods", h.periods)
}

type historyHandler struct{ *deps }

type historyEntry struct {
	Date       string `json:"date"`
	TotalValue string `json:"total_value"`
	Cash       string `json:"cash"`
	CostBasis  string `json:"cost_basis"`
	// PriceUnknown is set on a point that holds at least one instrument with
	// no price on or before its date. Those holdings count at cost in
	// total_value, so the point is incomplete, not wrong by the whole
	// holding (ADR 0003).
	PriceUnknown bool `json:"price_unknown,omitempty"`
	// BenchmarkValue is the shadow portfolio's value on this day in the
	// reporting currency, set only when a benchmark was requested and is
	// available for the range (ADR 0012).
	BenchmarkValue string `json:"benchmark_value,omitempty"`
	// TWR is the time-weighted return from the range's first point to this
	// one, a fraction with 6 decimals: "0.000000" on the first point, the
	// range's returns.twr on the last. null on every point when returns.twr
	// is null (ADR 0011).
	TWR *string `json:"twr"`
	// BenchmarkTWR is the same for the benchmark's shadow series: null on
	// every point without a benchmark, when the shadow has no value, or when
	// benchmark.returns.twr is null (ADR 0012).
	BenchmarkTWR *string `json:"benchmark_twr"`
}

// benchmarkResponse is the requested benchmark's shadow-portfolio returns
// (ADR 0012, docs/adr/0012-benchmarks.md). All three returns are null when
// the benchmark has no level on or before the range's first point, or when
// the portfolio's own are null for lack of a positive base.
// EstimatedFrom is the first day of the range whose TUFE level is
// extrapolated past the last stored month, null when none is.
type benchmarkResponse struct {
	Code          string          `json:"code"`
	Returns       returnsResponse `json:"returns"`
	EstimatedFrom *string         `json:"estimated_from"` // first extrapolated TUFE day, null when none
}

// benchmarkCurrencies maps each accepted ?benchmark= code to the currency
// its level is in (ADR 0012). USD and EUR have level 1; XU100 and TUFE
// take theirs from benchmark_values (benchmarkLevels).
var benchmarkCurrencies = map[string]string{"USD": "USD", "EUR": "EUR", benchmark.CodeXU100: "TRY", benchmark.CodeTUFE: "TRY", benchmark.CodeGOLD: "TRY"}

// historyResponse wraps the per-day series with the reporting currency it
// was converted into, so the client doesn't have to assume TRY.
//
// MissingRates is the union, over every day in the range, of the currencies
// that day's conversion left out for lack of a rate. Same field
// name and same caveat as the summary response: it names the currency that
// was excluded, not necessarily the one whose rate is absent. Non-empty
// means at least one point understates the portfolio -- which is what makes
// the chart jump on the day the rates start.
type historyResponse struct {
	Currency     string         `json:"currency"`
	Points       []historyEntry `json:"points"`
	MissingRates []string       `json:"missing_rates"`
	// MissingPrices is the union, over every point with price_unknown, of
	// the symbols held with no price yet on that point's date.
	MissingPrices []string `json:"missing_prices"`
	// NetFlow is the external cash that entered the portfolio after the
	// first point: deposits minus withdrawals dated after `from`, each
	// converted at its own day's rate like everything else in the series
	// (ADR 0003). Anything dated on or before `from` is already inside the
	// first point's total, so it is not counted here. The chart header
	// subtracts it from last - first, so money put in is not shown as a
	// gain. A flow whose currency has no rate on its own day is
	// carried and counted on the first later day that has one, because
	// that is the day its cash enters total_value.
	//
	// The gross amount is counted, not the amount net of its fee: a fee is
	// a cost, and costs belong in the performance the chart shows, exactly
	// like the fee on a buy, a sell or a standalone fee row, none of which
	// is a flow either.
	//
	// It also holds the first-price jump of every holding the first point
	// had no price for: on the day its first price lands, its value moves
	// from cost to market in one step, and that move is gain made before
	// the range as much as inside it, so it is counted here rather than as
	// the range's gain.
	NetFlow string `json:"net_flow"`
	// Returns is the range's simple, time-weighted and money-weighted
	// return (ADR 0011), from each point's total_value and the same
	// external flows net_flow counts, taken on the day they land. All three
	// are null when the first point's total_value is not positive or any
	// point's cash is negative.
	Returns returnsResponse `json:"returns"`
	// Benchmark is the ?benchmark= comparison: the shadow portfolio that
	// put the same external flows into the benchmark (ADR 0012,
	// docs/adr/0012-benchmarks.md). null when no benchmark was requested.
	Benchmark *benchmarkResponse `json:"benchmark"`
}

// get answers the portfolio's daily value series over a date range, with
// its returns and an optional benchmark comparison.
//
//	@Summary	Portfolio value history
//	@Description	One point per day from `from` to `to`, in the reporting currency. A `from` before the first transaction moves to it, unless that transaction is after `to`. Without `from` the range starts at the first transaction (at most 3660 days back); a `to` after today is clamped to today. The three returns are null when undefined for the range; benchmark is null when no benchmark was requested, and its returns are null when it has no level on or before the first point. Each point's twr is the time-weighted return from the first point to that one, ending on returns.twr, and null on every point when returns.twr is null; benchmark_twr is the same for the benchmark's shadow series.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id			path		string	true	"numeric id or `all`"
//	@Param		from		query		string	false	"first day, YYYY-MM-DD"
//	@Param		to			query		string	false	"last day, YYYY-MM-DD, default today"
//	@Param		benchmark	query		string	false	"USD, EUR, XU100, TUFE, GOLD, or instrument:<id> for an instrument with a transaction in the scope"
//	@Success	200	{object}	server.historyResponse
//	@Failure	400	{object}	object{error=string}	"unknown benchmark; to must be YYYY-MM-DD; from must be YYYY-MM-DD; from must not be after to; range exceeds 3660 days"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/history [get]
func (h *historyHandler) get(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	// One list per portfolio in scope: a numeric id or "all".
	lists, err := scopeTransactions(r.Context(), h.portfolios, h.transactions, userID, r.PathValue("id"))
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	// txs is every portfolio's rows merged by date, for the date walks and
	// the instrument and currency sets. Holdings are not computed from it:
	// each portfolio's List carries its own synthetic split rows, which one
	// shared State would apply once per portfolio.
	txs := flatByDate(lists)

	benchmarkCode := r.URL.Query().Get("benchmark")
	benchmarkInstrument, known := parseBenchmark(benchmarkCode, distinctInstrumentIDs(txs))
	if !known {
		auth.WriteError(w, http.StatusBadRequest, "unknown benchmark")
		return
	}

	from, to, ok := historyRange(w, r, txs)
	if !ok {
		return
	}

	s, err := h.sweep(r.Context(), userID, txs, from, to, benchmarkRef{benchmarkCode, benchmarkInstrument})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	entries := s.entries
	last := len(entries) - 1
	values, flows, external, negativeCash := s.rangeInputs(0, last)

	netFlow := s.netFlow(external)

	var benchmark *benchmarkResponse
	if benchmarkCode != "" {
		benchmark = s.benchmarkSeries(benchmarkCode, flows, negativeCash)
	}

	returns := periodReturns(values, flows, negativeCash)
	for i, twr := range twrPoints(values, flows, returns.TWR) {
		entries[i].TWR = twr
	}

	auth.WriteJSON(w, http.StatusOK, historyResponse{Currency: s.currency, Points: entries, MissingRates: s.missingRates, MissingPrices: s.missingPrices, NetFlow: netFlow.StringFixed(2), Returns: returns, Benchmark: benchmark})
}

// historyRange resolves the ?from= and ?to= of a daily series over txs (the
// scope's rows by date): to defaults to today and is clamped to it, from
// defaults to the first transaction (at most maxHistoryDays back). It writes
// the error response itself and returns ok false when the range is bad.
func historyRange(w http.ResponseWriter, r *http.Request, txs []transaction.Transaction) (from, to time.Time, ok bool) {
	var err error
	today := istanbulToday()
	to = today
	if q := r.URL.Query().Get("to"); q != "" {
		to, err = time.Parse(time.DateOnly, q)
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "to must be YYYY-MM-DD")
			return from, to, false
		}
		// A future `to` would draw made-up flat points past today and,
		// with no `from`, push the real history out of the default window.
		// Clamp rather than reject: a client whose clock is
		// ahead of Istanbul still gets its chart. A `from` after today is
		// then after `to` and gets the 400 below.
		if to.After(today) {
			to = today
		}
	}

	explicitFrom := r.URL.Query().Get("from") != ""
	if explicitFrom {
		from, err = time.Parse(time.DateOnly, r.URL.Query().Get("from"))
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "from must be YYYY-MM-DD")
			return from, to, false
		}
		// A range opening before the first transaction would draw zeros up
		// to it and weigh the first deposit as a mid-range flow, inflating
		// the range return. Start at the first transaction
		// instead, unless that is after `to` too.
		if len(txs) > 0 {
			first, err := time.Parse(time.DateOnly, txs[0].Date)
			if err != nil {
				auth.WriteInternalError(w, r, err)
				return from, to, false
			}
			if first.After(from) && !first.After(to) {
				from = first
			}
		}
	} else if len(txs) > 0 {
		from, err = defaultHistoryFrom(txs, to)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return from, to, false
		}
	} else {
		from = to
	}

	if from.After(to) {
		auth.WriteError(w, http.StatusBadRequest, "from must not be after to")
		return from, to, false
	}
	if days := int(to.Sub(from).Hours()/24) + 1; explicitFrom && days > maxHistoryDays {
		auth.WriteError(w, http.StatusBadRequest, "range exceeds 3660 days")
		return from, to, false
	}
	return from, to, true
}

// netFlow is the history response's net_flow, from the external flows of
// the whole range (rangeInputs).
func (s *historySweep) netFlow(external []decimal.Decimal) decimal.Decimal {
	// net_flow is read against the points' rounded total_value, so each
	// day's flow is added as the step it makes in the rounded total:
	// round(total) - round(total - flow). Rounding the sum once can miss
	// that step by a cent, and the header then shows a -0,01 change when
	// only flows moved the total.
	netFlow := decimal.Zero
	for i, flow := range external {
		netFlow = netFlow.Add(s.totals[i].Round(2).Sub(s.totals[i].Sub(flow).Round(2)))
	}
	return netFlow
}

// benchmarkSeries is the history response's benchmark block for the whole
// range, given its flows and negativeCash from rangeInputs; it also fills
// the benchmark value and twr of the entries. The block carries no returns
// when the benchmark has no level on a day of the range.
func (s *historySweep) benchmarkSeries(code string, flows []float64, negativeCash bool) *benchmarkResponse {
	benchmark := &benchmarkResponse{Code: code}
	if shadow, returns, ok := s.rangeBenchmark(0, len(s.entries)-1, flows, negativeCash); ok {
		for i, v := range shadow {
			s.entries[i].BenchmarkValue = decimal.NewFromFloat(v).StringFixed(2)
		}
		benchmark.Returns = returns
		for i, twr := range twrPoints(shadow, flows, returns.TWR) {
			s.entries[i].BenchmarkTWR = twr
		}
		if s.estimatedFrom != "" {
			benchmark.EstimatedFrom = &s.estimatedFrom
		}
	}
	return benchmark
}

// parseBenchmark validates a ?benchmark= value: "" (none), a fixed code of
// benchmarkCurrencies, or instrument:<id> for an instrument with a
// transaction in the scope. instrumentID is that id, 0 otherwise.
func parseBenchmark(code string, instrumentIDs []int64) (instrumentID int64, ok bool) {
	if _, fixed := benchmarkCurrencies[code]; code == "" || fixed {
		return 0, true
	}
	if idStr, found := strings.CutPrefix(code, "instrument:"); found {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil && id > 0 && strconv.FormatInt(id, 10) == idStr && slices.Contains(instrumentIDs, id) {
			return id, true
		}
	}
	return 0, false
}

// istanbulToday is today's Europe/Istanbul date as a UTC midnight, the
// zone of dates parsed from query params and transaction rows (which carry
// no time zone of their own), so they compare and format consistently.
func istanbulToday() time.Time {
	t, _ := time.Parse(time.DateOnly, price.Today())
	return t
}

// defaultHistoryFrom is the first day of a range with no `from`: the first
// transaction (txs is ordered by date, flatByDate), clamped to the max
// window instead of letting the range check reject our own default.
func defaultHistoryFrom(txs []transaction.Transaction, to time.Time) (time.Time, error) {
	from, err := time.Parse(time.DateOnly, txs[0].Date)
	if err != nil {
		return time.Time{}, err
	}
	if earliest := to.AddDate(0, 0, -maxHistoryDays); from.Before(earliest) {
		from = earliest
	}
	return from, nil
}

// historySweep is the daily walk behind the history and the periods
// endpoints: one point per day from `from` to `to`, and what any range of
// days inside it needs to get the returns the history endpoint would give
// for that range (rangeInputs, rangeBenchmark).
type historySweep struct {
	currency string
	entries  []historyEntry // date, total_value, cash, cost_basis, price_unknown
	totals   []decimal.Decimal
	values   []float64 // totals as float64, the V_i of ADR 0011
	// flows is the deposits and withdrawals that entered total_value on each
	// day, converted: a carried flow on the day it converts. Day
	// 0's are kept here; a range opening on a day drops that day's.
	flows []decimal.Decimal
	// unpriced is, per day, the instruments held with no price on or before
	// it; firstPrice is the jump from cost to market of each one on the day
	// its first price lands while held. The jump is a flow in a range that
	// opened while the instrument was unpriced.
	unpriced   [][]int64
	firstPrice map[int64]priceJump
	// negativeCash is set on a day whose cash, as the point reports it (2
	// decimals), is below zero: no base to measure a return on.
	// A sub-cent residual shown as 0.00 doesn't count.
	negativeCash  []bool
	missingRates  []string
	missingPrices []string
	// benchmarkPrices is P_i, the benchmark level in the reporting currency
	// (ADR 0012), 0 on a day with no level or no rate yet; nil without a
	// benchmark.
	benchmarkPrices []float64
	estimatedFrom   string
}

type priceJump struct {
	day    int
	amount decimal.Decimal
}

// rangeInputs is what periodReturns takes for days i..j, the way a history
// range from day i to day j counts it: the values, and the external flows,
// with day i's dropped (they are inside its value, F_0 = 0) and the
// first-price jumps of the instruments day i held unpriced added on their
// day. external is the same flows as exact decimals, for net_flow.
func (s *historySweep) rangeInputs(i, j int) (values, flows []float64, external []decimal.Decimal, negativeCash bool) {
	external = make([]decimal.Decimal, j-i+1)
	copy(external[1:], s.flows[i+1:j+1])
	for _, id := range s.unpriced[i] {
		if jump, ok := s.firstPrice[id]; ok && jump.day <= j {
			external[jump.day-i] = external[jump.day-i].Add(jump.amount)
		}
	}
	flows = make([]float64, len(external))
	for k, f := range external {
		flows[k] = f.InexactFloat64()
	}
	return s.values[i : j+1], flows, external, slices.Contains(s.negativeCash[i:j+1], true)
}

// rangeBenchmark is the benchmark's shadow series over days i..j and its
// returns, given that range's flows and negativeCash from rangeInputs. ok is
// false without a benchmark or when a day of the range has no level.
func (s *historySweep) rangeBenchmark(i, j int, flows []float64, negativeCash bool) (shadow []float64, returns returnsResponse, ok bool) {
	if s.benchmarkPrices == nil {
		return nil, returnsResponse{}, false
	}
	shadow, ok = shadowValues(s.values[i], s.benchmarkPrices[i:j+1], flows)
	if !ok {
		return nil, returnsResponse{}, false
	}
	return shadow, periodReturns(shadow, flows, negativeCash), true
}

// benchmarkRef is a requested ?benchmark=: code is "" for none, else
// validated by parseBenchmark, which gave instrumentID (0 unless code is
// instrument:<id>).
type benchmarkRef struct {
	code         string
	instrumentID int64
}

// sweepInputs is what sweep loads before its day loop, from the repos: the
// price series and splits, the instrument lookups, the user's reporting
// currency, the fx walk and the benchmark levels.
type sweepInputs struct {
	pricesByInstrument map[int64][]price.Price
	splitsOf           map[int64][]split.Split
	currencyOf         map[int64]string
	symbolOf           map[int64]string
	reporting          string
	benchmarkCurrency  string
	fxw                *fxWalk
	// levels is nil without a benchmark and for the benchmarks whose level
	// the sweep derives itself (see benchmarkLevels).
	levels        []float64
	estimatedFrom string
}

// loadSweepInputs loads everything sweep reads from the repos, for the
// instruments in txs over from..to.
func (h *historyHandler) loadSweepInputs(ctx context.Context, userID int64, txs []transaction.Transaction, from, to time.Time, bm benchmarkRef) (*sweepInputs, error) {
	benchmarkCode, benchmarkInstrument := bm.code, bm.instrumentID
	instrumentIDs := distinctInstrumentIDs(txs)
	in := &sweepInputs{}
	var err error
	// Each instrument's series, manual merged over fetched, led
	// by its last price before the range, which the sweep consumes on
	// day one, the same way the fx rates are seeded with fx.Latest.
	in.pricesByInstrument, err = holdings.MergedSeries(ctx, holdings.PriceSources{Prices: h.prices, Manual: h.manualPrices}, userID, instrumentIDs, from, to)
	if err != nil {
		return nil, err
	}
	in.splitsOf, err = h.splits.ListMany(ctx, instrumentIDs)
	if err != nil {
		return nil, err
	}

	instruments, err := h.instruments.ByIDs(ctx, instrumentIDs)
	if err != nil {
		return nil, err
	}
	in.currencyOf = make(map[int64]string, len(instrumentIDs))
	in.symbolOf = make(map[int64]string, len(instrumentIDs))
	for id, instr := range instruments {
		in.currencyOf[id] = instr.Currency
		in.symbolOf[id] = instr.Symbol
	}
	in.benchmarkCurrency = benchmarkCurrencies[benchmarkCode]
	if benchmarkInstrument != 0 {
		in.benchmarkCurrency = in.currencyOf[benchmarkInstrument]
	}

	usr, err := h.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	in.reporting = usr.ReportingCurrency

	// fxCurrencySet is every currency that could appear in a day's
	// conversion: currencies held (instrument or cash, via tx.Currency,
	// which already covers cash-only rows with no instrument); loadFXWalk
	// adds reporting and drops TRY.
	fxCurrencySet := map[string]bool{}
	for _, c := range in.currencyOf {
		fxCurrencySet[c] = true
	}
	for _, t := range txs {
		fxCurrencySet[t.Currency] = true
	}
	// The benchmark's rate is walked even when nothing held uses it.
	if benchmarkCode != "" {
		fxCurrencySet[in.benchmarkCurrency] = true
	}
	in.fxw, err = loadFXWalk(ctx, h.fx, fxCurrencySet, in.reporting, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	if benchmarkCode != "" {
		in.levels, in.estimatedFrom, err = benchmarkLevels(ctx, h.benchmarks, benchmarkCode, from, to)
		if err != nil {
			return nil, err
		}
	}
	return in, nil
}

// sweep walks txs (every portfolio's rows, flatByDate) day by day from
// `from` to `to`, with bm's shadow series alongside.
func (h *historyHandler) sweep(ctx context.Context, userID int64, txs []transaction.Transaction, from, to time.Time, bm benchmarkRef) (*historySweep, error) {
	in, err := h.loadSweepInputs(ctx, userID, txs, from, to, bm)
	if err != nil {
		return nil, err
	}
	days := int(to.Sub(from).Hours()/24) + 1
	s := &historySweep{
		currency:     in.reporting,
		entries:      make([]historyEntry, 0, days),
		totals:       make([]decimal.Decimal, 0, days),
		values:       make([]float64, 0, days),
		flows:        make([]decimal.Decimal, 0, days),
		unpriced:     make([][]int64, 0, days),
		firstPrice:   map[int64]priceJump{},
		negativeCash: make([]bool, 0, days),
	}
	if bm.code != "" {
		s.benchmarkPrices = make([]float64, 0, days)
		s.estimatedFrom = in.estimatedFrom
	}
	missingSet := map[string]bool{}
	missingPriceSet := map[string]bool{}
	txIdx := 0
	priceIdx := map[int64]int{}
	latest := map[int64]decimal.Decimal{}
	// One running state per portfolio for the whole sweep: each transaction
	// is applied once, instead of recomputing the holdings from the whole
	// prefix every day. flatByDate is stable, so each state still
	// sees its portfolio's rows in the order transaction.Repo.List returns
	// (SortForApply); the day's snapshots are summed.
	states := map[int64]*holdings.State{}
	// carriedFlow holds the flows no rate could convert yet, per currency,
	// including those dated on or before the first point (their cash is
	// only inside that point if it converted there).
	carriedFlow := map[string]decimal.Decimal{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		dayStr := day.Format(time.DateOnly)
		dayIdx := len(s.entries)

		// dayFlow collects the day's external cash movements per currency.
		// Converted below, after this day's fx rates are in, so a flow uses
		// the same rate the same day's totals do.
		var dayFlow map[string]decimal.Decimal
		txIdx, dayFlow = applyThrough(txs, txIdx, dayStr, states, in.currencyOf)

		// firstPriced is the instruments whose first price lands today.
		// Day 0's never count: no range holds them unpriced before it.
		firstPriced := advancePrices(in.pricesByInstrument, priceIdx, latest, in.splitsOf, dayStr, dayIdx > 0)

		in.fxw.advance(dayStr)
		currentRates := in.fxw.rates
		if bm.code != "" {
			s.benchmarkPrices = append(s.benchmarkPrices, in.benchmarkPrice(bm.instrumentID, from.Format(time.DateOnly), dayStr, dayIdx, latest, currentRates))
		}

		// Same conversion path as the totals, so a flow uses the rate its
		// own day's cash uses. A currency with no rate that day converts
		// to nothing here, but its cash is not lost either: it enters
		// total_value on the first later day that has a rate, so the flow
		// stays in carriedFlow until that same day and is counted there.
		// Without the carry the header reads that jump as a
		// gain.
		for currency, amount := range dayFlow {
			carriedFlow[currency] = carriedFlow[currency].Add(amount)
		}
		converted := decimal.Zero
		if len(carriedFlow) > 0 {
			converted, carriedFlow = convertCarried(carriedFlow, currentRates, in.reporting)
		}

		// shortcut: the sweep is now O(txs + days * instruments) -- the
		// per-day work is one snapshot over the instruments held, not a
		// replay of every transaction. A portfolio_snapshots table is still
		// the upgrade path if the remaining per-day term ever matters.
		// The states are snapshotted and summed in one pass, with only the
		// fields read below: a Summarize and a Merge per day cost the `all`
		// sweep over a second on ten years.
		summary := holdings.SummarizeAll(slices.Collect(maps.Values(states)), latest)
		totals := holdings.Convert(summary, currentRates, in.reporting)
		for _, currency := range totals.Missing {
			missingSet[currency] = true
		}
		unpriced := s.scanPositions(in, summary.Positions, currentRates, dayIdx, firstPriced, missingPriceSet)
		// CostBasis isn't a Totals field, but MarketValue - CostBasis =
		// UnrealizedPL per position and Convert's factor is linear, so the
		// identity holds after conversion too: derive it instead of
		// re-summing CostBasis (which, unconverted, would mix currencies).
		costBasis := totals.TotalValue.Sub(totals.Cash).Sub(totals.UnrealizedPL)

		s.totals = append(s.totals, totals.TotalValue)
		s.values = append(s.values, totals.TotalValue.InexactFloat64())
		s.flows = append(s.flows, converted)
		s.unpriced = append(s.unpriced, unpriced)
		s.negativeCash = append(s.negativeCash, totals.Cash.Round(2).IsNegative())
		s.entries = append(s.entries, historyEntry{
			Date:         dayStr,
			TotalValue:   totals.TotalValue.StringFixed(2),
			Cash:         totals.Cash.StringFixed(2),
			CostBasis:    costBasis.StringFixed(2),
			PriceUnknown: len(unpriced) > 0,
		})
	}

	// Sorted and never nil: an empty union is [] in the response.
	s.missingRates = slices.AppendSeq(make([]string, 0, len(missingSet)), maps.Keys(missingSet))
	slices.Sort(s.missingRates)
	s.missingPrices = slices.AppendSeq(make([]string, 0, len(missingPriceSet)), maps.Keys(missingPriceSet))
	slices.Sort(s.missingPrices)
	return s, nil
}

// benchmarkPrice is P_i for day dayIdx: the benchmark's level in the
// reporting currency, 0 with no rate yet. benchmarkInstrument is the
// instrument:<id> id, 0 for the other benchmarks; from and day are the
// range's first date and today's; latest is the closes the valuation uses.
func (in *sweepInputs) benchmarkPrice(benchmarkInstrument int64, from, day string, dayIdx int, latest map[int64]decimal.Decimal, rates map[string]decimal.Decimal) float64 {
	level := 1.0
	if in.levels != nil {
		level = in.levels[dayIdx]
	}
	if benchmarkInstrument != 0 {
		// The close the valuation uses, zero before the first one;
		// latest carries forward. It is in the day's terms, so the
		// shares of a fixed holding grow by each in-range split's
		// ratio: a split is not a loss.
		units := decimal.NewFromInt(1)
		for _, sp := range in.splitsOf[benchmarkInstrument] {
			if sp.Date > from && sp.Date <= day {
				units = units.Mul(sp.Ratio)
			}
		}
		level = units.Mul(latest[benchmarkInstrument]).InexactFloat64()
	}
	if f, ok := holdings.Factor(rates, in.benchmarkCurrency, in.reporting); ok {
		return level * f.InexactFloat64()
	}
	return 0
}

// scanPositions lists the instruments held without a price on the day
// (recording their symbols in missingPrices) and records in s.firstPrice
// the jump of each one of firstPriced whose first price lands today.
func (s *historySweep) scanPositions(in *sweepInputs, positions []holdings.Position, rates map[string]decimal.Decimal, dayIdx int, firstPriced []int64, missingPrices map[string]bool) (unpriced []int64) {
	for _, p := range positions {
		if !p.HasPrice {
			missingPrices[in.symbolOf[p.InstrumentID]] = true
			unpriced = append(unpriced, p.InstrumentID)
			continue
		}
		// shortcut: the whole first-price jump is taken out, including
		// the gain on shares bought inside the range before that first
		// price; splitting it would need a price at each of those buys.
		if slices.Contains(firstPriced, p.InstrumentID) {
			// No rate: zero, as the position is also left out of
			// total_value today.
			jump := decimal.Zero
			if f, ok := holdings.Factor(rates, p.Currency, in.reporting); ok {
				jump = p.UnrealizedPL.Mul(f)
			}
			s.firstPrice[p.InstrumentID] = priceJump{day: dayIdx, amount: jump}
		}
	}
	return unpriced
}

// applyThrough applies txs from index i on, up to the last one dated on or
// before day, each to its portfolio's state in states (created on first
// use). It returns the index of the first row left and the applied rows'
// deposits less withdrawals per currency.
func applyThrough(txs []transaction.Transaction, i int, day string, states map[int64]*holdings.State, currencyOf map[int64]string) (int, map[string]decimal.Decimal) {
	dayFlow := map[string]decimal.Decimal{}
	for i < len(txs) && txs[i].Date <= day {
		tx := txs[i]
		state, ok := states[tx.PortfolioID]
		if !ok {
			state = holdings.NewState(currencyOf)
			states[tx.PortfolioID] = state
		}
		state.Apply(tx)
		switch tx.Type {
		case transaction.TypeDeposit:
			dayFlow[tx.Currency] = dayFlow[tx.Currency].Add(tx.Amount)
		case transaction.TypeWithdrawal:
			dayFlow[tx.Currency] = dayFlow[tx.Currency].Sub(tx.Amount)
		}
		i++
	}
	return i, dayFlow
}

// advancePrices moves each instrument's series in prices on to day:
// priceIdx past every row on or before it, latest to the last such row's
// close in day's terms. It returns the instruments priced for the first
// time on day, always none when countFirst is false.
func advancePrices(prices map[int64][]price.Price, priceIdx map[int64]int, latest map[int64]decimal.Decimal, splitsOf map[int64][]split.Split, day string, countFirst bool) []int64 {
	var firstPriced []int64
	for id, rows := range prices {
		idx := priceIdx[id]
		for idx < len(rows) && rows[idx].Date <= day {
			idx++
		}
		priceIdx[id] = idx
		if idx == 0 {
			continue
		}
		if _, had := latest[id]; !had && countFirst {
			firstPriced = append(firstPriced, id)
		}
		// A carried close stays in pre-split terms past an ex-date
		// that has no close yet.
		latest[id] = splitScaled(rows[idx-1].Close, splitsOf[id], rows[idx-1].Date, day)
	}
	return firstPriced
}

// benchmarkLevels returns code's level on each day of from..to in its own
// currency, from benchmark_values (ADR 0012), 0 on a day before its first
// level, or nil levels for USD and EUR, whose level is 1, and for
// instrument:<id>, whose level the sweep reads from its own closes. A range
// with a day of level 0 has no benchmark (shadowValues). estimatedFrom is
// the first extrapolated TUFE day, "" when none is.
func benchmarkLevels(ctx context.Context, repo *benchmark.Repo, code string, from, to time.Time) (levels []float64, estimatedFrom string, err error) {
	fromStr, toStr := from.Format(time.DateOnly), to.Format(time.DateOnly)
	switch code {
	case benchmark.CodeXU100, benchmark.CodeGOLD:
		// Seeded like fxWalk: the last close on or before from, then every
		// row in the range, the last close carried over weekends and holidays.
		seed, found, err := repo.Latest(ctx, code, fromStr)
		if err != nil {
			return nil, "", err
		}
		rows, err := repo.Range(ctx, code, fromStr, toStr)
		if err != nil {
			return nil, "", err
		}
		level := 0.0
		if found {
			level = seed.Value.InexactFloat64()
		}
		idx := 0
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			dayStr := day.Format(time.DateOnly)
			for ; idx < len(rows) && rows[idx].Date <= dayStr; idx++ {
				level = rows[idx].Value.InexactFloat64()
			}
			levels = append(levels, level)
		}
		return levels, "", nil
	case benchmark.CodeTUFE:
		// shortcut: loads every stored month, a few hundred monthly rows
		// (TUFE is published since 2003), since the extrapolation needs the
		// last two stored months whatever the range.
		rows, err := repo.Range(ctx, code, "0000-01-01", toStr)
		if err != nil {
			return nil, "", err
		}
		months := make([]benchmark.MonthLevel, 0, len(rows))
		for _, row := range rows {
			months = append(months, benchmark.MonthLevel{Month: row.Date[:7], Level: row.Value.InexactFloat64()})
		}
		// DailyCPI's level is 0 on a day it has none, before the first
		// usable month; it extrapolates past the last one.
		levels, _, estimatedFrom := benchmark.DailyCPI(months, fromStr, toStr)
		return levels, estimatedFrom, nil
	}
	return nil, "", nil
}

// fxWalk replays fx rates forward one day at a time: after advance(day),
// rates holds each currency's latest rate on or before day. Days before a
// currency's first row ever have nothing to seed from, so it is simply
// absent: those amounts stay out and come back in missing_rates, never
// back-carried (ADR 0003). Shared by history and the summary's
// fx_pl so both convert a flow at the same rate.
type fxWalk struct {
	rows  map[string][]fx.Rate
	idx   map[string]int
	rates map[string]decimal.Decimal
}

// fxCurrencies turns set, the currencies a conversion will meet, into the
// sorted list whose rates must be loaded: set plus reporting (the divisor
// whenever another currency converts into it), minus TRY (implied 1, no
// fx_rates row). set is modified.
func fxCurrencies(set map[string]bool, reporting string) []string {
	set[reporting] = true
	delete(set, "TRY")
	return slices.Sorted(maps.Keys(set))
}

// loadFXWalk fetches everything a walk over [from, to] needs up front for
// converting the currencies in set into reporting (see fxCurrencies): the
// latest rate as of from (one index seek per currency), so the walk doesn't
// start at zero when from precedes the first in-range row, and one Range
// call for the window rather than one query per day.
func loadFXWalk(ctx context.Context, repo *fx.Repo, set map[string]bool, reporting, from, to string) (*fxWalk, error) {
	currencies := fxCurrencies(set, reporting)
	seed, err := repo.Latest(ctx, currencies, from)
	if err != nil {
		return nil, err
	}
	rows, err := repo.Range(ctx, currencies, from, to)
	if err != nil {
		return nil, err
	}
	w := &fxWalk{rows: map[string][]fx.Rate{}, idx: map[string]int{}, rates: make(map[string]decimal.Decimal, len(currencies))}
	for currency, rate := range seed {
		w.rates[currency] = rate.Rate
	}
	for _, row := range rows {
		w.rows[row.Currency] = append(w.rows[row.Currency], row)
	}
	return w, nil
}

// advance moves the walk forward to day (YYYY-MM-DD). Days must not go
// backwards.
func (w *fxWalk) advance(day string) {
	for currency, rows := range w.rows {
		idx := w.idx[currency]
		for idx < len(rows) && rows[idx].Date <= day {
			w.rates[currency] = rows[idx].Rate
			idx++
		}
		w.idx[currency] = idx
	}
}

// convertCarried converts what it can of carried (amounts per currency)
// into reporting at rates. The rest -- currencies with no rate yet -- comes
// back to be carried to the next day: its cash only enters total_value on
// the first day that has a rate, so the flow is counted on that same day.
func convertCarried(carried, rates map[string]decimal.Decimal, reporting string) (decimal.Decimal, map[string]decimal.Decimal) {
	converted := holdings.Convert(holdings.Summary{Cash: carried}, rates, reporting)
	rest := make(map[string]decimal.Decimal, len(converted.Missing))
	for _, currency := range converted.Missing {
		rest[currency] = carried[currency]
	}
	return converted.Cash, rest
}
