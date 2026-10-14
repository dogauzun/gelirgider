package instrument

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/textcheck"
)

// searchLimit caps how many rows GET /api/instruments returns.
const searchLimit = 20

// maxSymbolRunes and maxNameRunes cap what an admin adds, since every
// user's search lists it. The seed lists peak at 11 runes for a
// symbol and about 250 for a name, and both stay far below maxSearchRunes.
const (
	maxSymbolRunes = 32
	maxNameRunes   = 300
)

// RegisterRoutes wires the instrument search endpoint into mux.
func RegisterRoutes(mux *http.ServeMux, repo *Repo) {
	h := &handler{repo: repo}
	mux.HandleFunc("GET /api/instruments", h.search)
}

// RegisterAdminRoutes wires the admin-only instrument routes into admin,
// the mux server.New mounts behind auth.RequireAdmin. Instruments are a
// global table, so only an admin adds to it.
func RegisterAdminRoutes(admin *http.ServeMux, repo *Repo) {
	h := &handler{repo: repo}
	admin.HandleFunc("POST /api/admin/instruments", h.add)
}

type handler struct {
	repo *Repo
}

// FromPath reads an instrument id from r's key path value and looks it up,
// writing the 404 itself for a malformed id or an unknown instrument and the
// 500 for any other error, as portfolio.Owned does for portfolios. The bool
// reports whether the caller may go on. key is the route's wildcard name:
// {id} under /api/instruments, {instrument_id} under a portfolio.
func FromPath(w http.ResponseWriter, r *http.Request, repo *Repo, key string) (Instrument, bool) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "instrument not found")
		return Instrument{}, false
	}
	in, err := repo.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "instrument not found")
		return Instrument{}, false
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return Instrument{}, false
	}
	return in, true
}

type instrumentResponse struct {
	ID       int64  `json:"id"`
	Symbol   string `json:"symbol"`
	Market   string `json:"market"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

// search finds instruments by symbol or name, at most searchLimit of them.
//
//	@Summary	Search instruments
//	@Tags		instruments
//	@Produce	json
//	@Param		q	query		string	false	"symbol or name to search for; each word must appear in the name or start the symbol, in any order; empty returns an empty array"
//	@Success	200	{array}		instrument.instrumentResponse	"at most 20 matches"
//	@Failure	500	{object}	object{error=string}			"internal error"
//	@Router		/instruments [get]
func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 1 {
		auth.WriteJSON(w, http.StatusOK, []instrumentResponse{})
		return
	}

	results, err := h.repo.Search(r.Context(), q, searchLimit)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	out := make([]instrumentResponse, len(results))
	for i, in := range results {
		out[i] = instrumentResponse{
			ID:       in.ID,
			Symbol:   in.Symbol,
			Market:   in.Market,
			Name:     in.Name,
			Currency: in.Currency,
		}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

type addRequest struct {
	Market   string `json:"market"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

// seededPair reports whether market and currency match one of seedSources,
// so a new seeded market is accepted here without a second list to update.
func seededPair(market, currency string) bool {
	for _, src := range seedSources {
		if src.market == market && src.currency == currency {
			return true
		}
	}
	return false
}

// add inserts an instrument missing from the seed lists. It is idempotent:
// a known market+symbol returns the existing instrument, name unchanged.
//
//	@Summary	Add an instrument
//	@Tags		admin
//	@Accept		json
//	@Produce	json
//	@Param		body	body		instrument.addRequest			true	"market and currency must be a pair the seed lists use, e.g. US/USD or CRYPTO/TRY; symbol is trimmed, upper-cased and at most 32 characters; name is trimmed and at most 300 characters"
//	@Success	200		{object}	instrument.instrumentResponse	"the new instrument, or the existing one for a known market and symbol"
//	@Failure	400		{object}	object{error=string}			"invalid request body; market and currency must be a supported pair; symbol is required; name is required; symbol must be at most 32 characters; name must be at most 300 characters; symbol must not contain control characters; name must not contain control characters"
//	@Failure	500		{object}	object{error=string}			"internal error"
//	@Router		/admin/instruments [post]
func (h *handler) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	if !seededPair(req.Market, req.Currency) {
		auth.WriteError(w, http.StatusBadRequest, "market and currency must be a supported pair")
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	name := strings.TrimSpace(req.Name)
	switch {
	case symbol == "":
		auth.WriteError(w, http.StatusBadRequest, "symbol is required")
		return
	case name == "":
		auth.WriteError(w, http.StatusBadRequest, "name is required")
		return
	case utf8.RuneCountInString(symbol) > maxSymbolRunes:
		auth.WriteError(w, http.StatusBadRequest, "symbol must be at most 32 characters")
		return
	case utf8.RuneCountInString(name) > maxNameRunes:
		auth.WriteError(w, http.StatusBadRequest, "name must be at most 300 characters")
		return
	// Only combining marks render blank, which is refused for portfolio
	// names too.
	case textcheck.HasInvisible(symbol), !strings.ContainsFunc(symbol, textcheck.IsVisible):
		auth.WriteError(w, http.StatusBadRequest, "symbol must not contain control characters")
		return
	case textcheck.HasInvisible(name), !strings.ContainsFunc(name, textcheck.IsVisible):
		auth.WriteError(w, http.StatusBadRequest, "name must not contain control characters")
		return
	}

	id, err := h.repo.Insert(r.Context(), req.Market, symbol, name, req.Currency)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	in, err := h.repo.Get(r.Context(), id)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, instrumentResponse{
		ID:       in.ID,
		Symbol:   in.Symbol,
		Market:   in.Market,
		Name:     in.Name,
		Currency: in.Currency,
	})
}
