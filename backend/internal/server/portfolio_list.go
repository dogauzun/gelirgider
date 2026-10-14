package server

import (
	"context"
	"net/http"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
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
	// MissingRates currencies (GLG-158). UnrealizedPL is null under
	// summaryResponse.UnrealizedPL's rule: when every currency contributing
	// to it was excluded for lack of a rate, the sum is 0.00 only because
	// nothing was added to it, and a caller must render "-" rather than a
	// figure that reads as a real zero (GLG-113, GLG-129).
	Cash         string  `json:"cash"`
	UnrealizedPL *string `json:"unrealized_pl"`
	Currency     string  `json:"currency"`
	// MissingRates lists the currencies whose amounts TotalValue leaves out
	// for want of a conversion factor -- same field and same semantics as
	// summaryResponse.MissingRates: it names what was excluded, not what is
	// absent from the rate table (see holdings.Totals.Missing, GLG-114).
	// Without it the list silently understates a portfolio (GLG-121).
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
	for i, p := range portfolios {
		totals, plKnown, err := h.totals(r.Context(), userID, p.ID, reporting)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
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
		if plKnown {
			upl := totals.UnrealizedPL.StringFixed(2)
			out[i].UnrealizedPL = &upl
		}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// totals computes one portfolio's totals in reporting through the summary's
// value and latestRates, with holdings.Compute in place of computeSummary:
// only the totals are used here, never CostBasisReporting, so the trade-date
// rates it would load are not needed. The second result is unrealizedKnown's
// verdict on the unrealized total, so the list nulls unrealized_pl on exactly
// the input the summary does (GLG-158).
func (h *summaryHandler) totals(ctx context.Context, userID, portfolioID int64, reporting string) (holdings.Totals, bool, error) {
	txs, err := h.transactions.List(ctx, userID, portfolioID)
	if err != nil {
		return holdings.Totals{}, false, err
	}
	v, err := h.value(ctx, userID, distinctInstrumentIDs(txs))
	if err != nil {
		return holdings.Totals{}, false, err
	}
	summary := holdings.Compute(txs, v.latest, v.currencyOf)
	rates, err := h.latestRates(ctx, summary, reporting, v.asOf)
	if err != nil {
		return holdings.Totals{}, false, err
	}
	return holdings.Convert(summary, rates, reporting), unrealizedKnown(summary, rates, reporting), nil
}
