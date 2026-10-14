package portfolio

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/textcheck"
)

const maxNameLen = 100

// RegisterRoutes wires the portfolio endpoints into mux. mux must be
// mounted behind middleware that calls auth.RequireUser, so that
// auth.MustUserID is available in the request context.
//
// GET /api/portfolios is registered separately by the server package
// (registerSummaryRoutes in summary.go; the handler is summaryHandler.list in
// portfolio_list.go) -- it needs internal/transaction to compute each
// portfolio's total value, which would import-cycle back into this package
// (see the comment on registerSummaryRoutes for the same reasoning).
func RegisterRoutes(mux *http.ServeMux, repo *Repo) {
	h := &handler{repo: repo}
	mux.HandleFunc("POST /api/portfolios", h.create)
	mux.HandleFunc("PATCH /api/portfolios/{id}", h.rename)
	mux.HandleFunc("DELETE /api/portfolios/{id}", h.delete)
}

type handler struct {
	repo *Repo
}

type nameRequest struct {
	Name string `json:"name"`
}

type portfolioResponse struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func toResponse(p Portfolio) portfolioResponse {
	return portfolioResponse{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt.Format(time.RFC3339)}
}

func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n > 0 && n <= maxNameLen
}

// checkName trims a submitted portfolio name and validates it, writing the
// 400 itself. The second result reports whether the caller may go on.
//
// Control characters (newline, BEL, DEL...) pass through JSON untouched but
// render as nothing, so the stored name is not the one the user typed and
// two visibly identical names can differ. Format characters
// (zero-width space, direction marks, RTL override) are rejected for the same
// reason: they are invisible or reorder the text around them.
// A name made only of combining marks (and spaces between them) has nothing
// for the marks to sit on and renders blank too, so it gets the same 400.
//
// Every inner run of whitespace (double space, no-break space, em space...)
// is collapsed to one ASCII space, so names that render alike are stored
// alike and the duplicate check sees them as one. The length limit
// applies to that stored name, so a long inner run does not count against it.
// The control check still sees the name before collapsing, because
// newline and tab are whitespace that collapsing would hide.
func checkName(w http.ResponseWriter, name string) (string, bool) {
	stored := collapseSpace(name)
	if !validName(stored) {
		auth.WriteError(w, http.StatusBadRequest, "name must be 1-100 characters")
		return "", false
	}
	if textcheck.HasInvisible(strings.TrimSpace(name)) || !strings.ContainsFunc(stored, textcheck.IsVisible) {
		auth.WriteError(w, http.StatusBadRequest, "name must not contain control characters")
		return "", false
	}
	return stored, true
}

// Owned reads the portfolio id from r's {id} path value and checks that the
// signed-in user owns it, writing the 404 itself for a malformed id or a
// portfolio that is not the caller's (any Get error, as the handlers did
// before it). The last result reports whether the caller may go on. r must
// be behind auth.RequireUser.
func Owned(w http.ResponseWriter, r *http.Request, repo *Repo) (userID, id int64, ok bool) {
	userID = auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return 0, 0, false
	}
	if _, err := repo.Get(r.Context(), userID, id); err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return 0, 0, false
	}
	return userID, id, true
}

// collapseSpace trims s and replaces every run of unicode.IsSpace runes
// inside it with one ASCII space.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// create adds a portfolio for the caller.
//
//	@Summary	Create a portfolio
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		body	body		portfolio.nameRequest	true	"name, trimmed, inner whitespace runs collapsed to one space, 1-100 characters"
//	@Success	201		{object}	portfolio.portfolioResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; name must be 1-100 characters; name must not contain control characters"
//	@Failure	409		{object}	object{error=string}	"name already used"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios [post]
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	var req nameRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	name, ok := checkName(w, req.Name)
	if !ok {
		return
	}
	p, err := h.repo.Create(r.Context(), userID, name)
	if err != nil {
		if errors.Is(err, ErrNameTaken) {
			auth.WriteError(w, http.StatusConflict, "name already used")
			return
		}
		// A user deleted after the session check gets the 401 from here.
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusCreated, toResponse(p))
}

// rename changes the name of one of the caller's portfolios.
//
//	@Summary	Rename a portfolio
//	@Tags		portfolios
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int						true	"portfolio id"
//	@Param		body	body	portfolio.nameRequest	true	"new name, trimmed, inner whitespace runs collapsed to one space, 1-100 characters"
//	@Success	200		"renamed, empty body"
//	@Failure	400		{object}	object{error=string}	"invalid request body; name must be 1-100 characters; name must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	409		{object}	object{error=string}	"name already used"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id} [patch]
func (h *handler) rename(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	var req nameRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	name, ok := checkName(w, req.Name)
	if !ok {
		return
	}
	if err := h.repo.Rename(r.Context(), userID, id, name); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "portfolio not found")
			return
		}
		if errors.Is(err, ErrNameTaken) {
			auth.WriteError(w, http.StatusConflict, "name already used")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// delete removes one of the caller's portfolios. A portfolio that still has
// transactions is refused unless ?force=true, which deletes them with it.
//
//	@Summary	Delete a portfolio
//	@Tags		portfolios
//	@Produce	json
//	@Param		id		path	int		true	"portfolio id"
//	@Param		force	query	bool	false	"true also deletes the portfolio's transactions; anything else refuses a portfolio that has any"
//	@Success	204		"deleted"
//	@Failure	404		{object}	object{error=string}							"portfolio not found"
//	@Failure	409		{object}	object{error=string,transaction_count=int}	"portfolio has transactions (transaction_count is how many)"
//	@Failure	500		{object}	object{error=string}							"internal error"
//	@Router		/portfolios/{id} [delete]
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	// Deleting a portfolio's transactions with it is destructive and cannot
	// be undone, so it happens only when asked for by name: anything but
	// ?force=true keeps the old refusal.
	cascade := r.URL.Query().Get("force") == "true"
	n, err := h.repo.Delete(r.Context(), userID, id, cascade)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "portfolio not found")
			return
		}
		if errors.Is(err, ErrHasTransactions) {
			// Same status and same `error` string as before; the count is
			// added so the UI can name it in the second confirmation
			// ("Bu portföyde 40 işlem var...") without a second request.
			auth.WriteJSON(w, http.StatusConflict, map[string]any{
				"error":             "portfolio has transactions",
				"transaction_count": n,
			})
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
