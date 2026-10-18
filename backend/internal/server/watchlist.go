package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/watchlist"
	"github.com/shopspring/decimal"
)

// watchlistFetchDays is how far back adding an instrument fetches its price,
// so the row has a last close and a previous close at once.
const watchlistFetchDays = 14

// registerWatchlistRoutes wires POST and GET /api/watchlist and PUT and DELETE
// /api/watchlist/{id}. onDemandFetch is the transaction routes' price hook.
func registerWatchlistRoutes(mux *http.ServeMux, d *deps, onDemandFetch transaction.OnDemandFetch) {
	h := &watchlistHandler{deps: d, onDemandFetch: onDemandFetch}
	mux.HandleFunc("POST /api/watchlist", h.create)
	mux.HandleFunc("GET /api/watchlist", h.list)
	mux.HandleFunc("PUT /api/watchlist/{id}", h.update)
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

// watchEditRequest is the PUT body. A null or omitted target_price clears the
// target and its side; an omitted note keeps the stored one.
type watchEditRequest struct {
	TargetPrice *string `json:"target_price"`
	Note        *string `json:"note"`
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
	// Whether last_price has reached target_price: at or above it for side
	// "above", at or below it for "below". False without a target or a close.
	TargetReached bool   `json:"target_reached"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
	// The latest close on or before today (the caller's manual price wins on
	// the same date, else the newer row), scaled by the splits since its
	// date, in the instrument's currency; null without a close. Where it came
	// from ("manual" or the fetched source) and its date.
	LastPrice   *string `json:"last_price"`
	PriceDate   *string `json:"price_date"`
	PriceSource *string `json:"price_source"`
	// The latest close dated before price_date, scaled the same way, and its
	// date; null without one.
	PrevClose     *string `json:"prev_close"`
	PrevPriceDate *string `json:"prev_price_date"`
	// last_price - prev_close with 2 decimals; null unless both exist.
	DayChange *string `json:"day_change"`
}

// watchPrice is a watched instrument's last and previous close in today's
// terms. lastDate and prevDate are empty when that close was not found.
type watchPrice struct {
	last, prev           decimal.Decimal
	lastDate, lastSource string
	prevDate             string
}

// watchPrices prices the instruments of items as the summary does: the
// winning close as of today, then the previous close as of the day before the
// winner's date, both scaled by the splits since their dates. Ids are grouped
// by that day so each distinct date costs one LatestCloses call.
func (h *watchlistHandler) watchPrices(ctx context.Context, userID int64, items []watchlist.Item) (map[int64]watchPrice, error) {
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.InstrumentID
	}
	asOf := price.Today()
	winners, err := holdings.LatestCloses(ctx, h.priceSources(), userID, ids, asOf)
	if err != nil {
		return nil, err
	}
	splits, err := h.splits.ListMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]watchPrice, len(winners))
	idsBefore := make(map[string][]int64)
	for id, won := range winners {
		day, err := time.Parse(time.DateOnly, won.Date)
		if err != nil {
			return nil, err
		}
		before := day.AddDate(0, 0, -1).Format(time.DateOnly)
		idsBefore[before] = append(idsBefore[before], id)
		out[id] = watchPrice{last: splitScaled(won.Close, splits[id], won.Date, asOf), lastDate: won.Date, lastSource: won.Source}
	}
	for before, ids := range idsBefore {
		prevRows, err := holdings.LatestCloses(ctx, h.priceSources(), userID, ids, before)
		if err != nil {
			return nil, err
		}
		for id, prev := range prevRows {
			wp := out[id]
			wp.prev, wp.prevDate = splitScaled(prev.Close, splits[id], prev.Date, asOf), prev.Date
			out[id] = wp
		}
	}
	return out, nil
}

// toWatchItemResponse builds the response of it, whose instrument is in and
// whose closes are wp (the zero value when it has none).
func toWatchItemResponse(it watchlist.Item, in instrument.Instrument, wp watchPrice) watchItemResponse {
	out := watchItemResponse{
		ID: it.ID, InstrumentID: it.InstrumentID, Symbol: in.Symbol, Name: in.Name, Market: in.Market,
		Currency: in.Currency, TargetSide: it.TargetSide, Note: it.Note, CreatedAt: it.CreatedAt.Format(time.RFC3339),
	}
	if it.TargetPrice != nil {
		s := it.TargetPrice.String()
		out.TargetPrice = &s
	}
	if wp.lastDate == "" {
		return out
	}
	if it.TargetPrice != nil && it.TargetSide != nil {
		out.TargetReached = *it.TargetSide == watchlist.SideAbove && wp.last.GreaterThanOrEqual(*it.TargetPrice) ||
			*it.TargetSide == watchlist.SideBelow && wp.last.LessThanOrEqual(*it.TargetPrice)
	}
	last := wp.last.String()
	out.LastPrice, out.PriceDate, out.PriceSource = &last, &wp.lastDate, &wp.lastSource
	if wp.prevDate != "" {
		prev, change := wp.prev.String(), wp.last.Sub(wp.prev).StringFixed(2)
		out.PrevClose, out.PrevPriceDate, out.DayChange = &prev, &wp.prevDate, &change
	}
	return out
}

// create adds an instrument to the caller's watchlist.
//
//	@Summary	Add an instrument to the watchlist
//	@Description	Watches the instrument for the caller, one row per user and instrument. Fetches the instrument's price dated 14 days back, best effort, so the row has closes at once. last_price, price_date, price_source, prev_close, prev_price_date and day_change are null until a close exists. target_price and target_side are null until a target is set, and target_reached is false.
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
	prices, err := h.watchPrices(r.Context(), it.UserID, []watchlist.Item{it})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusCreated, toWatchItemResponse(it, in, prices[in.ID]))
}

// list returns the caller's watchlist.
//
//	@Summary	List the watchlist
//	@Description	Every row of the caller with its instrument, sorted by symbol then id, and its last close (the caller's manual price wins on the same date), previous close and day change (last_price minus prev_close, 2 decimals), each null when missing. target_reached says whether last_price is at or above target_price (target_side above) or at or below it (below); false without a target or a close.
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
	prices, err := h.watchPrices(r.Context(), auth.MustUserID(r.Context()), items)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	slices.SortStableFunc(items, func(a, b watchlist.Item) int {
		return strings.Compare(ins[a.InstrumentID].Symbol, ins[b.InstrumentID].Symbol)
	})
	out := make([]watchItemResponse, len(items))
	for i, it := range items {
		out[i] = toWatchItemResponse(it, ins[it.InstrumentID], prices[it.InstrumentID])
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// update sets or clears the target price and sets the note of a row.
//
//	@Summary	Set or clear a watched instrument's target price
//	@Description	Sets target_price and note from the body. A null or omitted target_price clears the target and its side; an omitted note keeps the stored one. With a target, target_side is fixed now: above when the target is above the latest close, else below (the target needs a close to compare with). target_reached on the answer is true when last_price is at or above the target (side above) or at or below it (side below).
//	@Tags		watchlist
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"watchlist item id"
//	@Param		body	body		server.watchEditRequest	true	"target_price a positive decimal string with at most 15 integer and 8 fraction digits, or null; note optional, at most 500 characters"
//	@Success	200		{object}	server.watchItemResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; target_price: must have at most 15 integer and 8 fraction digits; target_price: must be a positive decimal; target_price: no close to compare with; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"watchlist item not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/watchlist/{id} [put]
func (h *watchlistHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		auth.WriteError(w, http.StatusNotFound, watchlist.ErrNotFound.Error())
		return
	}
	var req watchEditRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	userID := auth.MustUserID(r.Context())
	it, err := h.watchlist.Get(r.Context(), userID, id)
	if errors.Is(err, watchlist.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if req.Note != nil {
		if it.Note, err = transaction.ParseNote(*req.Note); err != nil {
			auth.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.TargetPrice != nil && price.TooManyDigits(*req.TargetPrice) {
		auth.WriteError(w, http.StatusBadRequest, "target_price: must have at most 15 integer and 8 fraction digits")
		return
	}
	if req.TargetPrice != nil && !price.ValidClose(*req.TargetPrice) {
		auth.WriteError(w, http.StatusBadRequest, "target_price: must be a positive decimal")
		return
	}
	prices, err := h.watchPrices(r.Context(), userID, []watchlist.Item{it})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	wp := prices[it.InstrumentID]
	it.TargetPrice, it.TargetSide = nil, nil
	if req.TargetPrice != nil {
		if wp.lastDate == "" {
			auth.WriteError(w, http.StatusBadRequest, "target_price: no close to compare with")
			return
		}
		target := decimal.RequireFromString(*req.TargetPrice)
		side := watchlist.SideBelow
		if target.GreaterThan(wp.last) {
			side = watchlist.SideAbove
		}
		it.TargetPrice, it.TargetSide = &target, &side
	}
	if err := h.watchlist.Update(r.Context(), it); errors.Is(err, watchlist.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	in, err := h.instruments.Get(r.Context(), it.InstrumentID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, toWatchItemResponse(it, in, wp))
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
