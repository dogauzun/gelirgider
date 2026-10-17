// Package settings stores install-wide key/value config. It is not user
// data: no user_id. Secrets are AES-256-GCM encrypted with SECRET_KEY.
package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// ErrUndecryptable is returned by GetSecret for a stored value that does not
// decrypt, usually because SECRET_KEY changed since it was stored.
var ErrUndecryptable = errors.New("cannot decrypt, SECRET_KEY differs from the one it was stored with; re-enter the value")

// Repo persists settings. key is the 32-byte AES-256 key for secrets.
type Repo struct {
	db  *sql.DB
	key []byte
}

// NewRepo returns a Repo backed by db, encrypting secrets with key.
func NewRepo(db *sql.DB, key []byte) *Repo {
	return &Repo{db: db, key: key}
}

// Get returns the plain value stored under key, and false if there is none.
func (r *Repo) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := r.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get setting %s: %w", key, err)
	}
	return v, true, nil
}

// Set stores value under key (last writer wins). UPDATE then INSERT, for
// portable SQL, in one transaction: BeginTx takes the write lock up front
// (_txlock=immediate), so two concurrent Sets of a new key cannot both miss
// on UPDATE and both INSERT.
func (r *Repo) Set(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE settings SET value = ?, updated_at = ? WHERE key = ?", []any{value, now, key},
		"INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)", []any{key, value, now},
	); err != nil {
		return fmt.Errorf("upsert setting %s: %w", key, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setting %s: %w", key, err)
	}
	return nil
}

// SetSecret stores value under key as base64(nonce || ciphertext), with a
// fresh random nonce per write. An empty value deletes the row.
func (r *Repo) SetSecret(ctx context.Context, key, value string) error {
	if value == "" {
		if _, err := r.db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key); err != nil {
			return fmt.Errorf("delete setting %s: %w", key, err)
		}
		return nil
	}
	gcm, err := r.gcm()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("nonce: %w", err)
	}
	// The key name is the additional data, so a ciphertext copied to
	// another key does not decrypt.
	sealed := gcm.Seal(nonce, nonce, []byte(value), []byte(key))
	return r.Set(ctx, key, base64.StdEncoding.EncodeToString(sealed))
}

// GetSecret returns the decrypted value stored under key, and false if there
// is none. A value that does not decrypt (wrong SECRET_KEY) wraps
// ErrUndecryptable.
func (r *Repo) GetSecret(ctx context.Context, key string) (string, bool, error) {
	v, ok, err := r.Get(ctx, key)
	if err != nil || !ok {
		return "", ok, err
	}
	gcm, err := r.gcm()
	if err != nil {
		return "", false, err
	}
	sealed, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", false, fmt.Errorf("setting %s: stored secret is malformed", key)
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(key))
	if err != nil {
		return "", false, fmt.Errorf("setting %s: %w", key, ErrUndecryptable)
	}
	return string(plain), true, nil
}

func (r *Repo) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return cipher.NewGCM(block)
}
