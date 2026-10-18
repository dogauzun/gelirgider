// Package plan holds a user's saving plans: a monthly standing order, a buy
// of an instrument or a cash deposit, that the user books occurrence by
// occurrence. next_date is the state: the oldest occurrence not yet booked or
// skipped.
package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// ErrNotFound is returned when no plan matches the user and id.
var ErrNotFound = errors.New("plan not found")

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

// QuantityScale is the decimal places of a quantity per market.
var QuantityScale = map[string]int32{"BIST": 0, "US": 4, "TEFAS": 6, "CRYPTO": 8, "GOLD": 4}

// Booking returns the quantity that amount buys at closePrice, rounded down to the
// market's scale (0 places for a market not in QuantityScale), and what that
// quantity costs at closePrice, rounded to 2 places. A close that is not positive
// buys nothing.
func Booking(amount, closePrice decimal.Decimal, market string) (quantity, booked decimal.Decimal) {
	if !closePrice.IsPositive() {
		return decimal.Zero, decimal.Zero
	}
	quantity = amount.Div(closePrice).RoundDown(QuantityScale[market])
	return quantity, quantity.Mul(closePrice).Round(2)
}

// Missed returns the number of occurrences of day from next through today, 1
// when next is the only one.
func Missed(next, today string, day int) int {
	n := 0
	for d := next; d != "" && d <= today; d = After(d, day) {
		n++
	}
	return n
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

// querier is the QueryContext of *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryPlans runs a SELECT of columns over saving_plans and scans every row.
func queryPlans(ctx context.Context, q querier, where string, args ...any) ([]Plan, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+columns+` FROM saving_plans WHERE `+where, args...)
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
	return queryPlans(ctx, r.db, `user_id = ? AND portfolio_id = ? ORDER BY next_date, id`, userID, portfolioID)
}

// Get returns userID's plan with the given id, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, userID, id int64) (Plan, error) {
	return getPlan(ctx, r.db, userID, id)
}

// GetTx is Get inside tx, so the answer holds until tx ends.
func (r *Repo) GetTx(ctx context.Context, tx *sql.Tx, userID, id int64) (Plan, error) {
	return getPlan(ctx, tx, userID, id)
}

func getPlan(ctx context.Context, q querier, userID, id int64) (Plan, error) {
	ps, err := queryPlans(ctx, q, `user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return Plan{}, err
	}
	if len(ps) == 0 {
		return Plan{}, ErrNotFound
	}
	return ps[0], nil
}

// AdvanceTx moves p's next_date to next inside tx. It returns ErrNotFound when
// the plan is gone or its next_date is no longer p.NextDate, so two concurrent
// bookings of one occurrence cannot both pass.
func (r *Repo) AdvanceTx(ctx context.Context, tx *sql.Tx, p Plan, next string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE saving_plans SET next_date = ? WHERE id = ? AND user_id = ? AND next_date = ?`,
		next, p.ID, p.UserID, p.NextDate)
	if err != nil {
		return fmt.Errorf("advance plan: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}

// Update writes the editable fields of p (amount, day_of_month, end_date,
// next_date, fund, note) to the plan with p.ID and p.UserID, or returns
// ErrNotFound.
func (r *Repo) Update(ctx context.Context, p Plan) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE saving_plans SET amount = ?, day_of_month = ?, end_date = ?, next_date = ?, fund = ?, note = ?
		 WHERE id = ? AND user_id = ?`,
		p.Amount.String(), p.DayOfMonth, p.EndDate, p.NextDate, p.Fund, p.Note, p.ID, p.UserID)
	if err != nil {
		return fmt.Errorf("update plan: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}

// Delete removes userID's plan with the given id, or returns ErrNotFound.
func (r *Repo) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM saving_plans WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}
