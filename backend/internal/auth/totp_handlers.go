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
//	@Description	Needs a session. Checks the code against the pending secret, turns two-factor on and returns 10 one-time recovery codes, shown only this once. Signs out every other session of the user. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.totpConfirmRequest	true	"6-digit code from the authenticator"
//	@Success	200	{object}	auth.recoveryCodesResponse
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
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	err = h.users.ConfirmTOTP(r.Context(), userID, state.Pending, step, hashes)
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
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

// recoveryCodesResponse carries a user's new recovery codes, the only time
// their plaintext is shown.
type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"` // 10 codes, "ABCDE-FGHIJ"
}

// checkSecondFactor reports whether code is a valid second factor of the
// user: an authenticator code, which must be newer than the last accepted
// step (the step is the replay guard, so of two requests with one code one
// wins), or else a recovery code, which is spent by this call and leaves
// the step alone.
func (h *handler) checkSecondFactor(ctx context.Context, userID int64, state user.TOTPState, code string) (bool, error) {
	if !isTOTPCode(code) {
		return h.users.RedeemRecoveryCode(ctx, userID, recoveryCodeHash(code))
	}
	secret, err := h.settings.Open(totpAAD(userID), state.Secret)
	if err != nil {
		return false, err
	}
	step, ok := verifyTOTP(secret, code, time.Now(), state.LastStep)
	if !ok {
		return false, nil
	}
	return h.users.AdvanceTOTPStep(ctx, userID, step)
}

// authorizeTOTPChange checks the password and a second-factor code for a
// request that changes the user's two-factor state. It writes the error and
// returns false when the request must stop.
func (h *handler) authorizeTOTPChange(w http.ResponseWriter, r *http.Request, userID int64, password, code string) bool {
	u, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		WriteError(w, http.StatusUnauthorized, "current password is incorrect")
		return false
	}
	if !u.TOTPEnabled {
		WriteError(w, http.StatusConflict, "two-factor authentication is off")
		return false
	}
	state, err := h.users.TOTP(r.Context(), userID)
	if err != nil {
		WriteInternalError(w, r, err)
		return false
	}
	ok, err := h.checkSecondFactor(r.Context(), userID, state, code)
	if err != nil {
		WriteInternalError(w, r, err)
		return false
	}
	if !ok {
		WriteError(w, http.StatusBadRequest, "invalid code")
	}
	return ok
}

type totpDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// totpDisable handles DELETE /api/account/totp.
//
//	@Summary	Turn two-factor authentication off
//	@Description	Needs a session, the current password and a current code, so a stolen session alone cannot drop the second factor. The code is a 6-digit authenticator code or an unused recovery code. An authenticator code at or below the last accepted step is a replay and is rejected. Deletes the user's recovery codes. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Param		body	body		auth.totpDisableRequest	true	"current password and a 6-digit code from the authenticator or a recovery code"
//	@Success	204
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid code"
//	@Failure	401	{object}	object{error=string}	"current password is incorrect; unauthorized"
//	@Failure	409	{object}	object{error=string}	"two-factor authentication is off"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/account/totp [delete]
func (h *handler) totpDisable(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req totpDisableRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if !h.authorizeTOTPChange(w, r, userID, req.Password, req.Code) {
		return
	}
	if err := h.users.DisableTOTP(r.Context(), userID); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// totpRecoveryCodes handles POST /api/account/totp/recovery-codes.
//
//	@Summary	Regenerate recovery codes
//	@Description	Needs a session, the current password and a current code (a 6-digit authenticator code or an unused recovery code). Replaces all of the user's recovery codes with 10 new ones in one transaction and returns them; they are shown only this once. Rate-limited per client IP, 10 per minute.
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		auth.totpDisableRequest	true	"current password and a 6-digit code from the authenticator or a recovery code"
//	@Success	200	{object}	auth.recoveryCodesResponse
//	@Failure	400	{object}	object{error=string}	"invalid request body; invalid code"
//	@Failure	401	{object}	object{error=string}	"current password is incorrect; unauthorized"
//	@Failure	409	{object}	object{error=string}	"two-factor authentication is off"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/account/totp/recovery-codes [post]
func (h *handler) totpRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	userID := MustUserID(r.Context())

	var req totpDisableRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if !h.authorizeTOTPChange(w, r, userID, req.Password, req.Code) {
		return
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		WriteInternalError(w, r, err)
		return
	}
	if err := h.users.ReplaceRecoveryCodes(r.Context(), userID, hashes); err != nil {
		WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}
