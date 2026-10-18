package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/watchlist"
)

// watchlistFetchDays is how far back adding an instrument fetches its price,
// so the row has a last close and a previous close at once.
const watchlistFetchDays = 14

// registerWatchlistRoutes wires POST and GET /api/watchlist and DELETE
// /api/watchlist/{id}. onDemandFetch is the transaction routes' price hook.
func registerWatchlistRoutes(mux *http.ServeMux, d *deps, onDemandFetch transaction.OnDemandFetch) {
	h := &watchlistHandler{deps: d, onDemandFetch: onDemandFetch}
	mux.HandleFunc("POST /api/watchlist", h.create)
	mux.HandleFunc("GET /api/watchlist", h.list)
	mux.HandleFunc("DELETE /api/watchlist/{id}", h.delete)
}

type watchlistHandler struct {
	*deps
	onDemandFetch transaction.OnDemandFetch
}

// watchRequest is the POST body. note may be omitted.
type watchRequest struct {
	InstrumentID int64  `json:"instrument_id"`
	Note         string `json:"note"`
}

type watchItemResponse struct {
	ID           int64   `json:"id"`
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Market       string  `json:"market"`
	Currency     string  `json:"currency"`
	TargetPrice  *string `json:"target_price"`
	TargetSide   *string `json:"target_side"`
	Note         string  `json:"note"`
	CreatedAt    string  `json:"created_at"`
}

// toWatchItemResponse builds the response of it, whose instrument is in.
func toWatchItemResponse(it watchlist.Item, in instrument.Instrument) watchItemResponse {
	out := watchItemResponse{
		ID: it.ID, InstrumentID: it.InstrumentID, Symbol: in.Symbol, Name: in.Name, Market: in.Market,
		Currency: in.Currency, TargetSide: it.TargetSide, Note: it.Note, CreatedAt: it.CreatedAt.Format(time.RFC3339),
	}
	if it.TargetPrice != nil {
		s := it.TargetPrice.String()
		out.TargetPrice = &s
	}
	return out
}

// create adds an instrument to the caller's watchlist.
//
//	@Summary	Add an instrument to the watchlist
//	@Description	Watches the instrument for the caller, one row per user and instrument. Fetches the instrument's price dated 14 days back, best effort, so the row has closes at once. target_price and target_side are null until a target is set.
//	@Tags		watchlist
//	@Accept		json
//	@Produce	json
//	@Param		body	body		server.watchRequest	true	"instrument_id of a non-DEPOSIT instrument; note optional, at most 500 characters"
//	@Success	201		{object}	server.watchItemResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; instrument_id: unknown instrument; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	409		{object}	object{error=string}	"already on the watchlist"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/watchlist [post]
func (h *watchlistHandler) create(w http.ResponseWriter, r *http.Request) {
	var req watchRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	in, err := h.instruments.Get(r.Context(), req.InstrumentID)
	if errors.Is(err, instrument.ErrNotFound) || (err == nil && in.Market == instrument.MarketDeposit) {
		auth.WriteError(w, http.StatusBadRequest, "instrument_id: unknown instrument")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	note, err := transaction.ParseNote(req.Note)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	it, err := h.watchlist.Insert(r.Context(), watchlist.Item{UserID: auth.MustUserID(r.Context()), InstrumentID: in.ID, Note: note})
	if errors.Is(err, watchlist.ErrDuplicate) {
		auth.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	from, _ := time.Parse(time.DateOnly, price.Today())
	transaction.EnsurePrice(r.Context(), h.onDemandFetch, in, transaction.Transaction{
		InstrumentID: &in.ID, Currency: in.Currency, Date: from.AddDate(0, 0, -watchlistFetchDays).Format(time.DateOnly),
	})
	auth.WriteJSON(w, http.StatusCreated, toWatchItemResponse(it, in))
}

// list returns the caller's watchlist.
//
//	@Summary	List the watchlist
//	@Description	Every row of the caller with its instrument, sorted by symbol then id.
//	@Tags		watchlist
//	@Produce	json
//	@Success	200	{array}		server.watchItemResponse
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/watchlist [get]
func (h *watchlistHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.watchlist.List(r.Context(), auth.MustUserID(r.Context()))
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.InstrumentID
	}
	ins, err := h.instruments.ByIDs(r.Context(), ids)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	slices.SortStableFunc(items, func(a, b watchlist.Item) int {
		return strings.Compare(ins[a.InstrumentID].Symbol, ins[b.InstrumentID].Symbol)
	})
	out := make([]watchItemResponse, len(items))
	for i, it := range items {
		out[i] = toWatchItemResponse(it, ins[it.InstrumentID])
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// delete removes a row from the caller's watchlist.
//
//	@Summary	Remove an instrument from the watchlist
//	@Tags		watchlist
//	@Param		id	path	int	true	"watchlist item id"
//	@Success	204
//	@Failure	404	{object}	object{error=string}	"watchlist item not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/watchlist/{id} [delete]
func (h *watchlistHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	err := watchlist.ErrNotFound
	if ok {
		err = h.watchlist.Delete(r.Context(), auth.MustUserID(r.Context()), id)
	}
	if errors.Is(err, watchlist.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
