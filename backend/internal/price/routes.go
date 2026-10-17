package price

import (
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

// defaultHistoryLimit and maxHistoryLimit bound the ?limit query param on
// GET /api/instruments/{id}/prices.
const (
	defaultHistoryLimit = 30
	maxHistoryLimit     = 500
)

// manualSource is used for every write through this API. Automated
// providers (a later Phase 3 ticket) will write other source values into
// the same table.
const manualSource = "manual"

// RegisterRoutes wires the price endpoints into mux. mux must be mounted
// behind middleware that calls auth.RequireUser: every handler here reads
// or writes manual, scoped to the caller, so it needs the user id
// from context. instruments is used to check that an instrument exists
// before writing or reading its prices.
func RegisterRoutes(mux *http.ServeMux, repo *Repo, manual *ManualRepo, instruments *instrument.Repo) {
	h := &handler{repo: repo, manual: manual, instruments: instruments}
	mux.HandleFunc("PUT /api/instruments/{id}/prices/{date}", h.put)
	mux.HandleFunc("DELETE /api/instruments/{id}/prices/{date}", h.delete)
	mux.HandleFunc("GET /api/instruments/{id}/prices", h.history)
}

type handler struct {
	repo        *Repo
	manual      *ManualRepo
	instruments *instrument.Repo
}

// instrumentFromPath is instrument.FromPath that answers 404 for a DEPOSIT
// instrument too, for every caller: a deposit is valued by its computed price
// source, so it has no prices to set, list or delete here.
func (h *handler) instrumentFromPath(w http.ResponseWriter, r *http.Request) (instrument.Instrument, bool) {
	in, ok := instrument.FromPath(w, r, h.instruments, "id")
	if ok && in.Market == instrument.MarketDeposit {
		auth.WriteError(w, http.StatusNotFound, "instrument not found")
		return instrument.Instrument{}, false
	}
	return in, ok
}

type closeRequest struct {
	Close string `json:"close"`
}

type priceResponse struct {
	InstrumentID int64  `json:"instrument_id"`
	Date         string `json:"date"`
	Close        string `json:"close"`
	Source       string `json:"source"`
}

func toResponse(p Price) priceResponse {
	return priceResponse{InstrumentID: p.InstrumentID, Date: p.Date, Close: storedText(p.Close), Source: p.Source}
}

// storedText prints d as the close column holds it: a scanned decimal
// keeps the stored text's exponent, and String() would drop its trailing
// zeros ("60.00" -> "60").
func storedText(d decimal.Decimal) string {
	if d.Exponent() < 0 {
		return d.StringFixed(-d.Exponent())
	}
	return d.String()
}

// put stores the caller's own manual close for an instrument on a date,
// replacing any earlier manual close for that date.
//
//	@Summary	Set a manual close price
//	@Tags		prices
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"instrument id"
//	@Param		date	path		string				true	"market date, YYYY-MM-DD, 1990-01-01 to today"
//	@Param		body	body		price.closeRequest	true	"close as a positive decimal string"
//	@Success	200		{object}	price.priceResponse	"source is manual"
//	@Failure	400		{object}	object{error=string}	"date must be YYYY-MM-DD; date must not be in the future; date must be on or after 1990-01-01; invalid request body; close: must have at most 15 integer and 8 fraction digits; close must be a positive decimal"
//	@Failure	404		{object}	object{error=string}	"instrument not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/instruments/{id}/prices/{date} [put]
func (h *handler) put(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	in, ok := h.instrumentFromPath(w, r)
	if !ok {
		return
	}
	date := r.PathValue("date")
	// Manual entry has no legitimate future use; the bulletin fetcher only
	// writes past dates.
	if err := CheckDate(date); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "date "+err.Error())
		return
	}

	var req closeRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	if TooManyDigits(req.Close) {
		// Same wording as the transaction parser, so the frontend maps both
		// to decimal.error.tooManyDigits.
		auth.WriteError(w, http.StatusBadRequest, "close: must have at most 15 integer and 8 fraction digits")
		return
	}
	if !ValidClose(req.Close) {
		auth.WriteError(w, http.StatusBadRequest, "close must be a positive decimal")
		return
	}
	// Store and echo the normalized form ("0010.50" -> "10.5"), as
	// transactions do through decimal.String(). ValidClose
	// already guarantees a plain positive decimal, so parsing cannot fail.
	normalized := decimal.RequireFromString(req.Close).String()

	// Per-user override: stored in manual_prices, not the shared
	// prices table, so one user's manual entry never overwrites another
	// user's entry or a fetched (bist/us/tefas) price.
	if err := h.manual.Upsert(r.Context(), userID, in.ID, date, normalized); err != nil {
		// A user deleted after the session check gets the 401 from here.
		auth.WriteInternalError(w, r, err)
		return
	}

	auth.WriteJSON(w, http.StatusOK, priceResponse{
		InstrumentID: in.ID,
		Date:         date,
		Close:        normalized,
		Source:       manualSource,
	})
}

// history returns instrumentID's price history newest first: the shared
// fetched rows (the prices table) merged with the caller's own manual
// overrides (manual_prices), each row labelled by source. Without
// the manual rows a user cannot see -- let alone find and delete -- a
// wrong-dated manual entry that shadows every fetched close.
//
//	@Summary	List an instrument's prices
//	@Tags		prices
//	@Produce	json
//	@Param		id		path		int		true	"instrument id"
//	@Param		limit	query		int		false	"rows to return, default 30, clamped to 500"
//	@Success	200		{array}		price.priceResponse	"newest first; source is manual for the caller's own overrides"
//	@Failure	400		{object}	object{error=string}	"limit must be a positive integer"
//	@Failure	404		{object}	object{error=string}	"instrument not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/instruments/{id}/prices [get]
func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	in, ok := h.instrumentFromPath(w, r)
	if !ok {
		return
	}

	limit := defaultHistoryLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n <= 0 {
			auth.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if n > maxHistoryLimit {
			n = maxHistoryLimit
		}
		limit = n
	}

	fetched, err := h.repo.History(r.Context(), in.ID, limit)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	manual, err := h.manual.History(r.Context(), userID, in.ID, limit)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	results := mergeDesc(fetched, manual, limit)

	out := make([]priceResponse, len(results))
	for i, p := range results {
		out[i] = toResponse(p)
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// mergeDesc merges two date-descending price series into one, keeping at
// most limit rows. On a shared date the manual row wins, the same rule
// valuation applies (holdings.MergeSeries) -- calling that here would need
// an import cycle, since holdings imports this package.
func mergeDesc(fetched, manual []Price, limit int) []Price {
	out := make([]Price, 0, len(fetched)+len(manual))
	i, j := 0, 0
	for i < len(fetched) && j < len(manual) && len(out) < limit {
		switch {
		case fetched[i].Date > manual[j].Date:
			out = append(out, fetched[i])
			i++
		case fetched[i].Date < manual[j].Date:
			out = append(out, manual[j])
			j++
		default:
			out = append(out, manual[j])
			i++
			j++
		}
	}
	for ; i < len(fetched) && len(out) < limit; i++ {
		out = append(out, fetched[i])
	}
	for ; j < len(manual) && len(out) < limit; j++ {
		out = append(out, manual[j])
	}
	return out
}

// delete removes the caller's own manual price for instrumentID on date.
// Only manual_prices is touched: the shared fetched rows are market data no
// single user may remove. 404 when the caller has no manual row for that
// date, so a wrong date reads as not found rather than a silent success.
//
//	@Summary	Delete a manual close price
//	@Tags		prices
//	@Produce	json
//	@Param		id		path	int		true	"instrument id"
//	@Param		date	path	string	true	"market date, YYYY-MM-DD"
//	@Success	204		"deleted"
//	@Failure	400		{object}	object{error=string}	"date must be YYYY-MM-DD"
//	@Failure	404		{object}	object{error=string}	"instrument not found; price not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/instruments/{id}/prices/{date} [delete]
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	in, ok := h.instrumentFromPath(w, r)
	if !ok {
		return
	}
	date := r.PathValue("date")
	if !ValidDate(date) {
		auth.WriteError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}

	deleted, err := h.manual.Delete(r.Context(), userID, in.ID, date)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if !deleted {
		auth.WriteError(w, http.StatusNotFound, "price not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
