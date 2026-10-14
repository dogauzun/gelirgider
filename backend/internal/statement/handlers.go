package statement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/statement/pdftext"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
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
// own transaction.OnDemandFetchTimeout after it.
const PreviewLookupTimeout = 15 * time.Second

// Per-row verdicts of the preview.
const (
	statusOK                  = "ok"
	statusUnknownInstrument   = "unknown_instrument"
	statusSkipped             = "skipped"
	statusError               = "error"
	statusUnsupportedCurrency = "unsupported_currency"
)

// marketForCurrency derives an instrument's market from the currency its
// statement row is priced in, for a row with no market hint. A row priced
// in TRY that is not a BIST share carries the hint instead: Garanti marks
// its fund rows and statement "TEFAS", a CSV row takes its market column
// (ADR 0006).
var marketForCurrency = map[string]string{"USD": "US", "TRY": "BIST"}

// otherMarkets are the markets a row with no market hint is tried in once its
// currency's market (marketForCurrency) does not know the symbol. A CSV row
// can hold a fund, a coin or gold as well as a share.
var otherMarkets = map[string][]string{"TRY": {"TEFAS", "CRYPTO", "GOLD"}, "USD": {"CRYPTO"}}

// brokerNames maps a broker key to the display name used in the note of
// every transaction an import creates ("Midas 2025-12-01 – 2025-12-31").
// The note is broker name plus period only, so it reads the same in every
// locale and the "imported" pill carries the rest. A broker that
// is not listed is rejected, which also keeps an arbitrary client string
// out of the note.
var brokerNames = map[string]string{
	"midas":           "Midas",
	"csv":             "CSV",
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
// prices and manualPrices price the opening positions the preview proposes,
// which may also run onDemandFetch. syncBulletin (nil turns it
// off) syncs the BIST bulletin of a BIST row's date when its symbol is no
// instrument, so a share delisted since resolves.
func RegisterRoutes(mux *http.ServeMux, deps Deps) {
	h := &handler{imports: deps.Imports, transactions: deps.Transactions, splits: deps.Splits, portfolios: deps.Portfolios, instruments: deps.Instruments, prices: deps.Prices, manualPrices: deps.ManualPrices, onDemandFetch: deps.OnDemandFetch, lookupFund: deps.LookupFund, fundLookups: ratelimit.New(fundLookupLimit, time.Minute), syncBulletin: deps.SyncBulletin, bulletinSyncs: ratelimit.New(bulletinSyncLimit, time.Minute)}
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

// previewRow is one proposed operation of a parsed statement. Everything but
// key and status is optional: a skipped row carries only its reason and cells,
// an error row only what the parser could read. A number left out is nil; one
// set goes out as the JSON string of its decimal.String().
type previewRow struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Type   string `json:"type,omitempty"`
	Date   string `json:"date,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	// Market is the row's own market hint, as a CSV row can carry one; empty
	// when it has none.
	Market       string           `json:"market,omitempty"`
	InstrumentID *int64           `json:"instrument_id,omitempty"`
	Quantity     *decimal.Decimal `json:"quantity,omitempty" swaggertype:"string"`
	Price        *decimal.Decimal `json:"price,omitempty" swaggertype:"string"`
	Amount       *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	Fee          *decimal.Decimal `json:"fee,omitempty" swaggertype:"string"`
	Currency     string           `json:"currency,omitempty"`
	Description  string           `json:"description,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Section      string           `json:"section,omitempty"`
	Code         string           `json:"code,omitempty"`
	Value        string           `json:"value,omitempty"`
	Message      string           `json:"message,omitempty"`
	Raw          []string         `json:"raw,omitempty"`
	// CurrencyFallback marks a row the file gave no currency for: Currency
	// is the instrument's, TRY when none, so a pick in the page
	// takes the picked instrument's.
	CurrencyFallback bool `json:"currency_fallback,omitempty"`
	// DuplicateOf is the date of an earlier transaction, hand-entered or
	// imported, this row likely repeats (see flagDuplicates); the client
	// starts such a row unchecked.
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// UnknownInstrument marks an error row whose symbol is unknown as well,
	// so the preview still says so once checkRow made it an error.
	UnknownInstrument bool `json:"unknown_instrument,omitempty"`
	// ResolvedBy is "alias" when the instrument is the user's own earlier
	// pick for this symbol rather than a match of it, "name" when
	// it is the fund an earlier statement's balances printed under this name.
	ResolvedBy string `json:"resolved_by,omitempty"`
	// InstrumentSymbol and InstrumentName name the instrument of a row
	// resolved by an earlier pick, whose symbol is the statement's own, so
	// the user sees what the pick resolved it to.
	InstrumentSymbol string `json:"instrument_symbol,omitempty"`
	InstrumentName   string `json:"instrument_name,omitempty"`
	// Opening marks a row the preview proposes for what the account held
	// before period_start: a buy dated the day before it, or the deposit
	// that pays for those buys.
	Opening bool `json:"opening,omitempty"`
	// ApproximatePrice marks an opening buy priced with the statement's own
	// period-end price, as no stored price exists for its date.
	ApproximatePrice bool `json:"approximate_price,omitempty"`
	// CostPrice marks an opening buy priced with the average cost the
	// statement prints for the holding.
	CostPrice bool `json:"cost_price,omitempty"`
	// Closing marks a sell the preview proposes, dated the day before
	// period_start, for what the portfolio holds and the statement does not.
	// It starts unchecked: the gap may be months not imported.
	Closing bool `json:"closing,omitempty"`
}

// holdingResponse is one line of the statement's period-end snapshot, passed
// through for reconciliation; it is never imported.
type holdingResponse struct {
	Symbol   string `json:"symbol"`
	Quantity string `json:"quantity"`
	AvgCost  string `json:"avg_cost"`
	Currency string `json:"currency"`
}

// reconcileEntry compares one symbol's period-end quantity: what the
// statement says the account holds against what the portfolio would compute
// once this preview's ok rows are committed. StatementQuantity is null for a
// position the statement does not mention -- most often a month that was
// never imported. Average cost is not compared: Midas rounds it and
// its cost-basis rules are not necessarily ours (ADR 0006). InstrumentID is
// the instrument the line compares, left out for a holding that resolves to
// none: the page moves the line with the rows the user checks and edits.
type reconcileEntry struct {
	Symbol            string  `json:"symbol"`
	InstrumentID      int64   `json:"instrument_id,omitempty"`
	StatementQuantity *string `json:"statement_quantity"`
	PortfolioQuantity string  `json:"portfolio_quantity"`
	Match             bool    `json:"match"`
}

// cashReconcileEntry compares the account's period-end cash in the statement
// currency: the balance the statement prints against the cash the portfolio
// would hold at period_end once this preview's ok rows are committed. Other
// currencies are not compared: the statement says nothing about them. Match
// allows a difference of up to 0.01.
type cashReconcileEntry struct {
	Currency        string `json:"currency"`
	StatementAmount string `json:"statement_amount"`
	PortfolioAmount string `json:"portfolio_amount"`
	Match           bool   `json:"match"`
}

// cashTolerance is the largest difference between statement and portfolio
// cash that still counts as a match.
var cashTolerance = decimal.RequireFromString("0.01")

// warning is a preview notice. Code is stable and the UI translates it from
// the other fields; Message is the English text for a UI that does not know
// the code. "overlap" is a period that intersects an earlier
// import's and does not block the commit; "same_period" is the same statement
// again, which the commit refuses while the earlier import exists.
// "period" is a statement period the commit refuses outright (in the future,
// before 1990, a year or longer unless a CSV): Message is the commit's own
// error, and ImportID is 0 since no earlier import is involved.
type warning struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	ImportID    int64  `json:"import_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

type previewResponse struct {
	Broker         string            `json:"broker"`
	SHA256         string            `json:"sha256"`
	PeriodStart    string            `json:"period_start"`
	PeriodEnd      string            `json:"period_end"`
	Currency       string            `json:"currency"`
	Warnings       []warning         `json:"warnings"`
	Rows           []previewRow      `json:"rows"`
	Holdings       []holdingResponse `json:"holdings"`
	Reconciliation []reconcileEntry  `json:"reconciliation"`
	// CashReconciliation is null when the statement printed no cash
	// balance or no currency.
	CashReconciliation *cashReconcileEntry `json:"cash_reconciliation"`
	// Names are the fund names the balances printed, each once with the
	// instrument of its code (0: see statementNames), in the shape of
	// commit's aliases, for the client to send back as commit's names.
	// Left out for a statement that prints none.
	Names []aliasRequest `json:"names,omitempty"`
}

// parse is the first half of the import flow: it reads the uploaded PDF (or
// CSV, for broker csv), hands it to the broker's parser and returns the proposed rows for review.
// It writes no user data (ADR 0006); it may add shared market data: the
// instruments and prices createFund and bulletinInstrument add, and the
// prices and FX rates an opening position's onDemandFetch stores.
//
// PII: the log line below carries the broker, the portfolio and the row
// counts only. The file name, the rows and every statement field stay in the
// response to the user who uploaded them.
//
//	@Summary	Parse a broker statement
//	@Description	Reads an uploaded PDF statement (a CSV file for broker csv) and returns the proposed rows for review. Writes no user data; it may add shared market data: the instruments and prices described below, and the prices and FX rates fetched to price an opening buy. A TEFAS row whose fund code is no instrument is asked about on TEFAS, at most 10 codes a request and within the client IP's 20 TEFAS lookups a minute shared with /imports/funds, and the fund TEFAS confirms is added; a code not asked stays unknown_instrument. A trade or dividend in BIST whose symbol is no instrument, such as a share delisted since, is looked up again after the BIST bulletin of its date is synced, fetching at most 5 bulletins a request and within the client IP's 10 bulletin fetches a minute; the sync adds the symbols that bulletin lists as instruments, shared market data like their prices. The TEFAS lookups and bulletin syncs of one request share a 15 s deadline; none starts after it, and the rows they would have resolved stay unknown_instrument. With broker auto, a PDF goes through every PDF parser in key order and the first that accepts it picks the broker: the response's broker is that key, and everything below uses it as if it had been sent. A symbol that resolves to no instrument takes the instrument the user picked for it in an earlier commit of the same broker (commit's aliases); such a row has resolved_by "alias" and names that instrument in instrument_symbol and instrument_name. A holding resolves through the same pick, so its reconciliation entry and opening follow it. The optional picks are the instruments the user picked in this preview so far, in the shape of commit's aliases: for this request only, each resolves its symbol like a stored pick and wins over one, so rows, openings and the reconciliation follow it; nothing is stored. When the statement holds more of an instrument at period end than the portfolio would with the ok rows, and the portfolio has no transaction of it before period_start, an opening buy of the difference dated the day before period_start is proposed (opening true), priced with the average cost the statement prints for the holding when it is in the instrument's currency and the preview has no buy of it (cost_price true), else with the stored price on or before that date, for a BIST instrument also once the BIST bulletin of the last weekday on or before that date is synced (one sync a request, within the client IP's 10 bulletin fetches a minute and the opening fetches' deadline), else the statement's own price when the holding is in the instrument's currency (approximate_price true), else returned as an error row with code opening_price; one opening deposit per currency pays for the priced buys. When the statement lists holdings and the portfolio would hold more of an instrument at period end than the statement, holding it before period_start, a closing sell of the difference, at most what it held the day before period_start, is proposed on that day (closing true), priced like an opening buy but never with the average cost, else returned as an error row with code closing_price; one closing withdrawal per currency (closing true) takes the priced sells' proceeds out again, rounded down to the cent. Closing rows are left out of the reconciliation, as the page starts the sells unchecked and books the withdrawal in step with the checked ones. Nothing is proposed while any buy or sell row is unknown_instrument, unless the units of every such symbol's buy and sell rows net to zero. The reconciliation includes them. names lists each fund name the balances print (Garanti fund statements) with the instrument of its code, for the commit's names; its instrument_id is 0 when they print it for two codes or its code resolves to no instrument. A symbol that neither an instrument nor a pick resolves takes the one instrument of the learned names that equal it, start with it or that it starts with, with resolved_by "name" and instrument_symbol and instrument_name set; matches of more than one instrument leave it unknown_instrument, and so does a symbol that is one of the statement's holding codes or that matches one of its own balance names the same way (the parser leaves such a name unmapped only when it names two funds). A garanti TEFAS row's symbol none of that resolves, and that is neither such a holding code nor such a balance name, takes the one TEFAS fund whose name it is cut or abbreviated from: word by word, GPY and GP read as GARANTİ PORTFÖY and 1., 2., 3. as BİRİNCİ, İKİNCİ, ÜÇÜNCÜ, each of its words starts the fund name's word in the same place, case and punctuation ignored. A fund of the same words, else of the same words but a cut last one, wins over the others; it has resolved_by "name" too, and two funds of the closest kind leave it unknown_instrument. The optional names, in the shape of the response's names, are fund names other statements print, such as the later statements of the same upload batch: for this request only, each is a learned name, winning over a stored one of the same name, so a fund renamed on TEFAS that an earlier statement names only by its old name resolves; an entry of instrument_id 0 is ignored and nothing is stored. The body is capped at 5 MB instead of 64 KB.
//	@Tags		imports
//	@Accept		multipart/form-data
//	@Produce	json
//	@Param		broker			formData	string	true	"broker key, or auto to detect the broker of a PDF"	Enums(auto, midas, csv, garanti, garanti_yatirim)
//	@Param		portfolio_id	formData	int		true	"portfolio id"
//	@Param		file			formData	file	true	"statement PDF, or CSV (UTF-8, else read as Windows-1254) for broker csv, at most 5 MB"
//	@Param		picks			formData	string	false	"JSON array of {symbol, instrument_id}, at most 200 entries: the instruments picked in this preview"
//	@Param		names			formData	string	false	"JSON array of {symbol, instrument_id}, at most 200 entries: fund names other statements print, taken as learned names"
//	@Success	200				{object}	statement.previewResponse
//	@Failure	400				{object}	object{error=string}				"file: invalid upload; broker: unknown broker; portfolio_id: must be a number; file: required; file: not a PDF; file: not a readable PDF; file: no period row found: not a Midas statement; file: not a garanti fund statement; file: not a garanti_yatirim statement; file: not a supported statement (broker auto); file: not a CSV; file: not a readable CSV; file: not a csv statement; file: no rows; file: more than 5000 rows; picks: must be a JSON array of symbol and instrument_id; picks: must have at most 200 entries; picks[i].symbol: must be 1 to 200 characters; picks[i].symbol: must not contain control characters; picks[i].instrument_id: unknown instrument; names: must be a JSON array of symbol and instrument_id; names: must have at most 200 entries; names[i].symbol: must be 1 to 200 characters; names[i].symbol: must not contain control characters; names[i].instrument_id: unknown instrument"
//	@Failure	404				{object}	object{error=string}				"portfolio not found"
//	@Failure	409				{object}	object{error=string,import_id=int,portfolio_id=int,portfolio_name=string}	"already_imported (import_id is the user's earlier import of this file, portfolio_id and portfolio_name the portfolio it is in: this one or another of the user's)"
//	@Failure	413				{object}	object{error=string}				"file: larger than 5 MB"
//	@Failure	500				{object}	object{error=string}				"internal error"
//	@Router		/imports/parse [post]
func (h *handler) parse(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUserID(r.Context())

	// This route is exempt from the global 64 KB limit, so it sets its own.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeUploadError(w, err)
		return
	}
	defer r.MultipartForm.RemoveAll()

	broker := r.FormValue("broker")
	// auto: the PDF parser that accepts the file picks the broker.
	auto := broker == "auto"
	if _, ok := Parsers[broker]; !ok && !auto {
		auth.WriteError(w, http.StatusBadRequest, "broker: unknown broker")
		return
	}
	portfolioID, err := strconv.ParseInt(r.FormValue("portfolio_id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "portfolio_id: must be a number")
		return
	}
	if _, err := h.portfolios.Get(r.Context(), userID, portfolioID); err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	// The instruments the user picked in this preview so far, resolved for
	// this request only; the commit stores them.
	picks, ok := h.formAliases(w, r, "picks")
	if !ok {
		return
	}
	// The fund names the other statements of the user's batch print, used
	// like learned names for this request only: a fund renamed on TEFAS is
	// named in an early statement only by its old name, which a later
	// statement prints next to its code.
	batchIns, ok := h.formAliases(w, r, "names")
	if !ok {
		return
	}
	batch := make(map[string]int64, len(batchIns))
	for name, in := range batchIns {
		batch[name] = in.ID
	}

	isCSV := broker == "csv"
	data, sha, ok := h.readUpload(w, r, userID, isCSV)
	if !ok {
		return
	}
	broker, st, ok := parseUpload(w, r, data, broker)
	if !ok {
		return
	}

	req := requester{client: ratelimit.ClientIP(r), userID: userID, broker: broker}
	preview, err := h.previewRows(r.Context(), req, picks, batch, st)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// A CSV prints no period, so it spans its rows' dates, error rows
	// included since the user may fix one in the preview.
	onlyErrors := isCSV
	if isCSV {
		for _, row := range preview {
			onlyErrors = onlyErrors && row.Status == statusError
		}
		st.PeriodStart, st.PeriodEnd = csvPeriod(func(yield func(string) bool) {
			for _, row := range preview {
				if !yield(row.Date) {
					return
				}
			}
		})
	}
	// One scan of the portfolio's transactions per preview, for the
	// duplicate check, the openings and the reconciliation.
	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if err := flagDuplicates(txs, preview); err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	warnings, err := h.overlapWarnings(r.Context(), userID, portfolioID, broker, st)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// A CSV of error rows alone has no period when none of them has a date
	// the commit accepts. Its rows already say what is wrong, and the
	// warning would only name the internal field.
	if err := checkPeriod(broker, st.PeriodStart, st.PeriodEnd); err != nil && !onlyErrors {
		warnings = append(warnings, warning{Code: "period", Message: err.Error(), PeriodStart: st.PeriodStart, PeriodEnd: st.PeriodEnd})
	}
	// After previewRows, which may add a TEFAS fund a trade names.
	holdIns, err := h.holdingInstruments(r.Context(), userID, broker, picks, st)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if preview, err = h.proposeOpenings(r.Context(), req, txs, st, holdIns, preview); err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	reconciliation, cash, err := h.reconcile(r.Context(), txs, st, holdIns, preview)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	counts := map[string]int{}
	for _, row := range preview {
		counts[row.Status]++
	}
	slog.Default().Info("statement parsed",
		"broker", broker, "portfolio_id", portfolioID,
		statusOK, counts[statusOK],
		statusUnknownInstrument, counts[statusUnknownInstrument],
		statusUnsupportedCurrency, counts[statusUnsupportedCurrency],
		statusSkipped, counts[statusSkipped],
		statusError, counts[statusError],
	)

	// Each name once, in the statement's order; 0 tells the commit to forget
	// a name learned earlier (see statementNames).
	printed := statementNames(st, holdIns)
	var names []aliasRequest
	holdings := make([]holdingResponse, len(st.Holdings))
	for i, hold := range st.Holdings {
		if id, ok := printed[hold.Name]; ok {
			names = append(names, aliasRequest{Symbol: hold.Name, InstrumentID: id})
			delete(printed, hold.Name)
		}
		holdings[i] = holdingResponse{
			Symbol:   hold.Symbol,
			Quantity: hold.Quantity.String(),
			AvgCost:  hold.AvgCost.String(),
			Currency: hold.Currency,
		}
	}
	auth.WriteJSON(w, http.StatusOK, previewResponse{
		Broker:             broker,
		SHA256:             sha,
		PeriodStart:        st.PeriodStart,
		PeriodEnd:          st.PeriodEnd,
		Currency:           st.Currency,
		Warnings:           warnings,
		Rows:               preview,
		Holdings:           holdings,
		Reconciliation:     reconciliation,
		CashReconciliation: cash,
		Names:              names,
	})
}

// readUpload reads the form's file, checks it is a CSV for broker csv and a
// PDF otherwise, and returns it with its SHA-256. ok is false once it has
// written the error, also the already_imported one of a file the user
// imported before.
func (h *handler) readUpload(w http.ResponseWriter, r *http.Request, userID int64, isCSV bool) (data []byte, sha string, ok bool) {
	file, _, err := r.FormFile("file")
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "file: required")
		return nil, "", false
	}
	defer file.Close()
	data, err = io.ReadAll(file)
	if err != nil {
		writeUploadError(w, err)
		return nil, "", false
	}
	switch {
	case isCSV && bytes.HasPrefix(data, pdfMagic):
		auth.WriteError(w, http.StatusBadRequest, "file: not a CSV")
		return nil, "", false
	case !isCSV && !bytes.HasPrefix(data, pdfMagic):
		auth.WriteError(w, http.StatusBadRequest, "file: not a PDF")
		return nil, "", false
	}
	sum := sha256.Sum256(data)
	sha = hex.EncodeToString(sum[:])

	// The same file in the same portfolio is already a month of transactions;
	// the client offers the undo instead of a second copy. In another
	// portfolio it is the same account counted twice.
	switch imp, err := h.imports.BySHA256(r.Context(), userID, sha); {
	case err == nil:
		h.writeAlreadyImported(w, r, userID, imp)
		return nil, "", false
	case !errors.Is(err, ErrNotFound):
		auth.WriteInternalError(w, r, err)
		return nil, "", false
	}
	return data, sha, true
}

// parseUpload reads data's rows and parses them with broker's parser, or
// with every PDF parser for broker auto, returning the broker that parsed
// it. ok is false once it has written the error.
func parseUpload(w http.ResponseWriter, r *http.Request, data []byte, broker string) (string, Statement, bool) {
	var rows [][]string
	var err error
	isCSV, auto := broker == "csv", broker == "auto"
	if isCSV {
		// Like the PDF library's, the CSV reader's message quotes the file,
		// so it is neither logged nor returned.
		if rows, err = csvRecords(data); errors.Is(err, errTooManyRows) {
			auth.WriteError(w, http.StatusBadRequest, "file: "+err.Error())
			return "", Statement{}, false
		} else if err != nil {
			auth.WriteError(w, http.StatusBadRequest, "file: not a readable CSV")
			return "", Statement{}, false
		}
	} else if rows, err = pdftext.Rows(r.Context(), bytes.NewReader(data), int64(len(data))); err != nil {
		// The library's message can quote the document, so it is neither
		// logged nor returned.
		auth.WriteError(w, http.StatusBadRequest, "file: not a readable PDF")
		return "", Statement{}, false
	}
	var st Statement
	if auto {
		broker, st, err = Detect(rows)
	} else {
		st, err = Parsers[broker](rows)
	}
	if err != nil {
		// A parser returns only fixed strings: "not a statement of this
		// broker", or a CSV's "no rows" (ADR 0006); Detect "not a
		// supported statement".
		auth.WriteError(w, http.StatusBadRequest, "file: "+err.Error())
		return "", Statement{}, false
	}
	// Each row costs the preview instrument lookups, and a PDF parser has no
	// cap of its own.
	if len(st.Trades)+len(st.Dividends)+len(st.Cash)+len(st.Holdings)+len(st.Skipped)+len(st.Errors) > maxStatementRows {
		auth.WriteError(w, http.StatusBadRequest, "file: "+errTooManyRows.Error())
		return "", Statement{}, false
	}
	return broker, st, true
}

// cp1254High is Windows-1254's 0x80–0x9F, U+FFFD where it defines none.
// From 0xA0 on it is Latin-1 but for six letters, see fromWindows1254.
var cp1254High = []rune("€\uFFFD‚ƒ„…†‡ˆ‰Š‹Œ\uFFFD\uFFFD\uFFFD\uFFFD‘’“”•–—˜™š›œ\uFFFD\uFFFDŸ")

// fromWindows1254 decodes data, which Excel saves in a Turkish locale as
// plain "CSV", from Windows-1254 to UTF-8.
func fromWindows1254(data []byte) []byte {
	out := make([]byte, 0, len(data)+len(data)/4)
	for _, b := range data {
		r := rune(b)
		switch {
		case b >= 0x80 && b < 0xa0:
			r = cp1254High[b-0x80]
		case b == 0xd0:
			r = 'Ğ'
		case b == 0xdd:
			r = 'İ'
		case b == 0xde:
			r = 'Ş'
		case b == 0xf0:
			r = 'ğ'
		case b == 0xfd:
			r = 'ı'
		case b == 0xfe:
			r = 'ş'
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

// csvRecords splits an uploaded CSV into records. A spreadsheet saved in a
// Turkish locale separates with ";" because "," is its decimal point, so the
// separator is ";" when the first line has one and no ",". Rows may differ in
// length and a stray quote is read as text: the parser reports a bad row, the
// reader does not refuse the file over it. It reads record by record and stops
// past the header and maxStatementRows records with errTooManyRows. A file that
// is not UTF-8 is read as Windows-1254.
func csvRecords(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		data = fromWindows1254(data)
	}
	r := csv.NewReader(bytes.NewReader(data))
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.ContainsRune(first, ';') && !bytes.ContainsRune(first, ',') {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var records [][]string
	for {
		record, err := r.Read()
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if len(records) > maxStatementRows {
			return nil, errTooManyRows
		}
		records = append(records, record)
	}
}

// holdingInstruments resolves each of st's holdings, in order, to an
// instrument by the rule a trade row follows short of creating one: the
// symbol in the holding's market, else the instrument userID picked for it
// in an earlier commit of broker or in this preview (picks).
// The market is the statement's
// when it names one, else the holding's currency's (ADR 0006). A holding
// that resolves to none gets the zero Instrument.
func (h *handler) holdingInstruments(ctx context.Context, userID int64, broker string, picks map[string]instrument.Instrument, st Statement) ([]instrument.Instrument, error) {
	out := make([]instrument.Instrument, len(st.Holdings))
	for i, hold := range st.Holdings {
		market := st.Market
		if market == "" {
			market = marketForCurrency[hold.Currency]
		}
		in, err := h.instruments.BySymbol(ctx, market, hold.Symbol)
		if errors.Is(err, instrument.ErrNotFound) {
			in, err = h.aliasInstrument(ctx, userID, broker, picks, hold.Symbol)
		}
		if errors.Is(err, instrument.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

// periodEnd folds txs dated up to period_end and the preview's ok rows that
// are not flagged as duplicates through the holdings engine, and returns the
// summary with the instruments it names: what the portfolio would hold at
// period_end once a plain commit books this preview,
// splits included.
func (h *handler) periodEnd(ctx context.Context, txs []transaction.Transaction, st Statement, preview []previewRow) (holdings.Summary, map[int64]instrument.Instrument, error) {
	// Existing rows dated after the period are not in the broker's snapshot,
	// and neither are proposed ones: the comparison is as of period_end.
	applied := make([]transaction.Transaction, 0, len(txs)+len(preview))
	for _, t := range txs {
		// List's split rows are added again below, for the preview's rows
		// too.
		if t.Date <= st.PeriodEnd && t.Type != transaction.TypeSplit {
			applied = append(applied, t)
		}
	}
	for _, row := range preview {
		// An ok row either names an instrument or is a cash movement, which
		// the cash comparison needs. A row flagged as a duplicate is
		// left out, as the page leaves it unchecked: the comparison is of the
		// rows a plain commit books. So is a closing sell.
		if row.Status != statusOK || row.DuplicateOf != "" || row.Closing || row.Date > st.PeriodEnd {
			continue
		}
		applied = append(applied, transaction.Transaction{
			InstrumentID: row.InstrumentID,
			Type:         row.Type,
			Date:         row.Date,
			Quantity:     orZero(row.Quantity),
			Price:        orZero(row.Price),
			Amount:       orZero(row.Amount),
			Fee:          orZero(row.Fee),
			Currency:     row.Currency,
		})
	}
	// The split rows List returns once the preview is committed: each
	// instrument's splits dated after its first row, up to period_end and
	// today (ADR 0008). A preview's first row of an instrument can be its
	// first row at all, such as an opening buy before an in-period split.
	first := map[int64]string{}
	for _, t := range applied {
		if t.InstrumentID == nil {
			continue
		}
		if d, ok := first[*t.InstrumentID]; !ok || t.Date < d {
			first[*t.InstrumentID] = t.Date
		}
	}
	splits, err := h.splits.ListMany(ctx, slices.Collect(maps.Keys(first)))
	if err != nil {
		return holdings.Summary{}, nil, err
	}
	today := price.Today()
	for id, rows := range splits {
		for _, s := range rows {
			if s.Date > first[id] && s.Date <= st.PeriodEnd && s.Date <= today {
				applied = append(applied, transaction.Transaction{InstrumentID: &id, Type: transaction.TypeSplit, Date: s.Date, Quantity: s.Ratio})
			}
		}
	}
	// A proposed bonus row (a buy at price 0) books a split, as a stored
	// one does.
	applied, err = transaction.DropBookedSplits(applied)
	if err != nil {
		return holdings.Summary{}, nil, err
	}

	seen := map[int64]bool{}
	var ids []int64
	for _, t := range applied {
		if t.InstrumentID != nil && !seen[*t.InstrumentID] {
			seen[*t.InstrumentID] = true
			ids = append(ids, *t.InstrumentID)
		}
	}
	instruments, err := h.instruments.ByIDs(ctx, ids)
	if err != nil {
		return holdings.Summary{}, nil, err
	}
	currencyOf := make(map[int64]string, len(instruments))
	for id, in := range instruments {
		currencyOf[id] = in.Currency
	}

	// No prices are looked up: quantities and cash are all this compares,
	// and the proposed rows carry no id, so they sort first among their
	// date's rows -- which changes average cost but never the quantity a
	// position or the cash ends on.
	return holdings.Compute(applied, nil, currencyOf), instruments, nil
}

// reconcile compares the statement's period-end snapshot and cash balance
// with the positions and cash the portfolio would hold at period_end once
// the rows this preview starts checked are committed. Nothing is written:
// the transactions are folded through the holdings engine in memory, the
// same engine the summary endpoint runs, so position and cash maths live in
// exactly one place.
func (h *handler) reconcile(ctx context.Context, txs []transaction.Transaction, st Statement, holdIns []instrument.Instrument, preview []previewRow) ([]reconcileEntry, *cashReconcileEntry, error) {
	// A statement with no snapshot and no balance, such as a CSV, has
	// nothing to compare against: listing every position it is "silent
	// about" would flag the whole portfolio. A Midas statement
	// only lacks a balance when it lacks its currency too, and then lists no
	// silent positions either, so this changes nothing for it; one that
	// prints a balance but no positions is still compared.
	if len(st.Holdings) == 0 && st.CashBalance == nil {
		return []reconcileEntry{}, nil, nil
	}
	summary, instruments, err := h.periodEnd(ctx, txs, st, preview)
	if err != nil {
		return nil, nil, err
	}
	computed := map[int64]decimal.Decimal{}
	for _, p := range summary.Positions {
		computed[p.InstrumentID] = p.Quantity
	}

	// holdIns[i] is holding i's instrument (holdingInstruments), so a holding
	// named by a remembered pick compares with that instrument's position.
	// One that resolves to none is compared with nothing held.
	entries := []reconcileEntry{}
	mentioned := map[int64]bool{}
	for i, hold := range st.Holdings {
		var quantity decimal.Decimal
		if id := holdIns[i].ID; id != 0 {
			mentioned[id] = true
			quantity = computed[id]
		}
		stated := hold.Quantity.String()
		entries = append(entries, reconcileEntry{
			Symbol:            hold.Symbol,
			InstrumentID:      holdIns[i].ID,
			StatementQuantity: &stated,
			PortfolioQuantity: quantity.String(),
			Match:             hold.Quantity.Equal(quantity),
		})
	}

	// Positions the statement is silent about. Only the market the statement
	// covers is listed: it says nothing about the others, so a BIST position
	// is not a mismatch of a USD statement, nor of a TRY fund statement
	// whose Market is TEFAS.
	market := st.Market
	if market == "" {
		market = marketForCurrency[st.Currency]
	}
	for _, p := range summary.Positions {
		in := instruments[p.InstrumentID]
		if in.Market != market || mentioned[in.ID] {
			continue
		}
		entries = append(entries, reconcileEntry{
			Symbol:            in.Symbol,
			InstrumentID:      in.ID,
			PortfolioQuantity: p.Quantity.String(),
		})
	}

	var cash *cashReconcileEntry
	if st.CashBalance != nil && st.Currency != "" {
		held := summary.Cash[st.Currency]
		cash = &cashReconcileEntry{
			Currency:        st.Currency,
			StatementAmount: st.CashBalance.String(),
			PortfolioAmount: held.String(),
			Match:           st.CashBalance.Sub(held).Abs().LessThanOrEqual(cashTolerance),
		}
	}
	return entries, cash, nil
}

// Opening positions: the error code of an opening buy no price
// was found for, and how many instruments one preview may fetch prices for.
const (
	codeOpeningPrice  = "opening_price"
	codeClosingPrice  = "closing_price"
	maxOpeningFetches = 20
)

// proposeOpenings puts the opening rows in front of preview: what the account
// held before period_start, which the first statement of an account lists
// only as month-end balances. An instrument gets an opening buy
// when the statement holds more of it at period_end (0 when it does not list
// it) than the portfolio would with this preview's ok rows, and the
// portfolio has no transaction of it dated before period_start; one with
// earlier transactions is an ordinary mismatch. A split after that day
// scales the buy once committed, so its quantity is the shortfall divided
// by the splits' ratio and a price the statement prints is multiplied by it.
// The buy is dated the day
// before period_start and priced with the average cost the statement prints
// for the holding when the preview has no buy of it (CostPrice),
// else the stored price on or before
// that date, for a BIST instrument after syncing the bulletin of the last
// weekday on or before it (once per preview, as for an unknown symbol),
// else the one an on-demand fetch stores (for at most
// maxOpeningFetches instruments), else the statement's own period-end price
// (ApproximatePrice); with none it is an error row coded opening_price for
// the user to price. One deposit per currency, on the same day, pays for the
// priced buys so they do not push cash negative; it is rounded up to the
// cent, as an amount takes at most 9 fraction digits. A statement without holdings or a cash balance, such as a
// CSV, is not compared at all (see reconcile), and a symbol that resolves to
// no instrument gets nothing. A preview that still has a buy or sell of an
// unknown instrument gets nothing at all: that row may belong to an
// instrument whose holding does resolve (a Garanti fund row named by its
// name, its holding by its code), so no shortfall is known until the user
// resolves it. Rows of an unknown symbol whose units net to zero over the
// preview, such as a money-market fund bought and sold within the month,
// cannot change a period-end holding and do not block.
// An instrument the portfolio holds more of at period_end than the statement
// lists, and held before period_start, gets a closing sell of the
// difference instead, divided by the splits' ratio as an opening buy's
// shortfall is, at most what it held the day before period_start,
// dated and priced like an opening buy but never at the average cost; with
// no price it is an error row coded closing_price. It needs a statement that
// lists holdings, is keyed close-<instrument>, follows the opening rows and
// starts unchecked (Closing). One withdrawal per currency, also
// Closing and keyed close-cash-<currency>, follows them with the priced
// sells' proceeds rounded down to the cent; the page books it in step with
// the sells it checks.
// An opening row is keyed by its instrument or currency, not its place, so
// a preview requested again with a pick that adds one keeps the keys of the
// others, which the page keeps the user's edits by.
func (h *handler) proposeOpenings(ctx context.Context, req requester, txs []transaction.Transaction, st Statement, holdIns []instrument.Instrument, preview []previewRow) ([]previewRow, error) {
	if len(st.Holdings) == 0 && st.CashBalance == nil || unresolvedTrades(preview) {
		return preview, nil
	}
	start, err := time.Parse(time.DateOnly, st.PeriodStart)
	if err != nil {
		// The period warning already says the commit refuses it.
		return preview, nil
	}
	r := &openingRun{h: h, ctx: ctx, userID: req.userID, txs: txs, st: st, preview: preview, date: start.AddDate(0, 0, -1).Format(time.DateOnly)}
	summary, instruments, err := h.periodEnd(ctx, txs, st, preview)
	if err != nil {
		return nil, err
	}
	r.held = positionQuantities(summary)
	// What the portfolio holds on the day before the period, which a
	// closing sell on that day cannot exceed.
	dayBefore, _, err := h.periodEnd(ctx, txs, Statement{PeriodEnd: r.date}, nil)
	if err != nil {
		return nil, err
	}
	r.before = positionQuantities(dayBefore)
	r.earlier = earlierInstruments(txs, st.PeriodStart)
	cands := candidates(st, holdIns, preview, summary, instruments)
	if r.ratio, err = r.splitRatios(cands); err != nil {
		return nil, err
	}
	defer r.startPricing(req.client, start)()
	closings, openings, err := r.rows(cands)
	if err != nil {
		return nil, err
	}
	// One withdrawal per currency, after the closing sells, takes their
	// proceeds out again, rounded down to the cent so cash never goes
	// negative: the sales happened in months not imported, and so did
	// whatever the money went to.
	withdrawals, err := r.cashRows(transaction.TypeWithdrawal, r.proceeds, decimal.Decimal.RoundFloor)
	if err != nil {
		return nil, err
	}
	closings = append(closings, withdrawals...)
	if len(openings) == 0 {
		return append(closings, preview...), nil
	}
	cash, err := r.cashRows(transaction.TypeDeposit, r.deposits, decimal.Decimal.RoundCeil)
	if err != nil {
		return nil, err
	}
	return append(append(append(cash, openings...), closings...), preview...), nil
}

// unresolvedTrades reports whether preview still has a buy or sell of an
// unknown instrument whose symbol's units do not net to zero, or one whose
// quantity does not read, which proposeOpenings waits on.
func unresolvedTrades(preview []previewRow) bool {
	unknown := map[string]decimal.Decimal{}
	for _, row := range preview {
		trade := row.Type == transaction.TypeBuy || row.Type == transaction.TypeSell
		if !trade || (row.Status != statusUnknownInstrument && !row.UnknownInstrument) {
			continue
		}
		if row.Quantity == nil {
			return true
		}
		q := *row.Quantity
		if row.Type == transaction.TypeSell {
			q = q.Neg()
		}
		unknown[row.Symbol] = unknown[row.Symbol].Add(q)
	}
	for _, net := range unknown {
		if !net.IsZero() {
			return true
		}
	}
	return false
}

// positionQuantities maps each of summary's positions to its quantity.
func positionQuantities(summary holdings.Summary) map[int64]decimal.Decimal {
	q := map[int64]decimal.Decimal{}
	for _, p := range summary.Positions {
		q[p.InstrumentID] = p.Quantity
	}
	return q
}

// earlierInstruments holds the instruments of txs with a transaction dated
// before periodStart.
func earlierInstruments(txs []transaction.Transaction, periodStart string) map[int64]bool {
	earlier := map[int64]bool{}
	for _, t := range txs {
		if t.InstrumentID != nil && t.Date < periodStart {
			earlier[*t.InstrumentID] = true
		}
	}
	return earlier
}

// candidate is an instrument proposeOpenings may open or close: what the
// statement holds of it, its period-end price and its average cost, the
// last two zero when the statement prints none fit to price it with.
type candidate struct {
	in                  instrument.Instrument
	stated, price, cost decimal.Decimal
}

// candidates are the instruments the statement holds, in its order, then
// the positions of its market it is silent about, the same two lists
// reconcile compares.
func candidates(st Statement, holdIns []instrument.Instrument, preview []previewRow, summary holdings.Summary, instruments map[int64]instrument.Instrument) []candidate {
	// The average cost a statement prints at period end includes its own
	// buys of the period, so it is no price for the opening alone.
	bought := map[int64]bool{}
	for _, row := range preview {
		if row.Type == transaction.TypeBuy && row.InstrumentID != nil {
			bought[*row.InstrumentID] = true
		}
	}
	var cands []candidate
	seen := map[int64]bool{}
	for i, hold := range st.Holdings {
		in := holdIns[i]
		if in.ID == 0 || seen[in.ID] {
			continue
		}
		seen[in.ID] = true
		c := candidate{in: in, stated: hold.Quantity}
		// A unit price in another currency, such as a Garanti
		// foreign-currency fund's USD price of a TRY fund, is no
		// price for the instrument.
		if hold.Currency == in.Currency {
			c.price = hold.Price
			if !bought[in.ID] {
				c.cost = hold.AvgCost
			}
		}
		cands = append(cands, c)
	}
	market := st.Market
	if market == "" {
		market = marketForCurrency[st.Currency]
	}
	for _, p := range summary.Positions {
		if in := instruments[p.InstrumentID]; in.Market == market && !seen[in.ID] {
			cands = append(cands, candidate{in: in})
		}
	}
	return cands
}

// currencyTotals sums amounts per currency, keeping the currencies in the
// order they first came.
type currencyTotals struct {
	order []string
	sum   map[string]decimal.Decimal
}

func (t *currencyTotals) add(currency string, amount decimal.Decimal) {
	if _, ok := t.sum[currency]; !ok {
		if t.sum == nil {
			t.sum = map[string]decimal.Decimal{}
		}
		t.order = append(t.order, currency)
	}
	t.sum[currency] = t.sum[currency].Add(amount)
}

// openingRun is the state of one proposeOpenings call its row builders
// share.
type openingRun struct {
	h       *handler
	ctx     context.Context
	userID  int64
	txs     []transaction.Transaction
	st      Statement
	preview []previewRow
	// date is the day before period_start, every proposed row's date.
	date string
	// held is what the portfolio holds at period_end with the preview's ok
	// rows, before what it holds on date; ratio is the splits' ratio after
	// date per instrument; earlier holds the instruments with a transaction
	// before period_start.
	held, before, ratio map[int64]decimal.Decimal
	earlier             map[int64]bool
	// proceeds and deposits total the priced closing sells and opening
	// buys per currency.
	proceeds, deposits currencyTotals
	// The pricing state startPricing sets up for priceOf.
	fetchCtx     context.Context
	session      *lookupSession
	bulletinDate string
	fetches      int
}

// splitRatios is what one unit bought on date holds at period_end per
// candidate: the ratio of the splits after date that the committed opening
// is scaled by, as the statement's holding already is.
func (r *openingRun) splitRatios(cands []candidate) (map[int64]decimal.Decimal, error) {
	probes := slices.Clone(r.preview)
	one, zero := decimal.NewFromInt(1), decimal.Zero
	for _, c := range cands {
		id := c.in.ID
		probes = append(probes, previewRow{
			Status: statusOK, Type: transaction.TypeBuy, Date: r.date, InstrumentID: &id,
			Quantity: &one, Price: &one, Amount: &one, Fee: &zero, Currency: c.in.Currency,
		})
	}
	probed, _, err := r.h.periodEnd(r.ctx, r.txs, r.st, probes)
	if err != nil {
		return nil, err
	}
	ratio := map[int64]decimal.Decimal{}
	for _, p := range probed.Positions {
		ratio[p.InstrumentID] = p.Quantity.Sub(r.held[p.InstrumentID])
	}
	return ratio, nil
}

// startPricing sets up priceOf's fetches and bulletin sync, and returns the
// cancel of their deadline. One deadline for all the fetches, not one each:
// maxOpeningFetches hung fetches would outlast a proxy's timeout.
// An instrument left unfetched is priced as if its fetch found nothing. The
// bulletin sync shares that deadline. A weekend has no bulletin.
func (r *openingRun) startPricing(client string, start time.Time) context.CancelFunc {
	fetchCtx, cancel := context.WithTimeout(r.ctx, transaction.OnDemandFetchTimeout)
	r.fetchCtx = fetchCtx
	r.session = &lookupSession{ctx: fetchCtx, client: client, synced: map[string]bool{}}
	day := start.AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	r.bulletinDate = day.Format(time.DateOnly)
	return cancel
}

// priceOf prices row, c's opening or closing row, by the rule of
// proposeOpenings; an error row coded opening_price or closing_price when
// nothing does.
func (r *openingRun) priceOf(c candidate, row *previewRow) (decimal.Decimal, bool, error) {
	id := c.in.ID
	var p decimal.Decimal
	var found bool
	var err error
	if c.cost.IsPositive() {
		p, found, row.CostPrice = c.cost, true, true
	} else {
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	// The on-demand fetch only queues a backfill for an old BIST date,
	// so the bulletin of the last weekday on or before it is synced
	// here, once for all the BIST openings.
	if err == nil && !found && c.in.Market == "BIST" && r.h.syncBulletinOnce(r.session, r.bulletinDate) {
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	if err == nil && !found && r.h.onDemandFetch != nil && r.fetches < maxOpeningFetches && r.fetchCtx.Err() == nil {
		r.fetches++
		transaction.EnsurePrice(r.fetchCtx, r.h.onDemandFetch, c.in, transaction.Transaction{InstrumentID: &id, Date: r.date})
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	if err != nil {
		return p, false, err
	}
	if !found && c.price.IsPositive() {
		p, found, row.ApproximatePrice = c.price, true, true
	}
	if !found {
		row.Status, row.Code, row.Message = statusError, codeOpeningPrice, "no price for the opening position"
		if row.Closing {
			row.Code, row.Message = codeClosingPrice, "no price for the closing position"
		}
	}
	return p, found, nil
}

// tradeRow is the opening buy or, of kind sell, the closing sell of qty
// units of c, keyed open-<instrument> or close-<instrument>, priced by
// priceOf; the amount of a row still ok after checkRow is added to totals.
// The amount is quantity x price exactly, which the commit takes whatever
// its digits.
func (r *openingRun) tradeRow(c candidate, kind string, qty decimal.Decimal, totals *currencyTotals) (previewRow, error) {
	id, fee := c.in.ID, decimal.Zero
	row := previewRow{
		Key: fmt.Sprintf("open-%d", id), Status: statusOK, Type: kind, Date: r.date,
		Symbol: c.in.Symbol, Market: c.in.Market, InstrumentID: &id, Quantity: &qty,
		Fee: &fee, Currency: c.in.Currency, Opening: true,
	}
	if kind == transaction.TypeSell {
		row.Key, row.Opening, row.Closing = fmt.Sprintf("close-%d", id), false, true
	}
	p, found, err := r.priceOf(c, &row)
	if err != nil || !found {
		return row, err
	}
	amount := qty.Mul(p)
	row.Price, row.Amount = &p, &amount
	checkRow(&row, c.in)
	if row.Status == statusOK {
		totals.add(row.Currency, amount)
	}
	return row, nil
}

// rows is the closing sells and the opening buys of cands, in their order.
func (r *openingRun) rows(cands []candidate) (closings, openings []previewRow, err error) {
	// Never nil: a preview with no rows at all is still a JSON array.
	closings, openings = []previewRow{}, []previewRow{}
	for _, c := range cands {
		row, ok, err := r.closingRow(c)
		if err == nil && !ok {
			row, ok, err = r.openingRow(c)
			if ok {
				openings = append(openings, row)
			}
		} else if ok {
			closings = append(closings, row)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return closings, openings, nil
}

// closingRow is c's closing sell, and true, when the portfolio holds more
// of it than the statement, held since before the period. The
// difference and the statement's price are post-split, the sell before the
// splits, as for an opening.
func (r *openingRun) closingRow(c candidate) (previewRow, bool, error) {
	id := c.in.ID
	excess, closePrice := r.held[id].Sub(c.stated), c.price
	if ratio := r.ratio[id]; ratio.IsPositive() && !ratio.Equal(decimal.NewFromInt(1)) {
		excess, closePrice = excess.DivRound(ratio, 9), closePrice.Mul(ratio)
	}
	if excess = decimal.Min(excess, r.before[id]); !excess.IsPositive() || !r.earlier[id] || len(r.st.Holdings) == 0 {
		return previewRow{}, false, nil
	}
	c.cost, c.price = decimal.Zero, closePrice
	row, err := r.tradeRow(c, transaction.TypeSell, excess, &r.proceeds)
	return row, true, err
}

// openingRow is c's opening buy, and true, when the statement holds more of
// it than the portfolio, which has no transaction of it before the period.
func (r *openingRun) openingRow(c candidate) (previewRow, bool, error) {
	id := c.in.ID
	shortfall := c.stated.Sub(r.held[id])
	if !shortfall.IsPositive() || r.earlier[id] {
		return previewRow{}, false, nil
	}
	// The shortfall and the statement's prices are post-split, the
	// opening before the splits; 9 fraction digits is what a quantity
	// takes.
	if ratio := r.ratio[id]; ratio.IsPositive() && !ratio.Equal(decimal.NewFromInt(1)) {
		shortfall = shortfall.DivRound(ratio, 9)
		c.price, c.cost = c.price.Mul(ratio), c.cost.Mul(ratio)
	}
	row, err := r.tradeRow(c, transaction.TypeBuy, shortfall, &r.deposits)
	return row, true, err
}

// cashRows is one cash row of kind per currency of totals, in their order,
// each with its total rounded to the cent by round: the open-cash-*
// deposits that pay for the priced opening buys, or the close-cash-*
// withdrawals that take the priced closing sells' proceeds out again.
func (r *openingRun) cashRows(kind string, totals currencyTotals, round func(decimal.Decimal, int32) decimal.Decimal) ([]previewRow, error) {
	rows := make([]previewRow, 0, len(totals.order))
	for _, cur := range totals.order {
		amount, fee := round(totals.sum[cur], 2), decimal.Zero
		row := previewRow{
			Key: "open-cash-" + cur, Status: statusOK, Type: kind, Date: r.date,
			Amount: &amount, Fee: &fee, Currency: cur, Opening: true,
		}
		if kind == transaction.TypeWithdrawal {
			row.Key, row.Opening, row.Closing = "close-cash-"+cur, false, true
		}
		checkRow(&row, instrument.Instrument{})
		rows = append(rows, row)
	}
	return rows, nil
}

// storedPrice is the price of instrumentID on or before date: userID's
// manual price or the fetched one, the manual one winning on the same date,
// else the newer, as the summary merges them.
func (h *handler) storedPrice(ctx context.Context, userID, instrumentID int64, date string) (decimal.Decimal, bool, error) {
	winners, err := holdings.LatestCloses(ctx, holdings.PriceSources{Prices: h.prices, Manual: h.manualPrices}, userID, []int64{instrumentID}, date)
	if err != nil {
		return decimal.Decimal{}, false, err
	}
	won, ok := winners[instrumentID]
	return won.Close, ok, nil
}

// orZero is a preview row's number, zero when the row has none.
func orZero(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}

// text is a preview row's number as the request the import page sends for
// the row carries it, "" when the row has none.
func text(d *decimal.Decimal) *string {
	s := ""
	if d != nil {
		s = d.String()
	}
	return &s
}

// lookupSession is one request's state for the instrument lookups a preview
// makes: createFund's TEFAS lookups and bulletinInstrument's BIST bulletin
// syncs.
type lookupSession struct {
	// ctx is the preview's single lookup deadline: the lookups
	// and syncs run on it, and none starts once it is done.
	ctx    context.Context
	client string // the requester's IP, which the lookups count against
	userID int64
	broker string
	picks  map[string]instrument.Instrument // the picks made in this preview
	// asked holds the fund codes this request has asked TEFAS about.
	asked map[string]bool
	// synced holds the dates this request has synced the BIST bulletin of,
	// true for those it fetched.
	synced map[string]bool
	// memo holds this request's instrument lookups by key, not-found ones
	// too, so a symbol on thousands of rows is looked up once.
	// It is cleared whenever the request may have added instruments.
	memo map[string]memoEntry
	// funds is the TEFAS fund list nameInstrument matches names against,
	// read on first use and dropped with memo when createFund adds a fund.
	funds []namedFund
}

// memoEntry is one lookup memo holds: an instrument, or
// instrument.ErrNotFound.
type memoEntry struct {
	in  instrument.Instrument
	err error
}

// cached returns the lookup memo holds under key, else runs look and
// remembers what it found or did not; any other error is not remembered.
func (s *lookupSession) cached(key string, look func() (instrument.Instrument, error)) (instrument.Instrument, error) {
	if e, ok := s.memo[key]; ok {
		return e.in, e.err
	}
	in, err := look()
	if err == nil || errors.Is(err, instrument.ErrNotFound) {
		if s.memo == nil {
			s.memo = map[string]memoEntry{}
		}
		s.memo[key] = memoEntry{in, err}
	}
	return in, err
}

// bySymbol is instruments.BySymbol through s's memo.
func (h *handler) bySymbol(ctx context.Context, s *lookupSession, market, symbol string) (instrument.Instrument, error) {
	return s.cached("symbol\x00"+market+"\x00"+symbol, func() (instrument.Instrument, error) {
		return h.instruments.BySymbol(ctx, market, symbol)
	})
}

// requester is who a preview is for: the client IP its TEFAS lookups and
// bulletin syncs count against, and the user and broker that key the
// stored picks.
type requester struct {
	client string
	userID int64
	broker string
}

// previewRows turns a parsed statement into the rows the preview shows, in a
// stable order and with a stable key per row, so the client can address a row
// it wants to edit or uncheck. req's user and broker key the user's stored
// picks, and picks are the ones made in this preview. batch are
// fund names to take as learned for this request, a stored one of the same
// name giving way.
func (h *handler) previewRows(ctx context.Context, req requester, picks map[string]instrument.Instrument, batch map[string]int64, st Statement) ([]previewRow, error) {
	rows := []previewRow{}
	lookupCtx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	s := &lookupSession{ctx: lookupCtx, client: req.client, userID: req.userID, broker: req.broker, picks: picks, asked: map[string]bool{}, synced: map[string]bool{}}
	learned, err := h.imports.LearnedNames(ctx, req.userID, req.broker)
	if err != nil {
		return nil, err
	}
	maps.Copy(learned, batch)
	// This statement's own balances are newer than any learned name. The
	// parser maps a name that matches one of their names to its code, so a
	// name that still matches one matches two funds and resolves nothing.
	// A holding's code is a code, not a cut name, so no learned
	// name resolves it either. Every own name maps to 1, so learnedName
	// reports any match of them.
	own := map[string]int64{}
	codes := map[string]bool{}
	for _, hold := range st.Holdings {
		codes[hold.Symbol] = true
		if hold.Name != "" {
			own[hold.Name] = 1
		}
	}
	learnedFor := func(symbol string) map[string]int64 {
		if codes[symbol] || learnedName(symbol, own) != 0 {
			return nil
		}
		return learned
	}
	for i, t := range st.Trades {
		row := previewRow{
			Key: fmt.Sprintf("trade-%d", i), Type: t.Side, Date: t.Date, Symbol: t.Symbol,
			Quantity: &t.Quantity, Price: &t.Price,
			Amount: &t.Amount, Fee: &t.Fee, Currency: t.Currency, Market: t.Market,
		}
		in, err := h.resolveInstrument(ctx, s, learnedFor(t.Symbol), &row)
		if err != nil {
			return nil, err
		}
		checkRow(&row, in)
		rows = append(rows, row)
	}
	for i, d := range st.Dividends {
		row := previewRow{
			Key: fmt.Sprintf("div-%d", i), Type: "dividend", Date: d.Date, Symbol: d.Symbol,
			Amount: &d.Gross, Fee: &d.Tax, Currency: d.Currency, Market: d.Market,
		}
		in, err := h.resolveInstrument(ctx, s, learnedFor(d.Symbol), &row)
		if err != nil {
			return nil, err
		}
		checkRow(&row, in)
		rows = append(rows, row)
	}
	// A cash row's kind is already a transaction type: deposit, withdrawal,
	// dividend (interest) or fee. Its fee is the parser's, zero when it has
	// none; a fee row's own amount is the fee. A cash row has no instrument
	// to rule out its currency, so the commit's own list does.
	// The description is masked here, not in the parsers, which classify
	// rows by the raw text.
	for i, c := range st.Cash {
		row := previewRow{
			Key: fmt.Sprintf("cash-%d", i), Status: statusOK, Type: c.Kind, Date: c.Date,
			Amount: &c.Amount, Fee: &c.Fee, Currency: c.Currency, Description: MaskIdentifiers(c.Description),
		}
		if !transaction.IsValidCurrency(c.Currency) {
			row.Status = statusUnsupportedCurrency
		}
		checkRow(&row, instrument.Instrument{})
		rows = append(rows, row)
	}
	// A statement prints its sections in its own order (Midas lists trades
	// before cash), so the rows built above are grouped by section rather
	// than dated. The user reviews a month chronologically, so sort them
	// here; the keys are already assigned, so selection and edits in the
	// client survive it.
	sortByDate(rows)

	for i, s := range st.Skipped {
		// The cells go out as raw: the reason alone ("fx conversion") does not
		// tell the user which row of their statement was dropped, and the
		// parser keeps no per-broker description field.
		rows = append(rows, previewRow{
			Key: fmt.Sprintf("skip-%d", i), Status: statusSkipped,
			Section: s.Section, Reason: s.Reason, Raw: maskCells(s.Row),
		})
	}
	for i, e := range st.Errors {
		// An unknown row's value is its description.
		rows = append(rows, previewRow{
			Key: fmt.Sprintf("err-%d", i), Status: statusError, Date: e.Date,
			Section: e.Section, Code: e.Code, Value: MaskIdentifiers(e.Value), Message: e.Message, Raw: maskCells(e.Row),
		})
	}
	return rows, nil
}

// maskCells returns the raw cells of a skipped or error row with their
// IBANs and account numbers masked, as a cash row's description is: a
// Midas deposit that did not execute still names the bank's IBAN.
func maskCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = MaskIdentifiers(c)
	}
	return out
}

// flagDuplicates sets DuplicateOf on each ok row that an earlier
// transaction, hand-entered or imported, already accounts for, so the page
// starts it unchecked and a plain commit does not book it twice.
// Imported ones count too: a CSV may repeat rows first imported
// from a Midas statement or another CSV, which no overlap warning of the
// same broker and account catches.
//
// A dividend of an instrument pairs with a hand-entered one through the
// window and one-to-one pairing the expected dividends panel uses to hide a
// suggestion (dividend.MatchRecorded): a dividend added from that panel is
// dated on the ex-date, a statement row on the payment date. An imported one
// is dated on the payment date too, so it pairs only on the same date,
// currency and amount; the window would pair a monthly payer's dividend with
// the previous statement's. A trade pairs with an
// earlier one of the same instrument, side, date and quantity, and a cash row
// (deposit, withdrawal, a dividend with no instrument) with one of the same
// type, date, currency and amount; each earlier transaction accounts for at
// most one row.
//
// txs is List's rows: its split rows pair with nothing, and a bonus buy it
// moved to its split's ex-date pairs on the date it is stored on.
func flagDuplicates(txs []transaction.Transaction, preview []previewRow) error {
	// Both lists are date-ascending: txs by List, preview by sortByDate.
	// Hand-entered dividends of an instrument, for the window.
	dividends := map[int64][]string{}
	// Exact-date candidates, keyed by type, date and instrument (0 for a
	// cash row); a match is removed so it cannot pair twice.
	type exactKey struct {
		typ, date    string
		instrumentID int64
	}
	exact := map[exactKey][]transaction.Transaction{}
	for _, t := range txs {
		if t.Type == transaction.TypeDividend && t.InstrumentID != nil && t.ImportID == nil {
			dividends[*t.InstrumentID] = append(dividends[*t.InstrumentID], t.Date)
			continue
		}
		var id int64
		if t.InstrumentID != nil {
			id = *t.InstrumentID
		}
		k := exactKey{t.Type, t.StoredDate(), id}
		exact[k] = append(exact[k], t)
	}

	rowsOf := map[int64][]int{}
	for i, row := range preview {
		if row.Status != statusOK {
			continue
		}
		trade := row.Type == transaction.TypeBuy || row.Type == transaction.TypeSell
		instrumentDividend := row.Type == transaction.TypeDividend && row.InstrumentID != nil
		var id int64
		if trade || instrumentDividend {
			if row.InstrumentID == nil {
				continue
			}
			id = *row.InstrumentID
		}
		k := exactKey{row.Type, row.Date, id}
		candidates := exact[k]
		for j, t := range candidates {
			var same bool
			if trade {
				same = t.Quantity.Equal(orZero(row.Quantity))
			} else {
				// The key's instrument already tells a cash row from an
				// imported dividend of an instrument.
				same = t.Currency == row.Currency && t.Amount.Equal(orZero(row.Amount))
			}
			if same {
				preview[i].DuplicateOf = t.StoredDate()
				exact[k] = append(candidates[:j:j], candidates[j+1:]...)
				break
			}
		}
		if instrumentDividend && preview[i].DuplicateOf == "" {
			rowsOf[id] = append(rowsOf[id], i)
		}
	}

	for id, idx := range rowsOf {
		if len(dividends[id]) == 0 {
			continue
		}
		dates := make([]string, len(idx))
		for k, i := range idx {
			dates[k] = preview[i].Date
		}
		matched, err := dividend.MatchRecorded(dates, dividends[id])
		if err != nil {
			return err
		}
		for k, i := range idx {
			preview[i].DuplicateOf = matched[k]
		}
	}
	return nil
}

// sortByDate orders importable rows oldest first, keeping the statement's
// relative order within a date. Dates are YYYY-MM-DD, so a string compare is
// a date compare. Skipped and error rows are appended after this runs: they
// have nothing to import and often no date at all, so they stay at the
// bottom.
func sortByDate(rows []previewRow) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Date < rows[j].Date })
}

// resolveInstrument sets the status of a row that names a symbol: ok with the
// instrument id when the symbol is found, unknown_instrument when it is not.
// A row with a market hint is looked up in that market only. One without is
// looked up in the market its currency implies, then in that currency's other
// markets, where it resolves only when exactly one of them knows the symbol.
// A row with no currency (an empty CSV currency cell) is looked up like a TRY
// row and takes the currency of the instrument it resolves to, TRY when none.
// A currency that contradicts the instrument is left for checkRow,
// which says so with the commit's own message.
// The import creates no instrument on the statement's word, so an unknown or
// ambiguous symbol is the user's to fix in the preview (ADR 0006). The one
// exception is a TEFAS row's fund code that TEFAS itself confirms, which
// createFund adds. A BIST symbol, which may be a share delisted
// since, is looked up again after its date's bulletin is synced.
// Both ask on s, the request's lookup state. A symbol none of that
// resolves takes the instrument the user picked for it in an earlier commit
// of the same broker, if any, and says so in ResolvedBy: a real
// match always wins over a stored pick. A pick made in this preview resolves
// it the same way. With no pick either, it takes the instrument of
// the learned fund names it matches (see learnedName), else a garanti fund
// name the one TEFAS fund it is cut or abbreviated from (see nameInstrument),
// and says so in ResolvedBy. It returns the instrument
// of an ok row, else the zero Instrument. Every lookup goes through s's memo.
func (h *handler) resolveInstrument(ctx context.Context, s *lookupSession, learned map[string]int64, row *previewRow) (instrument.Instrument, error) {
	given := row.Currency
	if given == "" {
		row.Currency, row.CurrencyFallback = "TRY", true
	}
	market, ok := marketForCurrency[row.Currency]
	if !ok {
		row.Status = statusUnsupportedCurrency
		return instrument.Instrument{}, nil
	}
	if row.Market != "" {
		market = row.Market
	}
	in, err := h.bySymbol(ctx, s, market, row.Symbol)
	if errors.Is(err, instrument.ErrNotFound) && row.Market == "" {
		in, err = h.soleMatch(ctx, s, otherMarkets[row.Currency], row.Symbol, row.Currency)
	}
	if errors.Is(err, instrument.ErrNotFound) && row.Market == "TEFAS" {
		in, err = h.createFund(ctx, s, row.Symbol)
	}
	if errors.Is(err, instrument.ErrNotFound) && market == "BIST" {
		in, err = h.bulletinInstrument(ctx, s, row.Symbol, row.Date)
	}
	if errors.Is(err, instrument.ErrNotFound) {
		in, err = s.cached("alias\x00"+row.Symbol, func() (instrument.Instrument, error) {
			return h.aliasInstrument(ctx, s.userID, s.broker, s.picks, row.Symbol)
		})
		if err == nil {
			row.ResolvedBy = resolvedByAlias
			row.InstrumentSymbol, row.InstrumentName = in.Symbol, in.Name
		}
	}
	if errors.Is(err, instrument.ErrNotFound) {
		// learned is the same for every row of the symbol (see previewRows).
		in, err = s.cached("name\x00"+row.Market+"\x00"+row.Symbol, func() (instrument.Instrument, error) {
			return h.nameInstrument(ctx, s, learned, row)
		})
		if err == nil {
			row.ResolvedBy = resolvedByName
			row.InstrumentSymbol, row.InstrumentName = in.Symbol, in.Name
		}
	}
	if errors.Is(err, instrument.ErrNotFound) {
		row.Status = statusUnknownInstrument
		return instrument.Instrument{}, nil
	}
	if err != nil {
		return instrument.Instrument{}, err
	}
	if given == "" {
		row.Currency = in.Currency
	}
	row.Status, row.InstrumentID = statusOK, &in.ID
	return in, nil
}

// aliasInstrument returns the instrument userID picked for symbol: in this
// preview (picks), which wins, else in a statement of broker, or
// instrument.ErrNotFound when they picked none.
func (h *handler) aliasInstrument(ctx context.Context, userID int64, broker string, picks map[string]instrument.Instrument, symbol string) (instrument.Instrument, error) {
	if in, ok := picks[symbol]; ok {
		return in, nil
	}
	id, err := h.imports.Alias(ctx, userID, broker, symbol)
	if errors.Is(err, ErrNotFound) {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if err != nil {
		return instrument.Instrument{}, err
	}
	return h.instruments.Get(ctx, id)
}

// learnedName returns the instrument id of the learned names that equal
// symbol, start with it or that it starts with: the rule the garanti parser
// maps a movement to a balance by inside one statement, as statement names
// are cut to a fixed width. It returns 0 for no match, or when the matches
// name more than one instrument.
func learnedName(symbol string, learned map[string]int64) int64 {
	if symbol == "" {
		return 0
	}
	var match int64
	found := false
	for name, id := range learned {
		if strings.HasPrefix(name, symbol) || strings.HasPrefix(symbol, name) {
			if found && match != id {
				return 0
			}
			match, found = id, true
		}
	}
	return match
}

// statementNames is what st's balances say of each fund name they print:
// the instrument of its code, or 0 when they print it for two codes (a
// holding is one per code), like the parser's code() leaves such a name
// unresolved, or when its code resolves to no instrument. holdIns are st's
// holdings' instruments.
func statementNames(st Statement, holdIns []instrument.Instrument) map[string]int64 {
	out := map[string]int64{}
	for i, hold := range st.Holdings {
		if hold.Name == "" {
			continue
		}
		if _, seen := out[hold.Name]; seen {
			out[hold.Name] = 0
			continue
		}
		out[hold.Name] = holdIns[i].ID
	}
	return out
}

// checkRow runs an ok row through the commit's own validation
// (transaction.FromRequestWith), with the request the import page sends for
// it and in, the instrument the row resolved to, in place of a lookup,
// and turns a row the commit would refuse -- a zero quantity, a sell at
// price 0, a fee above a dividend -- into an error row with code "invalid"
// and the commit's "<field>: <message>" as its message. One such row would
// otherwise fail the whole commit. An unknown_instrument row gets
// the same rules short of the instrument lookup, so a price-0 sell is not
// passed off as ready once the user picks its instrument. Such a
// row keeps UnknownInstrument, so the user learns of both problems at once.
// The row's numbers go in as the strings the page sends: that
// validation is the commit's trust boundary, which the preview runs as is
// rather than repeating it on decimals.
func checkRow(row *previewRow, in instrument.Instrument) {
	if row.Status != statusOK && row.Status != statusUnknownInstrument {
		return
	}
	req := transaction.Request{
		InstrumentID: row.InstrumentID, Type: row.Type, Date: row.Date,
		Amount: text(row.Amount), Fee: text(row.Fee), Currency: &row.Currency,
	}
	if row.Quantity != nil {
		req.Quantity, req.Price = text(row.Quantity), text(row.Price)
	}
	var err error
	if row.Status == statusUnknownInstrument {
		var placeholder int64
		req.InstrumentID = &placeholder
		err = transaction.Validate(req)
	} else {
		_, _, err = transaction.FromRequestWith(req, func(id int64) (instrument.Instrument, error) {
			if id != in.ID {
				return instrument.Instrument{}, instrument.ErrNotFound
			}
			return in, nil
		})
	}
	if err != nil {
		row.UnknownInstrument = row.Status == statusUnknownInstrument
		row.Status, row.Code, row.Message = statusError, "invalid", err.Error()
	}
}

// createFund adds code as a TRY fund on TEFAS when lookupFund confirms it,
// named as TEFAS names it, and returns the instrument. Instruments are
// global, so only a fund-code-shaped symbol is asked about, never a name the
// statement printed. Each code is asked once per request (s.asked), at most
// maxFundLookups codes in all, and each lookup counts against s.client's
// fundLookupLimit, shared by every route that asks, unless it
// asked nothing (ErrFundLookupOff). A code not asked, not found
// or whose lookup failed is instrument.ErrNotFound; a
// failure is logged with the code only. A code not asked because the
// client's limit is spent is a fundLimitError, which is instrument.ErrNotFound
// as well. The lookup runs on s.ctx, and none is made once it is done;
// the store runs on ctx. Insert returns the existing row when
// another request added the code first.
func (h *handler) createFund(ctx context.Context, s *lookupSession, code string) (instrument.Instrument, error) {
	if h.lookupFund == nil || !fundCode.MatchString(code) || s.asked[code] || len(s.asked) >= maxFundLookups || s.ctx.Err() != nil {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if ok, retryAfter := h.fundLookups.Allow(s.client); !ok {
		return instrument.Instrument{}, fundLimitError(retryAfter)
	}
	s.asked[code] = true
	name, found, err := h.lookupFund(s.ctx, code)
	if errors.Is(err, ErrFundLookupOff) {
		h.fundLookups.Refund(s.client)
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if err != nil {
		slog.Default().Warn("tefas fund lookup failed", "code", code, "error", err.Error())
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if !found {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if name == "" {
		// The TEFAS fetcher renames it on its next run.
		name = code
	}
	id, err := h.instruments.Insert(ctx, "TEFAS", code, name, "TRY")
	if err != nil {
		return instrument.Instrument{}, err
	}
	clear(s.memo)
	s.funds = nil
	slog.Default().Info("tefas fund created from import", "code", code, "name", name)
	return h.instruments.Get(ctx, id)
}

// fundLimitError is createFund's error for a code not asked because the
// client's fundLookupLimit is spent; its value is the time until the limit
// resets. It unwraps to instrument.ErrNotFound, so the preview leaves the
// row unknown_instrument, and the fund route answers 429.
type fundLimitError time.Duration

func (fundLimitError) Error() string { return "fund lookup limit reached" }

func (fundLimitError) Unwrap() error { return instrument.ErrNotFound }

// bulletinInstrument syncs the BIST bulletin of date, which adds the symbols
// it lists as instruments, and looks symbol up again. A date
// syncBulletinOnce does not sync is instrument.ErrNotFound.
func (h *handler) bulletinInstrument(ctx context.Context, s *lookupSession, symbol, date string) (instrument.Instrument, error) {
	if !h.syncBulletinOnce(s, date) {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	return h.bySymbol(ctx, s, "BIST", symbol)
}

// syncBulletinOnce syncs the BIST bulletin of date and reports whether it
// ran without error. Each date is synced once per request (s.synced). Only
// a sync that fetched counts against maxBulletinSyncs, so dates whose
// bulletin is already stored do not use up the fetches a later date needs;
// a pause as a backfill makes goes before each one after the first. Each
// fetch also counts against s.client's bulletinSyncLimit; a sync
// that fetched nothing is refunded. The sync runs on s.ctx, and none starts
// once it is done. A failure is logged with the date only.
func (h *handler) syncBulletinOnce(s *lookupSession, date string) bool {
	d, err := time.Parse(time.DateOnly, date)
	if _, done := s.synced[date]; h.syncBulletin == nil || err != nil || done || s.ctx.Err() != nil {
		return false
	}
	fetches := 0
	for _, fetched := range s.synced {
		if fetched {
			fetches++
		}
	}
	if fetches >= maxBulletinSyncs {
		return false
	}
	if ok, _ := h.bulletinSyncs.Allow(s.client); !ok {
		return false
	}
	if fetches > 0 {
		time.Sleep(bist.RequestPause)
	}
	syncCtx, cancel := context.WithTimeout(s.ctx, bulletinSyncTimeout)
	s.synced[date], err = h.syncBulletin(syncCtx, d)
	cancel()
	// The sync may have added instruments the memo holds as not found.
	clear(s.memo)
	if !s.synced[date] {
		h.bulletinSyncs.Refund(s.client)
	}
	if err != nil {
		slog.Default().Warn("bist bulletin sync for import failed", "date", date, "error", err.Error())
		return false
	}
	return true
}

// soleMatch looks symbol up in each of markets and returns the instrument
// when exactly one market has it quoted in currency, else
// instrument.ErrNotFound. The lookups go through s's memo.
func (h *handler) soleMatch(ctx context.Context, s *lookupSession, markets []string, symbol, currency string) (instrument.Instrument, error) {
	var found []instrument.Instrument
	for _, market := range markets {
		in, err := h.bySymbol(ctx, s, market, symbol)
		if errors.Is(err, instrument.ErrNotFound) {
			continue
		}
		if err != nil {
			return instrument.Instrument{}, err
		}
		if in.Currency == currency {
			found = append(found, in)
		}
	}
	if len(found) != 1 {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	return found[0], nil
}

// overlapWarnings warns about importing the same period twice. An overlap is
// a warning and not an error: a quarterly statement over an imported month is
// legitimate (ADR 0006). The other currency's statement of it is the other
// account and gets no warning at all. The same statement again is
// flagged as same_period, which the commit refuses until the earlier import
// is undone.
func (h *handler) overlapWarnings(ctx context.Context, userID, portfolioID int64, broker string, st Statement) ([]warning, error) {
	overlapping, err := h.imports.Overlapping(ctx, userID, portfolioID, broker, st.PeriodStart, st.PeriodEnd)
	if err != nil {
		return nil, err
	}
	warnings := []warning{}
	for _, imp := range overlapping {
		if !imp.sameAccount(st.Currency) {
			continue
		}
		code, message := "overlap", "period overlaps an earlier import (%s – %s)"
		if imp.samePeriod(st.Currency, st.PeriodStart, st.PeriodEnd) {
			code, message = "same_period", "period already imported (%s – %s), undo that import first"
		}
		warnings = append(warnings, warning{
			Code:        code,
			Message:     fmt.Sprintf(message, imp.PeriodStart, imp.PeriodEnd),
			ImportID:    imp.ID,
			PeriodStart: imp.PeriodStart,
			PeriodEnd:   imp.PeriodEnd,
		})
	}
	return warnings, nil
}

// writeAlreadyImported answers 409 already_imported for imp, the user's
// earlier import of the same file, naming the portfolio it is in so the
// client can point there.
func (h *handler) writeAlreadyImported(w http.ResponseWriter, r *http.Request, userID int64, imp Import) {
	p, err := h.portfolios.Get(r.Context(), userID, imp.PortfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusConflict, map[string]any{
		"error": "already_imported", "import_id": imp.ID,
		"portfolio_id": p.ID, "portfolio_name": p.Name,
	})
}

// writeUploadError maps a body that ran past maxUploadBytes to 413 and any
// other malformed multipart body to 400.
func writeUploadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		auth.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file: larger than %d MB", maxUploadBytes>>20))
		return
	}
	auth.WriteError(w, http.StatusBadRequest, "file: invalid upload")
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
	if broker != "csv" && !end.Before(start.AddDate(1, 0, 0)) {
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
		if checkPeriod("csv", d, d) != nil {
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

	// This route is exempt from the global 64 KB limit, so it sets its own.
	r.Body = http.MaxBytesReader(w, r.Body, maxCommitBytes)
	var req commitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			auth.WriteError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("rows: larger than %d MB", maxCommitBytes>>20))
			return
		}
		auth.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Broker == "csv" {
		if err := req.setCSVPeriod(); err != nil {
			auth.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := req.validate(); err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.portfolios.Get(r.Context(), userID, req.PortfolioID); err != nil {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
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

	// The note never carries anything off the statement itself: the
	// description column holds the customer's bank name and IBAN (ADR 0006,
	// PII).
	note := fmt.Sprintf("%s %s – %s", brokerNames[req.Broker], req.PeriodStart, req.PeriodEnd)

	// Keyed by instrument id, or by currency for a cash row, so the fetch
	// runs once per distinct instrument and once per distinct cash currency
	// instead of once per row.
	targets := map[string]fetchTarget{}

	// validate checked the period's shape.
	start, _ := time.Parse(time.DateOnly, req.PeriodStart)
	openingDate := start.AddDate(0, 0, -1).Format(time.DateOnly)
	end, _ := time.Parse(time.DateOnly, req.PeriodEnd)
	interestDate := end.AddDate(0, 0, 1).Format(time.DateOnly)
	ts := make([]transaction.Transaction, 0, len(req.Rows))
	for i, row := range req.Rows {
		t, in, err := transaction.FromRequestWith(row, getInstrument)
		if err != nil {
			if errors.Is(err, transaction.ErrLookup) {
				auth.WriteInternalError(w, r, err)
				return
			}
			auth.WriteError(w, http.StatusBadRequest, fmt.Sprintf("rows[%d].%s", i, err.Error()))
			return
		}
		// The import's undo and history describe its period, so every row
		// must fall inside it; a Midas statement only lists rows dated in its
		// own period. The exceptions are the day before it, the
		// date of the opening rows the preview proposes, and the
		// day after it, when Midas books the month's interest it prints in
		// this statement.
		if (t.Date < req.PeriodStart && t.Date != openingDate) || (t.Date > req.PeriodEnd && t.Date != interestDate) {
			auth.WriteError(w, http.StatusBadRequest, fmt.Sprintf("rows[%d].date: must be within %s – %s", i, req.PeriodStart, req.PeriodEnd))
			return
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

	aliases, learn := commitAliases(req)

	// A CSV is one file, not an account: its currency is only the rows'
	// majority, so it is recorded as unknown and matches an earlier CSV of
	// any currency.
	account := req.Currency
	if req.Broker == "csv" {
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
// one after another, each within its own OnDemandFetchTimeout, but it
// waits for them only that long in all: the rows are committed, so a hung
// price host must not hold the response past a proxy's timeout, one target
// at a time. The rest run on after it returns; a client
// disconnect does not cancel them.
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
		if in, ok := known[id]; ok {
			return in, nil
		}
		return h.instruments.Get(ctx, id)
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
	in, err := h.instruments.BySymbol(r.Context(), "TEFAS", code)
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
