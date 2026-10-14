package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestSessionTokenNotStored checks that the sessions table holds only a hash
// of the token, so a copy of the database gives no working login, while the
// token itself still authenticates.
func TestSessionTokenNotStored(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	u, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessions := NewRepo(conn)
	s, err := sessions.CreateSession(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var stored string
	if err := conn.QueryRow("SELECT id FROM sessions").Scan(&stored); err != nil {
		t.Fatalf("select session id: %v", err)
	}
	if stored == s.ID {
		t.Errorf("sessions.id holds the plaintext token")
	}
	if _, err := sessions.GetSession(context.Background(), stored); err == nil {
		t.Errorf("GetSession(stored id) succeeded, want the DB value to be useless as a cookie")
	}
	if got, err := sessions.GetSession(context.Background(), s.ID); err != nil || got != u.ID {
		t.Errorf("GetSession(token) = %d, %v; want %d, nil", got, err, u.ID)
	}
}
