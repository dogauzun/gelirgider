// Package benchmark provides persistence for benchmark series that are not
// FX rates (BIST 100 closes, TUFE CPI levels, the gram gold series). This
// is shared market reference data (not user data), same category as prices
// and fx_rates, so it is not scoped by user_id.
package benchmark

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// Benchmark series codes.
const (
	CodeXU100 = "XU100"
	CodeTUFE  = "TUFE"
	CodeGOLD  = "GOLD"
)

// Value is one point of a benchmark series.
type Value struct {
	Code   string
	Date   string // YYYY-MM-DD; for TUFE the first day of the month
	Value  decimal.Decimal
	Source string
}

// Repo persists benchmark values.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert stores value for code on date, from source. An existing row for
// (code, date) is overwritten (last writer wins). value must be a decimal
// greater than zero. Implemented as UPDATE-then-INSERT rather than a
// dialect-specific upsert, per project convention of portable SQL, in one
// transaction so overlapping fetches of a new key cannot both INSERT
// (see price.Repo.Upsert).
func (r *Repo) Upsert(ctx context.Context, code, date, value, source string) error {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return fmt.Errorf("benchmark value %q: %w", value, err)
	}
	if !d.IsPositive() {
		return fmt.Errorf("benchmark value %q: must be > 0", value)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE benchmark_values SET value = ?, source = ? WHERE code = ? AND date = ?",
		[]any{value, source, code, date},
		"INSERT INTO benchmark_values (code, date, value, source) VALUES (?, ?, ?, ?)",
		[]any{code, date, value, source},
	); err != nil {
		return fmt.Errorf("upsert benchmark value: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit benchmark value: %w", err)
	}
	return nil
}

// Latest returns the row for code with the greatest date on or before
// asOf (YYYY-MM-DD). ok is false when no such row exists.
func (r *Repo) Latest(ctx context.Context, code, asOf string) (Value, bool, error) {
	var v Value
	err := r.db.QueryRowContext(ctx,
		`SELECT code, date, value, source FROM benchmark_values
		 WHERE code = ? AND date <= ? ORDER BY date DESC LIMIT 1`,
		code, asOf,
	).Scan(&v.Code, &v.Date, &v.Value, &v.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return Value{}, false, nil
	}
	if err != nil {
		return Value{}, false, fmt.Errorf("query latest benchmark value: %w", err)
	}
	return v, true, nil
}

// Range returns rows for code with date in [from, to] inclusive, ordered
// by date ascending.
func (r *Repo) Range(ctx context.Context, code, from, to string) ([]Value, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT code, date, value, source FROM benchmark_values
		 WHERE code = ? AND date >= ? AND date <= ? ORDER BY date`,
		code, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("query benchmark range: %w", err)
	}
	defer rows.Close()

	out := []Value{}
	for rows.Next() {
		var v Value
		if err := rows.Scan(&v.Code, &v.Date, &v.Value, &v.Source); err != nil {
			return nil, fmt.Errorf("scan benchmark value: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark range: %w", err)
	}
	return out, nil
}

// Have reports whether a row exists for code on date.
func (r *Repo) Have(ctx context.Context, code, date string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM benchmark_values WHERE code = ? AND date = ?)",
		code, date,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check benchmark value: %w", err)
	}
	return exists == 1, nil
}
