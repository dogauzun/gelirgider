package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// setupHandler serves first-time setup: while no admin exists, whoever
// holds the one-time token from the server log creates the first admin.
type setupHandler struct {
	*handler
	mu    sync.Mutex // guards token and the admin check-and-create
	token string     // in memory only; "" once setup is done or not needed
}

type setupRequest struct {
	Token string `json:"token"`
	credentials
}

// RegisterSetupRoutes wires the public setup endpoints into mux. When no
// admin exists it generates a setup token and logs it once at WARN; the
// token is never stored, so a restart prints a new one. onRegister is the
// same seeding callback RegisterRoutes takes.
func RegisterSetupRoutes(mux *http.ServeMux, logger *slog.Logger, users *user.Repo, sessions *Repo, cookieSecure bool, onRegister func(userID int64, locale string) error) {
	h := &setupHandler{handler: &handler{users: users, sessions: sessions, cookieSecure: cookieSecure, onRegister: onRegister}}
	switch n, err := users.CountAdmins(context.Background()); {
	case err != nil:
		logger.Error("setup: count admins failed, setup disabled until restart", "error", err.Error())
	case n == 0:
		b := make([]byte, 32)
		rand.Read(b) // never returns an error since Go 1.24; it crashes instead
		h.token = hex.EncodeToString(b)
		logger.Warn("setup required: open /setup and enter this token", "setup_token", h.token)
	}
	mux.HandleFunc("GET /api/setup/status", h.status)
	mux.Handle("POST /api/setup/admin", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.createAdmin)))
}

// status handles GET /api/setup/status.
//
//	@Summary	Setup status
//	@Description	needs_setup is true while no admin exists. Needs no session.
//	@Tags		setup
//	@Produce	json
//	@Success	200	{object}	object{needs_setup=bool}
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/setup/status [get]
func (h *setupHandler) status(w http.ResponseWriter, r *http.Request) {
	n, err := h.users.CountAdmins(r.Context())
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"needs_setup": n == 0})
}

// createAdmin handles POST /api/setup/admin. A new email becomes a new
// admin user, seeded like a registration; an existing one is promoted only
// when the password matches. Success starts a session like login.
//
//	@Summary	Create the first admin
//	@Description	Needs the one-time setup token from the server log. Sets the `session` cookie. Rate-limited per client IP, 10 per minute.
//	@Tags		setup
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.setupRequest	true	"setup token, email, password and locale (tr or en, default tr)"
//	@Success	201	{object}	auth.userResponse
//	@Header		201	{string}	Set-Cookie	"session=<id>; Path=/; Expires=<30 days>; HttpOnly; SameSite=Lax; Secure when COOKIE_SECURE=true"
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid email; email must be at most 254 characters; password must not be only whitespace; password must be at least 8 characters; password must be at most 72 bytes; locale: must be one of tr, en"
//	@Failure	403	{object}	object{error=string}	"invalid setup token; invalid credentials"
//	@Failure	409	{object}	object{error=string}	"setup already done; email already registered"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/setup/admin [post]
func (h *setupHandler) createAdmin(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	// ponytail: one global lock, fine for a once-per-install endpoint.
	h.mu.Lock()
	defer h.mu.Unlock()

	n, err := h.users.CountAdmins(r.Context())
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if n > 0 {
		WriteError(w, http.StatusConflict, "setup already done")
		return
	}
	// An empty token means none was issued at startup; never let "" match "".
	if h.token == "" || subtle.ConstantTimeCompare([]byte(req.Token), []byte(h.token)) != 1 {
		WriteError(w, http.StatusForbidden, "invalid setup token")
		return
	}

	email, locale, msg := validateRegistration(req.credentials)
	if msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}

	u, err := h.users.GetByEmail(r.Context(), email)
	switch {
	case err == nil:
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
			WriteError(w, http.StatusForbidden, "invalid credentials")
			return
		}
	case errors.Is(err, user.ErrNotFound):
		u, err = h.createUser(r.Context(), email, req.Password, locale)
		if errors.Is(err, user.ErrEmailTaken) {
			WriteError(w, http.StatusConflict, "email already registered")
			return
		}
		if err != nil {
			WriteInternalError(w, r, err)
			return
		}
	default:
		WriteInternalError(w, r, err)
		return
	}
	if err := h.users.SetAdmin(r.Context(), u.ID, true); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	h.token = ""

	session, err := h.sessions.CreateSession(r.Context(), u.ID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	http.SetCookie(w, h.cookie(session.ID, session.ExpiresAt))
	WriteJSON(w, http.StatusCreated, userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: true})
}
