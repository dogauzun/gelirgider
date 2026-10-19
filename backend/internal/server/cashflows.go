package server

import (
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// registerCashFlowRoutes wires GET /api/portfolios/{id}/cashflows. Lives in
// package server for the same reason registerHistoryRoutes does.
func registerCashFlowRoutes(mux *http.ServeMux, d *deps) {
	h := &cashFlowHandler{d}
	mux.HandleFunc("GET /api/portfolios/{id}/cashflows", h.get)
}

type cashFlowHandler struct{ *deps }

type cashFlowBucket struct {
	Period      string `json:"period"` // YYYY-MM, empty on the totals
	Deposits    string `json:"deposits"`
	Withdrawals string `json:"withdrawals"`
	Net         string `json:"net"`
	Dividends   string `json:"dividends"`
	Interest    string `json:"interest"`
	Fees        string `json:"fees"`
}

type cashFlowsResponse struct {
	Currency     string           `json:"currency"`
	Months       []cashFlowBucket `json:"months"`
	Totals       cashFlowBucket   `json:"totals"`
	MissingRates []string         `json:"missing_rates"`
}

// cashFlowSums is one bucket's exact running sums.
type cashFlowSums struct{ st holdings.Stats }

// bucket rounds s once to the 2-decimal strings of one bucket.
func (s *cashFlowSums) bucket(period string) cashFlowBucket {
	st := s.st
	return cashFlowBucket{
		Period:      period,
		Deposits:    st.Deposits.StringFixed(2),
		Withdrawals: st.Withdrawals.StringFixed(2),
		Net:         st.Deposits.Sub(st.Withdrawals).StringFixed(2),
		Dividends:   st.DividendsGross.Sub(st.DividendsTax).StringFixed(2),
		Interest:    st.Interest.StringFixed(2),
		Fees:        st.Fees.StringFixed(2),
	}
}

// add books one row, converted by factor f, the way holdings.State.Apply
// counts it.
func (s *cashFlowSums) add(t transaction.Transaction, f decimal.Decimal) {
	s.st = s.st.Add(holdings.StatsOf(t).Scale(f))
}

// get answers the scope's cash flows per calendar month.
//
//	@Summary	Monthly cash flows
//	@Description	One bucket per calendar month from the month of the first transaction to the current month, oldest first, zero-filled, plus totals over every month, in the reporting currency at each transaction's own-date rate (not today's, which would rewrite history). deposits and withdrawals are the gross amounts, net is deposits minus withdrawals, fees are the fee of every buy, sell, deposit and withdrawal plus the amount and fee of a fee row, dividends are dividends with an instrument net of their fee (stopaj), interest is a dividend without an instrument net of its fee; a split counts nothing. Every bucket is rounded once from its exact sum, so the months can differ from the totals by a cent. A row whose currency has no rate on or before its date is left out of every field and its currency listed in missing_rates. No transactions give no months and zero totals. Amounts are 2-decimal strings.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id	path		string	true	"numeric id or `all`"
//	@Success	200	{object}	server.cashFlowsResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/cashflows [get]
func (h *cashFlowHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, lists, ok := h.scopeLists(w, r)
	if !ok {
		return
	}
	reporting, err := h.users.ReportingCurrency(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	txs := flatByDate(lists)
	total := &cashFlowSums{}
	out := cashFlowsResponse{Currency: reporting, Months: []cashFlowBucket{}, Totals: total.bucket(""), MissingRates: []string{}}
	if len(txs) == 0 {
		auth.WriteJSON(w, http.StatusOK, out)
		return
	}
	set := map[string]bool{}
	for _, t := range txs {
		set[t.Currency] = true
	}
	today := price.Today()
	fxw, err := loadFXWalk(r.Context(), h.fx, set, reporting, txs[0].Date, today)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	months := map[string]*cashFlowSums{}
	missing := map[string]bool{}
	for _, t := range txs {
		if t.Type == transaction.TypeSplit {
			continue
		}
		fxw.advance(t.Date)
		f, ok := holdings.Factor(fxw.rates, t.Currency, reporting)
		if !ok {
			missing[t.Currency] = true
			continue
		}
		s := months[t.Date[:7]]
		if s == nil {
			s = &cashFlowSums{}
			months[t.Date[:7]] = s
		}
		s.add(t, f)
		total.add(t, f)
	}

	first, _ := time.Parse(time.DateOnly, txs[0].Date[:7]+"-01") // dates are stored valid
	last, _ := time.Parse(time.DateOnly, today)                  // price.Today is always a valid date
	for m := first; !m.After(last); m = time.Date(m.Year(), m.Month()+1, 1, 0, 0, 0, 0, time.UTC) {
		period := m.Format("2006-01")
		s := months[period]
		if s == nil {
			s = &cashFlowSums{}
		}
		out.Months = append(out.Months, s.bucket(period))
	}
	out.Totals = total.bucket("")
	out.MissingRates = slices.Sorted(maps.Keys(missing))
	if out.MissingRates == nil {
		out.MissingRates = []string{}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}
