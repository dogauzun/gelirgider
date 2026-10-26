package server

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerDividendRoutes wires GET /api/portfolios/{id}/dividends/expected,
// POST .../expected/dismiss, GET .../dividends/upcoming, GET .../dividends/timeline
// and GET .../dividends/forecast. It lives in
// server for the same import-cycle reason as the summary route.
func registerDividendRoutes(mux *http.ServeMux, d *deps) {
	h := &dividendHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/expected", h.expected)
	mux.HandleFunc("POST /api/portfolios/{id}/dividends/expected/dismiss", h.dismiss)
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/upcoming", h.upcoming)
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/timeline", h.timeline)
	mux.HandleFunc("GET /api/portfolios/{id}/dividends/forecast", h.forecast)
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
	dismissed, err := h.dividends.Dismissed(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	today := price.Today()
	h.writeSuggestions(w, r, userID, portfolioID, func(rows []transaction.Transaction, divs []dividend.Dividend) ([]dividend.Suggestion, error) {
		return dividend.Expected(rows, divs, dismissed, today)
	})
}

// upcoming answers the dividends whose ex-date is still ahead, for the
// quantity held now.
//
//	@Summary	List upcoming dividends
//	@Description	Stored ex-dates after today for the quantity the portfolio holds now, soonest first; none when it holds none. Not matched against recorded dividends and not dismissable: those apply only after the ex-date. Stopaj is pre-filled per market like the expected list. The all scope is not supported.
//	@Tags		dividends
//	@Produce	json
//	@Param		id	path	int	true	"portfolio id"
//	@Success	200	{array}		server.expectedDividendResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/dividends/upcoming [get]
func (h *dividendHandler) upcoming(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	today := price.Today()
	h.writeSuggestions(w, r, userID, portfolioID, func(rows []transaction.Transaction, divs []dividend.Dividend) ([]dividend.Suggestion, error) {
		return dividend.Upcoming(rows, divs, today), nil
	})
}

// dividendInputs is what the suggestion and forecast routes read: the
// portfolio's rows per instrument, still in transaction.SortForApply order,
// their stored dividends, the stopaj rate per market and the instruments.
type dividendInputs struct {
	rows        map[int64][]transaction.Transaction
	divs        map[int64][]dividend.Dividend
	stopajRates map[string]decimal.Decimal
	instruments map[int64]instrument.Instrument
}

func (h *dividendHandler) loadInputs(ctx context.Context, userID, portfolioID int64) (dividendInputs, error) {
	in := dividendInputs{rows: map[int64][]transaction.Transaction{}}
	txs, err := h.transactions.List(ctx, userID, portfolioID)
	if err != nil {
		return in, err
	}
	for _, t := range txs {
		if t.InstrumentID != nil {
			in.rows[*t.InstrumentID] = append(in.rows[*t.InstrumentID], t)
		}
	}
	ids := distinctInstrumentIDs(txs)
	if in.divs, err = h.dividends.ListFor(ctx, ids); err != nil {
		return in, err
	}
	// The pre-filled stopaj per market (ADR 0009); a market without a row gets 0.
	if in.stopajRates, err = h.dividends.StopajRates(ctx); err != nil {
		return in, err
	}
	in.instruments, err = h.instruments.ByIDs(ctx, ids)
	return in, err
}

// stopajNet is the pre-filled stopaj of a gross at a market's rate, rounded
// to 2 decimals, and the net left after it.
func stopajNet(gross, rate decimal.Decimal) (stopaj, net decimal.Decimal) {
	stopaj = gross.Mul(rate).Round(2)
	return stopaj, gross.Sub(stopaj)
}

// writeSuggestions loads the portfolio's rows and dividends, runs suggest on
// each instrument and writes the results with stopaj and net, by date then
// instrument id. expected and upcoming share it so their rows do not drift.
func (h *dividendHandler) writeSuggestions(w http.ResponseWriter, r *http.Request, userID, portfolioID int64, suggest func([]transaction.Transaction, []dividend.Dividend) ([]dividend.Suggestion, error)) {
	in, err := h.loadInputs(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	out := []expectedDividendResponse{}
	for id, rows := range in.rows {
		suggestions, err := suggest(rows, in.divs[id])
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		instr := in.instruments[id]
		for _, e := range suggestions {
			stopaj, net := stopajNet(e.Gross, in.stopajRates[instr.Market])
			out = append(out, expectedDividendResponse{
				InstrumentID: id,
				Symbol:       instr.Symbol,
				Market:       instr.Market,
				Date:         e.Date,
				Quantity:     e.Quantity.String(),
				PerShare:     e.PerShare.String(),
				Gross:        e.Gross.StringFixed(2),
				Stopaj:       stopaj.StringFixed(2),
				Net:          net.StringFixed(2),
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
//	@Failure	400	{object}	object{error=string}	"invalid request body; date must be YYYY-MM-DD; date must not be in the future; date must be on or after 1990-01-01; instrument_id: unknown instrument"
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
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	// Expected dividends are ex-dates from MinDate to today, so nothing
	// outside that range can be dismissed.
	if err := price.CheckDate(req.Date); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "date "+err.Error())
		return
	}
	if _, err := h.instruments.GetUsable(r.Context(), req.InstrumentID); err != nil {
		if errors.Is(err, instrument.ErrNotFound) {
			auth.WriteError(w, http.StatusBadRequest, "instrument_id: unknown instrument")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	err := h.dividends.Dismiss(r.Context(), userID, portfolioID, req.InstrumentID, req.Date)
	if err != nil {
		portfolio.WriteLookupError(w, r, err)
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
	Currency string           `json:"currency"`
	Months   []dividendBucket `json:"months"`
	Years    []dividendBucket `json:"years"`
	// One bucket over the current month and the eleven before it, period its first month; summed unrounded, so the rounded month buckets need not add up to it.
	Last12Months dividendBucket `json:"last_12_months"`
	// One bucket over every dividend recorded, period empty; summed unrounded, so the rounded year buckets need not add up to it.
	Total        dividendBucket            `json:"total"`
	ByInstrument []dividendInstrumentTotal `json:"by_instrument"`
	MissingRates []string                  `json:"missing_rates"`
}

// timeline totals the dividends received by month, by year and by
// instrument, in the user's reporting currency at each row's own-date rate
// (not today's, which would rewrite history; ADR 0009).
//
//	@Summary	Dividend timeline
//	@Description	Dividends received bucketed by month and year (only non-empty buckets, ascending) and totaled per instrument (net descending, then symbol), in the reporting currency at each transaction's own-date rate. A dividend with an instrument adds to gross (amount), tax (fee) and net; one without adds amount minus fee to interest only. A row whose currency has no rate on or before its date is left out and its currency listed in missing_rates. last_12_months is one bucket over the current month and the eleven before it (period is its first month), summed before rounding: the month buckets are each rounded, so adding them can differ from it by a cent. total is one bucket over every dividend recorded (period is empty), summed before rounding: the year buckets are each rounded, so adding them can differ from it by a cent. by_instrument covers every dividend recorded, like total, not the last_12_months window. Amounts are 2-decimal strings.
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
	reporting, err := h.users.ReportingCurrency(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// List returns rows date-ascending, which the fx walk needs.
	var rows []transaction.Transaction
	for _, t := range txs {
		if t.Type == transaction.TypeDividend {
			rows = append(rows, t)
		}
	}
	today := price.Today()
	now, _ := time.Parse(time.DateOnly, today) // price.Today is always a valid date
	since := time.Date(now.Year(), now.Month()-11, 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
	last12, total := &dividendSums{}, &dividendSums{}
	out := dividendTimelineResponse{Currency: reporting, Months: []dividendBucket{}, Years: []dividendBucket{}, ByInstrument: []dividendInstrumentTotal{}, MissingRates: []string{}, Last12Months: bucketOf(since, last12), Total: bucketOf("", total)}
	if len(rows) == 0 {
		auth.WriteJSON(w, http.StatusOK, out)
		return
	}
	months, years := map[string]*dividendSums{}, map[string]*dividendSums{}
	byInstrument := map[int64]*dividendInstrumentSums{}
	missing, err := walkRowFactors(r.Context(), h.fx, len(rows), func(i int) (string, string) { return rows[i].Date, rows[i].Currency }, reporting, func(i int, f decimal.Decimal) {
		t := rows[i]
		row := holdings.StatsOf(t).Scale(f)
		for _, b := range []struct {
			m      map[string]*dividendSums
			period string
		}{{months, t.Date[:7]}, {years, t.Date[:4]}} {
			s := b.m[b.period]
			if s == nil {
				s = &dividendSums{}
				b.m[b.period] = s
			}
			s.add(row)
		}
		if t.Date[:7] >= since {
			last12.add(row)
		}
		total.add(row)
		if t.InstrumentID == nil {
			return
		}
		e := byInstrument[*t.InstrumentID]
		if e == nil {
			e = &dividendInstrumentSums{total: dividendInstrumentTotal{InstrumentID: *t.InstrumentID}}
			if t.InstrumentSymbol != nil {
				e.total.Symbol = *t.InstrumentSymbol
			}
			if t.InstrumentName != nil {
				e.total.Name = *t.InstrumentName
			}
			byInstrument[*t.InstrumentID] = e
		}
		e.total.Count++
		e.net = e.net.Add(row.DividendsGross.Sub(row.DividendsTax))
		e.total.LastDate = t.Date
	})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	out.Months, out.Years = dividendBuckets(months), dividendBuckets(years)
	out.Last12Months = bucketOf(since, last12)
	out.Total = bucketOf("", total)
	out.ByInstrument = dividendInstrumentTotals(byInstrument)
	out.MissingRates = holdings.SortedKeys(missing)
	auth.WriteJSON(w, http.StatusOK, out)
}

// dividendSums is one bucket's running totals.
type dividendSums struct{ st holdings.Stats }

// add books one dividend row, already scaled to the reporting currency.
func (s *dividendSums) add(row holdings.Stats) { s.st = s.st.Add(row) }

// bucketOf rounds s to the 2-decimal strings of one bucket.
func bucketOf(period string, s *dividendSums) dividendBucket {
	return dividendBucket{Period: period, Gross: s.st.DividendsGross.StringFixed(2), Tax: s.st.DividendsTax.StringFixed(2), Net: s.st.DividendsGross.Sub(s.st.DividendsTax).StringFixed(2), Interest: s.st.Interest.StringFixed(2)}
}

// dividendInstrumentSums is one instrument's running total, with its net
// as a decimal for the sort.
type dividendInstrumentSums struct {
	total dividendInstrumentTotal
	net   decimal.Decimal
}

// dividendBuckets lists m's periods ascending.
func dividendBuckets(m map[string]*dividendSums) []dividendBucket {
	out := make([]dividendBucket, 0, len(m))
	for _, period := range slices.Sorted(maps.Keys(m)) {
		out = append(out, bucketOf(period, m[period]))
	}
	return out
}

// dividendInstrumentTotals lists the instruments net descending, then by
// symbol; never nil.
func dividendInstrumentTotals(m map[int64]*dividendInstrumentSums) []dividendInstrumentTotal {
	entries := slices.Collect(maps.Values(m))
	sort.Slice(entries, func(i, j int) bool {
		if c := entries[i].net.Cmp(entries[j].net); c != 0 {
			return c > 0
		}
		return entries[i].total.Symbol < entries[j].total.Symbol
	})
	out := make([]dividendInstrumentTotal, 0, len(entries))
	for _, e := range entries {
		e.total.Net = e.net.StringFixed(2)
		out = append(out, e.total)
	}
	return out
}

type dividendForecastRow struct {
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	Market       string  `json:"market"`
	Period       string  `json:"period"`
	Date         *string `json:"date"`
	Planned      bool    `json:"planned"`
	Quantity     string  `json:"quantity"`
	PerShare     string  `json:"per_share"`
	Gross        string  `json:"gross"`
	Stopaj       string  `json:"stopaj"`
	Net          string  `json:"net"`
	Currency     string  `json:"currency"`
	NetReporting *string `json:"net_reporting"`
}

type dividendForecastMonth struct {
	Period string `json:"period"`
	Net    string `json:"net"`
}

type dividendForecastResponse struct {
	Currency     string                  `json:"currency"`
	Months       []dividendForecastMonth `json:"months"`
	Rows         []dividendForecastRow   `json:"rows"`
	MissingRates []string                `json:"missing_rates"`
}

// forecast answers the dividends expected in the next twelve months.
//
//	@Summary	Dividend forecast
//	@Description	Expected dividends of the twelve months from next month: each dividend recorded since the first day of the month eleven months ago, repeated a year later at its per-share amount times the quantity held now, replaced by the stored ex-dates (planned) that fall in the same period. Stopaj is pre-filled per market like the expected list. net_reporting is the net in the reporting currency at today's rate (null, and the currency listed in missing_rates, when it has no rate); months sums it per period, zero-filled, rows without a rate left out. Rows are ordered by period, date, instrument id; date is null for a projected row. The all scope is not supported.
//	@Tags		dividends
//	@Produce	json
//	@Param		id	path	int	true	"portfolio id"
//	@Success	200	{object}	server.dividendForecastResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/dividends/forecast [get]
func (h *dividendHandler) forecast(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	reporting, err := h.users.ReportingCurrency(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	in, err := h.loadInputs(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	today := price.Today()
	var forecast []dividend.ForecastRow
	set := map[string]bool{}
	for id, rows := range in.rows {
		for _, f := range dividend.Forecast(rows, in.divs[id], today) {
			forecast = append(forecast, f)
			set[f.Currency] = true
		}
	}
	slices.SortFunc(forecast, func(a, b dividend.ForecastRow) int {
		return cmp.Or(cmp.Compare(a.Period, b.Period), cmp.Compare(a.Date, b.Date), cmp.Compare(a.InstrumentID, b.InstrumentID))
	})
	latest, err := h.fx.Latest(r.Context(), fxCurrencies(set, reporting), today)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	rates := make(map[string]decimal.Decimal, len(latest))
	for currency, rate := range latest {
		rates[currency] = rate.Rate
	}

	periods := dividend.ForecastPeriods(today)
	sums := map[string]decimal.Decimal{}
	missing := map[string]bool{}
	out := dividendForecastResponse{Currency: reporting, Months: make([]dividendForecastMonth, 0, len(periods)), Rows: make([]dividendForecastRow, 0, len(forecast)), MissingRates: []string{}}
	for _, f := range forecast {
		instr := in.instruments[f.InstrumentID]
		stopaj, net := stopajNet(f.Gross, in.stopajRates[instr.Market])
		row := dividendForecastRow{
			InstrumentID: f.InstrumentID,
			Symbol:       instr.Symbol,
			Market:       instr.Market,
			Period:       f.Period,
			Planned:      f.Planned,
			Quantity:     f.Quantity.String(),
			PerShare:     f.PerShare.String(),
			Gross:        f.Gross.StringFixed(2),
			Stopaj:       stopaj.StringFixed(2),
			Net:          net.StringFixed(2),
			Currency:     f.Currency,
		}
		if f.Date != "" {
			row.Date = &f.Date
		}
		if factor, ok := holdings.Factor(rates, f.Currency, reporting); ok {
			inReporting := net.Mul(factor).Round(2)
			sums[f.Period] = sums[f.Period].Add(inReporting)
			s := inReporting.StringFixed(2)
			row.NetReporting = &s
		} else {
			missing[f.Currency] = true
		}
		out.Rows = append(out.Rows, row)
	}
	for _, period := range periods {
		out.Months = append(out.Months, dividendForecastMonth{Period: period, Net: sums[period].StringFixed(2)})
	}
	out.MissingRates = holdings.SortedKeys(missing)
	auth.WriteJSON(w, http.StatusOK, out)
}
