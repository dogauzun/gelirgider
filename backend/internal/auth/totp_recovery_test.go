package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (e *loginEnv) recoveryRows(used bool) int {
	e.t.Helper()
	q := "SELECT COUNT(*) FROM totp_recovery_codes WHERE user_id = ?"
	if used {
		q += " AND used_at IS NOT NULL"
	}
	var n int
	if err := e.conn.QueryRow(q, e.userID).Scan(&n); err != nil {
		e.t.Fatalf("count recovery codes: %v", err)
	}
	return n
}

func TestNewRecoveryCodes(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil || len(codes) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("newRecoveryCodes = %d codes, %d hashes, %v", len(codes), len(hashes), err)
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 11 || c[5] != '-' || seen[c] || recoveryCodeHash(c) != hashes[i] {
			t.Fatalf("code %d = %q", i, c)
		}
		seen[c] = true
	}
	if recoveryCodeHash("abcde fghij") != recoveryCodeHash("ABCDE-FGHIJ") {
		t.Error("hash differs for the same code typed in another case and with a space")
	}
}

func TestRecoveryCodeLogsInOnce(t *testing.T) {
	e := newLoginEnv(t)
	if len(e.codes) != recoveryCodeCount {
		t.Fatalf("confirm returned %d codes, want %d", len(e.codes), recoveryCodeCount)
	}
	lastStep := e.state().LastStep
	typed := strings.ToLower(strings.ReplaceAll(e.codes[0], "-", " "))

	rec := e.code(e.challenge(), typed)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("recovery code = %d %s, want 200 with a session", rec.Code, rec.Body)
	}
	wantError(t, e.code(e.challenge(), e.codes[0]), http.StatusUnauthorized, "invalid code")
	if got := e.state().LastStep; got != lastStep {
		t.Errorf("last step = %d after a recovery login, want %d", got, lastStep)
	}
	// The authenticator still works, the other codes are untouched.
	if rec := e.code(e.challenge(), totpCode(e.key, e.next)); rec.Code != http.StatusOK {
		t.Errorf("authenticator code after a recovery login = %d %s", rec.Code, rec.Body)
	}
	if got := e.recoveryRows(true); got != 1 {
		t.Errorf("%d used codes, want 1", got)
	}
}

func TestRecoveryGuessesShareTheAttemptCap(t *testing.T) {
	e := newLoginEnv(t)
	token := e.challenge()
	for range maxChallengeAttempts {
		wantError(t, e.code(token, "AAAAA-AAAAA"), http.StatusUnauthorized, "invalid code")
	}
	wantError(t, e.code(token, e.codes[0]), http.StatusUnauthorized, "login expired")
	if got := e.recoveryRows(true); got != 0 {
		t.Errorf("%d used codes, want 0", got)
	}
}

func TestRegenerateRecoveryCodes(t *testing.T) {
	e := newLoginEnv(t)
	regen := func(password, code string) *httptest.ResponseRecorder {
		return e.post("/api/account/totp/recovery-codes", map[string]string{"password": password, "code": code})
	}
	wantError(t, regen("wrong-password", totpCode(e.key, e.next)), http.StatusUnauthorized, "current password is incorrect")
	wantError(t, regen("right-password", "ZZZZZ-ZZZZZ"), http.StatusBadRequest, "invalid code")

	// A recovery code authorises the call and is gone with the rest.
	rec := regen("right-password", e.codes[0])
	var fresh recoveryCodesResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &fresh) != nil || len(fresh.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("regenerate = %d %s, want 200 with %d codes", rec.Code, rec.Body, recoveryCodeCount)
	}
	if got := e.recoveryRows(false); got != recoveryCodeCount {
		t.Errorf("%d code rows, want %d", got, recoveryCodeCount)
	}
	wantError(t, e.code(e.challenge(), e.codes[1]), http.StatusUnauthorized, "invalid code")
	if rec := e.code(e.challenge(), fresh.RecoveryCodes[0]); rec.Code != http.StatusOK {
		t.Errorf("new code = %d %s, want 200", rec.Code, rec.Body)
	}

	// An authenticator code works too.
	if rec := regen("right-password", totpCode(e.key, e.next)); rec.Code != http.StatusOK {
		t.Errorf("regenerate with an authenticator code = %d %s", rec.Code, rec.Body)
	}
}

func TestDisableTOTPWithRecoveryCode(t *testing.T) {
	e := newLoginEnv(t)
	if got := e.recoveryRows(false); got != recoveryCodeCount {
		t.Fatalf("%d code rows after confirm, want %d", got, recoveryCodeCount)
	}
	rec := e.do(http.MethodDelete, "/api/account/totp", map[string]string{"password": "right-password", "code": e.codes[3]})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("disable with a recovery code = %d %s, want 204", rec.Code, rec.Body)
	}
	if e.totpEnabled() {
		t.Error("totp_enabled still true")
	}
	if got := e.recoveryRows(false); got != 0 {
		t.Errorf("%d code rows left after disable, want 0", got)
	}
}
