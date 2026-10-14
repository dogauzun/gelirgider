package dividend

import (
	"context"
	"fmt"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
)

// Dismissals are user data, unlike dividends: every query filters by user_id.

// Dismiss records that userID dismissed the expected dividend of
// instrumentID on date in portfolioID. Dismissing twice is a no-op, also
// when both run at once. The caller has already checked that
// userID owns portfolioID.
//
// Insert first, and on failure check whether the row is there: plain SQL
// instead of a dialect upsert, and no window between a check and the
// insert. A failed insert with the row present lost a race to an identical
// dismiss; any other failure leaves no row and is returned.
func (r *Repo) Dismiss(ctx context.Context, userID, portfolioID, instrumentID int64, date string) error {
	_, insErr := r.db.ExecContext(ctx,
		"INSERT INTO dividend_dismissals (user_id, portfolio_id, instrument_id, date) VALUES (?, ?, ?, ?)",
		userID, portfolioID, instrumentID, date,
	)
	if insErr == nil {
		return nil
	}
	var n int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dividend_dismissals WHERE user_id = ? AND portfolio_id = ? AND instrument_id = ? AND date = ?",
		userID, portfolioID, instrumentID, date,
	).Scan(&n); err != nil {
		return fmt.Errorf("count dividend dismissals: %w", err)
	}
	// The instrument was checked and is never deleted, so a foreign key
	// failure is the portfolio, or its user, deleted since the caller's
	// ownership check. A duplicate whose row is gone by the count
	// is the same delete, committed after the insert: only a portfolio or
	// user delete removes dismissals.
	if n == 0 && (db.IsForeignKeyViolation(insErr) || strings.Contains(strings.ToLower(insErr.Error()), "unique constraint")) {
		return fmt.Errorf("insert dividend dismissal: %w", portfolio.ErrNotFound)
	}
	if n == 0 {
		return fmt.Errorf("insert dividend dismissal: %w", insErr)
	}
	return nil
}

// DismissalKey identifies one dismissed dividend within a portfolio.
type DismissalKey struct {
	InstrumentID int64
	Date         string
}

// Dismissed returns the dividends userID dismissed in portfolioID, in one
// query.
func (r *Repo) Dismissed(ctx context.Context, userID, portfolioID int64) (map[DismissalKey]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT instrument_id, date FROM dividend_dismissals WHERE user_id = ? AND portfolio_id = ?",
		userID, portfolioID,
	)
	if err != nil {
		return nil, fmt.Errorf("query dividend dismissals: %w", err)
	}
	defer rows.Close()
	out := map[DismissalKey]bool{}
	for rows.Next() {
		var k DismissalKey
		if err := rows.Scan(&k.InstrumentID, &k.Date); err != nil {
			return nil, fmt.Errorf("scan dividend dismissal: %w", err)
		}
		out[k] = true
	}
	return out, rows.Err()
}
