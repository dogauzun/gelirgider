// Package price provides persistence for instrument closing prices. Repo
// holds fetched prices: shared market reference data (not user data), same
// category as instruments, so it is not scoped by user_id. ManualRepo holds
// each user's manual price overrides and is scoped by user_id.
package price

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/shopspring/decimal"

	// Embed the IANA tzdata into the binary: the prod image is
	// distroless/static-debian12, which has no /usr/share/zoneinfo, and
	// Today needs Europe/Istanbul.
	_ "time/tzdata"
)

// Price is a closing price for an instrument on a given date.
type Price struct {
	InstrumentID int64
	Date         string // YYYY-MM-DD
	Close        decimal.Decimal
	Source       string
}

// decimalPattern matches a positive decimal: digits, optionally a decimal
// point followed by more digits. No sign, no exponent. Digit counts are
// bounded (15 integer digits like the transaction parser, but 8 fraction
// digits where a transaction allows 9; frontend/src/lib/decimal.ts mirrors
// both) so an oversized string cannot be stored and later blow up decimal
// math in summary/history.
var decimalPattern = regexp.MustCompile(`^\d{1,15}(\.\d{1,8})?$`)

// ValidClose reports whether s is a positive decimal string suitable for
// storage in the close column. close is stored as text, never a float, per
// project convention for money values.
func ValidClose(s string) bool {
	return decimalPattern.MatchString(s) && !isAllZero(s)
}

// digitsPattern is decimalPattern without the digit limits.
var digitsPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

// TooManyDigits reports whether s is a positive plain decimal that
// ValidClose rejects only for its digit counts, so the error can name the
// limit instead of "must be a positive decimal".
func TooManyDigits(s string) bool {
	return !decimalPattern.MatchString(s) && digitsPattern.MatchString(s) && !isAllZero(s)
}

func isAllZero(s string) bool {
	for _, c := range s {
		if c != '0' && c != '.' {
			return false
		}
	}
	return true
}

// ValidDate reports whether s is a real calendar date written as
// YYYY-MM-DD. time.Parse with this layout is strict about both the shape
// (fixed digit counts, no trailing text) and the calendar, so impossible
// dates like 2026-02-31 or 2026-00-00 are rejected -- a regexp on the shape
// alone would store rows that can never match a trading day.
func ValidDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// MinDate is the earliest date a user may enter: a transaction, a manual
// price, the admin backfill start. It only catches mistyped years (0001,
// 0226 for 2026); any real holding is newer.
const MinDate = "1990-01-01"

// CheckDate checks a user-entered date: a real YYYY-MM-DD calendar date, not
// after Today, not before MinDate. The error is a bare message tail; each
// caller prefixes its field name ("date: ", "period_start: "), and the
// frontend translates the prefixed messages, so the tails must not change.
func CheckDate(s string) error {
	if !ValidDate(s) {
		return errors.New("must be YYYY-MM-DD")
	}
	// YYYY-MM-DD sorts lexically, so a string compare is a date compare.
	if s > Today() {
		return errors.New("must not be in the future")
	}
	if s < MinDate {
		return errors.New("must be on or after " + MinDate)
	}
	return nil
}

// Istanbul is the market time zone: every market date in this project is
// a Europe/Istanbul calendar date. Loaded once; the blank time/tzdata
// import above embeds the zone, so the load cannot fail.
var Istanbul = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	return loc
}()

// Today returns the current calendar date in Europe/Istanbul as
// YYYY-MM-DD. Market dates in this project are Istanbul dates, so a price
// row is "in the future" when its date is after this value.
func Today() string {
	return time.Now().In(Istanbul).Format(time.DateOnly)
}

// Midnight returns t's calendar date at 00:00 in t's own location.
func Midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Yesterday returns the Europe/Istanbul calendar day before now's, as an
// Istanbul midnight: the last date a bulletin or FX rate can exist for.
func Yesterday(now time.Time) time.Time {
	return Midnight(now.In(Istanbul)).AddDate(0, 0, -1)
}

// ParseDate parses a YYYY-MM-DD market date as an Istanbul midnight.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(time.DateOnly, s, Istanbul)
}

// Repo persists prices.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Upsert stores close as the price for instrumentID on date, from source.
// If a row for (instrumentID, date) already exists, its close and source
// are overwritten. Implemented as UPDATE-then-INSERT rather than a
// dialect-specific upsert, per project convention of portable SQL, run in
// one transaction: BeginTx takes the write lock up front (_txlock=immediate),
// so two overlapping fetches of a new key cannot both miss on UPDATE and
// both INSERT. A date that is not YYYY-MM-DD is an error: no
// query would ever find it.
func (r *Repo) Upsert(ctx context.Context, instrumentID int64, date, close, source string) error {
	if !ValidDate(date) {
		return fmt.Errorf("upsert price: date %q is not YYYY-MM-DD", date)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.Upsert(ctx, tx,
		"UPDATE prices SET close = ?, source = ? WHERE instrument_id = ? AND date = ?",
		[]any{close, source, instrumentID, date},
		"INSERT INTO prices (instrument_id, date, close, source) VALUES (?, ?, ?, ?)",
		[]any{instrumentID, date, close, source},
	); err != nil {
		return fmt.Errorf("upsert price: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit price: %w", err)
	}
	return nil
}

// hasSourceQuery is HasSource's lookup. Both EXISTS arms are index seeks:
// prices on idx_prices_date_source, empty_fetches on its
// (source, date) primary key. Without the index the prices arm scanned the
// whole table once per day of every backfill walk.
const hasSourceQuery = `SELECT EXISTS(SELECT 1 FROM prices WHERE date = ? AND source = ?)
	OR EXISTS(SELECT 1 FROM empty_fetches WHERE date = ? AND source = ?)`

// HasSource reports whether at least one price row exists for date with the
// given source, or MarkEmpty recorded date as having no data.
// Used by the historical backfill to skip dates it has already synced.
func (r *Repo) HasSource(ctx context.Context, date, source string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, hasSourceQuery,
		date, source, date, source,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check price source: %w", err)
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

// latestQuery is Latest's lookup, one statement for every id in its IN
// list. The plan walks the requested instrument ids and, per id, seeks the
// newest date on or before asOf, then the row at that date, all on the
// (instrument_id, date) primary key, so its cost does not grow with the
// length of price history nor cost one query per id.
// A MAX subquery correlated to every stored row re-ran per row instead.
const latestQuery = `SELECT p.instrument_id, p.date, p.close, p.source
	FROM instruments i JOIN prices p ON p.instrument_id = i.id
	WHERE i.id IN (%s) AND p.date = (
		SELECT q.date FROM prices q WHERE q.instrument_id = i.id AND q.date <= ?
		ORDER BY q.date DESC LIMIT 1)`

// Latest returns the newest price row dated on or before asOf (YYYY-MM-DD)
// for each of instrumentIDs. Rows dated after asOf are ignored so a
// future-dated row can never become an instrument's "last price".
// Instrument ids with no qualifying rows are omitted from the result.
func (r *Repo) Latest(ctx context.Context, instrumentIDs []int64, asOf string) (map[int64]Price, error) {
	out := make(map[int64]Price, len(instrumentIDs))
	if len(instrumentIDs) == 0 {
		return out, nil
	}
	placeholders, args := db.InList(instrumentIDs)
	rows, err := r.query(ctx, fmt.Sprintf(latestQuery, placeholders), append(args, asOf)...)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		out[p.InstrumentID] = p
	}
	return out, nil
}

// Range returns all price rows for instrumentIDs with date in [from, to]
// inclusive, ordered by instrument_id then date ascending. Used by the
// portfolio history endpoint to load prices for a whole date window in one
// query instead of once per day.
func (r *Repo) Range(ctx context.Context, instrumentIDs []int64, from, to string) ([]Price, error) {
	if len(instrumentIDs) == 0 {
		return []Price{}, nil
	}
	placeholders, args := db.InList(instrumentIDs)
	return r.query(ctx,
		fmt.Sprintf(`SELECT instrument_id, date, close, source FROM prices
		 WHERE instrument_id IN (%s) AND date >= ? AND date <= ?
		 ORDER BY instrument_id, date`,
			placeholders),
		append(args, from, to)...,
	)
}

// History returns up to limit price rows for instrumentID, newest first.
func (r *Repo) History(ctx context.Context, instrumentID int64, limit int) ([]Price, error) {
	return r.query(ctx,
		`SELECT instrument_id, date, close, source FROM prices
		 WHERE instrument_id = ?
		 ORDER BY date DESC
		 LIMIT ?`,
		instrumentID, limit,
	)
}

// query runs a SELECT of instrument_id, date, close, source in that order
// and scans every row.
func (r *Repo) query(ctx context.Context, query string, args ...any) ([]Price, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query prices: %w", err)
	}
	defer rows.Close()

	out := []Price{}
	for rows.Next() {
		var p Price
		if err := rows.Scan(&p.InstrumentID, &p.Date, &p.Close, &p.Source); err != nil {
			return nil, fmt.Errorf("scan price: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prices: %w", err)
	}
	return out, nil
}
