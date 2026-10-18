package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
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

// registerDepositRoutes wires POST and GET /api/portfolios/{id}/deposits.
// onDemandFetch is the transaction routes' price and FX hook.
func registerDepositRoutes(mux *http.ServeMux, d *deps, onDemandFetch transaction.OnDemandFetch) {
	h := &depositHandler{deps: d, onDemandFetch: onDemandFetch}
	mux.HandleFunc("POST /api/portfolios/{id}/deposits", h.create)
	mux.HandleFunc("GET /api/portfolios/{id}/deposits", h.list)
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
	ctx := r.Context()
	tx, err := h.conn.BeginTx(ctx, nil)
	if err != nil {
		return buy, dep, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

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
	if err != nil {
		return buy, dep, err
	}
	if err := tx.Commit(); err != nil {
		return buy, dep, fmt.Errorf("commit: %w", err)
	}
	return buy, dep, nil
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
	txs, err := h.transactions.List(r.Context(), userID, portfolioID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	sold := map[int64]bool{}
	for _, t := range txs {
		if t.Type == transaction.TypeSell && t.InstrumentID != nil {
			sold[*t.InstrumentID] = true
		}
	}
	today := price.Today()
	out := make([]depositResponse, 0, len(ds))
	for _, dep := range ds {
		out = append(out, toDepositResponse(dep, today, sold[dep.InstrumentID]))
	}
	auth.WriteJSON(w, http.StatusOK, out)
}
