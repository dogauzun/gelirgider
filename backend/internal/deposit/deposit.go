// Package deposit holds term deposits (vadeli mevduat): their stored terms
// and the daily net close that follows from them. A deposit is held as
// quantity = principal at price 1, so its close is its value per unit of
// principal. The package imports neither holdings nor price, which read it.
package deposit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// Source is the price source name on a close computed from the terms.
const Source = "deposit"

// Terms are what fixes a deposit's closes. Rate and StopajRate are fractions
// (0.45 for 45%); StartDate and MaturityDate are YYYY-MM-DD.
type Terms struct {
	Rate         decimal.Decimal
	StopajRate   decimal.Decimal
	StartDate    string
	MaturityDate string
}

// Deposit is a stored term deposit. FundingTransactionID is nil when opening
// it booked no cash deposit.
type Deposit struct {
	ID                   int64
	UserID               int64
	PortfolioID          int64
	InstrumentID         int64
	Currency             string
	Principal            decimal.Decimal
	Terms                Terms
	FundingTransactionID *int64
	CreatedAt            time.Time
}

// Close returns the deposit's close on day (YYYY-MM-DD): 1 plus simple
// interest, actual/365, net of stopaj, for the days from the start date to
// day or to the maturity date, whichever is earlier. It is false before the
// start date and when a date does not parse. Nothing is rounded.
func Close(terms Terms, day string) (decimal.Decimal, bool) {
	if day < terms.StartDate {
		return decimal.Zero, false
	}
	start, err := time.Parse(time.DateOnly, terms.StartDate)
	if err != nil {
		return decimal.Zero, false
	}
	end, err := time.Parse(time.DateOnly, min(day, terms.MaturityDate))
	if err != nil {
		return decimal.Zero, false
	}
	one := decimal.NewFromInt(1)
	days := decimal.NewFromInt(int64(end.Sub(start).Hours() / 24))
	interest := terms.Rate.Mul(days).Mul(one.Sub(terms.StopajRate)).Div(decimal.NewFromInt(365))
	return one.Add(interest), true
}

// Repo persists deposits.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// ByInstrumentIDs returns userID's deposits whose instrument is in ids,
// keyed by instrument id. An id that is not userID's deposit is absent.
func (r *Repo) ByInstrumentIDs(ctx context.Context, userID int64, ids []int64) (map[int64]Deposit, error) {
	out := make(map[int64]Deposit, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	in, args := db.InList(ids)
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate,
		        start_date, maturity_date, funding_transaction_id, created_at
		 FROM term_deposits WHERE user_id = ? AND instrument_id IN (`+in+`)`,
		append([]any{userID}, args...)...,
	)
	if err != nil {
		return nil, fmt.Errorf("query deposits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d Deposit
		var createdAt string
		if err := rows.Scan(&d.ID, &d.UserID, &d.PortfolioID, &d.InstrumentID, &d.Currency, &d.Principal,
			&d.Terms.Rate, &d.Terms.StopajRate, &d.Terms.StartDate, &d.Terms.MaturityDate,
			&d.FundingTransactionID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan deposit: %w", err)
		}
		if d.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return nil, fmt.Errorf("parse deposit created_at: %w", err)
		}
		out[d.InstrumentID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deposits: %w", err)
	}
	return out, nil
}
