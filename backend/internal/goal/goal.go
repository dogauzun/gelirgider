// Package goal holds a user's savings goal, per portfolio or for all
// portfolios together: an amount in a unit to reach by a date.
package goal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Units are the units an amount can be in: the reporting currency's nominal
// value, its real (inflation-adjusted) value, or USD.
var Units = []string{"nominal", "real", "usd"}

// MaxYears is how far ahead a target date may fall, counted from the current calendar month.
const MaxYears = 50

// ErrNotFound is returned when the scope has no goal.
var ErrNotFound = errors.New("goal not found")

// Goal is the goal of one scope. TargetDate is YYYY-MM-DD in Europe/Istanbul.
type Goal struct {
	ID         int64
	Amount     decimal.Decimal
	Unit       string
	TargetDate string
	CreatedAt  time.Time
}

// Repo persists goals.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// scope is the WHERE tail and arguments that pick one goal: a nil portfolioID
// is the all-portfolios goal.
func scope(userID int64, portfolioID *int64) (string, []any) {
	if portfolioID == nil {
		return "user_id = ? AND portfolio_id IS NULL", []any{userID}
	}
	return "user_id = ? AND portfolio_id = ?", []any{userID, *portfolioID}
}

// Get returns the goal of userID's portfolio, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, userID int64, portfolioID *int64) (Goal, error) {
	where, args := scope(userID, portfolioID)
	var g Goal
	var createdAt string
	// The CAST keeps the driver from turning the DATE column into a time.Time.
	err := r.db.QueryRowContext(ctx,
		"SELECT id, amount, unit, CAST(target_date AS TEXT), created_at FROM goals WHERE "+where, args...,
	).Scan(&g.ID, &g.Amount, &g.Unit, &g.TargetDate, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Goal{}, ErrNotFound
	}
	if err != nil {
		return Goal{}, fmt.Errorf("query goal: %w", err)
	}
	if g.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return Goal{}, fmt.Errorf("parse goal created_at: %w", err)
	}
	return g, nil
}

// Set replaces the goal of the scope with g in one transaction and returns it
// with its id and creation time set; g's ID and CreatedAt are ignored.
// Ownership of the portfolio and the validity of g are the caller's to check.
func (r *Repo) Set(ctx context.Context, userID int64, portfolioID *int64, g Goal) (Goal, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Goal{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	where, args := scope(userID, portfolioID)
	if _, err := tx.ExecContext(ctx, "DELETE FROM goals WHERE "+where, args...); err != nil {
		return Goal{}, fmt.Errorf("delete goal: %w", err)
	}
	g.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := tx.ExecContext(ctx,
		"INSERT INTO goals (user_id, portfolio_id, amount, unit, target_date, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		userID, portfolioID, g.Amount.String(), g.Unit, g.TargetDate, g.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return Goal{}, fmt.Errorf("insert goal: %w", err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return Goal{}, fmt.Errorf("get inserted goal id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Goal{}, fmt.Errorf("commit: %w", err)
	}
	return g, nil
}

// Delete removes the goal of the scope, or returns ErrNotFound.
func (r *Repo) Delete(ctx context.Context, userID int64, portfolioID *int64) error {
	where, args := scope(userID, portfolioID)
	res, err := r.db.ExecContext(ctx, "DELETE FROM goals WHERE "+where, args...)
	if err != nil {
		return fmt.Errorf("delete goal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("goal rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
