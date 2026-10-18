// Package portfolio provides persistence and HTTP handlers for user
// portfolios. Every query is scoped by user_id -- a user can never read or
// modify another user's portfolio.
package portfolio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// ErrNotFound is returned when no portfolio matches the lookup for the
// given user, including when the id belongs to another user.
var ErrNotFound = errors.New("portfolio not found")

// ErrHasTransactions is returned by Delete when the portfolio still has
// transactions; the caller must delete them first.
var ErrHasTransactions = errors.New("portfolio has transactions")

// ErrNameTaken is returned by Create and Rename when the user already has
// another portfolio with that name. Names are compared case-insensitively
// and with whitespace runs collapsed (see foldName): "Portföy", "portföy"
// and "PORTFÖY" are one name, as are "x y" and "x  y".
var ErrNameTaken = errors.New("name already used")

// Portfolio is a stored portfolio.
type Portfolio struct {
	ID        int64
	UserID    int64
	Name      string
	CreatedAt time.Time
}

// Repo persists portfolios.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// List returns all portfolios owned by userID, ordered by id.
func (r *Repo) List(ctx context.Context, userID int64) ([]Portfolio, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, user_id, name, created_at FROM portfolios WHERE user_id = ? ORDER BY id",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query portfolios: %w", err)
	}
	defer rows.Close()

	var out []Portfolio
	for rows.Next() {
		p, err := scanPortfolio(rows)
		if err != nil {
			return nil, fmt.Errorf("scan portfolio: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate portfolios: %w", err)
	}
	return out, nil
}

// Get returns the portfolio with the given id, if owned by userID, or
// ErrNotFound otherwise.
func (r *Repo) Get(ctx context.Context, userID, id int64) (Portfolio, error) {
	p, err := scanPortfolio(r.db.QueryRowContext(ctx,
		"SELECT id, user_id, name, created_at FROM portfolios WHERE id = ? AND user_id = ?",
		id, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Portfolio{}, ErrNotFound
	}
	if err != nil {
		return Portfolio{}, fmt.Errorf("query portfolio: %w", err)
	}
	return p, nil
}

// Create inserts a new portfolio for userID and returns it. Returns
// ErrNameTaken if the user already has a portfolio with that name, and
// user.ErrNotFound if the user no longer exists.
func (r *Repo) Create(ctx context.Context, userID int64, name string) (Portfolio, error) {
	// The check and the insert share one transaction, which takes the write
	// lock before the check (_txlock=immediate), so two creates of one name
	// cannot both pass the check.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Portfolio{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	taken, err := nameTaken(ctx, tx, userID, name, 0)
	if err != nil {
		return Portfolio{}, err
	}
	if taken {
		return Portfolio{}, ErrNameTaken
	}
	createdAt := time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO portfolios (user_id, name, created_at) VALUES (?, ?, ?)",
		userID, name, createdAt.Format(time.RFC3339),
	)
	// The user was deleted since the session check.
	if db.IsForeignKeyViolation(err) {
		return Portfolio{}, fmt.Errorf("insert portfolio: %w", user.MissingError{ID: userID})
	}
	if err != nil {
		return Portfolio{}, fmt.Errorf("insert portfolio: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Portfolio{}, fmt.Errorf("get inserted id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Portfolio{}, fmt.Errorf("commit transaction: %w", err)
	}
	return Portfolio{ID: id, UserID: userID, Name: name, CreatedAt: createdAt}, nil
}

// Rename updates the name of the portfolio with the given id, if owned by
// userID, or returns ErrNotFound otherwise. Returns ErrNameTaken if another
// portfolio of the same user already has that name; renaming a portfolio to
// the name it already has is a no-op, not a conflict.
func (r *Repo) Rename(ctx context.Context, userID, id int64, name string) error {
	// One transaction for check and update, as in Create.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	taken, err := nameTaken(ctx, tx, userID, name, id)
	if err != nil {
		return err
	}
	if taken {
		return ErrNameTaken
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE portfolios SET name = ? WHERE id = ? AND user_id = ?",
		name, id, userID,
	)
	if err != nil {
		return fmt.Errorf("update portfolio: %w", err)
	}
	if err := db.CheckAffected(res, ErrNotFound); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Delete removes the portfolio with the given id, if owned by userID, and
// returns how many transactions it holds. Returns ErrNotFound if there is no
// such portfolio. A portfolio that still has transactions is refused with
// ErrHasTransactions unless cascade is true, in which case its transactions
// are deleted along with it.
//
// Everything runs in one database transaction so a cascade cannot half
// happen, and every statement is scoped by user_id as well as portfolio id,
// so a guessed id can never reach another user's rows.
func (r *Repo) Delete(ctx context.Context, userID, id int64, cascade bool) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var n int
	err = tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM transactions WHERE portfolio_id = ? AND user_id = ?",
		id, userID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count transactions: %w", err)
	}
	if n > 0 {
		if !cascade {
			return n, ErrHasTransactions
		}
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM transactions WHERE portfolio_id = ? AND user_id = ?",
			id, userID,
		); err != nil {
			return n, fmt.Errorf("delete transactions: %w", err)
		}
	}
	// Dismissals can outlive the transactions they hid, so they go on every
	// delete, cascade or not.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM dividend_dismissals WHERE portfolio_id = ? AND user_id = ?",
		id, userID,
	); err != nil {
		return n, fmt.Errorf("delete dividend dismissals: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM allocation_targets WHERE portfolio_id = ? AND user_id = ?",
		id, userID,
	); err != nil {
		return n, fmt.Errorf("delete allocation targets: %w", err)
	}
	// Imports go after the transactions that reference them by import_id,
	// and on every delete: an import can outlive all its transactions.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM statement_imports WHERE portfolio_id = ? AND user_id = ?",
		id, userID,
	); err != nil {
		return n, fmt.Errorf("delete statement imports: %w", err)
	}
	// Term deposits go after the transactions, then their instruments: the
	// deposit rows reference them.
	depositInstruments, err := db.DepositInstrumentIDs(ctx, tx, userID, id)
	if err != nil {
		return n, err
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM term_deposits WHERE portfolio_id = ? AND user_id = ?",
		id, userID,
	); err != nil {
		return n, fmt.Errorf("delete term deposits: %w", err)
	}
	if err := db.DeleteInstruments(ctx, tx, depositInstruments); err != nil {
		return n, err
	}
	res, err := tx.ExecContext(ctx,
		"DELETE FROM portfolios WHERE id = ? AND user_id = ?",
		id, userID,
	)
	if err != nil {
		return n, fmt.Errorf("delete portfolio: %w", err)
	}
	if err := db.CheckAffected(res, ErrNotFound); err != nil {
		return n, err
	}
	if err := tx.Commit(); err != nil {
		return n, fmt.Errorf("commit transaction: %w", err)
	}
	return n, nil
}

// nameTaken reports whether userID already has a portfolio whose name
// folds (see foldName) to the same value as name, ignoring the portfolio
// with id excludeID (pass 0 to ignore none: ids start at 1, so no row is
// excluded). The comparison runs in Go because SQL LOWER is ASCII-only on
// SQLite; a user has a handful of portfolios, so reading their names is
// cheap. It runs inside the caller's write transaction, so no other write
// can land between this check and the caller's insert or update.
func nameTaken(ctx context.Context, tx *sql.Tx, userID int64, name string, excludeID int64) (bool, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT name FROM portfolios WHERE user_id = ? AND id <> ?",
		userID, excludeID,
	)
	if err != nil {
		return false, fmt.Errorf("query portfolio names: %w", err)
	}
	defer rows.Close()
	want := foldName(name)
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false, fmt.Errorf("scan portfolio name: %w", err)
		}
		if foldName(n) == want {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate portfolio names: %w", err)
	}
	return false, nil
}

// turkishI folds the four Turkish i letters to one before lowercasing:
// Turkish pairs i/İ and ı/I while English pairs i/I, so a name typed on
// either keyboard layout ("İŞ", "IŞ", "iş", "ış") matches the others.
var turkishI = strings.NewReplacer("İ", "i", "I", "i", "ı", "i")

// foldName is the key two portfolio names are compared by. Case is folded
// and whitespace runs collapse to one space, so a name stored
// before checkName collapsed them still matches; diacritics stay, so
// "Portfoy" and "Portföy" remain two names.
func foldName(s string) string {
	return strings.ToLower(turkishI.Replace(collapseSpace(s)))
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanPortfolio(s scanner) (Portfolio, error) {
	var p Portfolio
	var createdAt string
	if err := s.Scan(&p.ID, &p.UserID, &p.Name, &createdAt); err != nil {
		return Portfolio{}, err
	}
	var err error
	p.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return Portfolio{}, fmt.Errorf("parse created_at: %w", err)
	}
	return p, nil
}
