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
	days, ok := termDays(terms.StartDate, min(day, terms.MaturityDate))
	if !ok {
		return decimal.Zero, false
	}
	one := decimal.NewFromInt(1)
	interest := terms.Rate.Mul(decimal.NewFromInt(days)).Mul(one.Sub(terms.StopajRate)).Div(decimal.NewFromInt(365))
	return one.Add(interest), true
}

// termDays returns the whole days from start to end (YYYY-MM-DD), false when
// a date does not parse.
func termDays(start, end string) (int64, bool) {
	from, err := time.Parse(time.DateOnly, start)
	if err != nil {
		return 0, false
	}
	to, err := time.Parse(time.DateOnly, end)
	if err != nil {
		return 0, false
	}
	return int64(to.Sub(from).Hours() / 24), true
}

// Interest returns what principal earns over the whole term: the gross
// simple interest, actual/365, and the stopaj withheld from it, each rounded
// to 2 decimals. The net is gross minus stopaj. Both are zero when a date
// does not parse.
func Interest(principal decimal.Decimal, terms Terms) (gross, stopaj decimal.Decimal) {
	days, ok := termDays(terms.StartDate, terms.MaturityDate)
	if !ok {
		return decimal.Zero, decimal.Zero
	}
	gross = principal.Mul(terms.Rate).Mul(decimal.NewFromInt(days)).Div(decimal.NewFromInt(365)).Round(2)
	return gross, gross.Mul(terms.StopajRate).Round(2)
}

// Repo persists deposits.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// columns is the term_deposits column list every SELECT reads, in the order
// scanDeposit scans it.
const columns = `id, user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate,
	start_date, maturity_date, funding_transaction_id, created_at`

func scanDeposit(rows *sql.Rows) (Deposit, error) {
	var d Deposit
	var createdAt string
	if err := rows.Scan(&d.ID, &d.UserID, &d.PortfolioID, &d.InstrumentID, &d.Currency, &d.Principal,
		&d.Terms.Rate, &d.Terms.StopajRate, &d.Terms.StartDate, &d.Terms.MaturityDate,
		&d.FundingTransactionID, &createdAt); err != nil {
		return Deposit{}, fmt.Errorf("scan deposit: %w", err)
	}
	var err error
	if d.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return Deposit{}, fmt.Errorf("parse deposit created_at: %w", err)
	}
	return d, nil
}

// queryDeposits runs a SELECT of columns over term_deposits and scans every
// row.
func (r *Repo) queryDeposits(ctx context.Context, where string, args ...any) ([]Deposit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM term_deposits WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query deposits: %w", err)
	}
	defer rows.Close()
	var out []Deposit
	for rows.Next() {
		d, err := scanDeposit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deposits: %w", err)
	}
	return out, nil
}

// ByInstrumentIDs returns userID's deposits whose instrument is in ids,
// keyed by instrument id. An id that is not userID's deposit is absent.
func (r *Repo) ByInstrumentIDs(ctx context.Context, userID int64, ids []int64) (map[int64]Deposit, error) {
	out := make(map[int64]Deposit, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	in, args := db.InList(ids)
	ds, err := r.queryDeposits(ctx, `user_id = ? AND instrument_id IN (`+in+`)`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		out[d.InstrumentID] = d
	}
	return out, nil
}

// List returns userID's deposits in portfolioID, newest start date first.
func (r *Repo) List(ctx context.Context, userID, portfolioID int64) ([]Deposit, error) {
	return r.queryDeposits(ctx, `user_id = ? AND portfolio_id = ? ORDER BY start_date DESC, id DESC`, userID, portfolioID)
}

// InsertTx stores d inside tx and returns it with its id and creation time
// set; d's ID and CreatedAt are ignored. Ownership of the portfolio and the
// instrument is the caller's to check.
func (r *Repo) InsertTx(ctx context.Context, tx *sql.Tx, d Deposit) (Deposit, error) {
	d.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO term_deposits (user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate,
		                            start_date, maturity_date, funding_transaction_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.UserID, d.PortfolioID, d.InstrumentID, d.Currency, d.Principal.String(), d.Terms.Rate.String(),
		d.Terms.StopajRate.String(), d.Terms.StartDate, d.Terms.MaturityDate, d.FundingTransactionID,
		d.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return Deposit{}, fmt.Errorf("insert deposit: %w", err)
	}
	if d.ID, err = res.LastInsertId(); err != nil {
		return Deposit{}, fmt.Errorf("get inserted deposit id: %w", err)
	}
	return d, nil
}
