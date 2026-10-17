// Package fx provides persistence for currency exchange rates. This is
// shared market reference data (not user data), same category as prices,
// so it is not scoped by user_id.
//
// rate is TRY per 1 unit of currency, stored as decimal text. TRY itself
// is implied 1 and has no rows. Callers convert currency A to currency B
// as rate(A) / rate(B).
package fx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"
)

// Rate is an exchange rate for a currency on a given date.
type Rate struct {
	Currency string
	Date     string          // YYYY-MM-DD
	Rate     decimal.Decimal // TRY per 1 unit of Currency
	Source   string
}

// Repo persists FX rates.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert stores rate as the exchange rate for currency on date, from
// source. If a row for (currency, date) already exists, its rate and
// source are overwritten. Implemented as UPDATE-then-INSERT rather than a
// dialect-specific upsert, per project convention of portable SQL, in one
// transaction so overlapping fetches of a new key cannot both INSERT
// (see price.Repo.Upsert).
func (r *Repo) Upsert(ctx context.Context, currency, date, rate, source string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE fx_rates SET rate = ?, source = ? WHERE currency = ? AND date = ?",
		[]any{rate, source, currency, date},
		"INSERT INTO fx_rates (currency, date, rate, source) VALUES (?, ?, ?, ?)",
		[]any{currency, date, rate, source},
	); err != nil {
		return fmt.Errorf("upsert fx rate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fx rate: %w", err)
	}
	return nil
}

// hasSourceQuery is HasSource's lookup. Both EXISTS arms are index seeks:
// fx_rates on idx_fx_rates_date_source, empty_fetches on its
// (source, date) primary key. Without the index the fx_rates arm scanned
// the whole table once per day of every backfill walk.
const hasSourceQuery = `SELECT EXISTS(SELECT 1 FROM fx_rates WHERE date = ? AND source = ?)
	OR EXISTS(SELECT 1 FROM empty_fetches WHERE date = ? AND source = ?)`

// HasSource reports whether at least one fx_rates row exists for date with
// the given source, or MarkEmpty recorded date as having no data.
// Used by backfill to skip dates already synced.
func (r *Repo) HasSource(ctx context.Context, date, source string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, hasSourceQuery,
		date, source, date, source,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check fx rate source: %w", err)
	}
	return exists == 1, nil
}

// MarkEmpty records that source was fetched for date and had no data (a
// market holiday), so HasSource reports it as synced from then on.
func (r *Repo) MarkEmpty(ctx context.Context, date, source string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO empty_fetches (source, date) SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM empty_fetches WHERE source = ? AND date = ?)`,
		source, date, source, date,
	); err != nil {
		return fmt.Errorf("mark empty fetch: %w", err)
	}
	return nil
}

// latestQuery is Latest's lookup, one statement for every currency in its
// VALUES list. The plan walks the requested currencies and, per
// currency, seeks the newest date on or before asOf, then the row at that
// date, all on the (currency, date) primary key, so its cost does not grow
// with the length of rate history. fx_rates has no parent table
// to drive the walk the way instruments drives price.Repo.Latest, and an
// IN filter on fx_rates itself would run the date subquery once per stored
// row, hence the VALUES list.
const latestQuery = `WITH c(currency) AS (VALUES %s)
	SELECT f.currency, f.date, f.rate, f.source
	FROM c JOIN fx_rates f ON f.currency = c.currency
	WHERE f.date = (
		SELECT q.date FROM fx_rates q WHERE q.currency = c.currency AND q.date <= ?
		ORDER BY q.date DESC LIMIT 1)`

// Latest returns the newest fx_rates row dated on or before asOf
// (YYYY-MM-DD) for each of currencies. Rows dated after asOf are ignored
// so a future-dated row can never become a currency's "current" rate.
// Currencies with no qualifying rows are omitted from the result.
func (r *Repo) Latest(ctx context.Context, currencies []string, asOf string) (map[string]Rate, error) {
	out := make(map[string]Rate, len(currencies))
	if len(currencies) == 0 {
		return out, nil
	}
	placeholders, args := db.InList(currencies)
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(latestQuery, strings.ReplaceAll(placeholders, "?", "(?)")),
		append(args, asOf)...)
	if err != nil {
		return nil, fmt.Errorf("query latest fx rate: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rate Rate
		if err := rows.Scan(&rate.Currency, &rate.Date, &rate.Rate, &rate.Source); err != nil {
			return nil, fmt.Errorf("scan latest fx rate: %w", err)
		}
		out[rate.Currency] = rate
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest fx rate: %w", err)
	}
	return out, nil
}

// Range returns all fx_rates rows for currencies with date in [from, to]
// inclusive, ordered by date then currency ascending.
func (r *Repo) Range(ctx context.Context, currencies []string, from, to string) ([]Rate, error) {
	out := []Rate{}
	if len(currencies) == 0 {
		return out, nil
	}

	placeholders, args := db.InList(currencies)
	args = append(args, from, to)

	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT currency, date, rate, source FROM fx_rates
		 WHERE currency IN (%s) AND date >= ? AND date <= ?
		 ORDER BY date, currency`,
			placeholders),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query fx rate range: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rate Rate
		if err := rows.Scan(&rate.Currency, &rate.Date, &rate.Rate, &rate.Source); err != nil {
			return nil, fmt.Errorf("scan fx rate: %w", err)
		}
		out = append(out, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fx rate range: %w", err)
	}
	return out, nil
}
