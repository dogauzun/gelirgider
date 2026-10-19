package statement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"strconv"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/dogauzun/gelirgider/backend/internal/statement/pdftext"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// Per-row verdicts of the preview.
const (
	statusOK                  = "ok"
	statusUnknownInstrument   = "unknown_instrument"
	statusSkipped             = "skipped"
	statusError               = "error"
	statusUnsupportedCurrency = "unsupported_currency"
)

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

	form, ok := h.readParseForm(w, r, userID)
	if !ok {
		return
	}
	broker := form.broker

	req := requester{client: ratelimit.ClientIP(r), userID: userID, broker: broker}
	preview, txs, warnings, err := h.checkedRows(r.Context(), req, form)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	// checkedRows may have set a CSV's period.
	st := form.st
	// After previewRows, which may add a TEFAS fund a trade names.
	holdIns, err := h.holdingInstruments(r.Context(), userID, broker, form.picks, st)
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

	logParsed(broker, form.portfolioID, preview)
	holdings, names := holdingsAndNames(st, holdIns)
	auth.WriteJSON(w, http.StatusOK, previewResponse{
		Broker:             broker,
		SHA256:             form.sha,
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

// parseForm is what parse reads off the upload: the form's fields, the file's
// SHA-256 and the statement its broker's parser read.
type parseForm struct {
	broker      string // the one that parsed the file, also for broker auto
	isCSV       bool
	portfolioID int64
	picks       map[string]instrument.Instrument
	batch       map[string]int64
	sha         string
	st          Statement
}

// readParseForm reads the parse request's form, checks the portfolio and
// parses the uploaded file. ok is false once it has written the error.
func (h *handler) readParseForm(w http.ResponseWriter, r *http.Request, userID int64) (*parseForm, bool) {
	broker := r.FormValue("broker")
	// auto: the PDF parser that accepts the file picks the broker.
	if _, ok := Parsers[broker]; !ok && broker != "auto" {
		auth.WriteError(w, http.StatusBadRequest, "broker: unknown broker")
		return nil, false
	}
	portfolioID, err := strconv.ParseInt(r.FormValue("portfolio_id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "portfolio_id: must be a number")
		return nil, false
	}
	if _, err := h.portfolios.Get(r.Context(), userID, portfolioID); err != nil {
		portfolio.WriteLookupError(w, r, err)
		return nil, false
	}
	// The instruments the user picked in this preview so far, resolved for
	// this request only; the commit stores them.
	picks, ok := h.formAliases(w, r, "picks")
	if !ok {
		return nil, false
	}
	// The fund names the other statements of the user's batch print, used
	// like learned names for this request only: a fund renamed on TEFAS is
	// named in an early statement only by its old name, which a later
	// statement prints next to its code.
	batchIns, ok := h.formAliases(w, r, "names")
	if !ok {
		return nil, false
	}
	batch := make(map[string]int64, len(batchIns))
	for name, in := range batchIns {
		batch[name] = in.ID
	}

	isCSV := broker == "csv"
	data, sha, ok := h.readUpload(w, r, userID, isCSV)
	if !ok {
		return nil, false
	}
	broker, st, ok := parseUpload(w, r, data, broker)
	if !ok {
		return nil, false
	}
	return &parseForm{broker: broker, isCSV: isCSV, portfolioID: portfolioID, picks: picks, batch: batch, sha: sha, st: st}, true
}

// checkedRows builds the preview's rows and checks them against the
// portfolio: it sets a CSV's period from the rows, flags the duplicates of
// the portfolio's transactions (also returned, one scan per preview) and
// collects the period warnings.
func (h *handler) checkedRows(ctx context.Context, req requester, form *parseForm) (preview []previewRow, txs []transaction.Transaction, warnings []warning, err error) {
	st := &form.st
	preview, err = h.previewRows(ctx, req, form.picks, form.batch, *st)
	if err != nil {
		return nil, nil, nil, err
	}
	// A CSV prints no period, so it spans its rows' dates, error rows
	// included since the user may fix one in the preview.
	onlyErrors := form.isCSV
	if form.isCSV {
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
	txs, err = h.transactions.List(ctx, req.userID, form.portfolioID)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := flagDuplicates(txs, preview); err != nil {
		return nil, nil, nil, err
	}
	warnings, err = h.overlapWarnings(ctx, req.userID, form.portfolioID, form.broker, *st)
	if err != nil {
		return nil, nil, nil, err
	}
	// A CSV of error rows alone has no period when none of them has a date
	// the commit accepts. Its rows already say what is wrong, and the
	// warning would only name the internal field.
	if err := checkPeriod(form.broker, st.PeriodStart, st.PeriodEnd); err != nil && !onlyErrors {
		warnings = append(warnings, warning{Code: "period", Message: err.Error(), PeriodStart: st.PeriodStart, PeriodEnd: st.PeriodEnd})
	}
	return preview, txs, warnings, nil
}

// logParsed logs the row counts of a parsed statement per status.
func logParsed(broker string, portfolioID int64, preview []previewRow) {
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
}

// holdingsAndNames is the response's holdings and the fund names the
// balances printed.
func holdingsAndNames(st Statement, holdIns []instrument.Instrument) ([]holdingResponse, []aliasRequest) {
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
	return holdings, names
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
	names := newNameIndex(learned)
	// This statement's own balances are newer than any learned name. The
	// parser maps a name that matches one of their names to its code, so a
	// name that still matches one matches two funds and resolves nothing.
	// A holding's code is a code, not a cut name, so no learned
	// name resolves it either. Every own name maps to 1, so learnedName
	// reports any match of them.
	ownIDs := map[string]int64{}
	codes := map[string]bool{}
	for _, hold := range st.Holdings {
		codes[hold.Symbol] = true
		if hold.Name != "" {
			ownIDs[hold.Name] = 1
		}
	}
	own := newNameIndex(ownIDs)
	learnedFor := func(symbol string) *nameIndex {
		if codes[symbol] || learnedName(symbol, own) != 0 {
			return nil
		}
		return names
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
			Key: fmt.Sprintf("div-%d", i), Type: transaction.TypeDividend, Date: d.Date, Symbol: d.Symbol,
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
	dividends, exact := indexExisting(txs)

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
		pairExact(exact, exactKey{row.Type, row.Date, id}, trade, &preview[i])
		if instrumentDividend && preview[i].DuplicateOf == "" {
			rowsOf[id] = append(rowsOf[id], i)
		}
	}
	return pairWindow(preview, rowsOf, dividends)
}

// exactKey keys the exact-date candidates by type, date and instrument (0
// for a cash row).
type exactKey struct {
	typ, date    string
	instrumentID int64
}

// indexExisting splits txs into the hand-entered dividends of an instrument,
// for the window, and the exact-date candidates; a match is removed from the
// latter so it cannot pair twice.
func indexExisting(txs []transaction.Transaction) (dividends map[int64][]string, exact map[exactKey][]transaction.Transaction) {
	dividends = map[int64][]string{}
	exact = map[exactKey][]transaction.Transaction{}
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
	return dividends, exact
}

// pairExact sets row's DuplicateOf to the first earlier transaction under k
// that it matches, and removes that transaction from exact.
func pairExact(exact map[exactKey][]transaction.Transaction, k exactKey, trade bool, row *previewRow) {
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
			row.DuplicateOf = t.StoredDate()
			exact[k] = append(candidates[:j:j], candidates[j+1:]...)
			return
		}
	}
}

// pairWindow pairs the dividend rows of each instrument that pairExact found no
// exact match for (rowsOf: their indexes in preview) with its hand-entered
// dividends through the window.
func pairWindow(preview []previewRow, rowsOf map[int64][]int, dividends map[int64][]string) error {
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

// overlapWarnings warns about importing the same period twice. An overlap is
// a warning and not an error: a quarterly statement over an imported month is
// legitimate (ADR 0006). The other currency's statement of it is the other
// account and gets no warning at all. The same statement again is
// flagged as same_period, which the commit refuses until the earlier import
// is undone, except for a CSV.
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
