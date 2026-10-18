package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/currency"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// maxDepositTermYears caps the distance from a deposit's start to its maturity.
const maxDepositTermYears = 10

// registerDepositRoutes wires POST and GET /api/portfolios/{id}/deposits,
// GET .../deposits/due, POST /api/deposits/{id}/mature, POST
// /api/deposits/{id}/renew and DELETE /api/deposits/{id}.
// onDemandFetch is the transaction routes' price and FX hook.
func registerDepositRoutes(mux *http.ServeMux, d *deps, onDemandFetch transaction.OnDemandFetch) {
	h := &depositHandler{deps: d, onDemandFetch: onDemandFetch}
	mux.HandleFunc("POST /api/portfolios/{id}/deposits", h.create)
	mux.HandleFunc("GET /api/portfolios/{id}/deposits", h.list)
	mux.HandleFunc("GET /api/portfolios/{id}/deposits/due", h.due)
	mux.HandleFunc("POST /api/deposits/{id}/mature", h.mature)
	mux.HandleFunc("POST /api/deposits/{id}/renew", h.renew)
	mux.HandleFunc("DELETE /api/deposits/{id}", h.delete)
}

type depositHandler struct {
	*deps
	onDemandFetch transaction.OnDemandFetch
}

// depositRequest is the POST body. Decimals are strings; rate and stopaj_rate
// are fractions (0.45 for 45%).
type depositRequest struct {
	Currency     string `json:"currency"`
	Principal    string `json:"principal"`
	Rate         string `json:"rate"`
	StopajRate   string `json:"stopaj_rate"`
	StartDate    string `json:"start_date"`
	MaturityDate string `json:"maturity_date"`
	Note         string `json:"note"`
	// Fund books a cash deposit of the principal on the start date as well.
	Fund bool `json:"fund"`
}

type depositResponse struct {
	ID           int64  `json:"id"`
	InstrumentID int64  `json:"instrument_id"`
	Currency     string `json:"currency"`
	Principal    string `json:"principal"`
	Rate         string `json:"rate"`
	StopajRate   string `json:"stopaj_rate"`
	StartDate    string `json:"start_date"`
	MaturityDate string `json:"maturity_date"`
	// RenewedFrom is the id of the deposit this one renews, null for a deposit
	// opened on its own.
	RenewedFrom *int64 `json:"renewed_from"`
	// Value is the principal plus the net interest accrued up to today, 2 decimals.
	Value string `json:"value"`
	// AccruedInterest is Value minus the principal.
	AccruedInterest string `json:"accrued_interest"`
	// NetInterestAtMaturity is the interest over the whole term, after stopaj.
	NetInterestAtMaturity string `json:"net_interest_at_maturity"`
	// Payout is the principal plus NetInterestAtMaturity.
	Payout string `json:"payout"`
	// Status is matured (a sell is recorded on the deposit), else due (today is
	// on or after the maturity date), else active.
	Status string `json:"status"`
}

// parseDepositRequest validates req and returns the terms to store with the
// principal and the trimmed note. Errors are "<field>: <message>", as the
// transaction routes write them.
func parseDepositRequest(req depositRequest) (principal decimal.Decimal, terms deposit.Terms, note string, err error) {
	if !currency.IsValid(req.Currency) {
		return principal, terms, "", fmt.Errorf("currency: must be one of %s", strings.Join(currency.Valid, ", "))
	}
	if principal, err = transaction.ParseNonNegative(req.Principal); err != nil {
		return principal, terms, "", fmt.Errorf("principal: %w", err)
	}
	if !principal.IsPositive() {
		return principal, terms, "", errors.New("principal: must be a positive number")
	}
	if terms.Rate, err = transaction.ParseNonNegative(req.Rate); err != nil {
		return principal, terms, "", fmt.Errorf("rate: %w", err)
	}
	if !terms.Rate.IsPositive() || terms.Rate.GreaterThan(decimal.NewFromInt(10)) {
		return principal, terms, "", errors.New("rate: must be greater than 0 and at most 10")
	}
	if terms.StopajRate, err = transaction.ParseNonNegative(req.StopajRate); err != nil {
		return principal, terms, "", fmt.Errorf("stopaj_rate: %w", err)
	}
	if !terms.StopajRate.LessThan(decimal.NewFromInt(1)) {
		return principal, terms, "", errors.New("stopaj_rate: must be at least 0 and below 1")
	}
	if err = price.CheckDate(req.StartDate); err != nil {
		return principal, terms, "", fmt.Errorf("start_date: %w", err)
	}
	terms.StartDate = req.StartDate
	if !price.ValidDate(req.MaturityDate) {
		return principal, terms, "", errors.New("maturity_date: must be YYYY-MM-DD")
	}
	start, _ := time.Parse(time.DateOnly, req.StartDate)
	if req.MaturityDate <= req.StartDate {
		return principal, terms, "", errors.New("maturity_date: must be after start_date")
	}
	if req.MaturityDate > start.AddDate(maxDepositTermYears, 0, 0).Format(time.DateOnly) {
		return principal, terms, "", fmt.Errorf("maturity_date: must be at most %d years after start_date", maxDepositTermYears)
	}
	terms.MaturityDate = req.MaturityDate
	if note, err = transaction.ParseNote(req.Note); err != nil {
		return principal, terms, "", err
	}
	return principal, terms, note, nil
}

// newDepositSymbol returns a symbol no other instrument has: DEPOSIT
// instruments are not searched by symbol, so it only has to be unique.
func newDepositSymbol() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random deposit symbol: %w", err)
	}
	return "TD-" + hex.EncodeToString(b), nil
}

// toDepositResponse values dep on today. sold is whether a sell is recorded
// on its instrument.
func toDepositResponse(dep deposit.Deposit, today string, sold bool) depositResponse {
	value := dep.Principal
	if c, ok := deposit.Close(dep.Terms, today); ok {
		value = dep.Principal.Mul(c)
	}
	gross, stopaj := deposit.Interest(dep.Principal, dep.Terms)
	net := gross.Sub(stopaj)
	status := "active"
	switch {
	case sold:
		status = "matured"
	case today >= dep.Terms.MaturityDate:
		status = "due"
	}
	return depositResponse{
		ID:                    dep.ID,
		InstrumentID:          dep.InstrumentID,
		Currency:              dep.Currency,
		Principal:             dep.Principal.String(),
		Rate:                  dep.Terms.Rate.String(),
		StopajRate:            dep.Terms.StopajRate.String(),
		StartDate:             dep.Terms.StartDate,
		MaturityDate:          dep.Terms.MaturityDate,
		RenewedFrom:           dep.RenewedFrom,
		Value:                 value.StringFixed(2),
		AccruedInterest:       value.Round(2).Sub(dep.Principal).StringFixed(2),
		NetInterestAtMaturity: net.StringFixed(2),
		Payout:                dep.Principal.Add(net).StringFixed(2),
		Status:                status,
	}
}

// create opens a term deposit: its own DEPOSIT instrument, a buy of the
// principal at price 1 on the start date and, with fund, the cash deposit
// that pays for it, all in one SQL transaction.
//
//	@Summary	Open a term deposit
//	@Description	Books a buy of quantity = principal at price 1 on a new hidden DEPOSIT instrument, valued from the deposit's terms (simple interest, actual/365, net of stopaj). With fund true it also books a cash deposit of the principal on the start date, before the buy; leave it off when the cash is already recorded, or the cash counts twice. Decimals are strings with at most 15 integer and 9 fraction digits; rate and stopaj_rate are fractions (0.45 for 45%).
//	@Tags		deposits
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"portfolio id"
//	@Param		body	body		server.depositRequest	true	"principal > 0, rate > 0 and at most 10, stopaj_rate from 0 up to but not including 1, start_date not in the future, maturity_date after it and at most 10 years later"
//	@Success	201		{object}	server.depositResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; currency: must be one of TRY, USD, EUR; principal|rate|stopaj_rate: required; principal|rate|stopaj_rate: must be a non-negative number with at most 15 integer and 9 fraction digits; principal: must be a positive number; rate: must be greater than 0 and at most 10; stopaj_rate: must be at least 0 and below 1; start_date: must be YYYY-MM-DD; start_date: must not be in the future; start_date: must be on or after 1990-01-01; maturity_date: must be YYYY-MM-DD; maturity_date: must be after start_date; maturity_date: must be at most 10 years after start_date; note: must be at most 500 characters; note: must not contain control characters"
//	@Failure	404		{object}	object{error=string}	"portfolio not found"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/deposits [post]
func (h *depositHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	var req depositRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	principal, terms, note, err := parseDepositRequest(req)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	dep := deposit.Deposit{UserID: userID, PortfolioID: portfolioID, Currency: req.Currency, Principal: principal, Terms: terms}
	in := instrument.Instrument{Market: instrument.MarketDeposit, Currency: req.Currency, Name: fmt.Sprintf("Vadeli mevduat %s %s", req.Currency, terms.MaturityDate)}
	if in.Symbol, err = newDepositSymbol(); err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}

	buy, dep, err := h.open(r, dep, in, note, req.Fund)
	if errors.Is(err, portfolio.ErrNotFound) || db.IsForeignKeyViolation(err) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	in.ID = dep.InstrumentID
	transaction.EnsurePrice(r.Context(), h.onDemandFetch, in, buy)
	auth.WriteJSON(w, http.StatusCreated, toDepositResponse(dep, price.Today(), false))
}

// open writes the rows of a new deposit in one SQL transaction and returns the
// buy and the stored deposit.
func (h *depositHandler) open(r *http.Request, dep deposit.Deposit, in instrument.Instrument, note string, fund bool) (buy transaction.Transaction, _ deposit.Deposit, err error) {
	tx, err := h.conn.BeginTx(r.Context(), nil)
	if err != nil {
		return buy, dep, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if buy, dep, err = h.openTx(r.Context(), tx, dep, in, note, fund); err != nil {
		return buy, dep, err
	}
	if err := tx.Commit(); err != nil {
		return buy, dep, fmt.Errorf("commit: %w", err)
	}
	return buy, dep, nil
}

// openTx writes the rows of a new deposit inside tx, which the caller commits.
func (h *depositHandler) openTx(ctx context.Context, tx *sql.Tx, dep deposit.Deposit, in instrument.Instrument, note string, fund bool) (buy transaction.Transaction, _ deposit.Deposit, err error) {
	if dep.InstrumentID, err = h.instruments.InsertTx(ctx, tx, in.Market, in.Symbol, in.Name, in.Currency); err != nil {
		return buy, dep, err
	}
	// The cash goes in before the buy so it gets the lower id: on the start
	// date the engine applies the rows in id order.
	if fund {
		cash, err := h.transactions.CreateTx(ctx, tx, dep.UserID, transaction.Transaction{
			PortfolioID: dep.PortfolioID, Type: transaction.TypeDeposit, Date: dep.Terms.StartDate,
			Amount: dep.Principal, Fee: decimal.Zero, Currency: dep.Currency,
		})
		if err != nil {
			return buy, dep, err
		}
		dep.FundingTransactionID = &cash.ID
	}
	if dep, err = h.deposits.InsertTx(ctx, tx, dep); err != nil {
		return buy, dep, err
	}
	buy, err = h.transactions.CreateTx(ctx, tx, dep.UserID, transaction.Transaction{
		PortfolioID: dep.PortfolioID, InstrumentID: &dep.InstrumentID, Type: transaction.TypeBuy, Date: dep.Terms.StartDate,
		Quantity: dep.Principal, Price: decimal.NewFromInt(1), Amount: dep.Principal, Fee: decimal.Zero,
		Currency: dep.Currency, Note: note,
	})
	return buy, dep, err
}

// list returns the portfolio's deposits, newest start first.
//
//	@Summary	List a portfolio's term deposits
//	@Description	Newest start date first. value is today's net value (principal plus the net interest accrued), payout the principal plus the net interest at maturity, both 2 decimals. status is matured when a sell is recorded on the deposit, else due from the maturity date, else active.
//	@Tags		deposits
//	@Produce	json
//	@Param		id	path		int	true	"portfolio id"
//	@Success	200	{array}		server.depositResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/deposits [get]
func (h *depositHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	ds, err := h.deposits.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	sold, err := h.soldInstruments(r, userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	today := price.Today()
	out := make([]depositResponse, 0, len(ds))
	for _, dep := range ds {
		out = append(out, toDepositResponse(dep, today, sold[dep.InstrumentID]))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// soldInstruments returns the ids of the portfolio's instruments that have a
// sell recorded.
func (h *depositHandler) soldInstruments(r *http.Request, userID, portfolioID int64) (map[int64]bool, error) {
	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		return nil, err
	}
	sold := map[int64]bool{}
	for _, t := range txs {
		if t.Type == transaction.TypeSell && t.InstrumentID != nil {
			sold[*t.InstrumentID] = true
		}
	}
	return sold, nil
}

type dueDepositResponse struct {
	ID           int64  `json:"id"`
	Currency     string `json:"currency"`
	Principal    string `json:"principal"`
	MaturityDate string `json:"maturity_date"`
	// Gross, Stopaj and Net are the interest over the whole term, 2 decimals.
	Gross  string `json:"gross"`
	Stopaj string `json:"stopaj"`
	Net    string `json:"net"`
}

// due suggests the deposits to mature: it writes nothing.
//
//	@Summary	List due term deposits
//	@Description	The portfolio's deposits whose maturity date is today or past and that have no sell yet, oldest maturity first, each with the interest over the whole term (gross, stopaj, net; 2 decimals). Nothing is booked: POST /deposits/{id}/mature books one. The all scope is not supported.
//	@Tags		deposits
//	@Produce	json
//	@Param		id	path		int	true	"portfolio id"
//	@Success	200	{array}		server.dueDepositResponse
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/deposits/due [get]
func (h *depositHandler) due(w http.ResponseWriter, r *http.Request) {
	userID, portfolioID, ok := portfolio.Owned(w, r, h.portfolios)
	if !ok {
		return
	}
	ds, err := h.deposits.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	sold, err := h.soldInstruments(r, userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	today := price.Today()
	ds = slices.DeleteFunc(ds, func(d deposit.Deposit) bool { return sold[d.InstrumentID] || today < d.Terms.MaturityDate })
	slices.SortStableFunc(ds, func(a, b deposit.Deposit) int {
		return strings.Compare(a.Terms.MaturityDate, b.Terms.MaturityDate)
	})
	out := make([]dueDepositResponse, 0, len(ds))
	for _, d := range ds {
		gross, stopaj := deposit.Interest(d.Principal, d.Terms)
		out = append(out, dueDepositResponse{
			ID: d.ID, Currency: d.Currency, Principal: d.Principal.String(), MaturityDate: d.Terms.MaturityDate,
			Gross: gross.StringFixed(2), Stopaj: stopaj.StringFixed(2), Net: gross.Sub(stopaj).StringFixed(2),
		})
	}
	auth.WriteJSON(w, http.StatusOK, out)
}

// mature books a due deposit: the principal back to cash and the interest as
// income, both dated the maturity date.
//
//	@Summary	Mature a term deposit
//	@Description	Books a sell of the principal at price 1 on the maturity date (no realized profit or loss) and, when the gross interest is above zero, a dividend with no instrument for it: amount the gross interest, fee the stopaj, deposit currency, note the deposit's name. Both rows are written in one SQL transaction. 409 when today is before the maturity date or the deposit is already matured.
//	@Tags		deposits
//	@Produce	json
//	@Param		id	path		int	true	"deposit id"
//	@Success	200	{object}	server.depositResponse
//	@Failure	404	{object}	object{error=string}	"deposit not found"
//	@Failure	409	{object}	object{error=string}	"deposit is not due yet; deposit is already matured"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/deposits/{id}/mature [post]
func (h *depositHandler) mature(w http.ResponseWriter, r *http.Request) {
	dep, ok := h.ownedDeposit(w, r)
	if !ok {
		return
	}
	today := price.Today()
	if today < dep.Terms.MaturityDate {
		auth.WriteError(w, http.StatusConflict, "deposit is not due yet")
		return
	}
	in, err := h.instruments.Get(r.Context(), dep.InstrumentID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	booked, err := h.book(r, dep, in.Name)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if !booked {
		auth.WriteError(w, http.StatusConflict, "deposit is already matured")
		return
	}
	auth.WriteJSON(w, http.StatusOK, toDepositResponse(dep, today, true))
}

// book writes dep's sell and interest rows in one SQL transaction. It is false,
// having written nothing, when the deposit already has a sell. BeginTx takes
// the write lock, so two calls cannot both pass the check.
func (h *depositHandler) book(r *http.Request, dep deposit.Deposit, name string) (bool, error) {
	tx, err := h.conn.BeginTx(r.Context(), nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	booked, err := h.bookTx(r.Context(), tx, dep, name)
	if err != nil || !booked {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// bookTx writes dep's sell and interest rows inside tx, which the caller
// commits. It is false, having written nothing, when the deposit already has a
// sell.
func (h *depositHandler) bookTx(ctx context.Context, tx *sql.Tx, dep deposit.Deposit, name string) (bool, error) {
	sold, err := h.deposits.SoldTx(ctx, tx, dep.UserID, dep.InstrumentID)
	if err != nil || sold {
		return false, err
	}
	date := dep.Terms.MaturityDate
	_, err = h.transactions.CreateTx(ctx, tx, dep.UserID, transaction.Transaction{
		PortfolioID: dep.PortfolioID, InstrumentID: &dep.InstrumentID, Type: transaction.TypeSell, Date: date,
		Quantity: dep.Principal, Price: decimal.NewFromInt(1), Amount: dep.Principal, Fee: decimal.Zero, Currency: dep.Currency,
	})
	if err != nil {
		return false, err
	}
	if gross, stopaj := deposit.Interest(dep.Principal, dep.Terms); gross.IsPositive() {
		_, err = h.transactions.CreateTx(ctx, tx, dep.UserID, transaction.Transaction{
			PortfolioID: dep.PortfolioID, Type: transaction.TypeDividend, Date: date,
			Amount: gross, Fee: stopaj, Currency: dep.Currency, Note: name,
		})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// ownedDeposit returns the deposit named by the {id} path value when it is the
// caller's. Otherwise it writes the 404 or 500 and returns false.
func (h *depositHandler) ownedDeposit(w http.ResponseWriter, r *http.Request) (deposit.Deposit, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "deposit not found")
		return deposit.Deposit{}, false
	}
	dep, err := h.deposits.Get(r.Context(), auth.MustUserID(r.Context()), id)
	if errors.Is(err, deposit.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "deposit not found")
		return deposit.Deposit{}, false
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return deposit.Deposit{}, false
	}
	return dep, true
}

// renewRequest is the POST body of a renewal: the new terms. The principal,
// currency and start date come from the deposit being renewed.
type renewRequest struct {
	Rate         string `json:"rate"`
	StopajRate   string `json:"stopaj_rate"`
	MaturityDate string `json:"maturity_date"`
}

// renew opens a new deposit from a due or matured one: the principal plus the
// net interest, starting on the old maturity date.
//
//	@Summary	Renew a term deposit
//	@Description	Opens a new deposit of the old principal plus its net interest (gross minus stopaj, 2 decimals), starting on the old maturity date in the same portfolio and currency, with a buy of that principal dated that day and renewed_from set to the old deposit. When the old deposit has no sell yet, its maturity is booked first, as POST /deposits/{id}/mature does. Everything is written in one SQL transaction. A deposit can be renewed once. rate and stopaj_rate are fractions (0.45 for 45%).
//	@Tags		deposits
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"deposit id"
//	@Param		body	body		server.renewRequest	true	"rate > 0 and at most 10, stopaj_rate from 0 up to but not including 1, maturity_date after the old maturity date and at most 10 years later"
//	@Success	201		{object}	server.depositResponse
//	@Failure	400		{object}	object{error=string}	"invalid request body; rate|stopaj_rate: required; rate|stopaj_rate: must be a non-negative number with at most 15 integer and 9 fraction digits; rate: must be greater than 0 and at most 10; stopaj_rate: must be at least 0 and below 1; maturity_date: must be YYYY-MM-DD; maturity_date: must be after start_date; maturity_date: must be at most 10 years after start_date"
//	@Failure	404		{object}	object{error=string}	"deposit not found"
//	@Failure	409		{object}	object{error=string}	"deposit is not due yet; deposit is already renewed"
//	@Failure	500		{object}	object{error=string}	"internal error"
//	@Router		/deposits/{id}/renew [post]
func (h *depositHandler) renew(w http.ResponseWriter, r *http.Request) {
	old, ok := h.ownedDeposit(w, r)
	if !ok {
		return
	}
	var req renewRequest
	if !auth.DecodeJSON(w, r, &req) {
		return
	}
	if price.Today() < old.Terms.MaturityDate {
		auth.WriteError(w, http.StatusConflict, "deposit is not due yet")
		return
	}
	gross, stopaj := deposit.Interest(old.Principal, old.Terms)
	principal, terms, _, err := parseDepositRequest(depositRequest{
		Currency: old.Currency, Principal: old.Principal.Add(gross).Sub(stopaj).String(), Rate: req.Rate,
		StopajRate: req.StopajRate, StartDate: old.Terms.MaturityDate, MaturityDate: req.MaturityDate,
	})
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := h.instruments.Get(r.Context(), old.InstrumentID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	next := deposit.Deposit{UserID: old.UserID, PortfolioID: old.PortfolioID, Currency: old.Currency, Principal: principal, Terms: terms, RenewedFrom: &old.ID}
	nextIn := instrument.Instrument{Market: instrument.MarketDeposit, Currency: old.Currency, Name: fmt.Sprintf("Vadeli mevduat %s %s", old.Currency, terms.MaturityDate)}
	if nextIn.Symbol, err = newDepositSymbol(); err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	buy, next, err := h.renewRows(r, old, in.Name, next, nextIn)
	if errors.Is(err, deposit.ErrAlreadyRenewed) {
		auth.WriteError(w, http.StatusConflict, "deposit is already renewed")
		return
	}
	if db.IsForeignKeyViolation(err) {
		auth.WriteError(w, http.StatusNotFound, "deposit not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	nextIn.ID = next.InstrumentID
	transaction.EnsurePrice(r.Context(), h.onDemandFetch, nextIn, buy)
	auth.WriteJSON(w, http.StatusCreated, toDepositResponse(next, price.Today(), false))
}

// renewRows writes a renewal in one SQL transaction: old's maturity when it
// has no sell yet, then the new deposit and its buy. It returns
// deposit.ErrAlreadyRenewed, having written nothing, when a deposit already
// renews old. BeginTx takes the write lock, so two renewals cannot both pass
// the check.
func (h *depositHandler) renewRows(r *http.Request, old deposit.Deposit, name string, next deposit.Deposit, nextIn instrument.Instrument) (buy transaction.Transaction, _ deposit.Deposit, err error) {
	ctx := r.Context()
	tx, err := h.conn.BeginTx(ctx, nil)
	if err != nil {
		return buy, next, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	renewed, err := h.deposits.RenewedTx(ctx, tx, old.UserID, old.ID)
	if err != nil {
		return buy, next, err
	}
	if renewed {
		return buy, next, deposit.ErrAlreadyRenewed
	}
	// false only when the deposit is matured already, which is fine here.
	if _, err = h.bookTx(ctx, tx, old, name); err != nil {
		return buy, next, err
	}
	if buy, next, err = h.openTx(ctx, tx, next, nextIn, "", false); err != nil {
		return buy, next, err
	}
	if err := tx.Commit(); err != nil {
		return buy, next, fmt.Errorf("commit: %w", err)
	}
	return buy, next, nil
}

// delete removes an open deposit with the rows it booked.
//
//	@Summary	Delete a term deposit
//	@Description	Deletes the deposit, its buy, its funding cash deposit (when it was opened with fund true and the user has not deleted that row) and its hidden instrument, in one SQL transaction. A matured deposit (one with a sell) cannot be deleted. The interest row of a maturity is a plain dividend and is not touched here.
//	@Tags		deposits
//	@Produce	json
//	@Param		id	path	int	true	"deposit id"
//	@Success	204	"deleted"
//	@Failure	404	{object}	object{error=string}	"deposit not found"
//	@Failure	409	{object}	object{error=string}	"deposit is matured"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/deposits/{id} [delete]
func (h *depositHandler) delete(w http.ResponseWriter, r *http.Request) {
	dep, ok := h.ownedDeposit(w, r)
	if !ok {
		return
	}
	removed, err := h.remove(r, dep)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	if !removed {
		auth.WriteError(w, http.StatusConflict, "deposit is matured")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// remove deletes dep in one SQL transaction. It is false, having deleted
// nothing, when the deposit has a sell. BeginTx takes the write lock, so a
// mature cannot slip in between the check and the delete.
func (h *depositHandler) remove(r *http.Request, dep deposit.Deposit) (bool, error) {
	ctx := r.Context()
	tx, err := h.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	sold, err := h.deposits.SoldTx(ctx, tx, dep.UserID, dep.InstrumentID)
	if err != nil || sold {
		return false, err
	}
	if err := h.deposits.DeleteTx(ctx, tx, dep); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}
