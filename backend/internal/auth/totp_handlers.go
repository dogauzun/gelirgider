package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/user"
)

type totpSetupRequest struct {
	Password string `json:"password"`
}

type totpSetupResponse struct {
	Secret string `json:"secret"` // base32, for manual entry
	URI    string `json:"uri"`    // otpauth:// URI for a QR code
}

// totpSetup handles POST /api/account/totp/setup.
//
//	@Summary	Start two-factor enrolment
//	@Description	Needs a session and the current password, so a stolen session cannot enrol an attacker's authenticator. Stores a new pending secret (a second call replaces it) and returns it with its otpauth URI. Two-factor stays off until confirm. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.totpSetupRequest	true	"current password"
//	@Success	200	{object}	auth.totpSetupResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body"
//	@Failure	401	{object}	object{error=string}	"current password is incorrect; unauthorized"
//	@Failure	409	{object}	object{error=string}	"two-factor authentication is already on"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/account/totp/setup [post]
func (h *handler) totpSetup(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req totpSetupRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		WriteError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	secret, err := newTOTPSecret()
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	sealed, err := h.settings.Seal(totpAAD(userID), secret)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	err = h.users.SetTOTPPending(r.Context(), userID, sealed)
	if errors.Is(err, user.ErrTOTPOn) {
		WriteError(w, http.StatusConflict, "two-factor authentication is already on")
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, totpSetupResponse{Secret: secret, URI: totpURI(secret, u.Email)})
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

// totpConfirm handles POST /api/account/totp/confirm. On success it deletes
// every other session of the user, keeping only the one making this request.
//
//	@Summary	Confirm two-factor enrolment
//	@Description	Needs a session. Checks the code against the pending secret and turns two-factor on. Signs out every other session of the user. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Param		body	body		auth.totpConfirmRequest	true	"6-digit code from the authenticator"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid code"
//	@Failure	401	{object}	object{error=string}	"unauthorized"
//	@Failure	409	{object}	object{error=string}	"no pending two-factor setup"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/account/totp/confirm [post]
func (h *handler) totpConfirm(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req totpConfirmRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	const noPending = "no pending two-factor setup"
	state, err := h.users.TOTP(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if state.Pending == "" {
		WriteError(w, http.StatusConflict, noPending)
		return
	}
	secret, err := h.settings.Open(totpAAD(userID), state.Pending)
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	step, ok := verifyTOTP(secret, req.Code, time.Now(), state.LastStep)
	if !ok {
		WriteError(w, http.StatusBadRequest, "invalid code")
		return
	}
	err = h.users.ConfirmTOTP(r.Context(), userID, state.Pending, step)
	if errors.Is(err, user.ErrNoPendingTOTP) {
		WriteError(w, http.StatusConflict, noPending)
		return
	}
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	// The secret is already committed: a client disconnect must not skip
	// signing out the other sessions.
	if err := h.deleteOtherSessions(context.WithoutCancel(r.Context()), r, userID); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
