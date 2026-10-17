package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestSetPassword covers the operator escape hatch: the new password replaces
// the old one, the TOTP columns are cleared, every session of that user is deleted, and another user's
// session is left alone.
func TestSetPassword(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	users := user.NewRepo(conn)
	sessions := NewRepo(conn)

	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	alice, err := users.Create(context.Background(), "alice@example.com", string(oldHash))
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", string(oldHash))
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := conn.Exec("UPDATE users SET totp_secret = 'sealed', totp_pending_secret = 'pending', totp_last_step = 42 WHERE id = ?", alice.ID); err != nil {
		t.Fatalf("set totp columns: %v", err)
	}

	var aliceSessions []string
	for i := 0; i < 2; i++ {
		s, err := sessions.CreateSession(context.Background(), alice.ID)
		if err != nil {
			t.Fatalf("create alice session: %v", err)
		}
		aliceSessions = append(aliceSessions, s.ID)
	}
	bobSession, err := sessions.CreateSession(context.Background(), bob.ID)
	if err != nil {
		t.Fatalf("create bob session: %v", err)
	}

	// Email is normalized the same way login normalizes it.
	if err := SetPassword(context.Background(), users, sessions, "  Alice@Example.com ", "new-password"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	updated, err := users.GetByID(context.Background(), alice.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("new-password")); err != nil {
		t.Errorf("new password does not match stored hash: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("old-password")) == nil {
		t.Error("old password still matches stored hash")
	}

	totp, err := users.TOTP(context.Background(), alice.ID)
	if err != nil {
		t.Fatalf("TOTP: %v", err)
	}
	if totp != (user.TOTPState{}) {
		t.Errorf("totp state after reset = %+v, want zero", totp)
	}

	for _, id := range aliceSessions {
		if _, err := sessions.GetSession(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("session %q: err = %v, want ErrSessionNotFound", id, err)
		}
	}
	if _, err := sessions.GetSession(context.Background(), bobSession.ID); err != nil {
		t.Errorf("another user's session was affected: %v", err)
	}

	if err := SetPassword(context.Background(), users, sessions, "alice@example.com", "short"); err == nil {
		t.Error("short password accepted, want error")
	}
	// Rejected by validatePassword before bcrypt sees it, so the operator gets
	// the same message as the HTTP paths instead of bcrypt's own complaint.
	if err := SetPassword(context.Background(), users, sessions, "alice@example.com", strings.Repeat("a", maxPasswordLen+1)); err == nil || err.Error() != "password must be at most 72 bytes" {
		t.Errorf("long password: err = %v, want validation message", err)
	}
	if err := SetPassword(context.Background(), users, sessions, "nobody@example.com", "new-password"); !errors.Is(err, user.ErrNotFound) {
		t.Errorf("unknown email: err = %v, want user.ErrNotFound", err)
	}
}
