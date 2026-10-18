// Package plan holds a user's saving plans: a monthly standing order, a buy
// of an instrument or a cash deposit, that the user books occurrence by
// occurrence. next_date is the state: the oldest occurrence not yet booked or
// skipped.
package plan

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Plan is a stored saving plan. InstrumentID is nil for a cash deposit plan.
// Amount is in Currency, which is the instrument's currency for a buy plan.
// Dates are YYYY-MM-DD; EndDate is nil for an open-ended plan.
type Plan struct {
	ID           int64
	UserID       int64
	PortfolioID  int64
	InstrumentID *int64
	Currency     string
	Amount       decimal.Decimal
	DayOfMonth   int
	StartDate    string
	EndDate      *string
	NextDate     string
	Fund         bool
	Note         string
	CreatedAt    time.Time
}

// Occurrence returns the plan day of the month as YYYY-MM-DD, the day clamped
// to the month's length. A month past 12 rolls into the next year.
func Occurrence(year int, month time.Month, day int) string {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(day, last), 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

// After returns the first occurrence of day strictly after date, "" when date
// does not parse; callers validate it.
func After(date string, day int) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return ""
	}
	for i := 0; ; i++ {
		if c := Occurrence(t.Year(), t.Month()+time.Month(i), day); c > date {
			return c
		}
	}
}

// FirstOnOrAfter returns the first occurrence of day on or after date.
func FirstOnOrAfter(date string, day int) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return ""
	}
	return After(t.AddDate(0, 0, -1).Format(time.DateOnly), day)
}

// Ended reports whether the next occurrence is past the end date.
func (p Plan) Ended() bool { return p.EndDate != nil && p.NextDate > *p.EndDate }

// Due reports whether the next occurrence is on or before today (YYYY-MM-DD)
// and the plan has not ended.
func (p Plan) Due(today string) bool { return p.NextDate <= today && !p.Ended() }

// Repo persists plans.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// columns is the saving_plans column list every SELECT reads, in the order
// scanPlan scans it.
const columns = `id, user_id, portfolio_id, instrument_id, currency, amount, day_of_month,
	start_date, end_date, next_date, fund, note, created_at`

func scanPlan(rows *sql.Rows) (Plan, error) {
	var p Plan
	var createdAt string
	if err := rows.Scan(&p.ID, &p.UserID, &p.PortfolioID, &p.InstrumentID, &p.Currency, &p.Amount, &p.DayOfMonth,
		&p.StartDate, &p.EndDate, &p.NextDate, &p.Fund, &p.Note, &createdAt); err != nil {
		return Plan{}, fmt.Errorf("scan plan: %w", err)
	}
	var err error
	if p.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return Plan{}, fmt.Errorf("parse plan created_at: %w", err)
	}
	return p, nil
}

// queryPlans runs a SELECT of columns over saving_plans and scans every row.
func (r *Repo) queryPlans(ctx context.Context, where string, args ...any) ([]Plan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM saving_plans WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query plans: %w", err)
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plans: %w", err)
	}
	return out, nil
}

// Insert stores p and returns it with its id and creation time set; p's ID
// and CreatedAt are ignored. Ownership of the portfolio and the instrument,
// and the validity of p, are the caller's to check.
func (r *Repo) Insert(ctx context.Context, p Plan) (Plan, error) {
	p.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO saving_plans (user_id, portfolio_id, instrument_id, currency, amount, day_of_month,
		                           start_date, end_date, next_date, fund, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.PortfolioID, p.InstrumentID, p.Currency, p.Amount.String(), p.DayOfMonth,
		p.StartDate, p.EndDate, p.NextDate, p.Fund, p.Note, p.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return Plan{}, fmt.Errorf("insert plan: %w", err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return Plan{}, fmt.Errorf("get inserted plan id: %w", err)
	}
	return p, nil
}

// List returns userID's plans in portfolioID, the soonest next_date first.
func (r *Repo) List(ctx context.Context, userID, portfolioID int64) ([]Plan, error) {
	return r.queryPlans(ctx, `user_id = ? AND portfolio_id = ? ORDER BY next_date, id`, userID, portfolioID)
}
