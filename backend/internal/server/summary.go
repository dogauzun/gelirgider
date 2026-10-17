package server

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerSummaryRoutes wires GET /api/portfolios/{id}/summary and GET
// /api/portfolios (portfolio_list.go), which values each portfolio through
// the same pipeline.
//
// This lives in package server rather than internal/portfolio because
// internal/transaction already imports internal/portfolio (for the
// ownership check in its own handlers); importing internal/transaction from
// internal/portfolio would create an import cycle. server already
// constructs all repos this handler needs, so it is the natural place.
func registerSummaryRoutes(mux *http.ServeMux, d *deps) {
	h := &summaryHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/summary", h.get)
	mux.HandleFunc("GET /api/portfolios", h.list)
}

type summaryHandler struct{ *deps }

// splitResponse is one split that changed a position's quantity: ex-date and
// new shares per old share as an exact decimal.
type splitResponse struct {
	Date  string `json:"date"`
	Ratio string `json:"ratio"`
}

type positionResponse struct {
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Market       string  `json:"market"` // BIST, US, TEFAS or CRYPTO
	Currency     string  `json:"currency"`
	Quantity     string  `json:"quantity"`
	AvgCost      string  `json:"avg_cost"`
	CostBasis    string  `json:"cost_basis"`
	LastPrice    *string `json:"last_price"`
	HasPrice     bool    `json:"has_price"`
	// The latest close dated before price_date (manual winning on a date,
	// like last_price), scaled by any split after its date up to today. It,
	// prev_price_date, day_change and day_change_reporting are null when
	// either price is missing or the position is closed.
	PrevClose *string `json:"prev_close"`
	// The date of prev_close.
	PrevPriceDate *string `json:"prev_price_date"`
	// quantity × (last_price − prev_close), in the position's currency.
	DayChange *string `json:"day_change"`
	// day_change in the reporting currency; also null without a rate.
	DayChangeReporting *string `json:"day_change_reporting"`
	// Where last_price came from ("manual" for the caller's own entry, else
	// the fetching provider) and the date it is dated. Null when there is no
	// price. The page needs both to tag a manual price and to address it for
	// deletion -- DELETE /api/instruments/{id}/prices/{date}.
	PriceSource  *string `json:"price_source"`
	PriceDate    *string `json:"price_date"`
	MarketValue  string  `json:"market_value"`
	UnrealizedPL string  `json:"unrealized_pl"`
	RealizedPL   string  `json:"realized_pl"`
	// The cost of every share sold, in the position's currency: the base
	// realized_pl is a return on. Kept after the position closes.
	// Zero, or even negative, after a short: no base for a percentage.
	RealizedCost          string  `json:"realized_cost"`
	MarketValueReporting  *string `json:"market_value_reporting"`
	UnrealizedPLReporting *string `json:"unrealized_pl_reporting"`
	RealizedPLReporting   *string `json:"realized_pl_reporting"`
	// Unrealized plus realized P/L in the reporting currency, summed
	// unrounded and rounded once, so it can differ from the two rounded
	// fields' sum by a cent. Null without a rate.
	TotalPLReporting *string `json:"total_pl_reporting"`
	// The currency part of the position's gain in the reporting currency:
	// cost_basis at today's rate minus the cost with each buy converted at
	// its own trade date's rate. The price part is unrealized_pl_reporting.
	// Null on closed positions, on positions in the reporting currency, and
	// when today's rate or any open buy's trade-date rate is missing.
	FXPLReporting *string `json:"fx_pl_reporting"`
	// True for a closed position: no quantity left, listed for its
	// realized P/L. A quantity of 0 alone does not mean closed: in the
	// all-portfolios view a long and an equal short position in two
	// portfolios sum to 0 and stay open.
	Closed bool `json:"closed"`
	// The splits the holdings engine applied to this position, oldest
	// first: dated after the instrument's first transaction in this
	// portfolio, on or before today, and while shares were held.
	// Never null.
	Splits []splitResponse `json:"splits"`
}

type summaryResponse struct {
	Currency       string            `json:"currency"`
	Cash           string            `json:"cash"`
	CashByCurrency map[string]string `json:"cash_by_currency"`

	// Each cash_by_currency entry converted into the reporting currency,
	// keyed by its own currency. A currency with no rate is left out; it is
	// named in missing_rates.
	CashByCurrencyReporting map[string]string `json:"cash_by_currency_reporting"`

	TotalValue string `json:"total_value"`
	// The open positions' market value and cost in the reporting currency,
	// leaving out the same missing_rates currencies as total_value. Each is
	// summed unrounded and rounded once, so it matches the rows and history's
	// cost_basis; total_value less cash less unrealized_pl, each already
	// rounded, can be a cent off.
	PositionsValue string `json:"positions_value"`
	CostBasis      string `json:"cost_basis"`
	// Null when every currency contributing to the total was excluded for
	// lack of a rate (see totalKnown): the sum is then 0.00, which reads as
	// a real zero next to the per-position figures. The caller renders "-"
	// instead, the way it already does for a position with no reporting
	// value. Cash and total value stay plain strings: the page can tell the
	// same thing for those from cash_by_currency and market_value_reporting,
	// but realized P/L also counts closed positions, whose
	// currencies reach no other field of this response.
	UnrealizedPL *string `json:"unrealized_pl"`
	RealizedPL   *string `json:"realized_pl"`
	// FXPL is the part of the return that neither P/L figure holds: the
	// currency move on foreign cash and on the cost of foreign positions,
	// both of which unrealized and realized P/L value at today's rate.
	// Null when the portfolio has no currency other than the
	// reporting one, or when every such currency was excluded for lack of a
	// rate -- the same rule as the P/L fields above.
	FXPL *string `json:"fx_pl"`
	// The open positions' day_change_reporting summed unrounded and rounded
	// once, so it can differ from the rounded rows' sum by a cent.
	// A position with no previous close or no rate is left out; null when
	// none is left, so the page shows "-" rather than a zero.
	DayChange    *string            `json:"day_change"`
	MissingRates []string           `json:"missing_rates"`
	Positions    []positionResponse `json:"positions"`
	Stats        statsResponse      `json:"stats"`
}

// statsResponse is what the user put in, took out, paid and earned.
type statsResponse struct {
	// The earliest transaction date in scope; null with no transactions.
	FirstTransactionDate *string `json:"first_transaction_date"`
	// One entry per currency any row moved, in that currency.
	ByCurrency map[string]statsFields `json:"by_currency"`
	// by_currency summed into the reporting currency at today's rate. Null
	// when any by_currency entry with a nonzero figure has no rate, rather
	// than a partial sum; missing_rates names that currency.
	Reporting *statsFields `json:"reporting"`
}

// statsFields are one currency's totals, each a positive amount. fees counts
// every fee paid; dividends_tax is the stopaj withheld from dividends on an
// instrument; interest is instrument-less dividends, net of their fee.
type statsFields struct {
	Deposits    string `json:"deposits"`
	Withdrawals string `json:"withdrawals"`
	// deposits less withdrawals, and dividends_gross less dividends_tax,
	// each worked out unrounded and rounded once, so either can differ from
	// the rounded fields' difference by a cent.
	NetContribution string `json:"net_contribution"`
	Fees            string `json:"fees"`
	DividendsGross  string `json:"dividends_gross"`
	DividendsTax    string `json:"dividends_tax"`
	DividendsNet    string `json:"dividends_net"`
	Interest        string `json:"interest"`
}

func newStatsFields(s holdings.Stats) statsFields {
	return statsFields{
		Deposits:        s.Deposits.StringFixed(2),
		Withdrawals:     s.Withdrawals.StringFixed(2),
		NetContribution: s.Deposits.Sub(s.Withdrawals).StringFixed(2),
		Fees:            s.Fees.StringFixed(2),
		DividendsGross:  s.DividendsGross.StringFixed(2),
		DividendsTax:    s.DividendsTax.StringFixed(2),
		DividendsNet:    s.DividendsGross.Sub(s.DividendsTax).StringFixed(2),
		Interest:        s.Interest.StringFixed(2),
	}
}

// buildStats converts s's per-currency stats for the response; txs is
// date-ascending (flatByDate). Today's rate for every row is the recorded
// Phase 5 simplification, as for realized_pl.
func buildStats(s holdings.Summary, txs []transaction.Transaction, rates map[string]decimal.Decimal, reporting string) statsResponse {
	out := statsResponse{ByCurrency: make(map[string]statsFields, len(s.Stats))}
	if len(txs) > 0 {
		first := txs[0].Date
		out.FirstTransactionDate = &first
	}
	var sum holdings.Stats
	known := true
	for c, st := range s.Stats {
		out.ByCurrency[c] = newStatsFields(st)
		f, ok := holdings.Factor(rates, c, reporting)
		if !ok {
			// A currency whose figures are all zero converts to zero at any
			// rate, as in totalKnown.
			for _, v := range []decimal.Decimal{st.Deposits, st.Withdrawals, st.Fees, st.DividendsGross, st.DividendsTax, st.Interest} {
				known = known && v.IsZero()
			}
			continue
		}
		sum.Deposits = sum.Deposits.Add(st.Deposits.Mul(f))
		sum.Withdrawals = sum.Withdrawals.Add(st.Withdrawals.Mul(f))
		sum.Fees = sum.Fees.Add(st.Fees.Mul(f))
		sum.DividendsGross = sum.DividendsGross.Add(st.DividendsGross.Mul(f))
		sum.DividendsTax = sum.DividendsTax.Add(st.DividendsTax.Mul(f))
		sum.Interest = sum.Interest.Add(st.Interest.Mul(f))
	}
	if known {
		r := newStatsFields(sum)
		out.Reporting = &r
	}
	return out
}

// get answers one portfolio's positions and totals as of today, in the
// caller's reporting currency; for "all", the sum over every portfolio of
// the caller.
//
//	@Summary	Portfolio summary
//	@Description	Open and closed positions with cash and totals as of today (Europe/Istanbul), converted into the reporting currency. positions_value, cost_basis and day_change total the open positions, each rounded once; day_change is null when no open position has a previous close and a rate. unrealized_pl, realized_pl and fx_pl are null when they cannot be converted; missing_rates names the excluded currencies. stats totals deposits, withdrawals, fees, dividends and interest per currency and, when every currency converts, in the reporting currency. net_contribution, dividends_net and each position's total_pl_reporting are worked out unrounded and rounded once.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{object}	server.summaryResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/summary [get]
func (h *summaryHandler) get(w http.ResponseWriter, r *http.Request) {
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

	usr, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	reporting := usr.ReportingCurrency

	// txs is every portfolio's rows merged by date, for the instrument and
	// currency sets and fx_pl's flows. Holdings are not computed from it:
	// each portfolio's List carries its own synthetic split rows, which one
	// shared State would apply once per portfolio.
	txs := flatByDate(lists)

	v, err := h.value(r.Context(), userID, distinctInstrumentIDs(txs))
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	asOf := v.asOf

	prev, err := h.prevCloses(r.Context(), userID, v, txs)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	summaries := make([]holdings.Summary, 0, len(lists))
	for _, l := range lists {
		s, err := computeSummary(r.Context(), h.fx, l, v, reporting)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		summaries = append(summaries, s)
	}
	summary := holdings.Merge(summaries)

	rates, err := h.latestRates(r.Context(), summary, reporting, asOf)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	totals := holdings.Convert(summary, rates, reporting)

	cashByCurrency, cashByCurrencyReporting := cashMaps(summary, rates, reporting)
	positions, dayTotal := positionRows(summary, v, prev, rates, reporting)

	missingRates := totals.Missing
	if missingRates == nil {
		missingRates = []string{}
	}

	resp := summaryResponse{
		Currency:                reporting,
		Cash:                    totals.Cash.StringFixed(2),
		CashByCurrency:          cashByCurrency,
		CashByCurrencyReporting: cashByCurrencyReporting,
		TotalValue:              totals.TotalValue.StringFixed(2),
		PositionsValue:          totals.PositionsValue.StringFixed(2),
		CostBasis:               totals.CostBasis.StringFixed(2),
		MissingRates:            missingRates,
		Positions:               positions,
		Stats:                   buildStats(summary, txs, rates, reporting),
	}
	if dayTotal != nil {
		dc := dayTotal.StringFixed(2)
		resp.DayChange = &dc
	}
	if unrealizedKnown(summary, rates, reporting) {
		upl := totals.UnrealizedPL.StringFixed(2)
		resp.UnrealizedPL = &upl
	}
	if totalKnown(summary.RealizedPL, rates, reporting) {
		rpl := totals.RealizedPL.StringFixed(2)
		resp.RealizedPL = &rpl
	}

	if resp.FXPL, err = h.fxPL(r.Context(), txs, v, totals, reporting); err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	auth.WriteJSON(w, http.StatusOK, resp)
}

// cashMaps is summary's cash per currency and each entry converted into the
// reporting currency; a currency with no rate is left out of the second map.
func cashMaps(summary holdings.Summary, rates map[string]decimal.Decimal, reporting string) (byCurrency, byCurrencyReporting map[string]string) {
	byCurrency = make(map[string]string, len(summary.Cash))
	byCurrencyReporting = make(map[string]string, len(summary.Cash))
	for currency, amount := range summary.Cash {
		byCurrency[currency] = amount.StringFixed(2)
		if f, ok := holdings.Factor(rates, currency, reporting); ok {
			byCurrencyReporting[currency] = amount.Mul(f).StringFixed(2)
		}
	}
	return byCurrency, byCurrencyReporting
}

// positionRows is the open positions followed by the closed ones as response
// rows, and the open positions' day_change_reporting summed unrounded (nil
// when no row has one).
func positionRows(summary holdings.Summary, v valuation, prev map[int64]prevClose, rates map[string]decimal.Decimal, reporting string) ([]positionResponse, *decimal.Decimal) {
	// Closed positions are listed too, with quantity and market value zero
	// and their realized P/L set: without them the page can only total the
	// open rows, which disagrees with realized_pl.
	valued := make([]holdings.Position, 0, len(summary.Positions)+len(summary.Closed))
	valued = append(valued, summary.Positions...)
	valued = append(valued, summary.Closed...)

	positions := make([]positionResponse, 0, len(valued))
	var dayTotal *decimal.Decimal
	for i, p := range valued {
		pr, dcr := positionRow(p, i >= len(summary.Positions), v, prev, rates, reporting)
		if dcr != nil {
			sum := *dcr
			if dayTotal != nil {
				sum = dayTotal.Add(*dcr)
			}
			dayTotal = &sum
		}
		positions = append(positions, pr)
	}
	return positions, dayTotal
}

// prevClose is an instrument's previous close in today's terms and the date
// it was recorded on. tradeAdj is what the buys and sells dated on or after
// the price date change its day change by, over current quantity × (last −
// prev): a share bought that day moves from its price, one bought later did
// not move at all, and one sold later still moved with the price date.
type prevClose struct {
	close    decimal.Decimal
	date     string
	tradeAdj decimal.Decimal
}

// prevCloses finds the previous close per priced instrument of v: the same
// merge as v's, as of the day before the winner's date, so weekends and
// holidays fall out as the latest stored close before it.
// Ids are grouped by that day so each distinct date costs one LatestCloses
// call, not one per instrument. txs, every portfolio's rows in scope, gives
// each prevClose its tradeAdj.
func (h *summaryHandler) prevCloses(ctx context.Context, userID int64, v valuation, txs []transaction.Transaction) (map[int64]prevClose, error) {
	idsBefore := make(map[string][]int64)
	for id, won := range v.winner {
		day, err := time.Parse(time.DateOnly, won.Date)
		if err != nil {
			return nil, err
		}
		before := day.AddDate(0, 0, -1).Format(time.DateOnly)
		idsBefore[before] = append(idsBefore[before], id)
	}
	out := make(map[int64]prevClose, len(v.winner))
	for before, ids := range idsBefore {
		prevRows, err := holdings.LatestCloses(ctx, holdings.PriceSources{Prices: h.prices, Manual: h.manualPrices}, userID, ids, before)
		if err != nil {
			return nil, err
		}
		for id, prev := range prevRows {
			// In today's terms, like last_price, so day_change compares like
			// with like.
			out[id] = prevClose{close: splitScaled(prev.Close, v.splitsOf[id], prev.Date, v.asOf), date: prev.Date}
		}
	}
	for _, t := range txs {
		if t.InstrumentID == nil || (t.Type != transaction.TypeBuy && t.Type != transaction.TypeSell) {
			continue
		}
		pc, ok := out[*t.InstrumentID]
		// A zero-price buy is a bonus share (ADR 0008), held at prev like the
		// shares it came with, not a purchase at 0.
		if !ok || t.Date < v.winner[*t.InstrumentID].Date || t.Price.IsZero() {
			continue
		}
		// In today's terms like prev: the splits after the trade scale its
		// quantity up; its price is already per share of the day it traded.
		qty := t.Quantity
		for _, s := range v.splitsOf[*t.InstrumentID] {
			if s.Date > t.Date && s.Date <= v.asOf {
				qty = qty.Mul(s.Ratio)
			}
		}
		// A trade after the price date is priced at last: it contributes
		// nothing to the move.
		adj := qty.Mul(pc.close.Sub(v.latest[*t.InstrumentID]))
		if t.Date == v.winner[*t.InstrumentID].Date {
			adj = qty.Mul(pc.close).Sub(t.Quantity.Mul(t.Price))
		}
		if t.Type == transaction.TypeSell {
			adj = adj.Neg()
		}
		pc.tradeAdj = pc.tradeAdj.Add(adj)
		out[*t.InstrumentID] = pc
	}
	return out, nil
}

// positionRow builds one position's response; closed marks a position from
// summary.Closed. The second result is its day change in the reporting
// currency, nil when it has no previous close, price, quantity or rate.
func positionRow(p holdings.Position, closed bool, v valuation, prev map[int64]prevClose, rates map[string]decimal.Decimal, reporting string) (positionResponse, *decimal.Decimal) {
	instr := v.instruments[p.InstrumentID]
	pr := positionResponse{
		InstrumentID: p.InstrumentID,
		Symbol:       instr.Symbol,
		Name:         instr.Name,
		Market:       instr.Market,
		Currency:     p.Currency,
		// Quantities keep source precision (up to 9 fraction digits for
		// fractional shares), so the exact scale goes out as-is instead of
		// being rounded to 6 like the money fields.
		Quantity: p.Quantity.String(),
		// Unit prices keep source precision too (TEFAS NAVs carry 6
		// decimals, often below 1 TL); avg cost is a quotient, so it is
		// capped at 9 fraction digits like quantities.
		AvgCost:      p.AvgCost.Round(9).String(),
		CostBasis:    p.CostBasis.StringFixed(2),
		HasPrice:     p.HasPrice,
		MarketValue:  p.MarketValue.StringFixed(2),
		UnrealizedPL: p.UnrealizedPL.StringFixed(2),
		RealizedPL:   p.RealizedPL.StringFixed(2),
		RealizedCost: p.RealizedCost.StringFixed(2),
		Splits:       []splitResponse{},
		Closed:       closed,
	}
	for _, s := range p.Splits {
		pr.Splits = append(pr.Splits, splitResponse{Date: s.Date, Ratio: s.Ratio.String()})
	}
	if p.HasPrice {
		lp := p.LastPrice.Round(9).String()
		pr.LastPrice = &lp
		if won, ok := v.winner[p.InstrumentID]; ok {
			pr.PriceSource = &won.Source
			pr.PriceDate = &won.Date
		}
	}
	// Current quantity moves from prev; tradeAdj moves the shares traded on
	// price_date to their own prices and takes out the move of those traded
	// after it.
	var dayChange *decimal.Decimal
	if pc, ok := prev[p.InstrumentID]; ok && p.HasPrice && !p.Quantity.IsZero() {
		pd := pc.date
		prevStr := pc.close.Round(9).String()
		dc := p.Quantity.Mul(p.LastPrice.Sub(pc.close)).Add(pc.tradeAdj)
		change := dc.StringFixed(2)
		pr.PrevClose, pr.PrevPriceDate, pr.DayChange = &prevStr, &pd, &change
		if f, ok := holdings.Factor(rates, p.Currency, reporting); ok {
			dcr := dc.Mul(f)
			changeReporting := dcr.StringFixed(2)
			pr.DayChangeReporting = &changeReporting
			dayChange = &dcr
		}
	}
	if f, ok := holdings.Factor(rates, p.Currency, reporting); ok {
		mv := p.MarketValue.Mul(f).StringFixed(2)
		upl := p.UnrealizedPL.Mul(f).StringFixed(2)
		rpl := p.RealizedPL.Mul(f).StringFixed(2)
		total := p.UnrealizedPL.Add(p.RealizedPL).Mul(f).StringFixed(2)
		pr.MarketValueReporting = &mv
		pr.UnrealizedPLReporting = &upl
		pr.RealizedPLReporting = &rpl
		pr.TotalPLReporting = &total
		if !pr.Closed && p.Currency != reporting && !p.CostRateMissing {
			fxpl := p.CostBasis.Mul(f).Sub(p.CostBasisReporting).StringFixed(2)
			pr.FXPLReporting = &fxpl
		}
	}
	return pr, dayChange
}

// fxPL is the summary's fx_pl: total value less the flows at their own
// dates' rates and both P/Ls. Nil when no currency other than reporting,
// among txs and v's instruments, converts.
func (h *summaryHandler) fxPL(ctx context.Context, txs []transaction.Transaction, v valuation, totals holdings.Totals, reporting string) (*string, error) {
	foreign := map[string]bool{}
	for _, t := range txs {
		foreign[t.Currency] = true
	}
	for _, c := range v.currencyOf {
		foreign[c] = true
	}
	delete(foreign, reporting)
	converted := false
	for c := range foreign {
		converted = converted || !slices.Contains(totals.Missing, c)
	}
	if !converted {
		return nil, nil
	}
	flows, err := ownDateFlows(ctx, h.fx, txs, reporting, v.asOf)
	if err != nil {
		return nil, err
	}
	fxPL := totals.TotalValue.Sub(flows).Sub(totals.UnrealizedPL).Sub(totals.RealizedPL).StringFixed(2)
	return &fxPL, nil
}

// valuation is what the summary and the portfolio list share before the
// holdings engine runs: the day valued, every priced instrument's latest
// close in today's terms, the row it came from, its splits, and each
// instrument with its currency.
type valuation struct {
	asOf   string
	latest map[int64]decimal.Decimal
	// The winning row itself, kept so each position can report where its
	// last price came from and on what date.
	winner   map[int64]price.Price
	splitsOf map[int64][]split.Split
	// Fetched once and reused both for Compute's currencyOf and for each
	// position's symbol/name, instead of a second Get per position.
	instruments map[int64]instrument.Instrument
	currencyOf  map[int64]string
}

// value prices ids as of today (Europe/Istanbul, price.Today(); no as_of
// query parameter exists yet). Per instrument the caller's own manual price
// wins over the fetched one on the same date, else whichever is
// more recent, scaled by the splits since its date (splitScaled).
func (h *summaryHandler) value(ctx context.Context, userID int64, ids []int64) (valuation, error) {
	v := valuation{
		asOf:       price.Today(),
		latest:     make(map[int64]decimal.Decimal, len(ids)),
		winner:     make(map[int64]price.Price, len(ids)),
		currencyOf: make(map[int64]string, len(ids)),
	}
	winners, err := holdings.LatestCloses(ctx, holdings.PriceSources{Prices: h.prices, Manual: h.manualPrices}, userID, ids, v.asOf)
	if err != nil {
		return v, err
	}
	if v.splitsOf, err = h.splits.ListMany(ctx, ids); err != nil {
		return v, err
	}
	for id, won := range winners {
		v.latest[id] = splitScaled(won.Close, v.splitsOf[id], won.Date, v.asOf)
		v.winner[id] = won
	}
	if v.instruments, err = h.instruments.ByIDs(ctx, ids); err != nil {
		return v, err
	}
	for id, instr := range v.instruments {
		v.currencyOf[id] = instr.Currency
	}
	return v, nil
}

// latestRates loads the rate in force on asOf for every currency s needs
// converted into reporting: every cash and position currency, closed
// positions included (they are reported too, and their currency is
// not always one of the cash currencies), plus reporting itself (the divisor
// whenever a non-reporting currency is converted). TRY is never looked up --
// its rate is implied 1.
func (h *summaryHandler) latestRates(ctx context.Context, s holdings.Summary, reporting, asOf string) (map[string]decimal.Decimal, error) {
	needed := map[string]bool{}
	for currency := range s.Cash {
		needed[currency] = true
	}
	for _, p := range s.Positions {
		needed[p.Currency] = true
	}
	for _, p := range s.Closed {
		needed[p.Currency] = true
	}
	fxRows, err := h.fx.Latest(ctx, fxCurrencies(needed, reporting), asOf)
	if err != nil {
		return nil, err
	}
	rates := make(map[string]decimal.Decimal, len(fxRows))
	for currency, row := range fxRows {
		rates[currency] = row.Rate
	}
	return rates, nil
}

// splitScaled puts close, an unadjusted price dated priceDate (ADR 0008),
// in the terms of a quantity held on asOf: it divides close by the ratio of
// every split in splits dated after priceDate and on or before asOf. The
// instrument's own splits, not the position's: one first bought on or after
// the ex-date never had the split applied, yet its quantity is post-split
// too.
func splitScaled(close decimal.Decimal, splits []split.Split, priceDate, asOf string) decimal.Decimal {
	for _, s := range splits {
		if s.Date > priceDate && s.Date <= asOf {
			close = close.Div(s.Ratio)
		}
	}
	return close
}

// computeSummary runs the holdings engine over txs (one portfolio's: each
// portfolio's List carries its own synthetic split rows, so several merged
// would apply a split once per portfolio) with each buy's cost also converted
// into reporting at its own trade date's rate, so every position carries
// CostBasisReporting. v's latest and currencyOf are as for
// holdings.Compute; rates are read as of v.asOf. txs is not mutated.
func computeSummary(ctx context.Context, repo *fx.Repo, txs []transaction.Transaction, v valuation, reporting string) (holdings.Summary, error) {
	sorted := slices.Clone(txs)
	transaction.SortForApply(sorted)
	byDate, err := tradeDateRates(ctx, repo, sorted, reporting, v.asOf)
	if err != nil {
		return holdings.Summary{}, err
	}
	state := holdings.NewState(v.currencyOf)
	state.FactorOn = func(date, currency string) (decimal.Decimal, bool) {
		return holdings.Factor(byDate[date], currency, reporting)
	}
	for _, t := range sorted {
		state.Apply(t)
	}
	return state.Summarize(v.latest), nil
}

// tradeDateRates snapshots the fx rates in force on each distinct buy date
// of txs (date-ascending), keyed by that date: each currency's latest rate
// on or before it, the same walk history and fx_pl use. It loads only the
// buy currencies and reporting, never TRY (implied 1).
func tradeDateRates(ctx context.Context, repo *fx.Repo, txs []transaction.Transaction, reporting, asOf string) (map[string]map[string]decimal.Decimal, error) {
	byDate := map[string]map[string]decimal.Decimal{}
	set := map[string]bool{}
	first := ""
	for _, t := range txs {
		if t.Type != transaction.TypeBuy {
			continue
		}
		set[t.Currency] = true
		if first == "" {
			first = t.Date
		}
	}
	if first == "" {
		return byDate, nil
	}
	fxw, err := loadFXWalk(ctx, repo, set, reporting, first, asOf)
	if err != nil {
		return nil, err
	}
	for _, t := range txs {
		if t.Type != transaction.TypeBuy || byDate[t.Date] != nil {
			continue
		}
		fxw.advance(t.Date)
		byDate[t.Date] = maps.Clone(fxw.rates)
	}
	return byDate, nil
}

// ownDateFlows sums, in reporting, every transaction whose cash neither
// unrealized nor realized P/L accounts for, each at the rate of its own date:
// deposits and withdrawals, as history's net_flow converts them, plus
// standalone fees, dividends with no instrument (interest) and the fees on
// all of these, mirroring holdings.State.Apply. Those last ones are not
// currency moves, so leaving them out of this sum would pass them off as
// fx_pl. A flow whose currency has no rate on its own day is carried to the
// first later day that has one, exactly as in history; one with no
// rate even at the end is excluded, like its cash is from total_value.
func ownDateFlows(ctx context.Context, repo *fx.Repo, txs []transaction.Transaction, reporting, asOf string) (decimal.Decimal, error) {
	total := decimal.Zero
	if len(txs) == 0 {
		return total, nil
	}
	set := map[string]bool{}
	for _, t := range txs {
		set[t.Currency] = true
	}

	// txs is ordered by date, id ascending (transaction.Repo.List). A
	// future-dated row is already inside the summary's cash, so the walk
	// runs to it rather than stopping at asOf.
	first, last := txs[0].Date, max(asOf, txs[len(txs)-1].Date)
	fxw, err := loadFXWalk(ctx, repo, set, reporting, first, asOf)
	if err != nil {
		return total, err
	}
	from, err := time.Parse(time.DateOnly, first)
	if err != nil {
		return total, err
	}
	to, err := time.Parse(time.DateOnly, last)
	if err != nil {
		return total, err
	}
	carried := map[string]decimal.Decimal{}
	i := 0
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		dayStr := day.Format(time.DateOnly)
		fxw.advance(dayStr)
		for ; i < len(txs) && txs[i].Date <= dayStr; i++ {
			t := txs[i]
			switch {
			case t.Type == transaction.TypeDeposit, t.Type == transaction.TypeDividend && t.InstrumentID == nil:
				carried[t.Currency] = carried[t.Currency].Add(t.Amount.Sub(t.Fee))
			case t.Type == transaction.TypeWithdrawal, t.Type == transaction.TypeFee:
				carried[t.Currency] = carried[t.Currency].Sub(t.Amount.Add(t.Fee))
			}
		}
		if len(carried) > 0 {
			var converted decimal.Decimal
			converted, carried = convertCarried(carried, fxw.rates, reporting)
			total = total.Add(converted)
		}
	}
	return total, nil
}

// unrealizedKnown reports whether a converted unrealized P/L total is worth
// showing, under totalKnown's rule. Unrealized P/L is contributed by the open
// positions only -- realized P/L uses summary.RealizedPL, which unlike the
// positions also carries closed ones. Shared with the portfolio
// list so both endpoints null the field on exactly the same input.
func unrealizedKnown(s holdings.Summary, rates map[string]decimal.Decimal, reporting string) bool {
	byCurrency := make(map[string]decimal.Decimal, len(s.Positions))
	for _, p := range s.Positions {
		byCurrency[p.Currency] = byCurrency[p.Currency].Add(p.UnrealizedPL)
	}
	return totalKnown(byCurrency, rates, reporting)
}

// totalKnown reports whether a total summed from amounts (keyed by
// currency) is a number worth showing at all: false only when every nonzero
// amount was excluded for lack of a conversion factor, so the total is 0.00
// purely because nothing was added to it. A zero amount needs no rate -- it
// converts to zero at any rate -- and a partially converted total still
// shows a real figure, with missing_rates naming what it leaves out.
func totalKnown(amounts map[string]decimal.Decimal, rates map[string]decimal.Decimal, reporting string) bool {
	known := true
	for currency, amount := range amounts {
		if amount.IsZero() {
			continue
		}
		if _, ok := holdings.Factor(rates, currency, reporting); ok {
			return true
		}
		known = false
	}
	return known
}

// distinctInstrumentIDs collects the distinct non-nil instrument ids
// referenced by txs, in first-seen order.
func distinctInstrumentIDs(txs []transaction.Transaction) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, t := range txs {
		if t.InstrumentID == nil || seen[*t.InstrumentID] {
			continue
		}
		seen[*t.InstrumentID] = true
		ids = append(ids, *t.InstrumentID)
	}
	return ids
}
