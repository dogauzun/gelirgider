// Package dividend stores provider cash dividends. Dividends are shared
// market data like prices and splits: no user_id. It also stores the
// per-user dismissals of expected dividends, which do carry user_id.
package dividend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// Dividend is one dividend row. Amount is gross cash per share on the
// ex-date Date, in Currency.
type Dividend struct {
	InstrumentID int64
	Date         string
	Amount       decimal.Decimal
	Currency     string
	Source       string
}

// Repo persists dividends.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert stores a dividend for (instrumentID, date), overwriting amount,
// currency and source of an existing row (last writer wins). Same
// UPDATE-then-INSERT shape as split.Repo.Upsert, for portable SQL, in one
// transaction so overlapping fetches of a new key cannot both INSERT.
func (r *Repo) Upsert(ctx context.Context, instrumentID int64, date string, amount decimal.Decimal, currency, source string) error {
	if !amount.IsPositive() {
		return errors.New("dividend amount must be > 0")
	}
	if !price.ValidDate(date) {
		return fmt.Errorf("dividend date %q is not YYYY-MM-DD", date)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE dividends SET amount = ?, currency = ?, source = ? WHERE instrument_id = ? AND date = ?",
		[]any{amount.String(), currency, source, instrumentID, date},
		"INSERT INTO dividends (instrument_id, date, amount, currency, source) VALUES (?, ?, ?, ?, ?)",
		[]any{instrumentID, date, amount.String(), currency, source},
	); err != nil {
		return fmt.Errorf("upsert dividend: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit dividend: %w", err)
	}
	return nil
}

// ListFor returns the dividends of instrumentIDs in one query, keyed by
// instrument and ordered by date ascending. Instruments without dividends
// have no key.
func (r *Repo) ListFor(ctx context.Context, instrumentIDs []int64) (map[int64][]Dividend, error) {
	out := make(map[int64][]Dividend)
	if len(instrumentIDs) == 0 {
		return out, nil
	}
	placeholders, args := db.InList(instrumentIDs)
	rows, err := r.db.QueryContext(ctx,
		"SELECT instrument_id, date, amount, currency, source FROM dividends WHERE instrument_id IN ("+placeholders+") ORDER BY instrument_id, date",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query dividends: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var d Dividend
		if err := rows.Scan(&d.InstrumentID, &d.Date, &d.Amount, &d.Currency, &d.Source); err != nil {
			return nil, fmt.Errorf("scan dividend: %w", err)
		}
		out[d.InstrumentID] = append(out[d.InstrumentID], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dividends: %w", err)
	}
	return out, nil
}

// StopajRates returns the stopaj rate per market from the stopaj_rates
// table. A market without a row is absent: its rate is 0.
func (r *Repo) StopajRates(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT market, rate FROM stopaj_rates")
	if err != nil {
		return nil, fmt.Errorf("query stopaj rates: %w", err)
	}
	defer rows.Close()

	out := make(map[string]decimal.Decimal)
	for rows.Next() {
		var market string
		var rate decimal.Decimal
		if err := rows.Scan(&market, &rate); err != nil {
			return nil, fmt.Errorf("scan stopaj rate: %w", err)
		}
		out[market] = rate
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stopaj rates: %w", err)
	}
	return out, nil
}

// A dividend transaction dated from ex-date -MatchBeforeDays to
// +MatchAfterDays accounts for that ex-date (see MatchRecorded).
const (
	MatchBeforeDays = 7
	MatchAfterDays  = 45
)

// MatchRecorded pairs recorded dividends (dates ascending) with the ex-dates
// (ascending) they account for: the result holds, per recorded date, the
// ex-date it took, or "" for none. One recorded dividend takes at most one
// ex-date and an ex-date is taken once. Each recorded date, in
// order, takes one ex-date not yet taken from -MatchBeforeDays to
// +MatchAfterDays of it: the latest one on or before it, since a payment date
// follows its ex-date, else the earliest one after it. Plain nearest would
// give a Midas row paid a few days before the next ex-date to the wrong
// month.
func MatchRecorded(recorded, exDates []string) ([]string, error) {
	ex := make([]time.Time, len(exDates))
	for i, s := range exDates {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return nil, err
		}
		ex[i] = d
	}
	taken := map[string]bool{}
	out := make([]string, len(recorded))
	for r, s := range recorded {
		rd, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return nil, err
		}
		best, bestDist := -1, 0
		for i, d := range ex {
			days := int(rd.Sub(d).Hours() / 24)
			if taken[exDates[i]] || days < -MatchBeforeDays || days > MatchAfterDays {
				continue
			}
			// Rank: on or before by days since, then after by days until.
			dist := days
			if days < 0 {
				dist = MatchAfterDays - days
			}
			if best < 0 || dist < bestDist {
				best, bestDist = i, dist
			}
		}
		if best >= 0 {
			taken[exDates[best]] = true
			out[r] = exDates[best]
		}
	}
	return out, nil
}
