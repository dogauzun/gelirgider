package transaction

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/textcheck"
	"github.com/shopspring/decimal"
)

// OnDemandFetchTimeout bounds how long create/update waits on the on-demand
// price fetch callback. The transaction is already saved by the
// time this runs, so a slow or unreachable price source must never hold the
// response open for long; any error, including this timeout, is logged and
// swallowed. A request that fetches for several instruments, such as an
// import, waits this long in all, not this long per instrument.
const OnDemandFetchTimeout = 5 * time.Second

// maxNoteLen caps the note field so a very long, spaceless note can't stretch
// the transactions table off screen.
const maxNoteLen = 500

// OnDemandFetch fetches and stores prices and FX rates for in,
// best-effort. It is called after every create or update of a transaction
// that needs either, with the transaction's date (YYYY-MM-DD), so the
// summary opened next already has a price and the history chart
// has one per trading day back to date instead of waiting for
// the next scheduler run. in is the zero Instrument for a cash transaction,
// which the callback treats as "no price source, FX only".
// The callback decides what, if anything, still needs fetching.
type OnDemandFetch func(ctx context.Context, in instrument.Instrument, date string)

// RegisterRoutes wires the transaction endpoints into mux. mux must be
// mounted behind middleware that calls auth.RequireUser, so that
// auth.UserID is available in the request context. portfolios is used to
// check portfolio ownership before creating a transaction against it;
// instruments is used to reject unknown instrument_id values with a 400
// instead of letting the foreign key fail with a 500. onDemandFetch backs
// the on-demand price and FX fetch: after a create or update that
// references an instrument, or that is cash in a non-TRY currency, it is
// called with a bounded timeout. onDemandFetch may be nil, which disables
// the feature (e.g. in tests that don't need it).
func RegisterRoutes(mux *http.ServeMux, repo *Repo, portfolios *portfolio.Repo, instruments *instrument.Repo, onDemandFetch OnDemandFetch) {
	h := &handler{repo: repo, portfolios: portfolios, instruments: instruments, onDemandFetch: onDemandFetch}
	// The list is paged and newest first; see the list handler for ?limit=,
	// ?before=, ?type= and ?instrument_id=.
	mux.HandleFunc("GET /api/portfolios/{id}/transactions", h.list)
	mux.HandleFunc("GET /api/portfolios/{id}/transactions.csv", h.exportCSV)
	mux.HandleFunc("GET /api/export.zip", h.exportZip)
	mux.HandleFunc("POST /api/portfolios/{id}/transactions", h.create)
	mux.HandleFunc("PUT /api/transactions/{id}", h.update)
	mux.HandleFunc("DELETE /api/transactions/{id}", h.delete)
}

type handler struct {
	repo          *Repo
	portfolios    *portfolio.Repo
	instruments   *instrument.Repo
	onDemandFetch OnDemandFetch
}

// ErrLookup marks a failure that came from the database rather than from the
// request. FromRequest's callers answer 500 and log it, and treat every
// other error it returns as a 400 carrying its own "<field>: <message>"
// text.
var ErrLookup = errors.New("lookup failed")

// Validate runs req through FromRequest's rules without looking up its
// instrument, for a statement import row whose symbol is not resolved yet.
// req carries a placeholder InstrumentID in place of the real
// one, which only its presence is checked for.
func Validate(req Request) error {
	_, err := fromRequest(req)
	return err
}

// FromRequest validates req against the rules for its type, resolves the
// instrument it references, and returns the Transaction to store together
// with that instrument -- the zero Instrument when the request has none,
// which the on-demand price fetch reads as "cash, no price source".
// PortfolioID and ImportID are left unset; the caller fills them in.
//
// A request currency that contradicts the instrument's own is rejected
// rather than silently overwritten, so a user who records a dividend in USD
// hears that the instrument is TRY; otherwise the instrument's
// currency wins, since trades take their currency from the instrument.
//
// The statement import commit runs every one of its rows through
// this, so an imported transaction is validated exactly like a hand-entered
// one.
func FromRequest(ctx context.Context, instruments *instrument.Repo, req Request) (Transaction, instrument.Instrument, error) {
	return FromRequestWith(req, func(id int64) (instrument.Instrument, error) { return instruments.Get(ctx, id) })
}

// FromRequestWith is FromRequest with get in place of the instrument
// lookup, for a caller that already holds the instruments its requests
// name: the statement import validates thousands of rows of a few
// instruments. get returns instrument.ErrNotFound for an id it
// does not know.
func FromRequestWith(req Request, get func(id int64) (instrument.Instrument, error)) (Transaction, instrument.Instrument, error) {
	t, err := fromRequest(req)
	if err != nil {
		return Transaction{}, instrument.Instrument{}, err
	}
	if t.InstrumentID == nil {
		return t, instrument.Instrument{}, nil
	}
	in, err := get(*t.InstrumentID)
	if errors.Is(err, instrument.ErrNotFound) {
		return Transaction{}, instrument.Instrument{}, fmt.Errorf("instrument_id: unknown instrument")
	}
	if err != nil {
		return Transaction{}, instrument.Instrument{}, fmt.Errorf("%w: get instrument %d: %v", ErrLookup, *t.InstrumentID, err)
	}
	if req.Currency != nil && *req.Currency != "" && *req.Currency != in.Currency {
		return Transaction{}, instrument.Instrument{}, fmt.Errorf("currency: must match instrument currency (%s)", in.Currency)
	}
	t.Currency = in.Currency
	return t, in, nil
}

// EnsurePrice runs the on-demand price and FX fetch for t after it is
// saved, passing in (the zero Instrument when t has none). A cash
// transaction in a foreign currency carries no instrument but still needs
// TCMB rates back to its date for the history chart, so it goes through
// too; only a TRY cash transaction, which needs neither, is skipped.
// fetch may be nil, which disables the feature. Best-effort and
// synchronous: any error, including the bounded timeout, is logged and
// ignored by the callback, since the transaction is already saved and the
// response must go out regardless.
func EnsurePrice(ctx context.Context, fetch OnDemandFetch, in instrument.Instrument, t Transaction) {
	if fetch == nil {
		return
	}
	if t.InstrumentID == nil && t.Currency == "TRY" {
		return
	}
	fetchCtx, cancel := context.WithTimeout(ctx, OnDemandFetchTimeout)
	defer cancel()
	fetch(fetchCtx, in, t.Date)
}

// Request is the writable shape for POST/PUT. PortfolioID is not
// included: it comes from the URL on create, and cannot be changed on
// update.
type Request struct {
	// InstrumentID is required for buy and sell, optional for dividend (a
	// dividend with none is interest income), and rejected for the rest.
	InstrumentID *int64  `json:"instrument_id"`
	Type         string  `json:"type"`
	Date         string  `json:"date"`
	Quantity     *string `json:"quantity"`
	Price        *string `json:"price"`
	Amount       *string `json:"amount"`
	Fee          *string `json:"fee"`
	Currency     *string `json:"currency"`
	Note         *string `json:"note"`
}

type transactionResponse struct {
	ID               int64   `json:"id"`
	PortfolioID      int64   `json:"portfolio_id"`
	InstrumentID     *int64  `json:"instrument_id"`
	InstrumentSymbol *string `json:"instrument_symbol"`
	InstrumentName   *string `json:"instrument_name"`
	Type             string  `json:"type"`
	Date             string  `json:"date"`
	Quantity         *string `json:"quantity"`
	Price            *string `json:"price"`
	Amount           string  `json:"amount"`
	Fee              string  `json:"fee"`
	Currency         string  `json:"currency"`
	Note             *string `json:"note"`
	CreatedAt        string  `json:"created_at"`
	// ImportID is null for a hand-entered transaction, set for one that came
	// from a broker statement import.
	ImportID *int64 `json:"import_id"`
}

func toResponse(t Transaction) transactionResponse {
	resp := transactionResponse{
		ID:               t.ID,
		PortfolioID:      t.PortfolioID,
		InstrumentID:     t.InstrumentID,
		InstrumentSymbol: t.InstrumentSymbol,
		InstrumentName:   t.InstrumentName,
		Type:             t.Type,
		Date:             t.Date,
		Amount:           t.Amount.String(),
		Fee:              t.Fee.String(),
		Currency:         t.Currency,
		CreatedAt:        t.CreatedAt,
		ImportID:         t.ImportID,
	}
	if hasQuantityPrice(t.Type) {
		q, p := t.Quantity.String(), t.Price.String()
		resp.Quantity, resp.Price = &q, &p
	}
	if t.Note != "" {
		note := t.Note
		resp.Note = &note
	}
	return resp
}

// fromRequest validates req against the rules for its type and returns the
// Transaction to store. PortfolioID is left unset; the caller fills it in.
func fromRequest(req Request) (Transaction, error) {
	if !validTypes[req.Type] {
		return Transaction{}, fmt.Errorf("type: must be one of buy, sell, dividend, deposit, withdrawal, fee")
	}
	if err := price.CheckDate(req.Date); err != nil {
		return Transaction{}, fmt.Errorf("date: %w", err)
	}

	var err error
	fee := decimal.Zero
	if req.Fee != nil && *req.Fee != "" {
		if fee, err = parseNonNegativeDecimal(req.Fee); err != nil {
			return Transaction{}, fmt.Errorf("fee: %w", err)
		}
	}

	t := Transaction{Type: req.Type, Date: req.Date, Fee: fee}
	if req.Note != nil {
		if t.Note, err = parseNote(*req.Note); err != nil {
			return Transaction{}, err
		}
	}

	switch req.Type {
	case TypeBuy, TypeSell:
		err = fillTrade(req, &t)
	default: // dividend, deposit, withdrawal, fee
		err = fillCash(req, &t)
	}
	if err != nil {
		return Transaction{}, err
	}
	if err := checkFee(req.Type, fee, t.Amount); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

// parseNote trims and validates a transaction note.
func parseNote(raw string) (string, error) {
	// Trim first so surrounding whitespace neither counts against the
	// limit nor gets stored as an empty-looking note.
	note := strings.TrimSpace(raw)
	if utf8.RuneCountInString(note) > maxNoteLen {
		return "", fmt.Errorf("note: must be at most %d characters", maxNoteLen)
	}
	// Same rule as portfolio names: the note is a
	// single line in the UI, so tabs and newlines go too. A non-empty
	// note with no visible rune (only combining marks) renders blank,
	// which is refused for names too; an empty note is fine.
	if textcheck.HasInvisible(note) || note != "" && !strings.ContainsFunc(note, textcheck.IsVisible) {
		return "", fmt.Errorf("note: must not contain control characters")
	}
	return note, nil
}

// fillCash validates the instrument, amount and currency of the dividend,
// deposit, withdrawal or fee req and sets them on t.
func fillCash(req Request, t *Transaction) error {
	// A dividend with no instrument is interest income, like Midas's
	// monthly nema: cash in that is not a contribution. It
	// takes its currency like a deposit; one with an instrument gets the
	// instrument's in FromRequest.
	if req.InstrumentID != nil && req.Type != TypeDividend {
		return fmt.Errorf("instrument_id: not allowed for %s", req.Type)
	}
	amount, err := parsePositiveDecimal(req.Amount)
	if err != nil {
		return fmt.Errorf("amount: %w", err)
	}
	t.Amount = amount
	if req.InstrumentID != nil {
		t.InstrumentID = req.InstrumentID
		return nil
	}

	currency := "TRY"
	if req.Currency != nil && *req.Currency != "" {
		currency = *req.Currency
	}
	if !IsValidCurrency(currency) {
		return fmt.Errorf("currency: must be one of %s", strings.Join(ValidCurrencies, ", "))
	}
	t.Currency = currency
	return nil
}

// checkFee rejects a fee that makes no sense for the transaction type.
func checkFee(typ string, fee, amount decimal.Decimal) error {
	// For a dividend and a deposit the fee comes out of the amount, so a
	// fee above the amount would book a negative one. A
	// withdrawal is capped alongside them: its fee adds to the cash that
	// leaves, and a fee larger than the withdrawal itself is a
	// typo, not a movement. Buy and sell are excluded: there the fee adds
	// to the cost, which may well exceed the amount.
	switch typ {
	case TypeDividend, TypeDeposit, TypeWithdrawal:
		if fee.GreaterThan(amount) {
			return fmt.Errorf("fee: must not exceed amount")
		}
	case TypeFee:
		// A fee row's amount is the fee; a second fee on top of it has no
		// meaning and would only double-charge cash.
		if !fee.IsZero() {
			return fmt.Errorf("fee: must be 0 for a fee transaction")
		}
	}
	return nil
}

// fillTrade validates the instrument, quantity, price and amount of the buy
// or sell req and sets them on t.
func fillTrade(req Request, t *Transaction) error {
	if req.InstrumentID == nil {
		return fmt.Errorf("instrument_id: required for %s", req.Type)
	}
	qty, err := parsePositiveDecimal(req.Quantity)
	if err != nil {
		return fmt.Errorf("quantity: %w", err)
	}
	// A buy at price 0 records units received for free: staking, airdrops,
	// gifted shares (ADR 0007). A sell still needs a positive
	// price.
	parsePrice := parsePositiveDecimal
	if req.Type == TypeBuy {
		parsePrice = parseNonNegativeDecimal
	}
	price, err := parsePrice(req.Price)
	if err != nil {
		return fmt.Errorf("price: %w", err)
	}
	t.InstrumentID = req.InstrumentID
	t.Quantity = qty
	t.Price = price
	t.Amount = qty.Mul(price)
	// A statement's trade amount is the cash that moved (İşlem Tutarı, ADR
	// 0006), which the broker rounds to the cent while quantity × average
	// price is not. An explicit amount wins; the manual form sends
	// none and gets quantity × price. An amount that is quantity × price
	// itself skips the digit limit: the product can carry more fraction
	// digits than the parser accepts, and the edit form sends a stored
	// trade's amount back unchanged. A priced trade needs a
	// positive amount, or it books a position at no cost. A buy at
	// price 0 needs amount 0, or free units would still move cash and carry
	// a cost.
	if req.Amount != nil && *req.Amount != "" && *req.Amount != t.Amount.String() {
		parseAmount := parseNonNegativeDecimal
		if price.IsPositive() {
			parseAmount = parsePositiveDecimal
		}
		if t.Amount, err = parseAmount(req.Amount); err != nil {
			return fmt.Errorf("amount: %w", err)
		}
		if price.IsZero() && !t.Amount.IsZero() {
			return fmt.Errorf("amount: must be 0 when price is 0")
		}
	}
	return nil
}

// Accepted decimal shape: plain digits with an optional fraction, no sign,
// no exponent. Unbounded input lets decimal.NewFromString accept exponents
// up to int32, which overflows (panics) on Mul and materializes giant
// strings on String(). 15 integer digits covers any realistic TRY amount;
// 9 fraction digits covers TEFAS (6) and Midas fractional shares (9).
const (
	maxIntDigits  = 15
	maxFracDigits = 9
)

var (
	DecimalPattern = regexp.MustCompile(fmt.Sprintf(`^\d{1,%d}(\.\d{1,%d})?$`, maxIntDigits, maxFracDigits))
	// digitsPattern is the same shape without the digit limits: a value
	// that matches it but not DecimalPattern is a valid number that is
	// only too long.
	digitsPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)
	// errTooManyDigits names the limit itself, so a long positive value is
	// not reported as "must be positive". The frontend maps it to
	// decimal.error.tooManyDigits.
	errTooManyDigits = fmt.Errorf("must have at most %d integer and %d fraction digits", maxIntDigits, maxFracDigits)
)

// parseDecimal parses a non-negative decimal within the accepted shape.
// minimum names the lower bound ("non-negative" or "positive") in the error
// for a value that is not a plain number.
func parseDecimal(s *string, minimum string) (decimal.Decimal, error) {
	if s == nil || *s == "" {
		return decimal.Decimal{}, fmt.Errorf("required")
	}
	if !DecimalPattern.MatchString(*s) {
		if digitsPattern.MatchString(*s) {
			return decimal.Decimal{}, errTooManyDigits
		}
		return decimal.Decimal{}, fmt.Errorf("must be a %s number with at most %d integer and %d fraction digits", minimum, maxIntDigits, maxFracDigits)
	}
	return decimal.NewFromString(*s)
}

func parseNonNegativeDecimal(s *string) (decimal.Decimal, error) {
	return parseDecimal(s, "non-negative")
}

func parsePositiveDecimal(s *string) (decimal.Decimal, error) {
	d, err := parseDecimal(s, "positive")
	if err == nil && !d.IsPositive() {
		err = fmt.Errorf("must be a positive number with at most %d integer and %d fraction digits", maxIntDigits, maxFracDigits)
	}
	return d, err
}

// Paging for the transaction list. A client that sends no limit
// gets defaultListLimit rows; a larger one is clamped to maxListLimit rather
// than rejected, matching the price history endpoint.
const (
	defaultListLimit = 100
	maxListLimit     = 500
)

// parseLimit reads the ?limit= query value of the transaction list.
func parseLimit(q string) (int, error) {
	if q == "" {
		return defaultListLimit, nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	return min(n, maxListLimit), nil
}

// parseCursor reads the ?before= query value of the transaction list,
// "<YYYY-MM-DD>,<id>" as handed out in a previous page's X-Next-Cursor. Both
// halves are needed because the page order is (date, id); see Cursor. An
// empty value is the zero Cursor: start at the newest row.
func parseCursor(q string) (Cursor, error) {
	if q == "" {
		return Cursor{}, nil
	}
	malformed := fmt.Errorf("before: must be <YYYY-MM-DD>,<id>")
	date, rawID, ok := strings.Cut(q, ",")
	if !ok {
		return Cursor{}, malformed
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return Cursor{}, malformed
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return Cursor{}, malformed
	}
	return Cursor{Date: date, ID: id}, nil
}

// parseFilter reads the filter query values the transaction list and the
// CSV export share: ?type=, ?instrument_id=, ?import_id=, ?from= and ?to=.
func parseFilter(q url.Values) (Filter, error) {
	var f Filter
	var err error
	if f.Types, err = parseTypes(q.Get("type")); err != nil {
		return Filter{}, err
	}
	if f.InstrumentID, err = parseInstrumentID(q.Get("instrument_id")); err != nil {
		return Filter{}, err
	}
	if f.ImportID, err = parseImportID(q.Get("import_id")); err != nil {
		return Filter{}, err
	}
	if f.From, f.To, err = parseDateRange(q); err != nil {
		return Filter{}, err
	}
	return f, nil
}

// parseTypes reads the ?type= query value of the transaction list, a
// comma-separated list of transaction types ("deposit,withdrawal,fee"). An
// absent or empty value means no filter; any unknown or empty entry inside
// the list is rejected rather than ignored, so a typo doesn't silently widen
// the result.
func parseTypes(q string) ([]string, error) {
	if q == "" {
		return nil, nil
	}
	types := strings.Split(q, ",")
	for _, t := range types {
		if !validTypes[t] {
			return nil, fmt.Errorf("type: invalid")
		}
	}
	return types, nil
}

// parseInstrumentID reads the ?instrument_id= query value of the transaction
// list. An absent or empty value is 0, no filter.
func parseInstrumentID(q string) (int64, error) {
	if q == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(q, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("instrument_id: must be a positive integer")
	}
	return id, nil
}

// parseImportID reads the ?import_id= query value of the transaction list
// and the CSV export: a positive import id, or "none" for the rows entered
// by hand, returned as ImportNone. An absent or empty value is 0, no filter.
func parseImportID(q string) (int64, error) {
	if q == "none" {
		return ImportNone, nil
	}
	id, err := parseInstrumentID(q)
	if err != nil {
		return 0, fmt.Errorf("import_id: must be a positive integer or none")
	}
	return id, nil
}

// parseDateRange reads the optional ?from= and ?to= query values of the
// transaction list and the CSV export, an inclusive YYYY-MM-DD range.
// An absent value is "", no bound.
func parseDateRange(q url.Values) (from, to string, err error) {
	from, to = q.Get("from"), q.Get("to")
	if from != "" {
		if _, err := time.Parse(time.DateOnly, from); err != nil {
			return "", "", fmt.Errorf("from must be YYYY-MM-DD")
		}
	}
	if to != "" {
		if _, err := time.Parse(time.DateOnly, to); err != nil {
			return "", "", fmt.Errorf("to must be YYYY-MM-DD")
		}
	}
	if from != "" && to != "" && from > to {
		return "", "", fmt.Errorf("from must not be after to")
	}
	return from, to, nil
}

// writeRequestError answers a FromRequest failure: a 400 carrying the
// error's own "<field>: <message>" text, or the logged generic 500 when the
// lookup itself failed.
func writeRequestError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrLookup) {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteError(w, http.StatusBadRequest, err.Error())
}

// list returns one page of the portfolio's transactions, newest first, as a
// plain array. ?limit= (default 100, max 500) sizes the page and ?before=
// continues from a previous page's X-Next-Cursor header; a client that sends
// neither still gets an array, just the first 100 rows. The header
// is absent on the last page. ?type= narrows the page to a comma-separated
// list of transaction types, ?instrument_id= to one
// instrument's rows, ?import_id= to one import's rows or, as
// "none", the hand-entered ones, and ?from= and ?to= to an
// inclusive date range.
//
//	@Summary	List a portfolio's transactions
//	@Tags		transactions
//	@Produce	json
//	@Param		id				path		int		true	"portfolio id"
//	@Param		limit			query		int		false	"page size, default 100, clamped to 500"
//	@Param		before			query		string	false	"cursor from a previous page's X-Next-Cursor, <YYYY-MM-DD>,<id>"
//	@Param		type			query		string	false	"comma-separated types: buy, sell, dividend, deposit, withdrawal, fee"
//	@Param		instrument_id	query		int		false	"only this instrument's transactions"
//	@Param		import_id		query		string	false	"only this import's transactions (an id from GET /imports), or none for those entered by hand; another user's import id matches nothing"
//	@Param		from			query		string	false	"only transactions on or after this date, YYYY-MM-DD"
//	@Param		to				query		string	false	"only transactions on or before this date, YYYY-MM-DD"
//	@Success	200				{array}		transaction.transactionResponse	"newest first"
//	@Header		200				{string}	X-Next-Cursor					"cursor for the next page as ?before=; absent on the last page"
//	@Failure	400				{object}	object{error=string}			"limit must be a positive integer; before: must be <YYYY-MM-DD>,<id>; type: invalid; instrument_id: must be a positive integer; import_id: must be a positive integer or none; from must be YYYY-MM-DD; to must be YYYY-MM-DD; from must not be after to"
//	@Failure	404				{object}	object{error=string}			"portfolio not found"
//	@Failure	500				{object}	object{error=string}			"internal error"
//	@Router		/portfolios/{id}/transactions [get]
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}

	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	before, err := parseCursor(r.URL.Query().Get("before"))
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := parseFilter(r.URL.Query())
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// One row past the page says whether another page exists, so a cursor is
	// only handed out when following it actually returns something.
	txs, err := h.repo.ListPage(r.Context(), userID, portfolioID, limit+1, before, filter)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if len(txs) > limit {
		last := txs[limit-1]
		w.Header().Set("X-Next-Cursor", fmt.Sprintf("%s,%d", last.Date, last.ID))
		txs = txs[:limit]
	}

	out := make([]transactionResponse, len(txs))
	for i, t := range txs {
		out[i] = toResponse(t)
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// csvHeader is the export's column set. It is frozen: a
// spreadsheet or script built on it must keep working, so a new column goes
// at the end, never in between.
var csvHeader = []string{"date", "type", "symbol", "quantity", "price", "amount", "fee", "currency", "note", "market"}

// exportCSV writes every transaction of one of the caller's portfolios as a
// CSV download: comma separated, UTF-8 without BOM, dot decimals
// that the CSV import reads back (csvNumber),
// raw type codes, oldest first, each row on its stored date. Split rows are
// left out: they are derived from market data, not entered. A symbol, note or market starting with =,
// +, - or @ gets a leading ' so a spreadsheet does not run it as a formula. ?type=,
// ?instrument_id=, ?import_id=, ?from= and ?to= keep only the rows the list
// shows for the same values.
//
//	@Summary	Export a portfolio's transactions as CSV
//	@Tags		transactions
//	@Produce	text/csv
//	@Param		id				path		int		true	"portfolio id"
//	@Param		type			query		string	false	"comma-separated types: buy, sell, dividend, deposit, withdrawal, fee"
//	@Param		instrument_id	query		int		false	"only this instrument's transactions"
//	@Param		import_id		query		string	false	"only this import's transactions (an id from GET /imports), or none for those entered by hand; another user's import id matches nothing"
//	@Param		from			query		string	false	"only transactions on or after this date, YYYY-MM-DD"
//	@Param		to				query		string	false	"only transactions on or before this date, YYYY-MM-DD"
//	@Success	200	{string}	string					"columns date,type,symbol,quantity,price,amount,fee,currency,note,market; quantity and price empty outside buy and sell; market empty without an instrument"
//	@Header		200	{string}	Content-Disposition		"attachment; filename=portfolio-<id>-transactions.csv"
//	@Failure	400	{object}	object{error=string}	"type: invalid; instrument_id: must be a positive integer; import_id: must be a positive integer or none; from must be YYYY-MM-DD; to must be YYYY-MM-DD; from must not be after to"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/transactions.csv [get]
func (h *handler) exportCSV(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	filter, err := parseFilter(r.URL.Query())
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	// ListStored, not List: List moves a bonus buy to its split's ex-date,
	// so the export would write a date the list does not show and
	// a re-import would not see the row as a duplicate.
	txs, err := h.repo.ListStored(r.Context(), userID, portfolioID, filter)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=portfolio-%d-transactions.csv", portfolioID))
	// Headers are gone by now, so a write error can only be logged.
	if err := writeCSV(w, txs); err != nil {
		slog.ErrorContext(r.Context(), "write transactions csv", "err", err)
	}
}

// writeCSV writes txs, the stored rows of one portfolio, as the export's CSV.
func writeCSV(w io.Writer, txs []Transaction) error {
	cw := csv.NewWriter(w)
	cw.Write(csvHeader)
	for _, t := range txs {
		var symbol, market, quantity, price string
		if t.InstrumentSymbol != nil {
			symbol, market = *t.InstrumentSymbol, *t.InstrumentMarket
		}
		if hasQuantityPrice(t.Type) {
			quantity, price = csvNumber(t.Quantity), csvNumber(t.Price)
		}
		cw.Write([]string{t.Date, t.Type, csvSafe(symbol), quantity, price, csvNumber(t.Amount), csvNumber(t.Fee), t.Currency, csvSafe(t.Note), csvSafe(market)})
	}
	cw.Flush()
	return cw.Error()
}

// exportZip streams one zip of the caller's portfolios, an entry each named
// <portfolio name>-transactions.csv, holding what exportCSV writes with no
// filter. A caller with no portfolio gets an empty zip.
//
//	@Summary	Export every portfolio's transactions as a zip of CSV files
//	@Tags		transactions
//	@Produce	application/zip
//	@Success	200	{file}		binary					"one <portfolio name>-transactions.csv per portfolio, as GET /portfolios/{id}/transactions.csv writes it; the id (then id-n) stands in for a name that is empty once cleaned for a file name or that another entry already has, compared ignoring case"
//	@Header		200	{string}	Content-Disposition		"attachment; filename=gelirgider-export-<yyyyMMddTHHmm>.csv.zip"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/export.zip [get]
func (h *handler) exportZip(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	ps, err := h.portfolios.List(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=gelirgider-export-"+time.Now().UTC().Format("20060102T1504")+".csv.zip")
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	for _, p := range ps {
		// Keyed lowercase: "Main" and "main" are one file on Windows and macOS.
		name := fileSafe(p.Name)
		if name == "" || used[strings.ToLower(name)] {
			name = strconv.FormatInt(p.ID, 10)
		}
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%d-%d", p.ID, n)
		}
		used[strings.ToLower(name)] = true
		// Headers are gone by now, so a failure can only be logged and the
		// connection dropped, which the browser shows as a failed download.
		txs, err := h.repo.ListStored(r.Context(), userID, p.ID, Filter{})
		if err == nil {
			var f io.Writer
			if f, err = zw.Create(name + "-transactions.csv"); err == nil {
				err = writeCSV(f, txs)
			}
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "write export zip", "err", err)
			panic(http.ErrAbortHandler)
		}
	}
	if err := zw.Close(); err != nil {
		slog.ErrorContext(r.Context(), "write export zip", "err", err)
	}
}

// fileSafe turns a portfolio name into a file name: path separators, the
// characters Windows refuses, and control characters become _, and a
// leading or trailing space or dot goes.
func fileSafe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	return strings.Trim(s, " .")
}

// csvThousandsShape is a written number the CSV import refuses as an
// ambiguous thousands separator, like "7.407" (statement/csv).
var csvThousandsShape = regexp.MustCompile(`^[1-9]\d{0,2}\.\d{3}$`)

// csvNumber writes d so the CSV import reads it back: a value in
// thousands shape gets one trailing zero, "7.4070", which is the same number.
func csvNumber(d decimal.Decimal) string {
	s := d.String()
	if csvThousandsShape.MatchString(s) {
		return s + "0"
	}
	return s
}

// csvSafe prefixes a text cell starting with =, +, - or @ with ' so a
// spreadsheet does not run it as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}

// create adds a transaction to one of the caller's portfolios.
//
//	@Summary	Create a transaction
//	@Tags		transactions
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"portfolio id"
//	@Param		body	body		transaction.Request	true	"decimals are strings; quantity and price for buy and sell, amount for the rest; instrument_id required for buy and sell, optional for dividend (none is interest income, currency as for a deposit), not allowed for the rest"
//	@Success	201		{object}	transaction.transactionResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; type: must be one of buy, sell, dividend, deposit, withdrawal, fee; date: must be YYYY-MM-DD; date: must not be in the future; date: must be on or after 1990-01-01; instrument_id: required for <type>; instrument_id: not allowed for <type>; instrument_id: unknown instrument; quantity|price|amount|fee: required; quantity|price|amount|fee: must have at most 15 integer and 9 fraction digits; quantity|price|amount|fee: must be a positive|non-negative number with at most 15 integer and 9 fraction digits; amount: must be 0 when price is 0; fee: must not exceed amount; fee: must be 0 for a fee transaction; currency: must be one of TRY, USD, EUR; currency: must match instrument currency (<currency>); note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/transactions [post]
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}

	var req Request
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	t, in, err := FromRequest(r.Context(), h.instruments, req)
	if err != nil {
		writeRequestError(w, r, err)
		return
	}
	t.PortfolioID = portfolioID

	created, err := h.repo.Create(r.Context(), userID, t)
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	EnsurePrice(r.Context(), h.onDemandFetch, in, t)
	auth.WriteJSON(w, http.StatusCreated, toResponse(created))
}

// update replaces one of the caller's transactions. Its portfolio stays.
//
//	@Summary	Update a transaction
//	@Tags		transactions
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"transaction id"
//	@Param		body	body		transaction.Request	true	"the whole transaction, as on create; instrument_id is optional for dividend (none is interest income)"
//	@Success	200		{object}	transaction.transactionResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; type: must be one of buy, sell, dividend, deposit, withdrawal, fee; date: must be YYYY-MM-DD; date: must not be in the future; date: must be on or after 1990-01-01; instrument_id: required for <type>; instrument_id: not allowed for <type>; instrument_id: unknown instrument; quantity|price|amount|fee: required; quantity|price|amount|fee: must have at most 15 integer and 9 fraction digits; quantity|price|amount|fee: must be a positive|non-negative number with at most 15 integer and 9 fraction digits; amount: must be 0 when price is 0; fee: must not exceed amount; fee: must be 0 for a fee transaction; currency: must be one of TRY, USD, EUR; currency: must match instrument currency (<currency>); note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"transaction not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/transactions/{id} [put]
func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "transaction not found")
		return
	}

	var req Request
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	t, in, err := FromRequest(r.Context(), h.instruments, req)
	if err != nil {
		writeRequestError(w, r, err)
		return
	}

	updated, err := h.repo.Update(r.Context(), userID, id, t)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "transaction not found")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	EnsurePrice(r.Context(), h.onDemandFetch, in, t)
	auth.WriteJSON(w, http.StatusOK, toResponse(updated))
}

// delete removes one of the caller's transactions.
//
//	@Summary	Delete a transaction
//	@Tags		transactions
//	@Produce	json
//	@Param		id	path	int	true	"transaction id"
//	@Success	204	"deleted"
//	@Failure	404	{object}	object{error=string}	"transaction not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/transactions/{id} [delete]
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if err := h.repo.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "transaction not found")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
