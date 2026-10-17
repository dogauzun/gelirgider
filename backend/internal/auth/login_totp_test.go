package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// loginEnv is a totpEnv whose user has TOTP on, served through the public
// auth routes. key is the user's secret; next is the step after the one
// enrolment used, whose code is the first one the login accepts.
type loginEnv struct {
	*totpEnv
	pub  http.Handler
	key  []byte
	next int64
}

func newLoginEnv(t *testing.T) *loginEnv {
	t.Helper()
	e := &loginEnv{totpEnv: newTOTPEnv(t)}
	if err := e.users.SetAdmin(context.Background(), e.userID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	key, step := e.enrol()
	e.key, e.next = key, step+1
	mux := http.NewServeMux()
	RegisterRoutes(mux, e.users, e.sessions, e.cfg, false, nil)
	e.pub = mux
	return e
}

func (e *loginEnv) postPub(path string, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.pub.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

// login does a password login and returns the response.
func (e *loginEnv) login() *httptest.ResponseRecorder {
	e.t.Helper()
	return e.postPub("/api/auth/login", `{"email":"alice@example.com","password":"right-password"}`)
}

// challenge logs in with the right password and returns the token.
func (e *loginEnv) challenge() string {
	e.t.Helper()
	rec := e.login()
	var resp totpChallengeResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resp) != nil || !resp.TOTPRequired || resp.Token == "" {
		e.t.Fatalf("login = %d %s, want 200 with totp_required and a token", rec.Code, rec.Body)
	}
	return resp.Token
}

func (e *loginEnv) code(token, code string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.postPub("/api/auth/login/totp", `{"token":"`+token+`","code":"`+code+`"}`)
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), msg) {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, msg)
	}
}

func TestLoginTOTPSecondStep(t *testing.T) {
	e := newLoginEnv(t)
	good := totpCode(e.key, e.next)

	rec := e.login()
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("password step set a cookie: %v", rec.Result().Cookies())
	}
	wantError(t, e.postPub("/api/auth/login", `{"email":"alice@example.com","password":"wrong-password"}`), http.StatusUnauthorized, "invalid email or password")

	token := e.challenge()
	wantError(t, e.code(token, "000000"), http.StatusUnauthorized, "invalid code")
	rec = e.code(token, good)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"email":"alice@example.com"`) {
		t.Fatalf("right code = %d %s, want 200 with the user", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName {
		t.Fatalf("cookies = %v, want the session", cookies)
	}
	if !e.sessionAlive(cookies[0].Value) {
		t.Error("session from the second step is not valid")
	}
	// The challenge is spent, and the code's step with it.
	wantError(t, e.code(token, good), http.StatusUnauthorized, "login expired")
	wantError(t, e.code(e.challenge(), good), http.StatusUnauthorized, "invalid code")
}

func TestLoginTOTPSixthAttemptFails(t *testing.T) {
	e := newLoginEnv(t)
	token := e.challenge()
	for i := 0; i < maxChallengeAttempts; i++ {
		wantError(t, e.code(token, "000000"), http.StatusUnauthorized, "invalid code")
	}
	wantError(t, e.code(token, totpCode(e.key, e.next)), http.StatusUnauthorized, "login expired")
}

func TestLoginTOTPUserAttemptCap(t *testing.T) {
	e := newLoginEnv(t)
	ctx := context.Background()
	for range maxUserAttempts / maxChallengeAttempts {
		token, err := e.sessions.CreateChallenge(ctx, e.userID)
		if err != nil {
			t.Fatalf("CreateChallenge below the cap: %v", err)
		}
		for range maxChallengeAttempts {
			if _, ok, err := e.sessions.AttemptChallenge(ctx, token); !ok || err != nil {
				t.Fatalf("AttemptChallenge = %v, %v", ok, err)
			}
		}
	}
	wantError(t, e.login(), http.StatusTooManyRequests, "too many requests")
}

func TestLoginTOTPChallengeDoesNotSurviveReset(t *testing.T) {
	e := newLoginEnv(t)
	token := e.challenge()
	if err := SetPassword(context.Background(), e.users, e.sessions, "alice@example.com", "brand-new-password"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	wantError(t, e.code(token, totpCode(e.key, e.next)), http.StatusUnauthorized, "login expired")
	if _, ok, err := e.sessions.AttemptChallenge(context.Background(), token); ok || err != nil {
		t.Errorf("AttemptChallenge after reset = %v, %v; want the row gone", ok, err)
	}
	if got := e.recoveryRows(false); got != 0 {
		t.Errorf("%d recovery code rows after reset, want 0", got)
	}
}

func TestLoginTOTPChallengeExpires(t *testing.T) {
	e := newLoginEnv(t)
	token := e.challenge()
	if _, err := e.sessions.db.Exec("UPDATE login_challenges SET expires_at = ?", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatalf("expire: %v", err)
	}
	wantError(t, e.code(token, totpCode(e.key, e.next)), http.StatusUnauthorized, "login expired")
}
