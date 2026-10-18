// Package watchlist holds the instruments a user follows without holding
// them: one row per user and instrument, with an optional target price (ADR
// 0032).
package watchlist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// ErrNotFound is returned when no item matches the user and id.
var ErrNotFound = errors.New("watchlist item not found")

// ErrDuplicate is returned by Insert when the user already watches the instrument.
var ErrDuplicate = errors.New("already on the watchlist")

// The two TargetSide values.
const (
	SideAbove = "above"
	SideBelow = "below"
)

// Item is a stored watchlist row. TargetPrice is in the instrument's
// currency; TargetSide is "above" or "below". Both are nil without a target.
type Item struct {
	ID           int64
	UserID       int64
	InstrumentID int64
	TargetPrice  *decimal.Decimal
	TargetSide   *string
	Note         string
	CreatedAt    time.Time
}

// Repo persists watchlist items.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Insert stores it and returns it with its id and creation time set; its ID
// and CreatedAt are ignored. It returns ErrDuplicate when the user already
// watches the instrument. The instrument's existence is the caller's to check.
func (r *Repo) Insert(ctx context.Context, it Item) (Item, error) {
	it.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO watchlist (user_id, instrument_id, target_price, target_side, note, created_at)
		 SELECT ?, ?, ?, ?, ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM watchlist WHERE user_id = ? AND instrument_id = ?)`,
		it.UserID, it.InstrumentID, targetText(it.TargetPrice), it.TargetSide, it.Note, it.CreatedAt.Format(time.RFC3339),
		it.UserID, it.InstrumentID,
	)
	if err != nil {
		return Item{}, fmt.Errorf("insert watchlist item: %w", err)
	}
	if err := db.CheckAffected(res, ErrDuplicate); err != nil {
		return Item{}, err
	}
	if it.ID, err = res.LastInsertId(); err != nil {
		return Item{}, fmt.Errorf("get inserted watchlist item id: %w", err)
	}
	return it, nil
}

// targetText is the stored form of a target price, nil without one.
func targetText(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

// Get returns userID's item with the given id, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, userID, id int64) (Item, error) {
	items, err := r.query(ctx, `user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, ErrNotFound
	}
	return items[0], nil
}

// Update writes it's target price, target side and note to userID's row, or
// returns ErrNotFound. The instrument is fixed.
func (r *Repo) Update(ctx context.Context, it Item) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE watchlist SET target_price = ?, target_side = ?, note = ? WHERE id = ? AND user_id = ?`,
		targetText(it.TargetPrice), it.TargetSide, it.Note, it.ID, it.UserID)
	if err != nil {
		return fmt.Errorf("update watchlist item: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}

// List returns userID's items, oldest id first.
func (r *Repo) List(ctx context.Context, userID int64) ([]Item, error) {
	return r.query(ctx, `user_id = ? ORDER BY id`, userID)
}

// Delete removes userID's item with the given id, or returns ErrNotFound.
func (r *Repo) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM watchlist WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete watchlist item: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}

// query runs a SELECT over watchlist with the given WHERE clause and scans every row.
func (r *Repo) query(ctx context.Context, where string, args ...any) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, instrument_id, target_price, target_side, note, created_at FROM watchlist WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query watchlist: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var target decimal.NullDecimal
		var createdAt string
		if err := rows.Scan(&it.ID, &it.UserID, &it.InstrumentID, &target, &it.TargetSide, &it.Note, &createdAt); err != nil {
			return nil, fmt.Errorf("scan watchlist item: %w", err)
		}
		if target.Valid {
			it.TargetPrice = &target.Decimal
		}
		if it.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, fmt.Errorf("parse watchlist created_at: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watchlist: %w", err)
	}
	return out, nil
}
