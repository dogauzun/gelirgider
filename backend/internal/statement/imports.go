// Broker statement imports: one row per imported file, and the
// transactions it created. Only the statement's period, its account
// currency and the file's SHA-256 are stored, never the personal fields
// printed on it (name, TCKN, address, IBAN). Every query is scoped by
// user_id.

package statement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// ErrNotFound is returned when no import matches the lookup for the given
// user, including when the id belongs to another user.
var ErrNotFound = errors.New("import not found")

// ErrDuplicate is returned when the same user has already imported the same
// file, into this portfolio or another one.
var ErrDuplicate = errors.New("import already exists")

// ErrSamePeriod is returned when the user already has an import of the same
// statement: same broker, portfolio, account currency and exact period, from
// a different file. Midas regenerates the PDF on every download, so a
// re-downloaded month has a new hash and only this check catches it. A CSV
// is never the same period: it prints none, so a re-export of an edited
// portfolio spans the same dates, and its rows are checked one by one.
var ErrSamePeriod = errors.New("statement period already imported")

// Import is one imported statement file.
type Import struct {
	ID          int64
	UserID      int64
	PortfolioID int64
	Broker      string
	PeriodStart string // YYYY-MM-DD, Europe/Istanbul
	PeriodEnd   string // YYYY-MM-DD, Europe/Istanbul
	FileSHA256  string
	CreatedAt   string
	// Currency is the statement's account currency, "" when unknown: an
	// import recorded before it was stored, or a statement that printed none.
	Currency string

	// TransactionCount is only populated by List; it is 0 elsewhere.
	TransactionCount int
}

const importColumns = `id, user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, COALESCE(currency, '')`

// ImportRepo persists statement imports.
type ImportRepo struct {
	db           *sql.DB
	transactions *transaction.Repo
}

// NewImportRepo returns an ImportRepo backed by db.
func NewImportRepo(db *sql.DB) *ImportRepo {
	return &ImportRepo{db: db, transactions: transaction.NewRepo(db)}
}

// querier is satisfied by both *sql.DB and *sql.Tx, so an insert and the
// checks before it can run on their own or as part of a caller's
// transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewImport is the row Commit records: one statement file of Broker,
// imported by UserID into PortfolioID. Currency is the statement's account
// currency, "" when unknown; the period dates are YYYY-MM-DD, Europe/Istanbul.
type NewImport struct {
	UserID      int64
	PortfolioID int64
	Broker      string
	Currency    string
	PeriodStart string
	PeriodEnd   string
	SHA256      string
}

// Commit records the import n together with its transactions ts, the
// user's picks aliases and the statement's learned names, each a symbol and
// its instrument id, in one database transaction: either all of it lands or
// none of it (ADR 0006). Each t gets the new import's id; its portfolio and
// note are the caller's. Before anything is written it returns ErrSamePeriod,
// together with the existing import, when this user has already imported the
// same statement into this portfolio, and ErrDuplicate, together with the
// existing import when it is known, when they have already imported the same
// file into any of their portfolios. A name with instrument 0 forgets a
// learned name (see LearnNameTx). Learned names beyond maxLearnedNames per
// user and broker are evicted, oldest first, in the same transaction.
func (r *ImportRepo) Commit(ctx context.Context, n NewImport, ts []transaction.Transaction, aliases, names map[string]int64) (Import, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Import{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	imp, err := create(ctx, tx, n)
	if err != nil {
		return imp, err
	}
	for _, t := range ts {
		t.ImportID = &imp.ID
		if _, err := r.transactions.CreateTx(ctx, tx, n.UserID, t); err != nil {
			return Import{}, err
		}
	}
	for symbol, id := range aliases {
		if err := upsertAlias(ctx, tx, ImportAlias{UserID: n.UserID, Broker: n.Broker, Symbol: symbol, InstrumentID: id}); err != nil {
			return Import{}, err
		}
	}
	for name, id := range names {
		if err := r.LearnNameTx(ctx, tx, ImportAlias{UserID: n.UserID, Broker: n.Broker, Symbol: name, InstrumentID: id, PeriodEnd: n.PeriodEnd}); err != nil {
			return Import{}, err
		}
	}
	// A bound on a growing table must never turn a good statement into a
	// failed import, so a failed eviction is logged and the commit goes on.
	if len(names) > 0 {
		if err := evictLearnedNames(ctx, tx, n.UserID, n.Broker); err != nil {
			slog.Default().Warn("evict learned names failed", "user_id", n.UserID, "broker", n.Broker, "error", err.Error())
		}
	}
	if err := tx.Commit(); err != nil {
		return Import{}, fmt.Errorf("commit transaction: %w", err)
	}
	return imp, nil
}

// maxLearnedNames is how many learned names (import_aliases rows with
// learned = 1) one user keeps per broker. A commit that goes over it evicts
// the oldest by created_at; picks are never counted or evicted.
const maxLearnedNames = 10000

// evictLearnedNames deletes, inside tx, the learned names of userID and
// broker beyond the newest maxLearnedNames, ordered by created_at and then by
// id.
func evictLearnedNames(ctx context.Context, tx *sql.Tx, userID int64, broker string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM import_aliases
		 WHERE user_id = ? AND broker = ? AND learned = 1
		   AND id NOT IN (SELECT id FROM import_aliases WHERE user_id = ? AND broker = ? AND learned = 1
		                  ORDER BY created_at DESC, id DESC LIMIT ?)`,
		userID, broker, userID, broker, maxLearnedNames,
	); err != nil {
		return fmt.Errorf("evict learned names: %w", err)
	}
	return nil
}

func create(ctx context.Context, q querier, n NewImport) (Import, error) {
	// No unique constraint can say this: an unknown currency matches any,
	// and a database already holding such a pair must keep opening.
	existing, err := overlapping(ctx, q, n)
	if err != nil {
		return Import{}, err
	}
	for _, imp := range existing {
		if imp.samePeriod(n.Currency, n.PeriodStart, n.PeriodEnd) {
			return imp, ErrSamePeriod
		}
	}
	// The unique index is per portfolio; the file of one account is refused
	// in every other portfolio of the user too.
	switch imp, err := bySHA256(ctx, q, n.UserID, n.SHA256); {
	case err == nil:
		return imp, ErrDuplicate
	case !errors.Is(err, ErrNotFound):
		return Import{}, err
	}

	imp := Import{
		UserID:      n.UserID,
		PortfolioID: n.PortfolioID,
		Broker:      n.Broker,
		PeriodStart: n.PeriodStart,
		PeriodEnd:   n.PeriodEnd,
		FileSHA256:  n.SHA256,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Currency:    n.Currency,
	}
	res, err := q.ExecContext(ctx,
		`INSERT INTO statement_imports (user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at, currency)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		imp.UserID, imp.PortfolioID, imp.Broker, imp.PeriodStart, imp.PeriodEnd, imp.FileSHA256, imp.CreatedAt,
		sql.NullString{String: n.Currency, Valid: n.Currency != ""},
	)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return Import{}, ErrDuplicate
		}
		// The portfolio, or its user, was deleted since the caller's
		// ownership check.
		if db.IsForeignKeyViolation(err) {
			return Import{}, fmt.Errorf("insert import: %w", portfolio.ErrNotFound)
		}
		return Import{}, fmt.Errorf("insert import: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Import{}, fmt.Errorf("get inserted id: %w", err)
	}
	imp.ID = id
	return imp, nil
}

// samePeriod reports whether imp is the same statement as one of the given
// account currency and period: exactly the same dates, of the same account.
// Broker and portfolio are the caller's to match. A CSV is never the same
// statement, only overlapping rows are (flagDuplicates).
func (imp Import) samePeriod(currency, periodStart, periodEnd string) bool {
	return imp.Broker != BrokerCSV && imp.PeriodStart == periodStart && imp.PeriodEnd == periodEnd && imp.sameAccount(currency)
}

// sameAccount reports whether imp may be of the account with the given
// currency: the same currency, or either side's unknown. TRY and USD
// statements are two accounts, so a different known currency is another
// account.
func (imp Import) sameAccount(currency string) bool {
	return imp.Currency == "" || currency == "" || imp.Currency == currency
}

// BySHA256 returns the user's import of this file into any of their
// portfolios, the earliest when an older database holds it in several, or
// ErrNotFound. A statement file is one broker account, so the same file in a
// second portfolio is always a mistake; the parse endpoint answers
// "already imported" with it before parsing anything.
func (r *ImportRepo) BySHA256(ctx context.Context, userID int64, sha256 string) (Import, error) {
	return bySHA256(ctx, r.db, userID, sha256)
}

func bySHA256(ctx context.Context, q querier, userID int64, sha256 string) (Import, error) {
	var imp Import
	err := q.QueryRowContext(ctx,
		"SELECT "+importColumns+" FROM statement_imports WHERE user_id = ? AND file_sha256 = ? ORDER BY id LIMIT 1",
		userID, sha256,
	).Scan(&imp.ID, &imp.UserID, &imp.PortfolioID, &imp.Broker, &imp.PeriodStart, &imp.PeriodEnd, &imp.FileSHA256, &imp.CreatedAt, &imp.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return Import{}, ErrNotFound
	}
	if err != nil {
		return Import{}, fmt.Errorf("query import by hash: %w", err)
	}
	return imp, nil
}

// List returns every import of userID, newest first, each with the number of
// transactions still attached to it.
func (r *ImportRepo) List(ctx context.Context, userID int64) ([]Import, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT s.id, s.user_id, s.portfolio_id, s.broker, s.period_start, s.period_end, s.file_sha256, s.created_at,
		        COALESCE(s.currency, ''), COUNT(t.id)
		 FROM statement_imports s
		 LEFT JOIN transactions t ON t.import_id = s.id AND t.user_id = s.user_id
		 WHERE s.user_id = ?
		 GROUP BY s.id, s.user_id, s.portfolio_id, s.broker, s.period_start, s.period_end, s.file_sha256, s.created_at, s.currency
		 ORDER BY s.created_at DESC, s.id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer rows.Close()

	out := []Import{}
	for rows.Next() {
		var imp Import
		if err := rows.Scan(&imp.ID, &imp.UserID, &imp.PortfolioID, &imp.Broker, &imp.PeriodStart, &imp.PeriodEnd, &imp.FileSHA256, &imp.CreatedAt, &imp.Currency, &imp.TransactionCount); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		out = append(out, imp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate imports: %w", err)
	}
	return out, nil
}

// Overlapping returns the user's imports of the same broker into the same
// portfolio whose period intersects [periodStart, periodEnd], touching ends
// included. The parse endpoint turns each one into a warning about importing
// the same trades twice, and create refuses the one that is the same
// statement.
func (r *ImportRepo) Overlapping(ctx context.Context, userID, portfolioID int64, broker, periodStart, periodEnd string) ([]Import, error) {
	return overlapping(ctx, r.db, NewImport{UserID: userID, PortfolioID: portfolioID, Broker: broker, PeriodStart: periodStart, PeriodEnd: periodEnd})
}

// overlapping is Overlapping on q for n's user, portfolio, broker and
// period; n's currency and hash are not looked at.
func overlapping(ctx context.Context, q querier, n NewImport) ([]Import, error) {
	rows, err := q.QueryContext(ctx,
		"SELECT "+importColumns+
			` FROM statement_imports
		 WHERE user_id = ? AND portfolio_id = ? AND broker = ?
		   AND period_start <= ? AND period_end >= ?
		 ORDER BY period_start, id`,
		n.UserID, n.PortfolioID, n.Broker, n.PeriodEnd, n.PeriodStart)
	if err != nil {
		return nil, fmt.Errorf("query overlapping imports: %w", err)
	}
	defer rows.Close()

	out := []Import{}
	for rows.Next() {
		var imp Import
		if err := rows.Scan(&imp.ID, &imp.UserID, &imp.PortfolioID, &imp.Broker, &imp.PeriodStart, &imp.PeriodEnd, &imp.FileSHA256, &imp.CreatedAt, &imp.Currency); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		out = append(out, imp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overlapping imports: %w", err)
	}
	return out, nil
}

// Delete removes the import with the given id and every transaction it
// created, or returns ErrNotFound if userID has no such import. Both
// statements run in one database transaction and are scoped by user_id, so a
// guessed id can never reach another user's rows and a half-deleted import
// cannot be left behind.
func (r *ImportRepo) Delete(ctx context.Context, userID, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM transactions WHERE import_id = ? AND user_id = ?", id, userID,
	); err != nil {
		return fmt.Errorf("delete import transactions: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		"DELETE FROM statement_imports WHERE id = ? AND user_id = ?", id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete import: %w", err)
	}
	if err := db.CheckAffected(res, ErrNotFound); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// Alias returns the instrument id userID picked for symbol in a statement of
// broker, or ErrNotFound when they picked none. A learned name is
// no pick (see LearnedNames).
func (r *ImportRepo) Alias(ctx context.Context, userID int64, broker, symbol string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		"SELECT instrument_id FROM import_aliases WHERE user_id = ? AND broker = ? AND symbol = ? AND learned = 0",
		userID, broker, symbol,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("query import alias: %w", err)
	}
	return id, nil
}

// ImportAlias is one import_aliases row: UserID's instrument for Symbol, a
// code or a fund name, in a statement of Broker. PeriodEnd is "" for a pick,
// otherwise the period end of the statement that printed a learned name.
type ImportAlias struct {
	UserID       int64
	Broker       string
	Symbol       string
	InstrumentID int64
	PeriodEnd    string
}

// LearnNameTx stores, inside tx, that the fund name a.Symbol a committed
// statement of a.Broker, whose period ends on a.PeriodEnd, printed next to a
// code is a.InstrumentID. It replaces an earlier learned name for the same key
// but never a pick, and only when periodEnd is the same as or later than that
// name's own, so statements committed out of period order leave the newest
// statement's name. instrumentID 0, a name the statement printed for
// two funds or for a code of no instrument, removes the learned name under the
// same rule, leaves a pick alone and records the forget in
// import_forgotten_names, so a statement of an older period does not learn
// the name again. A learned name of the same or a later period
// removes that record.
func (r *ImportRepo) LearnNameTx(ctx context.Context, tx *sql.Tx, a ImportAlias) error {
	userID, broker, name, periodEnd, instrumentID := a.UserID, a.Broker, a.Symbol, a.PeriodEnd, a.InstrumentID
	// A forget of the same or an older period gives way to this statement; one
	// of a later period stays and is the only row left.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM import_forgotten_names WHERE user_id = ? AND broker = ? AND symbol = ? AND period_end <= ?",
		userID, broker, name, periodEnd,
	); err != nil {
		return fmt.Errorf("delete forgotten name: %w", err)
	}
	if instrumentID == 0 {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM import_aliases WHERE user_id = ? AND broker = ? AND symbol = ? AND learned = 1 AND (period_end IS NULL OR period_end <= ?)",
			userID, broker, name, periodEnd,
		); err != nil {
			return fmt.Errorf("delete learned name: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO import_forgotten_names (user_id, broker, symbol, period_end)
			 SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM import_forgotten_names WHERE user_id = ? AND broker = ? AND symbol = ?)`,
			userID, broker, name, periodEnd, userID, broker, name,
		); err != nil {
			return fmt.Errorf("insert forgotten name: %w", err)
		}
		return nil
	}
	var newer int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM import_forgotten_names WHERE user_id = ? AND broker = ? AND symbol = ?",
		userID, broker, name,
	).Scan(&newer); err != nil {
		return fmt.Errorf("query forgotten name: %w", err)
	}
	if newer > 0 {
		return nil
	}
	return upsertAlias(ctx, tx, a)
}

// LearnedNames returns the fund names userID's committed statements of
// broker printed, each with its instrument id.
func (r *ImportRepo) LearnedNames(ctx context.Context, userID int64, broker string) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT symbol, instrument_id FROM import_aliases WHERE user_id = ? AND broker = ? AND learned = 1",
		userID, broker)
	if err != nil {
		return nil, fmt.Errorf("query learned names: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var id int64
		if err := rows.Scan(&name, &id); err != nil {
			return nil, fmt.Errorf("scan learned name: %w", err)
		}
		out[name] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate learned names: %w", err)
	}
	return out, nil
}

// upsertAlias writes one import_aliases row: a pick when a.PeriodEnd is
// empty, otherwise a name learned from a statement whose period ends on it. A
// pick replaces any row of its key; a learned name replaces only a learned row
// of the same or an older period (NULL: older than any), so a pick of the
// same key and a newer statement's name stay.
func upsertAlias(ctx context.Context, tx *sql.Tx, a ImportAlias) error {
	userID, broker, symbol, instrumentID, periodEnd := a.UserID, a.Broker, a.Symbol, a.InstrumentID, a.PeriodEnd
	now := time.Now().UTC().Format(time.RFC3339)
	flag, only := 0, ""
	var period any // NULL for a pick
	if periodEnd != "" {
		flag, period, only = 1, periodEnd, " AND learned = 1 AND (period_end IS NULL OR period_end <= ?)"
	}
	args := []any{instrumentID, now, flag, period, userID, broker, symbol}
	if periodEnd != "" {
		args = append(args, periodEnd)
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE import_aliases SET instrument_id = ?, created_at = ?, learned = ?, period_end = ? WHERE user_id = ? AND broker = ? AND symbol = ?"+only,
		args...,
	)
	if err != nil {
		return fmt.Errorf("update import alias: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}
	// A pick of the same key, or a newer statement's name, is a row the
	// learned update left alone.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at, learned, period_end)
		 SELECT ?, ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM import_aliases WHERE user_id = ? AND broker = ? AND symbol = ?)`,
		userID, broker, symbol, instrumentID, now, flag, period, userID, broker, symbol,
	); err != nil {
		return fmt.Errorf("insert import alias: %w", err)
	}
	return nil
}
