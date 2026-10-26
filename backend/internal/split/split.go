// Package split stores stock splits and bonus issues. Splits are shared
// market data like prices: no user_id.
package split

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// Split is one split row. Ratio is new shares per old share.
type Split struct {
	InstrumentID int64
	Date         string
	Ratio        decimal.Decimal
	Source       string
}

// Factor is the product of the ratios of splits dated after the date after
// and on or before the date through, in (after, through]: the multiplier that
// carries a quantity held on after to through. 1 with none in range.
func Factor(splits []Split, after, through string) decimal.Decimal {
	f := decimal.NewFromInt(1)
	for _, s := range splits {
		if s.Date > after && s.Date <= through {
			f = f.Mul(s.Ratio)
		}
	}
	return f
}

// Repo persists splits.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert stores a split for (instrumentID, date), overwriting ratio and
// source of an existing row (last writer wins). Same UPDATE-then-INSERT
// shape as price.Repo.Upsert, for portable SQL, in one transaction so
// overlapping fetches of a new key cannot both INSERT.
func (r *Repo) Upsert(ctx context.Context, instrumentID int64, date string, ratio decimal.Decimal, source string) error {
	if !ratio.IsPositive() {
		return errors.New("split ratio must be > 0")
	}
	if !price.ValidDate(date) {
		return fmt.Errorf("split date %q is not YYYY-MM-DD", date)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE splits SET ratio = ?, source = ? WHERE instrument_id = ? AND date = ?",
		[]any{ratio.String(), source, instrumentID, date},
		"INSERT INTO splits (instrument_id, date, ratio, source) VALUES (?, ?, ?, ?)",
		[]any{instrumentID, date, ratio.String(), source},
	); err != nil {
		return fmt.Errorf("upsert split: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit split: %w", err)
	}
	return nil
}

// List returns instrumentID's splits ordered by date ascending.
func (r *Repo) List(ctx context.Context, instrumentID int64) ([]Split, error) {
	return r.query(ctx,
		"SELECT instrument_id, date, ratio, source FROM splits WHERE instrument_id = ? ORDER BY date",
		instrumentID,
	)
}

// ListMany returns the splits of ids in one query, keyed by instrument and
// ordered by date ascending. Instruments without splits have no key.
func (r *Repo) ListMany(ctx context.Context, ids []int64) (map[int64][]Split, error) {
	out := make(map[int64][]Split)
	if len(ids) == 0 {
		return out, nil
	}
	// shortcut: one IN list, no chunking; modernc sqlite's default
	// SQLITE_MAX_VARIABLE_NUMBER is 32766, chunk when a portfolio nears it.
	placeholders, args := db.InList(ids)
	rows, err := r.query(ctx,
		"SELECT instrument_id, date, ratio, source FROM splits WHERE instrument_id IN ("+placeholders+") ORDER BY instrument_id, date",
		args...,
	)
	if err != nil {
		return nil, err
	}
	for _, s := range rows {
		out[s.InstrumentID] = append(out[s.InstrumentID], s)
	}
	return out, nil
}

func (r *Repo) query(ctx context.Context, query string, args ...any) ([]Split, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query splits: %w", err)
	}
	defer rows.Close()

	out := []Split{}
	for rows.Next() {
		var s Split
		if err := rows.Scan(&s.InstrumentID, &s.Date, &s.Ratio, &s.Source); err != nil {
			return nil, fmt.Errorf("scan split: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate splits: %w", err)
	}
	return out, nil
}
