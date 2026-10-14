package server

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sort"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// stopajRate is the withholding tax pre-filled on an expected dividend, per
// market; any other market gets 0 (ADR 0009).
//
// ponytail: fixed rates per market, no per-instrument rate for ETFs or funds,
// and a law change is a code change. Move to a table if either matters.
var stopajRate = map[string]decimal.Decimal{
	"BIST": decimal.RequireFromString("0.15"),
	"US":   decimal.RequireFromString("0.20"),
}

// registerDividendRoutes wires GET /api/portfolios/{id}/dividends/expected,
// POST .../expected/dismiss and GET .../dividends/timeline. It lives in
// server for the same import-cycle reason as the summary route.
func registerDividendRoutes(mux *http.ServeMux, d *deps) {
	h := &dividendHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/expected", h.expected)
	mux.HandleFunc("POST /api/portfolios/{id}/dividends/expected/dismiss", h.dismiss)
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/timeline", h.timeline)
}

type dividendHandler struct{ *deps }

type expectedDividendResponse struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
	Market       string `json:"market"`
	Date         string `json:"date"`
	Quantity     string `json:"quantity"`
	PerShare     string `json:"per_share"`
	Gross        string `json:"gross"`
	Stopaj       string `json:"stopaj"`
	Net          string `json:"net"`
	Currency     string `json:"currency"`
}

// expected answers the dividends the portfolio held shares for on the
// ex-date and has not recorded or dismissed.
//
//	@Summary	List expected dividends
//	@Description	Ex-dates up to today on which the portfolio held the instrument, not yet recorded as a dividend transaction and not dismissed, oldest first. Stopaj is pre-filled per market.
//	@Tags		dividends
//	@Produce	json
//	@Param		id	path	int	true	"portfolio id"
//	@Success	200	{array}		server.expectedDividendResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/dividends/expected [get]
func (h *dividendHandler) expected(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}

	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// Rows per instrument, still in List's transaction.SortForApply order.
	byInstrument := map[int64][]transaction.Transaction{}
	for _, t := range txs {
		if t.InstrumentID != nil {
			byInstrument[*t.InstrumentID] = append(byInstrument[*t.InstrumentID], t)
		}
	}
	divs, err := h.dividends.ListFor(r.Context(), distinctInstrumentIDs(txs))
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	dismissed, err := h.dividends.Dismissed(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	var withDividends []int64
	for id := range byInstrument {
		if len(divs[id]) > 0 {
			withDividends = append(withDividends, id)
		}
	}
	instruments, err := h.instruments.ByIDs(r.Context(), withDividends)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	today := price.Today()
	out := []expectedDividendResponse{}
	for id, rows := range byInstrument {
		expected, err := dividend.Expected(rows, divs[id], dismissed, today)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		instr := instruments[id]
		for _, e := range expected {
			stopaj := e.Gross.Mul(stopajRate[instr.Market]).Round(2)
			out = append(out, expectedDividendResponse{
				InstrumentID: id,
				Symbol:       instr.Symbol,
				Market:       instr.Market,
				Date:         e.Date,
				Quantity:     e.Quantity.String(),
				PerShare:     e.PerShare.String(),
				Gross:        e.Gross.StringFixed(2),
				Stopaj:       stopaj.StringFixed(2),
				Net:          e.Gross.Sub(stopaj).StringFixed(2),
				Currency:     e.Currency,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].InstrumentID < out[j].InstrumentID
	})
	auth.WriteJSON(w, http.StatusOK, out)
}

// dismiss hides one expected dividend of the portfolio for good. Body
// {instrument_id, date}; idempotent; 204. No undo (ADR 0009).
//
//	@Summary	Dismiss an expected dividend
//	@Description	Hides one expected dividend of the portfolio for good. Idempotent; there is no undo.
//	@Tags		dividends
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int									true	"portfolio id"
//	@Param		body	body	object{instrument_id=int,date=string}	true	"instrument and ex-date, YYYY-MM-DD"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid JSON body; date must be YYYY-MM-DD; date must not be in the future; date must be on or after 1990-01-01; instrument not found"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/dividends/expected/dismiss [post]
func (h *dividendHandler) dismiss(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	var req struct {
		InstrumentID int64  `json:"instrument_id"`
		Date         string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// Expected dividends are ex-dates from MinDate to today, so nothing
	// outside that range can be dismissed.
	if err := price.CheckDate(req.Date); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "date "+err.Error())
		return
	}
	if _, err := h.instruments.Get(r.Context(), req.InstrumentID); err != nil {
		if errors.Is(err, instrument.ErrNotFound) {
			auth.WriteError(w, http.StatusBadRequest, "instrument not found")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	err := h.dividends.Dismiss(r.Context(), userID, portfolioID, req.InstrumentID, req.Date)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type dividendBucket struct {
	Period   string `json:"period"`
	Gross    string `json:"gross"`
	Tax      string `json:"tax"`
	Net      string `json:"net"`
	Interest string `json:"interest"`
}

type dividendInstrumentTotal struct {
	InstrumentID int64  `json:"instrument_id"`
	Symbol       string `json:"symbol"`
	Name         string `json:"name"`
	Count        int    `json:"count"`
	Net          string `json:"net"`
	LastDate     string `json:"last_date"`
}

type dividendTimelineResponse struct {
	Currency     string                    `json:"currency"`
	Months       []dividendBucket          `json:"months"`
	Years        []dividendBucket          `json:"years"`
	ByInstrument []dividendInstrumentTotal `json:"by_instrument"`
	MissingRates []string                  `json:"missing_rates"`
}

// timeline totals the dividends received by month, by year and by
// instrument, in the user's reporting currency at each row's own-date rate
// (not today's, which would rewrite history; ADR 0009).
//
//	@Summary	Dividend timeline
//	@Description	Dividends received bucketed by month and year (only non-empty buckets, ascending) and totaled per instrument (net descending, then symbol), in the reporting currency at each transaction's own-date rate. A dividend with an instrument adds to gross (amount), tax (fee) and net; one without adds amount minus fee to interest only. A row whose currency has no rate on or before its date is left out and its currency listed in missing_rates. Amounts are 2-decimal strings.
//	@Tags		dividends
//	@Produce	json
//	@Param		id	path	int	true	"portfolio id"
//	@Success	200	{object}	server.dividendTimelineResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/dividends/timeline [get]
func (h *dividendHandler) timeline(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	usr, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	reporting := usr.ReportingCurrency

	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// List returns rows date-ascending, which the fx walk needs.
	var rows []transaction.Transaction
	set := map[string]bool{}
	for _, t := range txs {
		if t.Type == transaction.TypeDividend {
			rows = append(rows, t)
			set[t.Currency] = true
		}
	}
	out := dividendTimelineResponse{Currency: reporting, Months: []dividendBucket{}, Years: []dividendBucket{}, ByInstrument: []dividendInstrumentTotal{}, MissingRates: []string{}}
	if len(rows) == 0 {
		auth.WriteJSON(w, http.StatusOK, out)
		return
	}
	fxw, err := loadFXWalk(r.Context(), h.fx, set, reporting, rows[0].Date, price.Today())
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	type sums struct{ gross, tax, net, interest decimal.Decimal }
	type instrumentSums struct {
		total dividendInstrumentTotal
		net   decimal.Decimal
	}
	months, years := map[string]*sums{}, map[string]*sums{}
	byInstrument := map[int64]*instrumentSums{}
	missing := map[string]bool{}
	for _, t := range rows {
		fxw.advance(t.Date)
		f, ok := holdings.Factor(fxw.rates, t.Currency, reporting)
		if !ok {
			missing[t.Currency] = true
			continue
		}
		amount, fee := t.Amount.Mul(f), t.Fee.Mul(f)
		net := amount.Sub(fee)
		for _, b := range []struct {
			m      map[string]*sums
			period string
		}{{months, t.Date[:7]}, {years, t.Date[:4]}} {
			s := b.m[b.period]
			if s == nil {
				s = &sums{}
				b.m[b.period] = s
			}
			if t.InstrumentID == nil {
				s.interest = s.interest.Add(net)
				continue
			}
			s.gross = s.gross.Add(amount)
			s.tax = s.tax.Add(fee)
			s.net = s.net.Add(net)
		}
		if t.InstrumentID == nil {
			continue
		}
		e := byInstrument[*t.InstrumentID]
		if e == nil {
			e = &instrumentSums{total: dividendInstrumentTotal{InstrumentID: *t.InstrumentID}}
			if t.InstrumentSymbol != nil {
				e.total.Symbol = *t.InstrumentSymbol
			}
			if t.InstrumentName != nil {
				e.total.Name = *t.InstrumentName
			}
			byInstrument[*t.InstrumentID] = e
		}
		e.total.Count++
		e.net = e.net.Add(net)
		e.total.LastDate = t.Date
	}

	buckets := func(m map[string]*sums) []dividendBucket {
		out := make([]dividendBucket, 0, len(m))
		for _, period := range slices.Sorted(maps.Keys(m)) {
			s := m[period]
			out = append(out, dividendBucket{Period: period, Gross: s.gross.StringFixed(2), Tax: s.tax.StringFixed(2), Net: s.net.StringFixed(2), Interest: s.interest.StringFixed(2)})
		}
		return out
	}
	out.Months, out.Years = buckets(months), buckets(years)
	entries := slices.Collect(maps.Values(byInstrument))
	sort.Slice(entries, func(i, j int) bool {
		if c := entries[i].net.Cmp(entries[j].net); c != 0 {
			return c > 0
		}
		return entries[i].total.Symbol < entries[j].total.Symbol
	})
	for _, e := range entries {
		e.total.Net = e.net.StringFixed(2)
		out.ByInstrument = append(out.ByInstrument, e.total)
	}
	out.MissingRates = slices.Sorted(maps.Keys(missing))
	if out.MissingRates == nil {
		out.MissingRates = []string{}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}
