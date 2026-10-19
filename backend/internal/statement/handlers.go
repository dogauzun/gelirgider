package statement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// ParsePath is the upload route. It is exported because internal/server has
// to exempt it from the global 64 KB body limit: a statement runs to several
// pages (ADR 0006). maxUploadBytes replaces that limit here.
const ParsePath = "/api/imports/parse"

// CommitPath is the commit route. It is exported for the same reason as
// ParsePath: its body is every reviewed row of a statement, which a 1000-row
// import pushes well past the global 64 KB limit. maxCommitBytes
// replaces that limit here.
const CommitPath = "/api/imports"

// maxUploadBytes caps an uploaded statement, and doubles as the multipart
// parser's memory budget so the file never reaches a temp file on disk: a
// real statement carries the customer's name, TCKN, address and IBAN, and
// nothing outside this request should ever see it (ADR 0006, PII).
const maxUploadBytes = 5 << 20

// maxCommitBytes caps the commit body. A reviewed row is around 160 bytes, so
// this leaves room for many times the rows a 5 MB statement can yield.
const maxCommitBytes = 2 << 20

// maxStatementRows caps the records under a CSV's header and the rows any
// parser's statement holds. Every one becomes a preview row, so a 5 MB file
// of short lines would otherwise cost gigabytes and a small PDF
// minutes of CPU; 5000 reviewed rows still fit in maxCommitBytes.
const maxStatementRows = 5000

// errTooManyRows is a CSV or a statement past maxStatementRows.
var errTooManyRows = fmt.Errorf("more than %d rows", maxStatementRows)

// pdfMagic starts every PDF. Anything else is rejected before it reaches the
// extractor, and a CSV upload that starts with it is refused as well.
var pdfMagic = []byte("%PDF-")

// utf8BOM starts a CSV that Excel saved as "CSV UTF-8".
var utf8BOM = []byte("\xef\xbb\xbf")

// sha256Hex is the file hash exactly as parse returns it.
var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// fundCode is the shape of a TEFAS fund code: the only unknown symbols the
// import asks TEFAS about.
var fundCode = regexp.MustCompile(`^[A-Z0-9]{3}$`)

// maxFundLookups caps the distinct codes one request asks TEFAS about.
const maxFundLookups = 10

// fundLookupLimit is how many TEFAS fund lookups one client IP may make a
// minute, the preview's and the fund route's together: it keeps a
// script from turning either route into a TEFAS crawler, which could get the
// server's IP blocked and stop fund prices for every user.
const fundLookupLimit = 20

// FundLookup asks a public source whether code is a TEFAS fund: found with
// its name, not found, or an error. It returns ErrFundLookupOff
// when it asked nothing, as with the tefas source off.
type FundLookup func(ctx context.Context, code string) (name string, found bool, err error)

// ErrFundLookupOff is a FundLookup's error for a lookup that asked nothing:
// the code is not found, and the lookup does not count against the
// client's fundLookupLimit.
var ErrFundLookupOff = errors.New("fund lookup off")

// BulletinSync stores the BIST bulletin of date, adding each symbol it lists
// that is no instrument yet, as the scheduled sync does. fetched
// is false when it asked the bulletin host nothing, as for a bulletin
// already stored.
type BulletinSync func(ctx context.Context, date time.Time) (fetched bool, err error)

// maxBulletinSyncs caps the distinct dates one request fetches a BIST
// bulletin for, and bulletinSyncTimeout bounds each sync.
// bulletinSyncLimit is how many bulletin fetches one client IP's previews
// may make a minute: a bulletin that keeps failing, or does not
// list the symbol, is fetched again by every preview, so repeated previews
// could otherwise hammer the bulletin host.
const (
	maxBulletinSyncs    = 5
	bulletinSyncTimeout = 10 * time.Second
	bulletinSyncLimit   = 10
)

// PreviewLookupTimeout bounds the TEFAS fund lookups and BIST bulletin
// syncs of one preview together, not each one: a hung host would otherwise
// hold the preview for maxFundLookups lookups and maxBulletinSyncs syncs one
// after another, far past a proxy's timeout. Once it has passed,
// the remaining lookups are skipped and their rows stay unknown_instrument.
// It leaves room for one full bulletin sync, and for the opening fetches'
// own transaction.OnDemandFetchTimeout after it, behind a wait for the fetch
// slot of at most as long.
const PreviewLookupTimeout = 15 * time.Second

// brokerNames maps a broker key to the display name used in the note of
// every transaction an import creates ("Midas 2025-12-01 – 2025-12-31").
// The note is broker name plus period only, so it reads the same in every
// locale and the "imported" pill carries the rest. A broker that
// is not listed is rejected, which also keeps an arbitrary client string
// out of the note.
var brokerNames = map[string]string{
	"midas":           "Midas",
	BrokerCSV:         "CSV",
	"garanti":         "Garanti BBVA",
	"garanti_yatirim": "Garanti BBVA Yatırım",
}

// RegisterRoutes wires the statement import endpoints into mux. mux must be
// mounted behind middleware that calls auth.RequireUser, so that auth.UserID
// is available in the request context. portfolios is used to check portfolio
// ownership, instruments to validate the rows' instrument_id, and
// onDemandFetch (which may be nil) to fetch prices for the imported
// instruments right after the commit, the same hook a single transaction
// create runs. lookupFund (nil turns it off) asks TEFAS about a
// TEFAS row's unknown fund code, so the preview can create the fund TEFAS
// confirms, and serves the preview picker's fund lookup.
// prices, manualPrices and deposits price the opening positions the preview proposes,
// which may also run onDemandFetch. syncBulletin (nil turns it
// off) syncs the BIST bulletin of a BIST row's date when its symbol is no
// instrument, so a share delisted since resolves.
func RegisterRoutes(mux *http.ServeMux, deps Deps) {
	h := &handler{imports: deps.Imports, transactions: deps.Transactions, splits: deps.Splits, portfolios: deps.Portfolios, instruments: deps.Instruments, prices: deps.Prices, manualPrices: deps.ManualPrices, deposits: deps.Deposits, onDemandFetch: deps.OnDemandFetch, lookupFund: deps.LookupFund, fundLookups: ratelimit.New(fundLookupLimit, time.Minute), syncBulletin: deps.SyncBulletin, bulletinSyncs: ratelimit.New(bulletinSyncLimit, time.Minute)}
	mux.HandleFunc("POST "+ParsePath, h.parse)
	mux.HandleFunc("POST "+CommitPath, h.commit)
	mux.HandleFunc("GET "+CommitPath, h.list)
	mux.HandleFunc("DELETE /api/imports/{id}", h.delete)
	mux.HandleFunc("POST /api/imports/funds", h.lookupFundRoute)
}

// Deps are the repos and fetchers RegisterRoutes injects into the import
// routes, named as the handler's fields.
type Deps struct {
	Imports       *ImportRepo
	Transactions  *transaction.Repo
	Splits        *split.Repo
	Portfolios    *portfolio.Repo
	Instruments   *instrument.Repo
	Prices        *price.Repo
	ManualPrices  *price.ManualRepo
	Deposits      holdings.DepositLookup
	OnDemandFetch transaction.OnDemandFetch
	LookupFund    FundLookup
	SyncBulletin  BulletinSync
}

type handler struct {
	imports       *ImportRepo
	transactions  *transaction.Repo
	splits        *split.Repo
	portfolios    *portfolio.Repo
	instruments   instrumentStore
	prices        *price.Repo
	manualPrices  *price.ManualRepo
	deposits      holdings.DepositLookup
	onDemandFetch transaction.OnDemandFetch
	lookupFund    FundLookup
	fundLookups   *ratelimit.Limiter
	syncBulletin  BulletinSync
	bulletinSyncs *ratelimit.Limiter
}

// instrumentStore is the part of instrument.Repo the import routes use, so
// a test can count their lookups.
type instrumentStore interface {
	Get(ctx context.Context, id int64) (instrument.Instrument, error)
	ByIDs(ctx context.Context, ids []int64) (map[int64]instrument.Instrument, error)
	BySymbol(ctx context.Context, market, symbol string) (instrument.Instrument, error)
	All(ctx context.Context, market string) ([]instrument.Instrument, error)
	Insert(ctx context.Context, market, symbol, name, currency string) (int64, error)
}

// commitRequest is the reviewed statement the user chose to import: the file
// it came from, its account currency and period, and the rows left checked in
// the preview. Currency is the preview's own, "" when the statement printed
// none; with the period it tells a re-downloaded month from a new one.
// A CSV's is only its rows' majority, so the import records none.
// The rows are plain transaction requests, which is what keeps
// this endpoint ignorant of brokers -- it is a bulk transaction create with
// an import record around it (ADR 0006). Aliases, optional, are the
// instruments the user picked for symbols the preview did not know; they are
// the only broker-aware part, stored per (user, broker, symbol) for the next
// preview. Names, optional, are the preview's names, the fund names
// the statement printed next to a code, learned the same way but never
// replacing a pick; instrument 0 forgets a learned name.
type commitRequest struct {
	Broker      string                `json:"broker"`
	PortfolioID int64                 `json:"portfolio_id"`
	SHA256      string                `json:"sha256"`
	Currency    string                `json:"currency"`
	PeriodStart string                `json:"period_start"`
	PeriodEnd   string                `json:"period_end"`
	Rows        []transaction.Request `json:"rows"`
	Aliases     []aliasRequest        `json:"aliases"`
	Names       []aliasRequest        `json:"names"`
}

// aliasRequest is one pick to remember: the preview row's symbol exactly as
// the parser emitted it, and the instrument the user chose for it.
type aliasRequest struct {
	Symbol       string `json:"symbol"`
	InstrumentID int64  `json:"instrument_id"`
}

// Limits of a commit's aliases: a statement names far fewer distinct
// symbols, and a parsed name is cut well short of this.
const (
	maxAliases     = 200
	maxAliasSymbol = 200
)

// resolvedByAlias is a preview row's resolved_by when its instrument is the
// user's stored pick, resolvedByName when it is a fund name learned from an
// earlier statement's balances.
const (
	resolvedByAlias = "alias"
	resolvedByName  = "name"
)

type commitResponse struct {
	ID               int64  `json:"id"`
	TransactionCount int    `json:"transaction_count"`
	PeriodStart      string `json:"period_start"`
	PeriodEnd        string `json:"period_end"`
}

type importResponse struct {
	ID          int64  `json:"id"`
	Broker      string `json:"broker"`
	PortfolioID int64  `json:"portfolio_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	// Currency is the statement's account currency, "" when unknown or a
	// CSV. It tells the TRY and USD statements of the same month apart.
	Currency         string `json:"currency"`
	TransactionCount int    `json:"transaction_count"`
	CreatedAt        string `json:"created_at"`
}

// validate checks the envelope around the rows; the rows themselves are
// validated one by one by transaction.FromRequestWith during the commit.
func (req commitRequest) validate() error {
	if _, ok := brokerNames[req.Broker]; !ok {
		return fmt.Errorf("broker: unknown broker")
	}
	// The duplicate check (409) keys on this hash, so only the shape parse
	// returns is accepted: a junk value would slip past it.
	if !sha256Hex.MatchString(req.SHA256) {
		return fmt.Errorf("sha256: must be 64 lowercase hex characters")
	}
	if req.Currency != "" && !currency.IsValid(req.Currency) {
		return fmt.Errorf("currency: must be one of %s", strings.Join(currency.Valid, ", "))
	}
	if err := checkAliases("aliases", req.Aliases); err != nil {
		return err
	}
	if err := checkAliases("names", req.Names); err != nil {
		return err
	}
	// No rows is valid: a month with nothing in it is still recorded, so it
	// shows in the history and a re-upload is caught.
	return checkPeriod(req.Broker, req.PeriodStart, req.PeriodEnd)
}

// checkAliases checks the shape of commit's aliases and parse's picks;
// field names the list in the error.
func checkAliases(field string, aliases []aliasRequest) error {
	if len(aliases) > maxAliases {
		return fmt.Errorf("%s: must have at most %d entries", field, maxAliases)
	}
	for i, a := range aliases {
		if n := utf8.RuneCountInString(a.Symbol); n == 0 || n > maxAliasSymbol {
			return fmt.Errorf("%s[%d].symbol: must be 1 to %d characters", field, i, maxAliasSymbol)
		}
		if strings.ContainsFunc(a.Symbol, unicode.IsControl) {
			return fmt.Errorf("%s[%d].symbol: must not contain control characters", field, i)
		}
	}
	return nil
}

// formAliases reads the parse form's field, a JSON array in the shape of
// commit's aliases, checks it like those and returns the instruments by
// symbol. ok is false when it answered 400 or 500 instead.
func (h *handler) formAliases(w http.ResponseWriter, r *http.Request, field string) (map[string]instrument.Instrument, bool) {
	var list []aliasRequest
	if raw := r.FormValue(field); raw != "" {
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			auth.WriteError(w, http.StatusBadRequest, field+": must be a JSON array of symbol and instrument_id")
			return nil, false
		}
	}
	if err := checkAliases(field, list); err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	out, bad, err := h.aliasInstruments(r.Context(), field, list)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return nil, false
	}
	if bad != "" {
		auth.WriteError(w, http.StatusBadRequest, bad)
		return nil, false
	}
	return out, true
}

// aliasInstruments looks up the instrument of each of aliases, in one query,
// and returns them by symbol, a later entry for the same symbol
// winning. An alias of no instrument returns the 400 message naming it
// instead.
func (h *handler) aliasInstruments(ctx context.Context, field string, aliases []aliasRequest) (map[string]instrument.Instrument, string, error) {
	// A commit's name of instrument 0 is one to forget.
	forget := func(a aliasRequest) bool { return field == "names" && a.InstrumentID == 0 }
	ids := make([]int64, 0, len(aliases))
	for _, a := range aliases {
		if !forget(a) {
			ids = append(ids, a.InstrumentID)
		}
	}
	lookup, err := h.instrumentLookup(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	out := make(map[string]instrument.Instrument, len(aliases))
	for i, a := range aliases {
		if forget(a) {
			continue
		}
		in, err := lookup(a.InstrumentID)
		if errors.Is(err, instrument.ErrNotFound) {
			return nil, fmt.Sprintf("%s[%d].instrument_id: unknown instrument", field, i), nil
		}
		if err != nil {
			return nil, "", err
		}
		out[a.Symbol] = in
	}
	return out, "", nil
}

// checkPeriod checks a statement period the way the commit refuses it. The
// parse step runs it too, so the preview says up front that the commit will
// fail.
func checkPeriod(broker, periodStart, periodEnd string) error {
	// The period is range-checked like any user-entered date: a bogus one
	// would overlap, and so block, every later import of this account.
	if err := price.CheckDate(periodStart); err != nil {
		return fmt.Errorf("period_start: %w", err)
	}
	if err := price.CheckDate(periodEnd); err != nil {
		return fmt.Errorf("period_end: %w", err)
	}
	// Both parse: CheckDate accepted them. YYYY-MM-DD sorts lexically, so a
	// string compare is a date compare.
	start, _ := time.Parse(time.DateOnly, periodStart)
	end, _ := time.Parse(time.DateOnly, periodEnd)
	if periodEnd < periodStart {
		return fmt.Errorf("period_end: must not be before period_start")
	}
	// A broker statement covers a month, a quarter at most (ADR 0006), so a
	// year is a generous cap. A CSV spans whatever history it holds, such as
	// the app's own export, so it has no cap.
	if broker != BrokerCSV && !end.Before(start.AddDate(1, 0, 0)) {
		return fmt.Errorf("period_end: period must be shorter than a year")
	}
	return nil
}

// csvPeriod is a CSV's period, which it does not print: the span of its
// dates. A date the commit refuses outright is left out, in the preview and
// the commit alike: one mistyped year would otherwise push the
// period past checkPeriod and block every other row. Both are ""
// when no date is left.
func csvPeriod(dates iter.Seq[string]) (start, end string) {
	for d := range dates {
		if checkPeriod(BrokerCSV, d, d) != nil {
			continue
		}
		if start == "" || d < start {
			start = d
		}
		end = max(end, d)
	}
	return start, end
}

// setCSVPeriod sets a CSV commit's period. A CSV prints no period, so it
// spans the rows committed, not the preview's, which is "" for a file of
// error rows alone and misses a row re-dated in the preview. A
// date the commit refuses is left out, so that row's own error is the one
// returned.
func (req *commitRequest) setCSVPeriod() error {
	start, end := csvPeriod(func(yield func(string) bool) {
		for _, row := range req.Rows {
			if !yield(row.Date) {
				return
			}
		}
	})
	if start != "" {
		req.PeriodStart, req.PeriodEnd = start, end
		return nil
	}
	// No row gives a period, so the one sent (often "") would fail first.
	// Every row's date is refused, so a row's own error is returned instead.
	for i, row := range req.Rows {
		if err := transaction.Validate(row); err != nil {
			return fmt.Errorf("rows[%d].%w", i, err)
		}
	}
	return nil
}

// fetchTarget is one on-demand price fetch to run after the commit: the
// instrument (the zero value for a cash row) and the earliest transaction of
// this import that references it, since the fetch backfills from that date
// forward.
type fetchTarget struct {
	in instrument.Instrument
	t  transaction.Transaction
}

// commit writes the import row and every one of its transactions in one
// database transaction: the first invalid row rolls all of it back, so a
// half-imported statement can never exist. Re-uploading a file already
// imported into this portfolio is a 409 rather than a second copy of the
// month's trades (ADR 0006), and so is another file of the same statement,
// which names the import to undo first.
//
//	@Summary	Commit a reviewed statement
//	@Description	Creates the import and all its transactions in one database transaction; the first invalid row rolls everything back. For broker csv the period is the committed rows' dates and the one sent is used only when it has no rows; when no row has a valid date, the first row's own error is returned. Each of the optional aliases is stored in the same database transaction as the user's pick for that symbol in this broker's statements, replacing an earlier pick; the next preview resolves an unknown symbol with it. Each of the optional names, the preview's names, is stored the same way as a learned fund name, replacing an earlier learned name but never a pick; a name sent with instrument_id 0 or with two different instruments is stored for neither and removes a learned one. A row may be dated the day before period_start, the date of the preview's opening rows, or the day after period_end, when Midas books the interest of the month it prints. The body is capped at 2 MB instead of 64 KB.
//	@Tags		imports
//	@Accept		json
//	@Produce	json
//	@Param		body	body		statement.commitRequest		true	"the preview's broker, sha256, currency and period, the rows left checked, the instruments picked for unknown symbols, and the preview's names"
//	@Success	201		{object}	statement.commitResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; broker: unknown broker; sha256: must be 64 lowercase hex characters; currency: must be one of TRY, USD, EUR; period_start|period_end: must be YYYY-MM-DD; period_start|period_end: must not be in the future; period_start|period_end: must be on or after 1990-01-01; period_end: must not be before period_start; period_end: period must be shorter than a year (not for broker csv); rows[i].<field>: <message> (the transaction validation errors, e.g. rows[3].fee: must not exceed amount); rows[i].date: must be within <period_start> – <period_end> (or the day before period_start or the day after period_end); aliases: must have at most 200 entries; aliases[i].symbol: must be 1 to 200 characters; aliases[i].symbol: must not contain control characters; aliases[i].instrument_id: unknown instrument; names: must have at most 200 entries; names[i].symbol: must be 1 to 200 characters; names[i].symbol: must not contain control characters; names[i].instrument_id: unknown instrument"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	409		{object}	object{error=string,import_id=int,period_start=string,period_end=string,portfolio_id=int,portfolio_name=string}	"already_imported (import_id, period_start and period_end name the earlier import of the same period in this portfolio, to undo first, also when the same file is committed again; when this file is already imported with another period or currency, e.g. a CSV committed again with other rows, or into another of the user's portfolios, import_id, portfolio_id and portfolio_name name that import and its portfolio instead)"
//	@Failure	413		{object}	object{error=string}	"rows: larger than 2 MB"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/imports [post]
func (h *handler) commit(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())

	req, ok := decodeCommit(w, r)
	if !ok {
		return
	}
	if _, err := h.portfolios.Get(r.Context(), userID, req.PortfolioID); err != nil {
		portfolio.WriteLookupError(w, r, err)
		return
	}
	_, bad, err := h.aliasInstruments(r.Context(), "aliases", req.Aliases)
	if err == nil && bad == "" {
		_, bad, err = h.aliasInstruments(r.Context(), "names", req.Names)
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if bad != "" {
		auth.WriteError(w, http.StatusBadRequest, bad)
		return
	}

	getInstrument, err := h.rowInstruments(r.Context(), req.Rows)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	ts, targets, ok := commitTransactions(w, r, req, getInstrument)
	if !ok {
		return
	}
	h.storeImport(w, r, userID, req, ts, targets)
}

// decodeCommit reads and validates the commit's body. ok is false once it
// has written the error.
func decodeCommit(w http.ResponseWriter, r *http.Request) (req commitRequest, ok bool) {
	// This route is exempt from the global 64 KB limit, so it sets its own.
	r.Body = http.MaxBytesReader(w, r.Body, maxCommitBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			auth.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("rows: larger than %d MB", maxCommitBytes>>20))
			return req, false
		}
		auth.WriteError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	if req.Broker == BrokerCSV {
		if err := req.setCSVPeriod(); err != nil {
			auth.WriteError(w, http.StatusBadRequest, err.Error())
			return req, false
		}
	}
	if err := req.validate(); err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return req, false
	}
	return req, true
}

// commitTransactions validates req's rows into the transactions to store,
// and the price fetch to run for each distinct instrument or cash currency
// after the commit. ok is false once it has written the error.
func commitTransactions(w http.ResponseWriter, r *http.Request, req commitRequest, getInstrument func(int64) (instrument.Instrument, error)) (ts []transaction.Transaction, targets map[string]fetchTarget, ok bool) {
	// The note never carries anything off the statement itself: the
	// description column holds the customer's bank name and IBAN (ADR 0006,
	// PII).
	note := fmt.Sprintf("%s %s – %s", brokerNames[req.Broker], req.PeriodStart, req.PeriodEnd)

	// Keyed by instrument id, or by currency for a cash row, so the fetch
	// runs once per distinct instrument and once per distinct cash currency
	// instead of once per row.
	targets = map[string]fetchTarget{}

	// validate checked the period's shape.
	start, _ := time.Parse(time.DateOnly, req.PeriodStart)
	openingDate := start.AddDate(0, 0, -1).Format(time.DateOnly)
	end, _ := time.Parse(time.DateOnly, req.PeriodEnd)
	interestDate := end.AddDate(0, 0, 1).Format(time.DateOnly)
	ts = make([]transaction.Transaction, 0, len(req.Rows))
	for i, row := range req.Rows {
		t, in, err := transaction.FromRequestWith(row, getInstrument)
		if err != nil {
			if errors.Is(err, transaction.ErrLookup) {
				auth.WriteInternalError(w, r, err)
				return nil, nil, false
			}
			auth.WriteError(w, http.StatusBadRequest, fmt.Sprintf("rows[%d].%s", i, err.Error()))
			return nil, nil, false
		}
		// The import's undo and history describe its period, so every row
		// must fall inside it; a Midas statement only lists rows dated in its
		// own period. The exceptions are the day before it, the
		// date of the opening rows the preview proposes, and the
		// day after it, when Midas books the month's interest it prints in
		// this statement.
		if (t.Date < req.PeriodStart && t.Date != openingDate) || (t.Date > req.PeriodEnd && t.Date != interestDate) {
			auth.WriteError(w, http.StatusBadRequest, fmt.Sprintf("rows[%d].date: must be within %s – %s", i, req.PeriodStart, req.PeriodEnd))
			return nil, nil, false
		}
		t.PortfolioID = req.PortfolioID
		t.Note = note
		ts = append(ts, t)

		key := "cash:" + t.Currency
		if t.InstrumentID != nil {
			key = "instrument:" + strconv.FormatInt(*t.InstrumentID, 10)
		}
		if prev, seen := targets[key]; !seen || t.Date < prev.t.Date {
			targets[key] = fetchTarget{in: in, t: t}
		}
	}
	return ts, targets, true
}

// storeImport writes the import row, ts and the aliases, answers with the
// import, and fetches the prices of targets once the rows are committed.
func (h *handler) storeImport(w http.ResponseWriter, r *http.Request, userID int64, req commitRequest, ts []transaction.Transaction, targets map[string]fetchTarget) {
	aliases, learn := commitAliases(req)

	// A CSV is one file, not an account: its currency is only the rows'
	// majority, so it is recorded as unknown and matches an earlier CSV of
	// any currency.
	account := req.Currency
	if req.Broker == BrokerCSV {
		account = ""
	}
	imp, err := h.imports.Commit(r.Context(), NewImport{
		UserID: userID, PortfolioID: req.PortfolioID, Broker: req.Broker, Currency: account,
		PeriodStart: req.PeriodStart, PeriodEnd: req.PeriodEnd, SHA256: req.SHA256,
	}, ts, aliases, learn)
	if errors.Is(err, ErrSamePeriod) {
		auth.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "already_imported", "import_id": imp.ID,
			"period_start": imp.PeriodStart, "period_end": imp.PeriodEnd,
		})
		return
	}
	if errors.Is(err, ErrDuplicate) {
		if imp.ID == 0 {
			auth.WriteError(w, http.StatusConflict, "already_imported")
			return
		}
		h.writeAlreadyImported(w, r, userID, imp)
		return
	}
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	h.ensurePrices(r.Context(), targets)

	auth.WriteJSON(w, http.StatusCreated, commitResponse{
		ID:               imp.ID,
		TransactionCount: len(req.Rows),
		PeriodStart:      imp.PeriodStart,
		PeriodEnd:        imp.PeriodEnd,
	})
}

// commitAliases is what req's commit stores: the instrument picked per
// symbol (aliases) and per fund name (learn). A name sent with instrument
// 0, the preview's word for one its statement prints for two funds or for a
// code of no instrument, or sent with two instruments, is learned for
// neither and forgets what an earlier statement taught (0).
func commitAliases(req commitRequest) (aliases, learn map[string]int64) {
	aliases = map[string]int64{}
	for _, a := range req.Aliases {
		aliases[a.Symbol] = a.InstrumentID
	}
	learn = map[string]int64{}
	for _, n := range req.Names {
		if id, seen := learn[n.Symbol]; seen && id != n.InstrumentID {
			learn[n.Symbol] = 0
		} else if !seen {
			learn[n.Symbol] = n.InstrumentID
		}
	}
	return aliases, learn
}

// ensurePrices fetches a price for each committed target. The fetches run
// one after another, but the response waits for them at most
// OnDemandFetchTimeout in all, which may end before the fetch slot frees:
// the rows are committed, so a hung price host must not hold the response
// past a proxy's timeout. The fetches go on in the background after it
// returns, each within its own EnsurePrice budget; a client disconnect does
// not cancel them.
func (h *handler) ensurePrices(ctx context.Context, targets map[string]fetchTarget) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx := context.WithoutCancel(ctx)
		for _, target := range targets {
			transaction.EnsurePrice(ctx, h.onDemandFetch, target.in, target.t)
		}
	}()
	select {
	case <-done:
	case <-time.After(transaction.OnDemandFetchTimeout):
	}
}

// rowInstruments loads the instruments rows name in one query rather than
// one per row and returns the lookup the commit validates them
// with.
func (h *handler) rowInstruments(ctx context.Context, rows []transaction.Request) (func(int64) (instrument.Instrument, error), error) {
	var ids []int64
	for _, row := range rows {
		if row.InstrumentID != nil {
			ids = append(ids, *row.InstrumentID)
		}
	}
	return h.instrumentLookup(ctx, ids)
}

// instrumentLookup loads the distinct instruments of ids in one ByIDs and
// returns a lookup by id. An unknown id fails ByIDs as a whole, so then each
// id is looked up on its own and the caller's first bad entry answers, as it
// always has.
func (h *handler) instrumentLookup(ctx context.Context, ids []int64) (func(int64) (instrument.Instrument, error), error) {
	known, err := h.instruments.ByIDs(ctx, slices.Compact(slices.Sorted(slices.Values(ids))))
	if err != nil && !errors.Is(err, instrument.ErrNotFound) {
		return nil, err
	}
	return func(id int64) (instrument.Instrument, error) {
		in, ok := known[id]
		if !ok {
			var err error
			if in, err = h.instruments.Get(ctx, id); err != nil {
				return in, err
			}
		}
		if in.Market == instrument.MarketDeposit {
			return instrument.Instrument{}, instrument.ErrNotFound
		}
		return in, nil
	}, nil
}

// list returns the caller's statement imports across all portfolios.
//
//	@Summary	List statement imports
//	@Tags		imports
//	@Produce	json
//	@Success	200	{array}		statement.importResponse
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/imports [get]
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	imports, err := h.imports.List(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	out := make([]importResponse, len(imports))
	for i, imp := range imports {
		out[i] = importResponse{
			ID:               imp.ID,
			Broker:           imp.Broker,
			PortfolioID:      imp.PortfolioID,
			PeriodStart:      imp.PeriodStart,
			PeriodEnd:        imp.PeriodEnd,
			Currency:         imp.Currency,
			TransactionCount: imp.TransactionCount,
			CreatedAt:        imp.CreatedAt,
		}
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// delete is the undo button: it removes the import and every transaction it
// created. Imports of another user are 404, never touched.
//
//	@Summary	Undo a statement import
//	@Tags		imports
//	@Produce	json
//	@Param		id	path	int	true	"import id"
//	@Success	204	"deleted with its transactions"
//	@Failure	404	{object}	object{error=string}	"import not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/imports/{id} [delete]
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "import not found")
		return
	}
	if err := h.imports.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			auth.WriteError(w, http.StatusNotFound, "import not found")
			return
		}
		auth.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fundResponse is the instrument the fund lookup found or created.
type fundResponse struct {
	ID       int64  `json:"id"`
	Symbol   string `json:"symbol"`
	Market   string `json:"market"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

// lookupFundRoute resolves a fund code the user typed into the preview's
// instrument picker: an import row that names its fund by name has no code
// for the preview to ask TEFAS about. A TEFAS fund already known is
// returned as is; otherwise createFund asks TEFAS and adds the fund it
// confirms. Not found covers a code TEFAS does not know, a failed lookup and the
// tefas source being off. Only a code that is asked about counts against the
// client IP's limit of 20 TEFAS lookups a minute, which the preview's lookups share.
//
//	@Summary	Look up a TEFAS fund by code
//	@Tags		imports
//	@Accept		json
//	@Produce	json
//	@Param		body	body		object{code=string}		true	"fund code, three letters or digits; trimmed and upper-cased"
//	@Success	200		{object}	statement.fundResponse	"the known or newly created TEFAS fund"
//	@Failure	400		{object}	object{error=string}	"invalid request body; code: must be a TEFAS fund code"
//	@Failure	404		{object}	object{error=string}	"fund not found"
//	@Failure	429		{object}	object{error=string}	"too many requests: the client IP asked TEFAS about 20 codes this minute, here and in previews together"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/imports/funds [post]
func (h *handler) lookupFundRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if !fundCode.MatchString(code) {
		auth.WriteError(w, http.StatusBadRequest, "code: must be a TEFAS fund code")
		return
	}
	in, err := h.instruments.BySymbol(r.Context(), instrument.MarketTEFAS, code)
	if errors.Is(err, instrument.ErrNotFound) {
		in, err = h.createFund(r.Context(), &lookupSession{ctx: r.Context(), client: ratelimit.ClientIP(r), asked: map[string]bool{}}, code)
	}
	var limited fundLimitError
	if errors.As(err, &limited) {
		ratelimit.WriteTooMany(w, time.Duration(limited))
		return
	}
	if errors.Is(err, instrument.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "fund not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, fundResponse{ID: in.ID, Symbol: in.Symbol, Market: in.Market, Name: in.Name, Currency: in.Currency})
}
