package db

import (
	"context"
	"database/sql"
	"fmt"
)

// DepositInstrumentIDs returns the instrument ids of userID's term deposits
// in portfolioID, or in every portfolio when portfolioID is 0 (ids start at
// 1). Deleting a user or a portfolio reads them before deleting the deposit
// rows, then hands them to DeleteInstruments.
func DepositInstrumentIDs(ctx context.Context, tx *sql.Tx, userID, portfolioID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT instrument_id FROM term_deposits WHERE user_id = ? AND (? = 0 OR portfolio_id = ?)",
		userID, portfolioID, portfolioID,
	)
	if err != nil {
		return nil, fmt.Errorf("list deposit instruments: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deposit instrument: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list deposit instruments: %w", err)
	}
	return ids, nil
}

// DeleteInstruments deletes the instruments with the given ids. Nothing but
// their term_deposits row references a DEPOSIT instrument, so the caller
// deletes that row first.
func DeleteInstruments(ctx context.Context, tx *sql.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	in, args := InList(ids)
	if _, err := tx.ExecContext(ctx, "DELETE FROM instruments WHERE id IN ("+in+")", args...); err != nil {
		return fmt.Errorf("delete instruments: %w", err)
	}
	return nil
}
