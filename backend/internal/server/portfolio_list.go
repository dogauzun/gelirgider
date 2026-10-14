package server

import (
	"net/http"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// GET /api/portfolios is registered by registerSummaryRoutes in summary.go:
// the list values each portfolio through the summary's own pipeline, and it
// lives in package server for the same import-cycle reason documented there.

type portfolioListResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	TotalValue string `json:"total_value"`
	// Cash and UnrealizedPL in the reporting currency, taken from the same
	// holdings.Convert result as TotalValue, so they leave out the same
	// MissingRates currencies. UnrealizedPL is null under
	// summaryResponse.UnrealizedPL's rule: when every currency contributing
	// to it was excluded for lack of a rate, the sum is 0.00 only because
	// nothing was added to it, and a caller must render "-" rather than a
	// figure that reads as a real zero.
	Cash         string  `json:"cash"`
	UnrealizedPL *string `json:"unrealized_pl"`
	Currency     string  `json:"currency"`
	// MissingRates lists the currencies whose amounts TotalValue leaves out
	// for want of a conversion factor -- same field and same semantics as
	// summaryResponse.MissingRates: it names what was excluded, not what is
	// absent from the rate table (see holdings.Totals.Missing).
	// Without it the list silently understates a portfolio.
	MissingRates []string `json:"missing_rates"`
}

// list answers every portfolio of the caller with its totals in the
// caller's reporting currency.
//
//	@Summary	List portfolios
//	@Description	Every portfolio of the caller, with total value, cash and unrealized P/L in the reporting currency. unrealized_pl is null when every currency contributing to it has no rate.
//	@Tags		portfolios
//	@Produce	json
//	@Success	200	{array}		server.portfolioListResponse
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios [get]
func (h *summaryHandler) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())

	portfolios, err := h.portfolios.List(r.Context(), userID)
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

	out := make([]portfolioListResponse, len(portfolios))
	if len(portfolios) == 0 {
		auth.WriteJSON(w, http.StatusOK, out)
		return
	}

	// Price every instrument and load every rate once for the whole list:
	// the same instrument held in several portfolios is valued a single time.
	// The lists are loaded here, not through scopeTransactions, so they stay
	// index-aligned with portfolios even if one is created in between.
	lists := make([][]transaction.Transaction, len(portfolios))
	for i, p := range portfolios {
		if lists[i], err = h.transactions.List(r.Context(), userID, p.ID); err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
	}
	v, err := h.value(r.Context(), userID, distinctInstrumentIDs(flatByDate(lists)))
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// Each portfolio gets its own holdings.Compute (each List carries its own
	// synthetic split rows); only the rate lookup is shared, over the union of
	// the currencies they hold. Only the totals are used here, never
	// CostBasisReporting, so the trade-date rates computeSummary loads are
	// not needed.
	summaries := make([]holdings.Summary, len(lists))
	for i, l := range lists {
		summaries[i] = holdings.Compute(l, v.latest, v.currencyOf)
	}
	rates, err := h.latestRates(r.Context(), holdings.Merge(summaries), reporting, v.asOf)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	for i, p := range portfolios {
		totals := holdings.Convert(summaries[i], rates, reporting)
		missingRates := totals.Missing
		if missingRates == nil {
			missingRates = []string{}
		}
		out[i] = portfolioListResponse{
			ID:           p.ID,
			Name:         p.Name,
			CreatedAt:    p.CreatedAt.Format(time.RFC3339),
			TotalValue:   totals.TotalValue.StringFixed(2),
			Cash:         totals.Cash.StringFixed(2),
			Currency:     reporting,
			MissingRates: missingRates,
		}
		// unrealizedKnown's verdict on the unrealized total, so the list
		// nulls unrealized_pl on exactly the input the summary does.
		if unrealizedKnown(summaries[i], rates, reporting) {
			upl := totals.UnrealizedPL.StringFixed(2)
			out[i].UnrealizedPL = &upl
		}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}
