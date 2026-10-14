package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestCreateSessionPurgesExpired covers the cleanup: a session whose
// expires_at is in the past is gone from the table after the next login,
// while still-valid sessions survive.
func TestCreateSessionPurgesExpired(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	users := user.NewRepo(conn)
	sessions := NewRepo(conn)

	alice, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// An expired row, as left behind by a user who stopped logging in.
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := conn.Exec(
		"INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		"stale", alice.ID, past.Add(-sessionTTL).Format(time.RFC3339), past.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	bobSession, err := sessions.CreateSession(context.Background(), bob.ID)
	if err != nil {
		t.Fatalf("CreateSession(bob): %v", err)
	}

	// A fresh login purges the expired row...
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", "stale").Scan(&count); err != nil {
		t.Fatalf("count stale session: %v", err)
	}
	if count != 0 {
		t.Errorf("expired session row count = %d, want 0", count)
	}
	if _, err := sessions.GetSession(context.Background(), "stale"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("GetSession(stale) error = %v, want %v", err, ErrSessionNotFound)
	}

	// ...and leaves valid sessions alone, including one created before it.
	if _, err := sessions.GetSession(context.Background(), bobSession.ID); err != nil {
		t.Errorf("GetSession(bob) after purge: %v", err)
	}
	aliceSession, err := sessions.CreateSession(context.Background(), alice.ID)
	if err != nil {
		t.Fatalf("CreateSession(alice): %v", err)
	}
	if _, err := sessions.GetSession(context.Background(), aliceSession.ID); err != nil {
		t.Errorf("GetSession(alice) after purge: %v", err)
	}
	if _, err := sessions.GetSession(context.Background(), bobSession.ID); err != nil {
		t.Errorf("GetSession(bob) after second purge: %v", err)
	}
}
