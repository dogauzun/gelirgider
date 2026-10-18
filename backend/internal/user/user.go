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
	TOTPEnabled       bool
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
		"SELECT id, email, password_hash, reporting_currency, is_admin, totp_secret IS NOT NULL, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.ReportingCurrency, &u.IsAdmin, &u.TOTPEnabled, &createdAt)
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

// ResetPassword sets a new password hash and wipes the user's TOTP state in
// one transaction. It backs the admin and CLI resets, the only way back for a
// user who lost their authenticator and has no recovery code.
func (r *Repo) ResetPassword(ctx context.Context, id int64, passwordHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reset password: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", passwordHash, id); err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	if err := clearTOTP(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// clearTOTP is the one place that wipes a user's TOTP state. It also drops
// the user's login challenges and recovery codes: with no secret left, a code
// has nothing to be checked against.
func clearTOTP(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM totp_recovery_codes WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM login_challenges WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("clear login challenges: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET totp_secret = NULL, totp_pending_secret = NULL, totp_last_step = 0 WHERE id = ?", id); err != nil {
		return fmt.Errorf("clear totp: %w", err)
	}
	return nil
}

// UpdateReportingCurrency sets the reporting currency for the user with the
// given id. Callers are responsible for validating currency before calling.
func (r *Repo) UpdateReportingCurrency(ctx context.Context, id int64, currency string) error {
	return r.exec(ctx, "update reporting currency", "UPDATE users SET reporting_currency = ? WHERE id = ?", currency, id)
}

// ErrTOTPOn is returned by SetTOTPPending when the user already has TOTP on.
var ErrTOTPOn = errors.New("totp already on")

// ErrNoPendingTOTP is returned by ConfirmTOTP when there is no pending
// secret matching the one the caller verified against.
var ErrNoPendingTOTP = errors.New("no pending totp secret")

// TOTPState is the sealed TOTP columns of a user: Secret is set once TOTP is
// on, Pending only during enrolment ("" means NULL). LastStep is the last
// accepted 30-second step.
type TOTPState struct {
	Secret, Pending string
	LastStep        int64
}

// TOTP returns the sealed TOTP columns of the user with the given id, or a
// MissingError, which is ErrNotFound.
func (r *Repo) TOTP(ctx context.Context, id int64) (TOTPState, error) {
	var s TOTPState
	var secret, pending sql.NullString
	err := r.db.QueryRowContext(ctx,
		"SELECT totp_secret, totp_pending_secret, totp_last_step FROM users WHERE id = ?", id,
	).Scan(&secret, &pending, &s.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPState{}, MissingError{ID: id}
	}
	if err != nil {
		return TOTPState{}, fmt.Errorf("query totp: %w", err)
	}
	s.Secret, s.Pending = secret.String, pending.String
	return s, nil
}

// SetTOTPPending stores the sealed pending secret of the user, replacing an
// earlier one. It returns ErrTOTPOn when TOTP is already on, or a
// MissingError when the user is gone.
func (r *Repo) SetTOTPPending(ctx context.Context, id int64, sealed string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE users SET totp_pending_secret = ? WHERE id = ? AND totp_secret IS NULL", sealed, id)
	if err != nil {
		return fmt.Errorf("set pending totp: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set pending totp: rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := r.GetByID(ctx, id); err != nil {
		return err
	}
	return ErrTOTPOn
}

// ConfirmTOTP turns the pending secret into the active one, records step as
// the last accepted step and stores the user's recovery code hashes, in one
// transaction. pending is the sealed value the caller verified the code
// against: a setup that replaced it meanwhile gives ErrNoPendingTOTP, as does
// no pending secret or TOTP already on.
func (r *Repo) ConfirmTOTP(ctx context.Context, id int64, pending string, step int64, recoveryHashes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin confirm totp: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_secret = totp_pending_secret, totp_pending_secret = NULL, totp_last_step = ?
		 WHERE id = ? AND totp_pending_secret = ? AND totp_secret IS NULL`, step, id, pending)
	if err != nil {
		return fmt.Errorf("confirm totp: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm totp: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNoPendingTOTP
	}
	if err := replaceRecoveryCodes(ctx, tx, id, recoveryHashes); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceRecoveryCodes deletes the user's recovery codes and stores the given
// hashes in their place, in one transaction.
func (r *Repo) ReplaceRecoveryCodes(ctx context.Context, id int64, hashes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace recovery codes: %w", err)
	}
	defer tx.Rollback()
	if err := replaceRecoveryCodes(ctx, tx, id, hashes); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, id int64, hashes []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM totp_recovery_codes WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO totp_recovery_codes (user_id, code_hash) VALUES (?, ?)", id, h); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return nil
}

// RedeemRecoveryCode spends the recovery code with the given hash and reports
// whether it did. The check and the spend are one statement, so of two
// requests carrying the same code only one gets true.
func (r *Repo) RedeemRecoveryCode(ctx context.Context, id int64, hash string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		"UPDATE totp_recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), id, hash)
	if err != nil {
		return false, fmt.Errorf("redeem recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("redeem recovery code: rows affected: %w", err)
	}
	return n == 1, nil
}

// AdvanceTOTPStep records step as the last accepted step when it is above the
// stored one and reports whether it did. It is the atomic replay guard: of two
// requests carrying the same code, only one gets true.
func (r *Repo) AdvanceTOTPStep(ctx context.Context, id, step int64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		"UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?", step, id, step)
	if err != nil {
		return false, fmt.Errorf("advance totp step: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("advance totp step: rows affected: %w", err)
	}
	return n > 0, nil
}

// DisableTOTP wipes the TOTP state of the user with the given id.
func (r *Repo) DisableTOTP(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin disable totp: %w", err)
	}
	defer tx.Rollback()
	if err := clearTOTP(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
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
// reference portfolios, term deposits and allocation targets reference portfolios. A new user-data table must be added here;
// TestAdminDeleteUser fails otherwise.
var userDataTables = []string{
	"transactions",
	"dividend_dismissals",
	"statement_imports",
	"import_aliases",
	"import_forgotten_names",
	"manual_prices",
	"sessions",
	"login_challenges",
	"totp_recovery_codes",
	"term_deposits",
	"allocation_targets",
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
		"SELECT id, email, password_hash, reporting_currency, is_admin, totp_secret IS NOT NULL, created_at FROM users WHERE email = ?",
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.ReportingCurrency, &u.IsAdmin, &u.TOTPEnabled, &createdAt)
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
