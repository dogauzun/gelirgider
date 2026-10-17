// Package user provides persistence for user accounts. It only depends on
// database/sql, never on a specific driver.
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// ErrNotFound is returned when no user matches the lookup.
var ErrNotFound = errors.New("user not found")

// MissingError is ErrNotFound for one user id: errors.Is(err, ErrNotFound)
// holds for it, and it names the id, so auth.WriteInternalError can tell
// the signed-in user, deleted since the session check (401), from any other
// user. Returned by GetByID and by the writes whose user foreign
// key failed.
type MissingError struct{ ID int64 }

func (e MissingError) Error() string { return ErrNotFound.Error() }

func (e MissingError) Is(target error) bool { return target == ErrNotFound }

// ErrEmailTaken is returned by Create when another user already has the
// email. The insert is the authority, so two concurrent sign-ups with the
// same address get one user and one ErrEmailTaken.
var ErrEmailTaken = errors.New("email already registered")

// DefaultReportingCurrency is the reporting_currency new users get, matching
// the schema column's default.
const DefaultReportingCurrency = "TRY"

// User is a stored account.
type User struct {
	ID                int64
	Email             string
	PasswordHash      string
	ReportingCurrency string
	IsAdmin           bool
	CreatedAt         time.Time
}

// Repo persists users.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Create inserts a new user and returns it with its assigned ID.
func (r *Repo) Create(ctx context.Context, email, passwordHash string) (User, error) {
	createdAt := time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)",
		email, passwordHash, createdAt.Format(time.RFC3339),
	)
	// Matched on the message, not a driver error type, so no driver import
	// leaks in: "UNIQUE constraint failed" on SQLite, "duplicate key value
	// violates unique constraint" on PostgreSQL. email is the only unique
	// column besides the primary key, which the insert does not set.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("get inserted id: %w", err)
	}
	return User{ID: id, Email: email, PasswordHash: passwordHash, ReportingCurrency: DefaultReportingCurrency, CreatedAt: createdAt}, nil
}

// GetByID returns the user with the given id, or a MissingError, which is
// ErrNotFound.
func (r *Repo) GetByID(ctx context.Context, id int64) (User, error) {
	var u User
	var createdAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, email, password_hash, reporting_currency, is_admin, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.ReportingCurrency, &u.IsAdmin, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, MissingError{ID: id}
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}
	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return User{}, fmt.Errorf("parse created_at: %w", err)
	}
	return u, nil
}

// UpdatePasswordHash sets a new password hash for the user with the given id.
func (r *Repo) UpdatePasswordHash(ctx context.Context, id int64, passwordHash string) error {
	return r.exec(ctx, "update password hash", "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id)
}

// UpdateReportingCurrency sets the reporting currency for the user with the
// given id. Callers are responsible for validating currency before calling.
func (r *Repo) UpdateReportingCurrency(ctx context.Context, id int64, currency string) error {
	return r.exec(ctx, "update reporting currency", "UPDATE users SET reporting_currency = ? WHERE id = ?", currency, id)
}

// AttentionRules returns the stored attention rule overrides of the user
// with the given id, a JSON object text ("{}" when nothing is overridden),
// or a MissingError, which is ErrNotFound.
func (r *Repo) AttentionRules(ctx context.Context, id int64) (string, error) {
	var rules string
	err := r.db.QueryRowContext(ctx, "SELECT attention_rules FROM users WHERE id = ?", id).Scan(&rules)
	if errors.Is(err, sql.ErrNoRows) {
		return "", MissingError{ID: id}
	}
	if err != nil {
		return "", fmt.Errorf("query attention rules: %w", err)
	}
	return rules, nil
}

// UpdateAttentionRules replaces the attention rule overrides of the user
// with the given id. Callers validate rules, a JSON object text, first.
func (r *Repo) UpdateAttentionRules(ctx context.Context, id int64, rules string) error {
	return r.exec(ctx, "update attention rules", "UPDATE users SET attention_rules = ? WHERE id = ?", rules, id)
}

// SetAdmin sets or clears the admin flag for the user with the given id.
func (r *Repo) SetAdmin(ctx context.Context, id int64, admin bool) error {
	return r.exec(ctx, "update is_admin", "UPDATE users SET is_admin = ? WHERE id = ?", admin, id)
}

// exec runs one statement and wraps its error with what.
func (r *Repo) exec(ctx context.Context, what, query string, args ...any) error {
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// ErrLastAdmin is returned by Demote when the user is the only admin left.
var ErrLastAdmin = errors.New("last admin")

// Demote clears the admin flag of the user with the given id, unless that
// would leave no admin. The check and the update are one statement, so two
// concurrent demotions cannot both pass the check and leave zero admins.
// Demoting a user who is not an admin is a no-op.
func (r *Repo) Demote(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_admin = 0
		 WHERE id = ? AND is_admin = 1
		   AND EXISTS (SELECT 1 FROM users WHERE is_admin = 1 AND id <> ?)`,
		id, id,
	)
	if err != nil {
		return fmt.Errorf("demote user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("demote user: rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	u, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if u.IsAdmin {
		return ErrLastAdmin
	}
	return nil
}

// userDataTables lists every table with a user_id column, children before
// parents so no foreign key is left dangling mid-transaction: transactions
// reference statement_imports and portfolios, imports and dismissals
// reference portfolios, term deposits reference portfolios. A new user-data table must be added here;
// TestAdminDeleteUser fails otherwise.
var userDataTables = []string{
	"transactions",
	"dividend_dismissals",
	"statement_imports",
	"import_aliases",
	"import_forgotten_names",
	"manual_prices",
	"sessions",
	"term_deposits",
	"portfolios",
}

// Delete removes the user with the given id and every row of theirs in
// userDataTables, in one transaction. Global market data is untouched. It
// refuses with ErrLastAdmin when the user is the only admin left: that
// check is part of the final DELETE statement, so two concurrent deletes
// cannot both pass it and leave zero admins.
func (r *Repo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete user: %w", err)
	}
	defer tx.Rollback()
	// The deposits' instruments are read before their rows go and deleted
	// after them: term_deposits references instruments.
	depositInstruments, err := db.DepositInstrumentIDs(ctx, tx, id, 0)
	if err != nil {
		return err
	}
	for _, table := range userDataTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id = ?", id); err != nil {
			return fmt.Errorf("delete user %s: %w", table, err)
		}
	}
	if err := db.DeleteInstruments(ctx, tx, depositInstruments); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM users
		 WHERE id = ?
		   AND (is_admin = 0 OR EXISTS (SELECT 1 FROM users WHERE is_admin = 1 AND id <> ?))`,
		id, id,
	)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user: rows affected: %w", err)
	}
	if n == 0 {
		tx.Rollback()
		if _, err := r.GetByID(ctx, id); err != nil {
			return err
		}
		return ErrLastAdmin
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete user: %w", err)
	}
	return nil
}

// List returns every user ordered by id. PasswordHash and ReportingCurrency
// are left empty: callers only need the account fields.
func (r *Repo) List(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, email, is_admin, created_at FROM users ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var createdAt string
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &createdAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		if u.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns how many users have the admin flag set.
func (r *Repo) CountAdmins(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&n); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

// GetByEmail returns the user with the given email, or ErrNotFound.
func (r *Repo) GetByEmail(ctx context.Context, email string) (User, error) {
	var u User
	var createdAt string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, email, password_hash, reporting_currency, is_admin, created_at FROM users WHERE email = ?",
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.ReportingCurrency, &u.IsAdmin, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}
	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return User{}, fmt.Errorf("parse created_at: %w", err)
	}
	return u, nil
}
