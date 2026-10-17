package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

type totpEnv struct {
	t        *testing.T
	users    *user.Repo
	sessions *Repo
	cfg      *settings.Repo
	srv      http.Handler
	userID   int64
	cookie   string // the session making the requests
	other    string // a second session of the same user
}

func newTOTPEnv(t *testing.T) *totpEnv {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	e := &totpEnv{t: t, users: user.NewRepo(conn), sessions: NewRepo(conn), cfg: settings.NewRepo(conn, bytes.Repeat([]byte{1}, 32))}
	hash, err := bcrypt.GenerateFromPassword([]byte("right-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	u, err := e.users.Create(context.Background(), "alice@example.com", string(hash))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	e.userID = u.ID
	mux := http.NewServeMux()
	RegisterProtectedRoutes(mux, e.users, e.sessions, e.cfg)
	e.srv = e.sessions.RequireUser(mux)
	for _, dst := range []*string{&e.cookie, &e.other} {
		s, err := e.sessions.CreateSession(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		*dst = s.ID
	}
	return e
}

func (e *totpEnv) post(path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: e.cookie})
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *totpEnv) setup(password string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.post("/api/account/totp/setup", map[string]string{"password": password})
}

func (e *totpEnv) state() user.TOTPState {
	e.t.Helper()
	s, err := e.users.TOTP(context.Background(), e.userID)
	if err != nil {
		e.t.Fatalf("TOTP: %v", err)
	}
	return s
}

func (e *totpEnv) sessionAlive(id string) bool {
	e.t.Helper()
	_, err := e.sessions.GetSession(context.Background(), id)
	if err != nil && !errors.Is(err, ErrSessionNotFound) {
		e.t.Fatalf("GetSession: %v", err)
	}
	return err == nil
}

func TestTOTPSetupWrongPassword(t *testing.T) {
	e := newTOTPEnv(t)
	rec := e.setup("wrong-password")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "current password is incorrect") {
		t.Fatalf("got %d %s, want 401 current password is incorrect", rec.Code, rec.Body)
	}
	if s := e.state(); s.Pending != "" || s.Secret != "" {
		t.Errorf("state after rejected setup = %+v, want empty", s)
	}
}

func TestTOTPEnrolment(t *testing.T) {
	e := newTOTPEnv(t)
	rec := e.setup("right-password")
	if rec.Code != http.StatusOK {
		t.Fatalf("setup = %d %s", rec.Code, rec.Body)
	}
	var resp totpSetupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret == "" || !strings.Contains(resp.URI, resp.Secret) {
		t.Fatalf("response = %+v", resp)
	}
	pending := e.state().Pending
	if pending == "" || strings.Contains(pending, resp.Secret) {
		t.Fatalf("pending secret not stored sealed: %q", pending)
	}

	// A second setup replaces the pending secret.
	rec = e.setup("right-password")
	var second totpSetupResponse
	json.Unmarshal(rec.Body.Bytes(), &second)
	if second.Secret == resp.Secret || e.state().Pending == pending {
		t.Error("second setup did not replace the pending secret")
	}

	if rec := e.post("/api/account/totp/confirm", map[string]string{"code": "000000"}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid code") {
		t.Fatalf("wrong code = %d %s, want 400 invalid code", rec.Code, rec.Body)
	}
	if s := e.state(); s.Secret != "" || s.Pending == "" || !e.sessionAlive(e.other) {
		t.Errorf("rejected confirm changed state: %+v", s)
	}

	key, err := totpEncoding.DecodeString(second.Secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	step := time.Now().Unix() / totpStepSeconds
	if rec := e.post("/api/account/totp/confirm", map[string]string{"code": totpCode(key, step)}); rec.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body)
	}
	s := e.state()
	if s.Secret == "" || s.Pending != "" || s.LastStep != step {
		t.Errorf("state after confirm = %+v, want secret set, pending cleared, last step %d", s, step)
	}
	plain, err := e.cfg.Open(totpAAD(e.userID), s.Secret)
	if err != nil || plain != second.Secret {
		t.Errorf("stored secret opens to %q, %v; want %q", plain, err, second.Secret)
	}
	if !e.sessionAlive(e.cookie) || e.sessionAlive(e.other) {
		t.Error("confirm must keep the current session and end the other one")
	}

	if rec := e.setup("right-password"); rec.Code != http.StatusConflict {
		t.Errorf("setup with TOTP on = %d, want 409", rec.Code)
	}
	if rec := e.post("/api/account/totp/confirm", map[string]string{"code": totpCode(key, step)}); rec.Code != http.StatusConflict {
		t.Errorf("confirm with TOTP on = %d, want 409", rec.Code)
	}
}

func TestTOTPConfirmWithoutSetup(t *testing.T) {
	e := newTOTPEnv(t)
	if rec := e.post("/api/account/totp/confirm", map[string]string{"code": "123456"}); rec.Code != http.StatusConflict {
		t.Errorf("confirm without pending secret = %d, want 409", rec.Code)
	}
}
