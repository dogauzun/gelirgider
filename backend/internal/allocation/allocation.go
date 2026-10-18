// Package allocation holds the target weight of each asset class, per portfolio
// or for all portfolios together (ADR 0028).
package allocation

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/shopspring/decimal"
)

// Classes are the six asset classes of ADR 0026, in display order.
var Classes = []string{"equity", "fund", "commodity", "crypto", "deposit", "cash"}

// Target is the weight of one asset class, in percent.
type Target struct {
	AssetClass string
	Weight     decimal.Decimal
}

// Repo persists allocation targets.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// scope is the WHERE tail and arguments that pick one set: a nil portfolioID
// is the all-portfolios set.
func scope(userID int64, portfolioID *int64) (string, []any) {
	if portfolioID == nil {
		return "user_id = ? AND portfolio_id IS NULL", []any{userID}
	}
	return "user_id = ? AND portfolio_id = ?", []any{userID, *portfolioID}
}

// List returns the set of userID's portfolio, ordered by the class's position
// in Classes. Empty when no set is stored.
func (r *Repo) List(ctx context.Context, userID int64, portfolioID *int64) ([]Target, error) {
	where, args := scope(userID, portfolioID)
	rows, err := r.db.QueryContext(ctx, "SELECT asset_class, weight FROM allocation_targets WHERE "+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query allocation targets: %w", err)
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.AssetClass, &t.Weight); err != nil {
			return nil, fmt.Errorf("scan allocation target: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate allocation targets: %w", err)
	}
	slices.SortFunc(out, func(a, b Target) int {
		return slices.Index(Classes, a.AssetClass) - slices.Index(Classes, b.AssetClass)
	})
	return out, nil
}

// Replace swaps the stored set for targets in one transaction; an empty
// targets clears it. Ownership of the portfolio and the validity of targets
// are the caller's to check.
func (r *Repo) Replace(ctx context.Context, userID int64, portfolioID *int64, targets []Target) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	where, args := scope(userID, portfolioID)
	if _, err := tx.ExecContext(ctx, "DELETE FROM allocation_targets WHERE "+where, args...); err != nil {
		return fmt.Errorf("delete allocation targets: %w", err)
	}
	for _, t := range targets {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO allocation_targets (user_id, portfolio_id, asset_class, weight) VALUES (?, ?, ?, ?)",
			userID, portfolioID, t.AssetClass, t.Weight.String(),
		); err != nil {
			return fmt.Errorf("insert allocation target: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
