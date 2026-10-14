package price

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// manualPriceSource is the Source value filled into every Price returned by
// ManualRepo. manual_prices has no source column of its own -- every row in
// it is, by definition, a manual entry.
const manualPriceSource = "manual"

// ManualRepo persists per-user manual price overrides (GLG-103). Unlike
// Repo, which stores shared fetched market data, every query here is
// scoped by user_id: a manual price is visible only to the user who
// entered it and never overwrites another user's entry or a fetched price.
type ManualRepo struct {
	db *sql.DB
}

// NewManualRepo returns a ManualRepo backed by db.
func NewManualRepo(db *sql.DB) *ManualRepo {
	return &ManualRepo{db: db}
}

// Upsert stores close as userID's manual price for instrumentID on date.
// If a row for (userID, instrumentID, date) already exists, its close is
// overwritten. Same UPDATE-then-INSERT shape as Repo.Upsert, for portable
// SQL, run in one transaction: BeginTx takes the write lock up front
// (_txlock=immediate), so two concurrent saves of a new key cannot both miss
// on UPDATE and both INSERT (GLG-1019). Returns user.ErrNotFound if the user
// no longer exists.
func (r *ManualRepo) Upsert(ctx context.Context, userID, instrumentID int64, date, close string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"UPDATE manual_prices SET close = ? WHERE user_id = ? AND instrument_id = ? AND date = ?",
		close, userID, instrumentID, date,
	)
	if err != nil {
		return fmt.Errorf("update manual price: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		_, err = tx.ExecContext(ctx,
			"INSERT INTO manual_prices (user_id, instrument_id, date, close) VALUES (?, ?, ?, ?)",
			userID, instrumentID, date, close,
		)
		// Instruments are never deleted, so a foreign key failure is the user
		// deleted since the session check (GLG-1012).
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("insert manual price: %w", user.MissingError{ID: userID})
		}
		if err != nil {
			return fmt.Errorf("insert manual price: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit manual price: %w", err)
	}
	return nil
}

// latestManualQuery is ManualRepo.Latest's lookup, the same shape as
// latestQuery: per requested id, seeks on the (user_id, instrument_id,
// date) primary key, one statement for all ids (GLG-503, GLG-1234).
const latestManualQuery = `SELECT m.instrument_id, m.date, m.close
	FROM instruments i JOIN manual_prices m ON m.user_id = ? AND m.instrument_id = i.id
	WHERE i.id IN (%s) AND m.date = (
		SELECT q.date FROM manual_prices q
		WHERE q.user_id = m.user_id AND q.instrument_id = i.id AND q.date <= ?
		ORDER BY q.date DESC LIMIT 1)`

// Latest returns userID's newest manual price row dated on or before asOf
// for each of instrumentIDs. Mirrors Repo.Latest's shape and future-date
// exclusion so callers can merge the two results uniformly. Instrument ids
// with no qualifying manual row are omitted from the result.
func (r *ManualRepo) Latest(ctx context.Context, userID int64, instrumentIDs []int64, asOf string) (map[int64]Price, error) {
	out := make(map[int64]Price, len(instrumentIDs))
	if len(instrumentIDs) == 0 {
		return out, nil
	}
	placeholders, args := db.InList(instrumentIDs)
	args = append([]any{userID}, args...)
	rows, err := r.query(ctx, fmt.Sprintf(latestManualQuery, placeholders), append(args, asOf)...)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		out[p.InstrumentID] = p
	}
	return out, nil
}

// Range returns userID's manual price rows for instrumentIDs with date in
// [from, to] inclusive, ordered by instrument_id then date ascending.
// Mirrors Repo.Range.
func (r *ManualRepo) Range(ctx context.Context, userID int64, instrumentIDs []int64, from, to string) ([]Price, error) {
	if len(instrumentIDs) == 0 {
		return []Price{}, nil
	}
	placeholders, args := db.InList(instrumentIDs)
	args = append([]any{userID}, args...)
	return r.query(ctx,
		fmt.Sprintf(`SELECT instrument_id, date, close FROM manual_prices
		 WHERE user_id = ? AND instrument_id IN (%s) AND date >= ? AND date <= ?
		 ORDER BY instrument_id, date`,
			placeholders),
		append(args, from, to)...,
	)
}

// History returns up to limit of userID's manual price rows for
// instrumentID, newest first. Mirrors Repo.History, scoped by user_id so a
// manual entry stays visible only to whoever made it.
func (r *ManualRepo) History(ctx context.Context, userID, instrumentID int64, limit int) ([]Price, error) {
	return r.query(ctx,
		`SELECT instrument_id, date, close FROM manual_prices
		 WHERE user_id = ? AND instrument_id = ?
		 ORDER BY date DESC
		 LIMIT ?`,
		userID, instrumentID, limit,
	)
}

// query runs a SELECT of instrument_id, date, close in that order, scans
// every row and sets its Source to manualPriceSource.
func (r *ManualRepo) query(ctx context.Context, query string, args ...any) ([]Price, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query manual prices: %w", err)
	}
	defer rows.Close()

	out := []Price{}
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.InstrumentID, &p.Date, &p.Close); err != nil {
			return nil, fmt.Errorf("scan manual price: %w", err)
		}
		p.Source = manualPriceSource
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate manual prices: %w", err)
	}
	return out, nil
}

// Delete removes userID's manual price for instrumentID on date and reports
// whether a row was actually removed. The user_id filter is what keeps one
// user from deleting another's entry for the same instrument and date.
func (r *ManualRepo) Delete(ctx context.Context, userID, instrumentID int64, date string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM manual_prices WHERE user_id = ? AND instrument_id = ? AND date = ?",
		userID, instrumentID, date,
	)
	if err != nil {
		return false, fmt.Errorf("delete manual price: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}
