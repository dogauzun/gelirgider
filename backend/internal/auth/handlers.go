package auth

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/textcheck"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

const (
	cookieName     = "session"
	minPasswordLen = 8
	// maxPasswordLen is bcrypt's own input limit, in bytes. Anything longer
	// makes bcrypt.GenerateFromPassword fail, which is a client error, not a 500.
	maxPasswordLen = 72

	// authRateLimit and authRateWindow bound credential-guessing attempts
	// on the login, register, setup and password-change endpoints.
	// Hardcoded, not configurable.
	authRateLimit  = 10
	authRateWindow = time.Minute
)

// RegisterRoutes wires the register/login/logout and auth config endpoints
// into mux. Login and register are individually rate-limited per client IP.
// Register answers 403 while the registration toggle in cfg is closed, and
// both register and login answer 403 until first-time setup is done.
// onRegister, if non-nil, is called with the id of a newly created user and
// the validated registration locale (tr or en) right after registration
// succeeds (e.g. to seed default per-user data). A failure there fails the
// registration request.
func RegisterRoutes(mux *http.ServeMux, users *user.Repo, sessions *Repo, cfg *settings.Repo, cookieSecure bool, onRegister func(userID int64, locale string) error) {
	h := &handler{users: users, sessions: sessions, settings: cfg, cookieSecure: cookieSecure, onRegister: onRegister}
	mux.HandleFunc("GET /api/auth/config", h.authConfig)
	mux.Handle("POST /api/auth/register", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.register)))
	mux.Handle("POST /api/auth/login", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.login)))
	mux.HandleFunc("POST /api/auth/logout", h.logout)
}

// RegisterProtectedRoutes wires endpoints that require an authenticated
// user into mux. The caller must wrap mux with (*Repo).RequireUser.
// The password change is rate-limited per client IP like login: it checks
// the current password, so a stolen session could otherwise guess it.
func RegisterProtectedRoutes(mux *http.ServeMux, users *user.Repo, sessions *Repo) {
	h := &handler{users: users, sessions: sessions}
	mux.HandleFunc("GET /api/me", h.me)
	mux.HandleFunc("PUT /api/me/reporting-currency", h.updateReportingCurrency)
	mux.Handle("POST /api/account/password", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.changePassword)))
}

type handler struct {
	users        *user.Repo
	sessions     *Repo
	settings     *settings.Repo
	cookieSecure bool
	onRegister   func(userID int64, locale string) error
}

// dummyHash is compared against on login with an unknown email, so that path
// pays the same bcrypt cost as a wrong password and response time does not
// reveal whether an email is registered. Built once, on first use.
var dummyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
})

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Locale   string `json:"locale"` // register only: tr (default) or en; login ignores it
}

type userResponse struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	ReportingCurrency string `json:"reporting_currency"`
	IsAdmin           bool   `json:"is_admin"`
}

// setupPending answers 403 setup_required and returns true while no admin
// exists: until first-time setup is done only the setup endpoint may create
// or sign in to an account (ADR 0010).
func (h *handler) setupPending(w http.ResponseWriter, r *http.Request) bool {
	switch n, err := h.users.CountAdmins(r.Context()); {
	case err != nil:
		WriteInternalError(w, r, err)
		return true
	case n == 0:
		WriteError(w, http.StatusForbidden, "setup_required")
		return true
	}
	return false
}

// register handles POST /api/auth/register.
//
//	@Summary	Register a user
//	@Description	Creates a user and seeds its default portfolio in the given locale. Does not sign in: no cookie is set, call login next. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.credentials	true	"email, password and locale (tr or en, default tr)"
//	@Success	201	{object}	auth.userResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid email; email must be at most 254 characters; password must not be only whitespace; password must be at least 8 characters; password must be at most 72 bytes; locale: must be one of tr, en"
//	@Failure	403	{object}	object{error=string}	"setup_required; registration closed"
//	@Failure	404	{object}	object{error=string}	"user not found: an admin deleted the new user while it was being created"
//	@Failure	409	{object}	object{error=string}	"email already registered"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/auth/register [post]
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	if h.setupPending(w, r) {
		return
	}
	switch open, err := registrationOpen(r.Context(), h.settings); {
	case err != nil:
		WriteInternalError(w, r, err)
		return
	case !open:
		WriteError(w, http.StatusForbidden, "registration closed")
		return
	}
	var creds credentials
	if !DecodeJSON(w, r, &creds) {
		return
	}
	email, locale, msg := validateRegistration(creds)
	if msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}

	u, err := h.createUser(r.Context(), email, creds.Password, locale)
	if errors.Is(err, user.ErrEmailTaken) {
		WriteError(w, http.StatusConflict, "email already registered")
		return
	}
	// An admin deleted the new user before its default portfolio was seeded:
	// the 404 the admin's create-user route gives for it.
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}

	WriteJSON(w, http.StatusCreated, userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: u.IsAdmin})
}

// validateRegistration checks creds the way registration does and returns
// the normalized email and the locale (tr when empty), or a 400 message.
func validateRegistration(creds credentials) (email, locale, msg string) {
	email = normalizeEmail(creds.Email)
	if msg := validateEmail(email); msg != "" {
		return "", "", msg
	}
	if msg := validatePassword(creds.Password); msg != "" {
		return "", "", msg
	}
	locale = creds.Locale
	if locale == "" {
		locale = "tr"
	}
	if locale != "tr" && locale != "en" {
		return "", "", "locale: must be one of tr, en"
	}
	return email, locale, ""
}

// createUser stores a new user with password's bcrypt hash and runs
// onRegister for it, as registration does.
func (h *handler) createUser(ctx context.Context, email, password, locale string) (user.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return user.User{}, err
	}
	u, err := h.users.Create(ctx, email, string(hash))
	if err != nil {
		return user.User{}, err
	}
	if h.onRegister != nil {
		if err := h.onRegister(u.ID, locale); err != nil {
			return user.User{}, err
		}
	}
	return u, nil
}

// login handles POST /api/auth/login.
//
//	@Summary	Sign in
//	@Description	Checks email and password and sets the `session` cookie. The locale field is ignored. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.credentials	true	"email and password"
//	@Success	200	{object}	auth.userResponse
//	@Header		200	{string}	Set-Cookie	"session=<id>; Path=/; Expires=<30 days>; HttpOnly; SameSite=Lax; Secure when COOKIE_SECURE=true"
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	401	{object}	object{error=string}	"invalid email or password"
//	@Failure	403	{object}	object{error=string}	"setup_required"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/auth/login [post]
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if h.setupPending(w, r) {
		return
	}
	var creds credentials
	if !DecodeJSON(w, r, &creds) {
		return
	}
	// Not run through validateEmail on purpose: addresses that were accepted
	// before that check existed must keep working as login identifiers. An
	// address that is not in the database falls through to badCreds below.
	email := normalizeEmail(creds.Email)

	const badCreds = "invalid email or password"

	u, err := h.users.GetByEmail(r.Context(), email)
	if err != nil {
		// Same message and same bcrypt cost for unknown email and wrong
		// password below, so we never leak which one was wrong.
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(creds.Password))
		WriteError(w, http.StatusUnauthorized, badCreds)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(creds.Password)) != nil {
		WriteError(w, http.StatusUnauthorized, badCreds)
		return
	}

	session, err := h.sessions.CreateSession(r.Context(), u.ID)
	// Deleted after the password check: the answer an unknown account gets.
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, badCreds)
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	http.SetCookie(w, h.cookie(session.ID, session.ExpiresAt))

	WriteJSON(w, http.StatusOK, userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: u.IsAdmin})
}

// logout handles POST /api/auth/logout.
//
//	@Summary	Sign out
//	@Description	Deletes the current session, if any, and clears the `session` cookie. Needs no session: always answers 204.
//	@Tags		auth
//	@Success	204
//	@Header		204	{string}	Set-Cookie	"session=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0; HttpOnly; SameSite=Lax; Secure when COOKIE_SECURE=true"
//	@Router		/auth/logout [post]
func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		// Logout always answers 204, so a client disconnect must not skip
		// the delete and leave the session valid.
		h.sessions.DeleteSession(context.WithoutCancel(r.Context()), c.Value)
	}
	expired := h.cookie("", time.Unix(0, 0))
	expired.MaxAge = -1
	http.SetCookie(w, expired)
	w.WriteHeader(http.StatusNoContent)
}

// me handles GET /api/me.
//
//	@Summary	Current user
//	@Description	Needs a session.
//	@Tags		auth
//	@Produce	json
//	@Success	200	{object}	auth.userResponse
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/me [get]
func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())
	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: u.IsAdmin})
}

type reportingCurrencyRequest struct {
	ReportingCurrency string `json:"reporting_currency"`
}

// updateReportingCurrency handles PUT /api/me/reporting-currency.
//
//	@Summary	Set reporting currency
//	@Description	Needs a session.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.reportingCurrencyRequest	true	"TRY, USD or EUR"
//	@Success	200	{object}	auth.userResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body; reporting_currency: must be one of TRY, USD, EUR"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/me/reporting-currency [put]
func (h *handler) updateReportingCurrency(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req reportingCurrencyRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if !currency.IsValid(req.ReportingCurrency) {
		WriteError(w, http.StatusBadRequest, "reporting_currency: must be one of TRY, USD, EUR")
		return
	}

	if err := h.users.UpdateReportingCurrency(r.Context(), userID, req.ReportingCurrency); err != nil {
		WriteInternalError(w, r, err)
		return
	}

	// An admin deleted the user since the session check, before or after
	// the update: WriteInternalError answers 401.
	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: u.IsAdmin})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword handles POST /api/account/password. On success it deletes
// every other session of the user, keeping only the one making this request.
//
//	@Summary	Change own password
//	@Description	Needs a session. Signs out every other session of the user. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.changePasswordRequest	true	"current and new password"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body; password must not be only whitespace; password must be at least 8 characters; password must be at most 72 bytes"
//	@Failure	401	{object}	object{error=string}	"current password is incorrect; unauthorized"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/account/password [post]
func (h *handler) changePassword(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req changePasswordRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if msg := validatePassword(req.NewPassword); msg != "" {
		WriteError(w, http.StatusBadRequest, msg)
		return
	}

	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.CurrentPassword)) != nil {
		WriteError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if err := h.users.UpdatePasswordHash(r.Context(), userID, string(hash)); err != nil {
		WriteInternalError(w, r, err)
		return
	}

	// RequireUser guarantees the cookie, so keep is the session making this
	// request. If it ever went missing, an empty keep signs out every session
	// of this user instead of leaving them all valid: the password has
	// already changed, so failing closed is the only safe direction.
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = c.Value
	}
	// The new hash is already committed: a client disconnect must not skip
	// signing out the other sessions, so the delete ignores cancellation.
	if err := h.sessions.DeleteOtherSessions(context.WithoutCancel(r.Context()), userID, keep); err != nil {
		WriteInternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// validatePassword returns an error message for password, or "" when it is
// acceptable. The minimum counts characters, matching its message and the
// frontend minLength; the maximum counts bytes, because that is the unit
// bcrypt itself limits. A password made only of whitespace and format
// characters (zero-width space, BOM...) is rejected outright, but
// the password itself is never trimmed: padding a real password with spaces
// is the user's choice and has to keep working at login.
func validatePassword(password string) string {
	switch {
	case !strings.ContainsFunc(password, func(r rune) bool { return !unicode.IsSpace(r) && !unicode.Is(unicode.Cf, r) }):
		return "password must not be only whitespace"
	case utf8.RuneCountInString(password) < minPasswordLen:
		return "password must be at least 8 characters"
	case len(password) > maxPasswordLen:
		return "password must be at most 72 bytes"
	}
	return ""
}

// validateEmail returns an error message for email, or "" when it is
// acceptable. It is the only place registration decides what an address may
// look like; login and SetPassword only normalize, so rows created before
// this check keep working.
//
// net/mail rejects "a@", "@b", "x@y@z" and "a b@c.com", but it also accepts
// a display-name form ("Alice <a@b.com>") and a dotless domain ("a@b"),
// neither of which is usable as a login identifier or a delivery address.
// The 254-character cap is the RFC 5321 path limit.
//
// net/mail also accepts any UTF-8 in the local part, so a control or format
// character (U+200B...), a line separator or a blank filler
// (U+3164, U+2800...) is rejected here: it would make one address
// render like another in the admin user list. HasInvisible lets an emoji
// joiner and a subdivision flag's tags through for names;
// an address has no such exception, so every control and format character
// is refused as well.
//
// So are space separators (Zs: U+00A0, U+2003, U+3000...) and the rest of
// Unicode's default-ignorable set, the characters a renderer draws as
// nothing: the invisible marks U+034F, U+17B4, U+17B5 and the variation
// selectors. With Cc and Cf that covers every default-ignorable
// code point by property rather than by list, while a visible combining
// accent still passes.
func validateEmail(email string) string {
	if utf8.RuneCountInString(email) > 254 {
		return "email must be at most 254 characters"
	}
	if textcheck.HasInvisible(email) || strings.ContainsFunc(email, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zs, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector)
	}) {
		return "invalid email"
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "invalid email"
	}
	if domain := email[strings.LastIndex(email, "@")+1:]; !strings.Contains(domain, ".") {
		return "invalid email"
	}
	return ""
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
