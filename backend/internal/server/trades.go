package server

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// registerTradeRoutes wires GET /api/portfolios/{id}/trades. It lives in
// package server for the same reason registerHistoryRoutes does.
func registerTradeRoutes(mux *http.ServeMux, d *deps) {
	h := &tradeHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/trades", h.get)
}

type tradeHandler struct{ *deps }

// tradeResponse is one sell. Money is in the instrument's currency except
// realized_reporting; the fractions and day counts are null as ADR 0034 says.
type tradeResponse struct {
	PortfolioID       int64   `json:"portfolio_id"`
	Date              string  `json:"date"`
	InstrumentID      int64   `json:"instrument_id"`
	Symbol            string  `json:"symbol"`
	Name              string  `json:"name"`
	Market            string  `json:"market"`
	Currency          string  `json:"currency"`
	Quantity          string  `json:"quantity"`
	Proceeds          string  `json:"proceeds"`
	Cost              string  `json:"cost"`
	Fee               string  `json:"fee"`
	Realized          string  `json:"realized"`
	RealizedPct       *string `json:"realized_pct"`
	OpenedAt          *string `json:"opened_at"`
	Days              *int    `json:"days"`
	RealizedReporting *string `json:"realized_reporting"`
}

type tradesResponse struct {
	Currency     string          `json:"currency"`
	Trades       []tradeResponse `json:"trades"`
	MissingRates []string        `json:"missing_rates"`
}

// get answers the realized sells of the scope, newest first.
//
//	@Summary	Realized trades
//	@Description	One row per sell in the scope, newest first (date descending, then apply order descending). Each portfolio is applied on its own at the average cost, so cost is the quantity sold times the average cost, proceeds is the sell's amount less its fee and realized is proceeds minus cost. realized_pct is realized over cost, null when cost is not positive. opened_at is the date the position was opened and days the sell date minus it, both null for a sell made while the quantity was zero or negative. realized_reporting is realized in the reporting currency at the latest rate on or before the sell date, null (and the currency in missing_rates) when it has none. instrument_id keeps one instrument's sells. A term deposit's maturity sell is left out. Amounts are 2-decimal strings; quantity keeps its stored scale.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id				path		string	true	"numeric id or `all`"
//	@Param		instrument_id	query		integer	false	"only this instrument's sells"
//	@Success	200	{object}	server.tradesResponse
//	@Failure	400	{object}	object{error=string}	"instrument_id must be a number"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/trades [get]
func (h *tradeHandler) get(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	var only int64
	if q := r.URL.Query().Get("instrument_id"); q != "" {
		n, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "instrument_id must be a number")
			return
		}
		only = n
	}
	lists, err := scopeTransactions(r.Context(), h.portfolios, h.transactions, userID, r.PathValue("id"))
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	instruments, err := h.instruments.ByIDs(r.Context(), distinctInstrumentIDs(flatByDate(lists)))
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	usr, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	out, err := h.buildTrades(r.Context(), lists, instruments, usr.ReportingCurrency, only)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// portfolioTrade is a sell with the portfolio it was made in.
type portfolioTrade struct {
	portfolioID int64
	holdings.Trade
}

// buildTrades applies each portfolio's rows in its own State, keeps the sells
// of non-deposit instruments (only, when not 0, names the one wanted), orders
// them newest first and converts each realized at its sell date's rate.
func (h *tradeHandler) buildTrades(ctx context.Context, lists [][]transaction.Transaction, instruments map[int64]instrument.Instrument, reporting string, only int64) (tradesResponse, error) {
	currencyOf := make(map[int64]string, len(instruments))
	for id, instr := range instruments {
		currencyOf[id] = instr.Currency
	}
	var kept []portfolioTrade
	for _, txs := range lists {
		state := holdings.NewState(currencyOf)
		for _, tx := range txs {
			state.Apply(tx)
		}
		for _, t := range state.Trades {
			if instruments[t.InstrumentID].Market == instrument.MarketDeposit || (only != 0 && t.InstrumentID != only) {
				continue
			}
			kept = append(kept, portfolioTrade{txs[0].PortfolioID, t})
		}
	}
	// Reversing first makes the stable date sort break ties by apply position, latest first.
	slices.Reverse(kept)
	slices.SortStableFunc(kept, func(a, b portfolioTrade) int { return cmp.Compare(b.Date, a.Date) })

	out := tradesResponse{Currency: reporting, Trades: make([]tradeResponse, len(kept)), MissingRates: []string{}}
	if len(kept) == 0 {
		return out, nil
	}
	set := map[string]bool{}
	for _, t := range kept {
		set[t.Currency] = true
	}
	// The walk goes forward in time, so the newest-first list is filled from its end.
	fxw, err := loadFXWalk(ctx, h.fx, set, reporting, kept[len(kept)-1].Date, price.Today())
	if err != nil {
		return out, err
	}
	missing := map[string]bool{}
	for i := len(kept) - 1; i >= 0; i-- {
		t := kept[i]
		fxw.advance(t.Date)
		row := newTradeResponse(t, instruments[t.InstrumentID])
		if f, ok := holdings.Factor(fxw.rates, t.Currency, reporting); ok {
			s := t.Realized.Mul(f).StringFixed(2)
			row.RealizedReporting = &s
		} else {
			missing[t.Currency] = true
		}
		out.Trades[i] = row
	}
	out.MissingRates = slices.Sorted(maps.Keys(missing))
	if out.MissingRates == nil {
		out.MissingRates = []string{}
	}
	return out, nil
}

// newTradeResponse formats t without its reporting conversion.
func newTradeResponse(t portfolioTrade, instr instrument.Instrument) tradeResponse {
	row := tradeResponse{
		PortfolioID:  t.portfolioID,
		Date:         t.Date,
		InstrumentID: t.InstrumentID,
		Symbol:       instr.Symbol,
		Name:         instr.Name,
		Market:       instr.Market,
		Currency:     t.Currency,
		Quantity:     t.Quantity.String(),
		Proceeds:     t.Proceeds.StringFixed(2),
		Cost:         t.Cost.StringFixed(2),
		Fee:          t.Fee.StringFixed(2),
		Realized:     t.Realized.StringFixed(2),
	}
	if t.Cost.IsPositive() {
		s := t.Realized.Div(t.Cost).StringFixed(4)
		row.RealizedPct = &s
	}
	if t.OpenedAt != "" {
		row.OpenedAt = &t.OpenedAt
		// Both dates are stored valid, so a parse error cannot happen.
		sold, _ := time.Parse(time.DateOnly, t.Date)
		opened, _ := time.Parse(time.DateOnly, t.OpenedAt)
		days := int(sold.Sub(opened).Hours() / 24)
		row.Days = &days
	}
	return row
}
