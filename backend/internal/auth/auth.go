// Package auth manages sessions and the account HTTP handlers: first-run
// setup, register, login, logout, the current user and password change,
// and admin user management, with the RequireUser and RequireAdmin
// middleware. It only depends on database/sql, never on a specific driver.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// ErrSessionNotFound is returned when a session id is unknown or expired.
var ErrSessionNotFound = errors.New("session not found")

// sessionTTL is how long a session stays valid after creation.
const sessionTTL = 30 * 24 * time.Hour

// Session is a login session. ID is the token sent to the client as the
// cookie value; the sessions table only ever stores its hash (see hashToken).
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Repo persists sessions.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// CreateSession creates and stores a new session for userID, valid for 30 days.
// It first purges expired sessions: nothing else deletes them, and a login is
// the only moment the table is guaranteed to be written anyway. Returns
// user.ErrNotFound if the user no longer exists.
func (r *Repo) CreateSession(ctx context.Context, userID int64) (Session, error) {
	id, err := newSessionID()
	if err != nil {
		return Session{}, fmt.Errorf("generate session id: %w", err)
	}
	if err := r.DeleteExpiredSessions(ctx); err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	s := Session{ID: id, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(sessionTTL)}

	_, err = r.db.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hashToken(s.ID), s.UserID, s.CreatedAt.Format(time.RFC3339), s.ExpiresAt.Format(time.RFC3339),
	)
	// The user was deleted since the caller checked it (GLG-1012).
	if db.IsForeignKeyViolation(err) {
		return Session{}, fmt.Errorf("insert session: %w", user.ErrNotFound)
	}
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}
	return s, nil
}

// GetSession returns the user id for a valid session, or ErrSessionNotFound
// if the session is unknown or has expired.
func (r *Repo) GetSession(ctx context.Context, id string) (int64, error) {
	var userID int64
	var expiresAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT user_id, expires_at FROM sessions WHERE id = ?", hashToken(id),
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("query session: %w", err)
	}

	expiry, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return 0, fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(expiry) {
		return 0, ErrSessionNotFound
	}
	return userID, nil
}

// DeleteSession removes a session. Deleting an unknown id is not an error.
func (r *Repo) DeleteSession(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", hashToken(id)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteOtherSessions removes every session belonging to userID except
// keepSessionID. Used after a password change to sign the user out
// everywhere else.
func (r *Repo) DeleteOtherSessions(ctx context.Context, userID int64, keepSessionID string) error {
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id = ? AND id != ?", userID, hashToken(keepSessionID),
	); err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes every session whose expires_at is in the
// past. GetSession already rejects those rows, so this only reclaims space;
// it keeps stale random tokens out of the table and out of every backup.
// expires_at is stored as UTC RFC3339, which sorts lexicographically, so a
// plain string comparison is enough and stays portable.
func (r *Repo) DeleteExpiredSessions(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE expires_at < ?", time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}

// hashToken returns the hex sha256 of a session token, which is what the
// sessions.id column holds. A copy of the database (or a -backup file) then
// carries no usable login. A plain unsalted hash is enough: the token is 32
// random bytes, so there is nothing to brute-force.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSessionID returns a base64url-encoded 32 byte random session id.
func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
