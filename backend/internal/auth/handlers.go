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
// into mux. Login, its second step and register are individually
// rate-limited per client IP.
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
	mux.Handle("POST /api/auth/login/totp", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.loginTOTP)))
	mux.HandleFunc("POST /api/auth/logout", h.logout)
}

// RegisterProtectedRoutes wires endpoints that require an authenticated
// user into mux. The caller must wrap mux with (*Repo).RequireUser.
// The password change is rate-limited per client IP like login: it checks
// the current password, so a stolen session could otherwise guess it. The
// two-factor enrolment and disable routes are limited the same way; cfg seals the secret.
func RegisterProtectedRoutes(mux *http.ServeMux, users *user.Repo, sessions *Repo, cfg *settings.Repo) {
	h := &handler{users: users, sessions: sessions, settings: cfg}
	mux.HandleFunc("GET /api/me", h.me)
	mux.HandleFunc("PUT /api/me/reporting-currency", h.updateReportingCurrency)
	mux.HandleFunc("GET /api/me/attention-rules", h.getAttentionRules)
	mux.HandleFunc("PUT /api/me/attention-rules", h.updateAttentionRules)
	mux.Handle("POST /api/account/password", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.changePassword)))
	mux.Handle("POST /api/account/totp/setup", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.totpSetup)))
	mux.Handle("POST /api/account/totp/confirm", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.totpConfirm)))
	mux.Handle("DELETE /api/account/totp", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.totpDisable)))
	mux.Handle("POST /api/account/totp/recovery-codes", ratelimit.Limit(authRateLimit, authRateWindow)(http.HandlerFunc(h.totpRecoveryCodes)))
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

// totpChallengeResponse is login's answer for a user with two-factor on.
type totpChallengeResponse struct {
	TOTPRequired bool   `json:"totp_required"`
	Token        string `json:"token"`
}

type userResponse struct {
	ID                int64  `json:"id"`
	Email             string `json:"email"`
	ReportingCurrency string `json:"reporting_currency"`
	IsAdmin           bool   `json:"is_admin"`
	TOTPEnabled       bool   `json:"totp_enabled"`
}

func newUserResponse(u user.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, ReportingCurrency: u.ReportingCurrency, IsAdmin: u.IsAdmin, TOTPEnabled: u.TOTPEnabled}
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

	WriteJSON(w, http.StatusCreated, newUserResponse(u))
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
//	@Description	Checks email and password. Without two-factor it sets the `session` cookie and answers with the user. With two-factor on it sets no cookie and answers 200 with {"totp_required": true, "token": "..."} (the token is valid 5 minutes) instead of the user: send the token and a code to /auth/login/totp. A user whose recent tokens already hold 10 code checks gets 429, even with the right password. The locale field is ignored. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.credentials	true	"email and password"
//	@Success	200	{object}	auth.userResponse	"the user; or {totp_required, token} when two-factor is on"
//	@Header		200	{string}	Set-Cookie	"session=<id>; Path=/; Expires=<30 days>; HttpOnly; SameSite=Lax; Secure when COOKIE_SECURE=true; set only when two-factor is off"
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	401	{object}	object{error=string}	"invalid email or password"
//	@Failure	403	{object}	object{error=string}	"setup_required"
//	@Failure	429	{object}	object{error=string}	"too many requests"
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

	if u.TOTPEnabled {
		token, err := h.sessions.CreateChallenge(r.Context(), u.ID)
		switch {
		case errors.Is(err, ErrTooManyChallenges):
			WriteError(w, http.StatusTooManyRequests, "too many requests")
		// Deleted after the password check: the answer an unknown account gets.
		case errors.Is(err, user.ErrNotFound):
			WriteError(w, http.StatusUnauthorized, badCreds)
		case err != nil:
			WriteInternalError(w, r, err)
		default:
			w.Header().Set("Cache-Control", "no-store")
			WriteJSON(w, http.StatusOK, totpChallengeResponse{TOTPRequired: true, Token: token})
		}
		return
	}

	h.startSession(w, r, u, badCreds)
}

// startSession creates a session for u, sets the cookie and answers with the
// user. A user deleted since the caller checked them gets 401 with notFoundMsg.
func (h *handler) startSession(w http.ResponseWriter, r *http.Request, u user.User, notFoundMsg string) {
	session, err := h.sessions.CreateSession(r.Context(), u.ID)
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, notFoundMsg)
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	http.SetCookie(w, h.cookie(session.ID, session.ExpiresAt))

	WriteJSON(w, http.StatusOK, newUserResponse(u))
}

type loginTOTPRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

// loginTOTP handles POST /api/auth/login/totp.
//
//	@Summary	Sign in, second step
//	@Description	Trades the token login returned for a session, given the current 6-digit code or an unused recovery code, and sets the `session` cookie. A code that is not 6 digits is tried as a recovery code and is spent by a match; it counts against the token's 5 checks like any other. A token allows 5 code checks and lives 5 minutes; an unknown, expired or used-up token, and one whose user turned two-factor off since, all answer 401 login expired. An authenticator code at or below the last accepted step is a replay and answers 401 invalid code. A secret sealed under another SECRET_KEY answers 401 two-step secret unreadable; an admin's password reset turns two-step login off (ADR 0025). Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.loginTOTPRequest	true	"token from login and a 6-digit code from the authenticator or a recovery code"
//	@Success	200	{object}	auth.userResponse
//	@Header		200	{string}	Set-Cookie	"session=<id>; Path=/; Expires=<30 days>; HttpOnly; SameSite=Lax; Secure when COOKIE_SECURE=true"
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	401	{object}	object{error=string}	"login expired; invalid code; two-step secret unreadable, ask an admin to reset your password"
//	@Failure	403	{object}	object{error=string}	"setup_required"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/auth/login/totp [post]
func (h *handler) loginTOTP(w http.ResponseWriter, r *http.Request) {
	if h.setupPending(w, r) {
		return
	}
	var req loginTOTPRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	const expired = "login expired"

	// The attempt is counted before the code is looked at.
	userID, ok, err := h.sessions.AttemptChallenge(r.Context(), req.Token)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusUnauthorized, expired)
		return
	}
	state, err := h.users.TOTP(r.Context(), userID)
	if errors.Is(err, user.ErrNotFound) || (err == nil && state.Secret == "") {
		WriteError(w, http.StatusUnauthorized, expired)
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	ok, err = h.checkSecondFactor(r.Context(), userID, state, req.Code)
	if err != nil {
		writeSecretError(w, r, userID, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusUnauthorized, "invalid code")
		return
	}
	u, err := h.users.GetByID(r.Context(), userID)
	if errors.Is(err, user.ErrNotFound) {
		WriteError(w, http.StatusUnauthorized, expired)
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if err := h.sessions.DeleteChallenge(r.Context(), req.Token); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	h.startSession(w, r, u, expired)
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
	WriteJSON(w, http.StatusOK, newUserResponse(u))
}

type reportingCurrencyRequest struct {
	ReportingCurrency string `json:"reporting_currency"`
}

// updateReportingCurrency handles PUT /api/me/reporting-currency.
//
//	@Summary	Set reporting currency
//	@Description	Needs a session. Refused with 409 while the user has a goal the new currency would break: delete the goals first.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.reportingCurrencyRequest	true	"TRY, USD or EUR"
//	@Success	200	{object}	auth.userResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body; reporting_currency: must be one of TRY, USD, EUR"
//	@Failure	409	{object}	object{error=string}	"goals depend on the reporting currency"
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
		if errors.Is(err, user.ErrGoalsDependOnCurrency) {
			WriteError(w, http.StatusConflict, "goals depend on the reporting currency")
			return
		}
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
	WriteJSON(w, http.StatusOK, newUserResponse(u))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// checkCurrentPassword reloads the user and compares password with the stored
// bcrypt hash. It writes the error (401 on a mismatch) and returns false when
// the request must stop.
func (h *handler) checkCurrentPassword(w http.ResponseWriter, r *http.Request, userID int64, password string) (user.User, bool) {
	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return user.User{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		WriteError(w, http.StatusUnauthorized, "current password is incorrect")
		return user.User{}, false
	}
	return u, true
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

	if _, ok := h.checkCurrentPassword(w, r, userID, req.CurrentPassword); !ok {
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

	// The new hash is already committed: a client disconnect must not skip
	// signing out the other sessions, so the delete ignores cancellation.
	if err := h.deleteOtherSessions(context.WithoutCancel(r.Context()), r, userID); err != nil {
		WriteInternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// deleteOtherSessions deletes every session of userID except the one making
// r. RequireUser guarantees the cookie; if it ever went missing, an empty
// keep signs out every session of this user instead of leaving them all
// valid: the caller has already changed a credential, so failing closed is
// the only safe direction.
func (h *handler) deleteOtherSessions(ctx context.Context, r *http.Request, userID int64) error {
	keep := ""
	if c, err := r.Cookie(cookieName); err == nil {
		keep = c.Value
	}
	return h.sessions.DeleteOtherSessions(ctx, userID, keep)
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
	const invalid = "invalid email"
	if utf8.RuneCountInString(email) > 254 {
		return "email must be at most 254 characters"
	}
	if textcheck.HasInvisible(email) || strings.ContainsFunc(email, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zs, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector)
	}) {
		return invalid
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return invalid
	}
	if domain := email[strings.LastIndex(email, "@")+1:]; !strings.Contains(domain, ".") {
		return invalid
	}
	return ""
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
