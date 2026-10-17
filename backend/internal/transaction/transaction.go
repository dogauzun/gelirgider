// Package transaction provides persistence and HTTP handlers for portfolio
// transactions (buy, sell, dividend, deposit, withdrawal, fee), plus the
// synthetic split rows List adds from market data. Every query is scoped by
// user_id -- a user can never read or modify another user's transactions.
// Holdings and cash balances are derived from these rows by
// internal/holdings; this package only stores and validates them.
package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/shopspring/decimal"
)

// Transaction types.
const (
	TypeBuy        = "buy"
	TypeSell       = "sell"
	TypeDividend   = "dividend"
	TypeDeposit    = "deposit"
	TypeWithdrawal = "withdrawal"
	TypeFee        = "fee"
	// TypeSplit is a holdings-engine-only row built from market data
	// (ADR 0008): Quantity carries the ratio, new shares per old share. It is
	// deliberately absent from validTypes so the transactions API rejects it.
	TypeSplit = "split"
)

var validTypes = map[string]bool{
	TypeBuy:        true,
	TypeSell:       true,
	TypeDividend:   true,
	TypeDeposit:    true,
	TypeWithdrawal: true,
	TypeFee:        true,
}

// ValidCurrencies lists the currencies accepted on transactions that have no
// instrument to derive a currency from (deposit, withdrawal, fee). Trades
// (buy, sell, dividend) always take their currency from the instrument
// instead. Kept as an alias to internal/currency so existing callers in this
// package don't need to change.
var ValidCurrencies = currency.Valid

// IsValidCurrency reports whether c is one of ValidCurrencies.
func IsValidCurrency(c string) bool {
	return currency.IsValid(c)
}

// ErrNotFound is returned when no transaction matches the lookup for the
// given user, including when the id belongs to another user.
var ErrNotFound = errors.New("transaction not found")

// Transaction is a stored portfolio event. Quantity and Price are the zero
// decimal.Decimal for types that don't carry them (dividend, deposit,
// withdrawal, fee); only buy and sell set them.
type Transaction struct {
	ID           int64
	UserID       int64
	PortfolioID  int64
	InstrumentID *int64
	Type         string
	Date         string // YYYY-MM-DD, Europe/Istanbul
	Quantity     decimal.Decimal
	Price        decimal.Decimal
	Amount       decimal.Decimal
	Fee          decimal.Decimal
	Currency     string
	Note         string
	CreatedAt    string

	// ImportID is the statement import that created this transaction, nil
	// for one entered by hand. Update never touches it: editing an imported
	// transaction keeps it attached to its import.
	ImportID *int64

	// InstrumentSymbol, InstrumentName and InstrumentMarket are joined in
	// from instruments by every read (List, ListPage and the rows Create and
	// Update return) so the frontend doesn't need a separate lookup-by-id
	// call. All are nil when InstrumentID is nil or unset.
	InstrumentSymbol *string
	InstrumentName   *string
	InstrumentMarket *string

	// bookedSplit marks a zero-price buy DropBookedSplits put in place of a
	// split row, so SortForApply applies it first on its date, as it would
	// the split.
	bookedSplit bool
	// storedDate is the date a bonus buy DropBookedSplits moved to its
	// split's ex-date is stored on, "" for every other row (StoredDate).
	storedDate string
}

// StoredDate returns the date t is stored on: Date, unless DropBookedSplits
// moved t, a bonus buy, to its split's ex-date. The statement preview's
// duplicate check reads List's rows through it.
func (t Transaction) StoredDate() string {
	if t.storedDate != "" {
		return t.storedDate
	}
	return t.Date
}

// SortForApply sorts txs into the order the holdings engine applies them:
// date, then splits first (they apply at the start of their ex-date), and
// with them the bonus buys DropBookedSplits put in their place, then id,
// which is typing or statement order. Rows with the same date and id
// (the statement preview's unsaved rows all carry id 0) keep their input
// order.
//
// Id order is not always trade order: a newest-first broker history enters
// a day trade sell-first, which would sell at an average cost of 0.
// So a sell that would take its instrument's quantity below zero
// has the same instrument's later buys of that date moved ahead of it, one
// at a time, until it no longer does or none are left. Any other day keeps
// id order, so a sell followed by a same-day rebuy realizes against the old
// lot.
func SortForApply(txs []Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		if txs[i].Date != txs[j].Date {
			return txs[i].Date < txs[j].Date
		}
		if si, sj := txs[i].Type == TypeSplit || txs[i].bookedSplit, txs[j].Type == TypeSplit || txs[j].bookedSplit; si != sj {
			return si
		}
		return txs[i].ID < txs[j].ID
	})

	// qty mirrors the quantity the holdings engine would hold per instrument.
	qty := map[int64]decimal.Decimal{}
	for i := 0; i < len(txs); i++ {
		t := txs[i]
		if t.InstrumentID == nil {
			continue
		}
		id := *t.InstrumentID
		switch t.Type {
		case TypeBuy:
			qty[id] = qty[id].Add(t.Quantity)
		case TypeSplit:
			qty[id] = qty[id].Mul(t.Quantity)
		case TypeSell:
			if !qty[id].Sub(t.Quantity).IsNegative() {
				qty[id] = qty[id].Sub(t.Quantity)
				continue
			}
			for j := i + 1; j < len(txs) && txs[j].Date == t.Date; j++ {
				b := txs[j]
				if b.Type == TypeBuy && b.InstrumentID != nil && *b.InstrumentID == id {
					copy(txs[i+1:j+1], txs[i:j])
					txs[i] = b
					break
				}
			}
			if txs[i].Type == TypeBuy {
				i-- // apply the moved buy, then look at the sell again
				continue
			}
			qty[id] = qty[id].Sub(t.Quantity)
		}
	}
}

// hasQuantityPrice reports whether transactions of txType carry a quantity
// and price (only buy and sell do).
func hasQuantityPrice(txType string) bool {
	return txType == TypeBuy || txType == TypeSell
}

// Repo persists transactions.
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo backed by db.
func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// joinedSelect reads transactions with the instrument's symbol/name/market
// joined in, so callers (the transaction API) don't need a separate
// lookup-by-id call. Every query built on it adds a user_id filter.
const joinedSelect = `SELECT t.id, t.user_id, t.portfolio_id, t.instrument_id, t.type, t.date, t.quantity, t.price, t.amount, t.fee, t.currency, t.note, t.created_at, t.import_id,
		        i.symbol, i.name, i.market
		 FROM transactions t
		 LEFT JOIN instruments i ON i.id = t.instrument_id`

// listSelect is the shared head of List and ListPage. Both append their own
// ORDER BY; neither may drop the user_id filter.
const listSelect = joinedSelect + `
		 WHERE t.user_id = ? AND t.portfolio_id = ?`

// get returns the transaction with the given id owned by userID, the shape
// List returns, for Create and Update to hand back.
func (r *Repo) get(ctx context.Context, userID, id int64) (Transaction, error) {
	return scanTransactionWithInstrument(r.db.QueryRowContext(ctx,
		joinedSelect+" WHERE t.id = ? AND t.user_id = ?", id, userID,
	))
}

// ImportNone as Filter.ImportID keeps only the rows entered by hand, those
// with no import. Import ids are positive, so it names none of them.
const ImportNone int64 = -1

// Filter narrows ListPage and ListStored to the rows the transaction list
// and its CSV export show for the same query values. The zero Filter keeps
// every row.
type Filter struct {
	// Types, when non-empty, keeps only rows of those types.
	Types []string
	// InstrumentID, when non-zero, keeps only that instrument's rows.
	InstrumentID int64
	// ImportID, when non-zero, keeps only that import's rows or, as
	// ImportNone, the hand-entered ones.
	ImportID int64
	// From and To, when non-empty, keep only rows in that inclusive
	// YYYY-MM-DD range.
	From, To string
}

// where returns f as conditions to append to listSelect's WHERE, each
// starting with AND, and their arguments in order.
func (f Filter) where() (string, []any) {
	var query string
	var args []any
	if len(f.Types) > 0 {
		placeholders, typeArgs := db.InList(f.Types)
		query += " AND t.type IN (" + placeholders + ")"
		args = append(args, typeArgs...)
	}
	if f.InstrumentID != 0 {
		query += " AND t.instrument_id = ?"
		args = append(args, f.InstrumentID)
	}
	switch {
	case f.ImportID == ImportNone:
		query += " AND t.import_id IS NULL"
	case f.ImportID != 0:
		query += " AND t.import_id = ?"
		args = append(args, f.ImportID)
	}
	if f.From != "" {
		query += " AND t.date >= ?"
		args = append(args, f.From)
	}
	if f.To != "" {
		query += " AND t.date <= ?"
		args = append(args, f.To)
	}
	return query, args
}

// Cursor marks the last row of a page. Paging continues strictly before it in
// the page order, date then id, both descending. The zero Cursor starts at the
// newest row.
//
// It carries the date as well as the id because many transactions share a
// date: ordering or paging on date alone would skip or repeat rows around a
// shared date, and on id alone would cut across the date order entirely.
type Cursor struct {
	Date string
	ID   int64
}

// listSplits is the second half of List: one synthetic TypeSplit row per
// splits row of an instrument this portfolio holds transactions in (ADR
// 0008), with id 0, the ratio as quantity, zero amount and fee and the
// instrument's currency. Splits dated on or before the instrument's first
// transaction here are left out: they change nothing, and history takes its
// default start date from List's first row. Splits dated after today
// (Europe/Istanbul) are left out too: they have not happened yet, and the
// summary would apply them to today's position. The id and date aliases let
// List's ORDER BY name them: SQLite resolves a compound select's ORDER BY
// against aliases, not against listSelect's t.id and t.date.
const listSplits = ` UNION ALL
		 SELECT 0 AS id, f.user_id, f.portfolio_id, s.instrument_id, 'split', s.date AS date, s.ratio, NULL, '0', '0', i.currency, '', '', NULL,
		        i.symbol, i.name, i.market
		 FROM splits s
		 JOIN instruments i ON i.id = s.instrument_id
		 JOIN (SELECT user_id, portfolio_id, instrument_id, MIN(date) AS first_date
		       FROM transactions
		       WHERE user_id = ? AND portfolio_id = ? AND instrument_id IS NOT NULL
		       GROUP BY user_id, portfolio_id, instrument_id) f
		   ON f.instrument_id = s.instrument_id AND s.date > f.first_date
		 WHERE s.date <= ?`

// List returns all transactions for portfolioID owned by userID plus the
// synthetic split rows of its instruments (listSplits) minus those a
// zero-price buy already books (DropBookedSplits), in SortForApply
// order, which the history sweep and the dividend walk rely on, since they
// apply rows in the order List returns them. Summary, history, the portfolio
// list and the statement preview read every row through it; the paged list
// API uses ListPage instead, which never returns split rows.
func (r *Repo) List(ctx context.Context, userID, portfolioID int64) ([]Transaction, error) {
	rows, err := r.db.QueryContext(ctx, listSelect+listSplits+" ORDER BY date, id", userID, portfolioID, userID, portfolioID, price.Today())
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()
	txs, err := scanList(rows)
	if err != nil {
		return nil, err
	}
	txs, err = DropBookedSplits(txs)
	if err != nil {
		return nil, err
	}
	SortForApply(txs)
	return txs, nil
}

// ListStored returns the transactions stored for portfolioID owned by
// userID that f keeps, by date then id, as they were booked: no split rows,
// and a bonus buy on its own date, which List moves to the ex-date.
// The CSV export writes it.
func (r *Repo) ListStored(ctx context.Context, userID, portfolioID int64, f Filter) ([]Transaction, error) {
	where, filterArgs := f.where()
	args := append([]any{userID, portfolioID}, filterArgs...)
	rows, err := r.db.QueryContext(ctx, listSelect+where+" ORDER BY t.date, t.id", args...)
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()
	return scanList(rows)
}

// bonusWindowDays is how far, in calendar days either side of a split's
// ex-date, a zero-price buy of the same instrument counts as that split's
// bonus shares: a statement prints the day the shares were credited, which
// can trail the ex-date by a few days.
const bonusWindowDays = 10

// bonusWindow is bonusWindowDays as a duration; dates parse as UTC
// midnight, so every day is 24 hours.
const bonusWindow = bonusWindowDays * 24 * time.Hour

// DropBookedSplits removes the split rows of txs, one portfolio's rows, that
// a buy of the same instrument at price 0 dated within bonusWindowDays of the
// ex-date already books (ADR 0008). That buy is the broker's own
// record of the bonus shares the account received, fractions included, so
// applying the split on top would scale the position twice. Only a buy whose
// quantity matches the shares the split would add books it (bookedSplits):
// shares gifted by hand next to a split do not. Price scaling by
// the instrument's splits is market data and is not affected, so each such
// buy is moved to the ex-date of the split it books (the first one listed,
// if it books more than one): the shares are held from the day the price is
// scaled, and value history neither dips nor spikes between the ex-date and
// the date the statement prints. Each such buy is also marked so
// SortForApply applies it before the other rows of the ex-date, as the
// split would be: a sell on the ex-date realizes against the post-split
// average cost. Order is kept, so a moved buy may be out of date
// order; List sorts the result afterwards.
func DropBookedSplits(txs []Transaction) ([]Transaction, error) {
	bonus := map[int64][]bonusBuy{}
	for _, t := range txs {
		if !isBonusBuy(t) {
			continue
		}
		d, err := time.Parse(time.DateOnly, t.Date)
		if err != nil {
			return nil, fmt.Errorf("parse transaction date %q: %w", t.Date, err)
		}
		bonus[*t.InstrumentID] = append(bonus[*t.InstrumentID], bonusBuy{d, t.Quantity})
	}
	if len(bonus) == 0 {
		return txs, nil
	}
	booked, err := bookedSplits(txs, bonus)
	if err != nil {
		return nil, err
	}
	// exDates holds, per instrument, the ex-dates of the splits dropped.
	exDates := map[int64][]time.Time{}
	out := make([]Transaction, 0, len(txs))
	for _, t := range txs {
		if t.Type == TypeSplit && t.InstrumentID != nil && booked[splitKey{*t.InstrumentID, t.Date}] {
			ex, err := time.Parse(time.DateOnly, t.Date)
			if err != nil {
				return nil, fmt.Errorf("parse split date %q: %w", t.Date, err)
			}
			exDates[*t.InstrumentID] = append(exDates[*t.InstrumentID], ex)
			continue
		}
		out = append(out, t)
	}
	for i, t := range out {
		if !isBonusBuy(t) {
			continue
		}
		d, err := time.Parse(time.DateOnly, t.Date)
		if err != nil {
			return nil, fmt.Errorf("parse transaction date %q: %w", t.Date, err)
		}
		for _, ex := range exDates[*t.InstrumentID] {
			if within(d, ex) {
				out[i].storedDate = t.Date
				out[i].Date = ex.Format(time.DateOnly)
				out[i].bookedSplit = true
				break
			}
		}
	}
	return out, nil
}

// bonusBuy is a zero-price buy, by its date and quantity.
type bonusBuy struct {
	date time.Time
	qty  decimal.Decimal
}

// splitKey names a split row: splits are unique per instrument and ex-date.
type splitKey struct {
	instrumentID int64
	date         string
}

// bonusTolerance is how far, in shares, the zero-price buys in a split's
// window may be from the shares the split would add and still book it: a
// broker pays the fraction in cash, so the credit is a whole number.
var bonusTolerance = decimal.NewFromInt(1)

// bookedSplits returns the splits of txs that the zero-price buys in bonus
// book. It walks txs in apply order, tracking the held quantity per
// instrument. A split is booked when the zero-price buys within its window
// add up to held x (ratio - 1), the shares it would add, give or take
// bonusTolerance; held leaves out those buys, so a bonus dated before the
// ex-date is not counted twice. A split whose window buys do not match is
// kept and scales the position as usual, so a hand-entered gift next to it
// does not lose it.
//
// shortcut: with no position held before the ex-date (a history that starts
// at the bonus row) there is nothing to match against and the buy books the
// split; the upgrade path is an explicit marker on imported bonus rows.
func bookedSplits(txs []Transaction, bonus map[int64][]bonusBuy) (map[splitKey]bool, error) {
	sorted := slices.Clone(txs)
	SortForApply(sorted)
	booked := map[splitKey]bool{}
	qty := map[int64]decimal.Decimal{}
	for _, t := range sorted {
		if t.InstrumentID == nil {
			continue
		}
		id := *t.InstrumentID
		switch t.Type {
		case TypeBuy:
			qty[id] = qty[id].Add(t.Quantity)
		case TypeSell:
			qty[id] = qty[id].Sub(t.Quantity)
		case TypeSplit:
			ex, err := time.Parse(time.DateOnly, t.Date)
			if err != nil {
				return nil, fmt.Errorf("parse split date %q: %w", t.Date, err)
			}
			held, got, found := qty[id], decimal.Zero, false
			for _, b := range bonus[id] {
				if !within(b.date, ex) {
					continue
				}
				found = true
				got = got.Add(b.qty)
				if b.date.Before(ex) {
					held = held.Sub(b.qty)
				}
			}
			adds := held.Mul(t.Quantity.Sub(decimal.NewFromInt(1)))
			if found && (!held.IsPositive() || got.Sub(adds).Abs().LessThanOrEqual(bonusTolerance)) {
				booked[splitKey{id, t.Date}] = true
			} else {
				qty[id] = qty[id].Mul(t.Quantity)
			}
		}
	}
	return booked, nil
}

// isBonusBuy reports whether t is a buy of an instrument at price 0, the
// shape a statement's bonus row is booked in.
func isBonusBuy(t Transaction) bool {
	return t.Type == TypeBuy && t.InstrumentID != nil && t.Price.IsZero()
}

// within reports whether d is within bonusWindowDays of ex.
func within(d, ex time.Time) bool {
	gap := d.Sub(ex)
	return gap >= -bonusWindow && gap <= bonusWindow
}

// ListPage returns at most limit transactions for portfolioID owned by
// userID that f keeps, newest first (date then id, both descending),
// starting strictly before the cursor. A zero Cursor starts at the newest
// row, so walking the pages visits every row exactly once. f is
// applied before the cursor and the limit, so a filtered walk pages over the
// filtered rows and nothing else.
func (r *Repo) ListPage(ctx context.Context, userID, portfolioID int64, limit int, before Cursor, f Filter) ([]Transaction, error) {
	where, filterArgs := f.where()
	query := listSelect + where
	args := append([]any{userID, portfolioID}, filterArgs...)
	if before.ID != 0 {
		query += " AND (t.date < ? OR (t.date = ? AND t.id < ?))"
		args = append(args, before.Date, before.Date, before.ID)
	}
	query += " ORDER BY t.date DESC, t.id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()
	return scanList(rows)
}

// scanList reads every row of a listSelect query.
func scanList(rows *sql.Rows) ([]Transaction, error) {
	out := []Transaction{}
	for rows.Next() {
		t, err := scanTransactionWithInstrument(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transactions: %w", err)
	}
	return out, nil
}

// execer is satisfied by both *sql.DB and *sql.Tx, so an insert can run on
// its own or as part of a caller's transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Create inserts t for userID and returns the stored row as List reads it
// (with id, created_at and the instrument's symbol, name and market set).
// t.PortfolioID must already be set by the caller; ownership of that
// portfolio is the caller's responsibility to check. Returns
// portfolio.ErrNotFound if the portfolio is deleted meanwhile.
func (r *Repo) Create(ctx context.Context, userID int64, t Transaction) (Transaction, error) {
	created, err := create(ctx, r.db, userID, t)
	if err != nil {
		return Transaction{}, err
	}
	stored, err := r.get(ctx, userID, created.ID)
	// A forced delete of the portfolio, or of its user, committed between
	// the insert and this read and took the new row with it.
	if errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, fmt.Errorf("query created transaction: %w", portfolio.ErrNotFound)
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("query created transaction: %w", err)
	}
	return stored, nil
}

// CreateTx is Create inside tx, returning t with its id and created_at
// set but without the instrument fields, for callers that write transactions together
// with other rows (the statement import commit writes the import row and its
// transactions in one go, so a failure halfway leaves neither behind).
func (r *Repo) CreateTx(ctx context.Context, tx *sql.Tx, userID int64, t Transaction) (Transaction, error) {
	return create(ctx, tx, userID, t)
}

// CreateAll inserts ts for userID in one database transaction, so a failure
// halfway leaves none behind, and returns their ids in order. Each t's
// PortfolioID must already be set and owned by userID; a portfolio deleted
// meanwhile is portfolio.ErrNotFound.
func (r *Repo) CreateAll(ctx context.Context, userID int64, ts []Transaction) ([]int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	ids := make([]int64, 0, len(ts))
	for _, t := range ts {
		created, err := create(ctx, tx, userID, t)
		if err != nil {
			return nil, err
		}
		ids = append(ids, created.ID)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return ids, nil
}

func create(ctx context.Context, exec execer, userID int64, t Transaction) (Transaction, error) {
	t.UserID = userID
	t.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	var quantity, price any
	if hasQuantityPrice(t.Type) {
		quantity, price = t.Quantity.String(), t.Price.String()
	}

	res, err := exec.ExecContext(ctx,
		`INSERT INTO transactions (user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, note, created_at, import_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.UserID, t.PortfolioID, nullInt(t.InstrumentID), t.Type, t.Date,
		quantity, price, t.Amount.String(), t.Fee.String(), t.Currency, t.Note, t.CreatedAt,
		nullInt(t.ImportID),
	)
	// Instruments are never deleted and an import's id is written in the
	// same SQL transaction as its rows, so a foreign key failure here is the
	// portfolio, or its user, deleted since the caller's ownership check.
	if db.IsForeignKeyViolation(err) {
		return Transaction{}, fmt.Errorf("insert transaction: %w", portfolio.ErrNotFound)
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("insert transaction: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Transaction{}, fmt.Errorf("get inserted id: %w", err)
	}
	t.ID = id
	return t, nil
}

// Update replaces the transaction with the given id, if owned by userID, and
// returns the updated row as List reads it, or ErrNotFound if it is not
// there, also when it is deleted meanwhile. PortfolioID and CreatedAt are not changed.
func (r *Repo) Update(ctx context.Context, userID, id int64, t Transaction) (Transaction, error) {
	var quantity, price any
	if hasQuantityPrice(t.Type) {
		quantity, price = t.Quantity.String(), t.Price.String()
	}

	res, err := r.db.ExecContext(ctx,
		`UPDATE transactions SET instrument_id = ?, type = ?, date = ?, quantity = ?, price = ?, amount = ?, fee = ?, currency = ?, note = ?
		 WHERE id = ? AND user_id = ?`,
		nullInt(t.InstrumentID), t.Type, t.Date, quantity, price, t.Amount.String(), t.Fee.String(), t.Currency, t.Note,
		id, userID,
	)
	if err != nil {
		return Transaction{}, fmt.Errorf("update transaction: %w", err)
	}
	if err := db.CheckAffected(res, ErrNotFound); err != nil {
		return Transaction{}, err
	}

	updated, err := r.get(ctx, userID, id)
	// A forced delete of the portfolio, or of its user, committed between
	// the update and this read and took the row with it.
	if errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, fmt.Errorf("query updated transaction: %w", ErrNotFound)
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("query updated transaction: %w", err)
	}
	return updated, nil
}

// Delete removes the transaction with the given id, if owned by userID, or
// returns ErrNotFound otherwise.
func (r *Repo) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM transactions WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("delete transaction: %w", err)
	}
	return db.CheckAffected(res, ErrNotFound)
}

// EarliestDate returns the earliest transaction date across every user and
// portfolio: of any transaction when market is empty, else of a transaction
// on an instrument in that market. This intentionally is not scoped by
// user_id: it backs the price backfill's choice of start date, an internal
// batch job, not a per-user API query. ok is false when no transaction
// matches.
func (r *Repo) EarliestDate(ctx context.Context, market string) (date string, ok bool, err error) {
	query, args := "SELECT MIN(date) FROM transactions", []any{}
	if market != "" {
		query = `SELECT MIN(t.date) FROM transactions t
			JOIN instruments i ON i.id = t.instrument_id
			WHERE i.market = ?`
		args = append(args, market)
	}
	var d sql.NullString
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&d); err != nil {
		return "", false, fmt.Errorf("query earliest transaction date: %w", err)
	}
	if !d.Valid {
		return "", false, nil
	}
	return d.String, true, nil
}

// EarliestDateByInstrument returns each instrument in market's earliest
// transaction date, keyed by instrument id. Like EarliestDate it is not
// scoped by user_id: it backs the US history backfill, an
// internal batch job.
func (r *Repo) EarliestDateByInstrument(ctx context.Context, market string) (map[int64]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.instrument_id, MIN(t.date) FROM transactions t
		 JOIN instruments i ON i.id = t.instrument_id
		 WHERE i.market = ?
		 GROUP BY t.instrument_id`,
		market,
	)
	if err != nil {
		return nil, fmt.Errorf("query earliest transaction dates: %w", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var d string
		if err := rows.Scan(&id, &d); err != nil {
			return nil, fmt.Errorf("scan earliest transaction date: %w", err)
		}
		out[id] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate earliest transaction dates: %w", err)
	}
	return out, nil
}

// nullInt returns nil for a nil pointer, or the pointed-to value, for use as
// a driver parameter.
func nullInt(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanTransactionWithInstrument scans a row of a joinedSelect query: the
// usual columns plus the instrument's symbol/name/market.
func scanTransactionWithInstrument(rows scanner) (Transaction, error) {
	var t Transaction
	var instrumentID, importID sql.NullInt64
	var quantity, price decimal.NullDecimal
	var note sql.NullString
	var symbol, name, market sql.NullString

	if err := rows.Scan(
		&t.ID, &t.UserID, &t.PortfolioID, &instrumentID, &t.Type, &t.Date,
		&quantity, &price, &t.Amount, &t.Fee, &t.Currency, &note, &t.CreatedAt, &importID,
		&symbol, &name, &market,
	); err != nil {
		return Transaction{}, err
	}
	if instrumentID.Valid {
		id := instrumentID.Int64
		t.InstrumentID = &id
	}
	if importID.Valid {
		id := importID.Int64
		t.ImportID = &id
	}

	// note is nullable in the schema; the API only ever writes '', but rows
	// from an import or a hand-run SQL fix can be NULL. Read those as empty
	// instead of failing the whole portfolio's list with a scan error.
	t.Note = note.String

	// A NULL quantity or price (cash rows) stays the zero decimal.
	t.Quantity, t.Price = quantity.Decimal, price.Decimal
	if symbol.Valid {
		t.InstrumentSymbol, t.InstrumentName, t.InstrumentMarket = &symbol.String, &name.String, &market.String
	}
	return t, nil
}
