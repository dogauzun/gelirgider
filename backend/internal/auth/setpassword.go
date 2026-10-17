package auth

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// SetPassword replaces the password of the user with the given email, clears
// their TOTP state and deletes every session that user has, so any open login
// is signed out.
//
// This is the operator escape hatch behind the server's -set-password flag:
// there is no self-service reset, because sending mail is not part of the
// product yet. Anyone who can run the binary against the database can take
// over any account, which is the same trust level as reading the database
// file directly.
func SetPassword(ctx context.Context, users *user.Repo, sessions *Repo, email, password string) error {
	// Same rule as the HTTP endpoints, so the operator flag cannot set a
	// password the user could not have chosen themselves.
	if msg := validatePassword(password); msg != "" {
		return errors.New(msg)
	}
	u, err := users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("look up user %q: %w", email, err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := users.ResetPassword(ctx, u.ID, string(hash)); err != nil {
		return err
	}
	// Session ids are never empty, so "keep nothing" deletes all of this
	// user's sessions. The new hash is already committed, so the delete
	// ignores cancellation: a dropped request must not leave them valid.
	return sessions.DeleteOtherSessions(context.WithoutCancel(ctx), u.ID, "")
}
