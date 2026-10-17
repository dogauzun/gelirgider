package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

const (
	challengeTTL = 5 * time.Minute
	// challengeWindow is how long a challenge row is kept, and so how far
	// back the per-user attempt cap looks.
	challengeWindow = 15 * time.Minute
	// maxChallengeAttempts caps code checks per token; maxUserAttempts caps
	// them over all of a user's rows in the window.
	maxChallengeAttempts = 5
	maxUserAttempts      = 10
)

// ErrTooManyChallenges is returned by CreateChallenge when the user's recent
// challenges already hold maxUserAttempts code checks.
var ErrTooManyChallenges = errors.New("too many login challenges")

// CreateChallenge stores a login challenge for userID, valid for 5 minutes,
// and returns its token; only the token's hash is stored. It first purges
// challenges older than 15 minutes, then refuses with ErrTooManyChallenges
// when the rest hold 10 or more attempts of this user: without that cap a
// caller who knows the password gets a fresh 5 guesses with every login.
// Returns user.ErrNotFound if the user no longer exists.
func (r *Repo) CreateChallenge(ctx context.Context, userID int64) (string, error) {
	token, err := newSessionID()
	if err != nil {
		return "", fmt.Errorf("generate challenge token: %w", err)
	}
	now := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE created_at < ?",
		now.Add(-challengeWindow).Format(time.RFC3339)); err != nil {
		return "", fmt.Errorf("purge login challenges: %w", err)
	}
	var attempts int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(attempts), 0) FROM login_challenges WHERE user_id = ?", userID,
	).Scan(&attempts); err != nil {
		return "", fmt.Errorf("sum login challenge attempts: %w", err)
	}
	if attempts >= maxUserAttempts {
		return "", ErrTooManyChallenges
	}
	_, err = r.db.ExecContext(ctx,
		"INSERT INTO login_challenges (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hashToken(token), userID, now.Format(time.RFC3339), now.Add(challengeTTL).Format(time.RFC3339))
	if db.IsForeignKeyViolation(err) {
		return "", fmt.Errorf("insert login challenge: %w", user.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("insert login challenge: %w", err)
	}
	return token, nil
}

// AttemptChallenge counts one code check against the challenge and returns
// its user id. ok is false for an unknown, expired or used-up token. The
// count and the cap are one statement, so parallel requests cannot get past
// the cap of 5.
func (r *Repo) AttemptChallenge(ctx context.Context, token string) (userID int64, ok bool, err error) {
	err = r.db.QueryRowContext(ctx,
		`UPDATE login_challenges SET attempts = attempts + 1
		 WHERE token_hash = ? AND attempts < ? AND expires_at > ? RETURNING user_id`,
		hashToken(token), maxChallengeAttempts, time.Now().UTC().Format(time.RFC3339),
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("count login challenge attempt: %w", err)
	}
	return userID, true, nil
}

// DeleteChallenge removes a challenge. Deleting an unknown token is not an error.
func (r *Repo) DeleteChallenge(ctx context.Context, token string) error {
	return r.deleteByToken(ctx, "login challenge", "DELETE FROM login_challenges WHERE token_hash = ?", token)
}
