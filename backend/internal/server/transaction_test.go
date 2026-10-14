package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	statementcsv "github.com/dogauzun/gelirgider/backend/internal/statement/csv"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// transactionTestEnv is the shared fixture for the transaction API tests: a
// fresh SQLite database with one instrument, the full server handler, and a
// helper to send authenticated requests as any user.
type transactionTestEnv struct {
	t            *testing.T
	handler      http.Handler
	db           *sql.DB
	instrumentID int64
	users        *user.Repo
	sessions     *auth.Repo
}

func newTransactionTestEnv(t *testing.T) *transactionTestEnv {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn := openBackfillTestDB(t)

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	return &transactionTestEnv{
		t:            t,
		handler:      New(logger, conn, false, settings.NewRepo(conn, nil), nil),
		db:           conn,
		instrumentID: instrumentID,
		users:        user.NewRepo(conn),
		sessions:     auth.NewRepo(conn),
	}
}

// openBackfillTestDB opens a fresh test DB for a test that can queue
// backfills. Its cleanup closes the DB, then waits for every backfill
// worker to exit, then removes the temp dir: a walk still going fails fast
// on the closed DB instead of walking on, and none outlives the test to
// write into the next one or into the removed dir (GLG-387).
func openBackfillTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(backfillWorkers.Wait)
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// login registers a user and returns a session ID for it.
func (e *transactionTestEnv) login(email string) string {
	e.t.Helper()
	usr, err := e.users.Create(context.Background(), email, "hash")
	if err != nil {
		e.t.Fatalf("users.Create: %v", err)
	}
	session, err := e.sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		e.t.Fatalf("sessions.CreateSession: %v", err)
	}
	return session.ID
}

// do sends a JSON request as the given session and returns the recorder.
func (e *transactionTestEnv) do(sessionID, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, reqBody)
	r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

// createPortfolio creates a portfolio for the session and returns its ID.
func (e *transactionTestEnv) createPortfolio(sessionID, name string) int64 {
	e.t.Helper()
	rec := e.do(sessionID, http.MethodPost, "/api/portfolios", map[string]string{"name": name})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create portfolio status = %d, want %d", rec.Code, http.StatusCreated)
	}
	var portfolio struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&portfolio); err != nil {
		e.t.Fatalf("decode portfolio: %v", err)
	}
	return portfolio.ID
}

// createTransaction posts body to the portfolio and returns the new ID.
func (e *transactionTestEnv) createTransaction(sessionID string, portfolioID int64, body map[string]any) int64 {
	e.t.Helper()
	rec := e.do(sessionID, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID), body)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create %s status = %d, want %d; body: %s", body["type"], rec.Code, http.StatusCreated, rec.Body.String())
	}
	var tx struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&tx); err != nil {
		e.t.Fatalf("decode transaction: %v", err)
	}
	return tx.ID
}

// TestTransactionsListAndValidation covers the two behaviors required of
// GLG-25: creating one transaction of each of the 6 types and listing them
// back in date order -- newest first, the paging order since GLG-141 -- and
// that a buy without instrument_id is rejected.
func TestTransactionsListAndValidation(t *testing.T) {
	env := newTransactionTestEnv(t)
	instrumentID := env.instrumentID
	session := env.login("alice@example.com")
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		return env.do(session, method, path, body)
	}

	txPath := fmt.Sprintf("/api/portfolios/%d/transactions", env.createPortfolio(session, "Test"))

	// One of each type, deliberately out of date order.
	bodies := []map[string]any{
		{"type": "dividend", "date": "2024-03-01", "instrument_id": instrumentID, "amount": "10"},
		{"type": "buy", "date": "2024-01-01", "instrument_id": instrumentID, "quantity": "10", "price": "5"},
		{"type": "deposit", "date": "2023-12-01", "amount": "1000"},
		{"type": "sell", "date": "2024-02-01", "instrument_id": instrumentID, "quantity": "5", "price": "6"},
		{"type": "withdrawal", "date": "2024-04-01", "amount": "20"},
		{"type": "fee", "date": "2024-05-01", "amount": "1"},
	}
	for _, b := range bodies {
		rec := do(http.MethodPost, txPath, b)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s status = %d, want %d; body: %s", b["type"], rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	listRec := do(http.MethodGet, txPath, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var list []struct {
		ID               int64   `json:"id"`
		Type             string  `json:"type"`
		Date             string  `json:"date"`
		InstrumentSymbol *string `json:"instrument_symbol"`
		InstrumentName   *string `json:"instrument_name"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 6 {
		t.Fatalf("list length = %d, want 6", len(list))
	}
	wantDates := []string{"2024-05-01", "2024-04-01", "2024-03-01", "2024-02-01", "2024-01-01", "2023-12-01"}
	for i, tx := range list {
		if tx.Date != wantDates[i] {
			t.Fatalf("row %d date = %s, want %s (not in date order): %+v", i, tx.Date, wantDates[i], list)
		}
		// GLG-40: buy/sell/dividend rows carry an instrument, so the list
		// response must join in its symbol/name; deposit/withdrawal/fee
		// rows have no instrument and must not.
		hasInstrument := tx.Type == "buy" || tx.Type == "sell" || tx.Type == "dividend"
		if hasInstrument {
			if tx.InstrumentSymbol == nil || *tx.InstrumentSymbol != "TEST" {
				t.Fatalf("row %d (%s) instrument_symbol = %v, want \"TEST\"", i, tx.Type, tx.InstrumentSymbol)
			}
			if tx.InstrumentName == nil || *tx.InstrumentName != "Test A.S." {
				t.Fatalf("row %d (%s) instrument_name = %v, want \"Test A.S.\"", i, tx.Type, tx.InstrumentName)
			}
		} else if tx.InstrumentSymbol != nil || tx.InstrumentName != nil {
			t.Fatalf("row %d (%s) instrument_symbol/name = %v/%v, want nil/nil", i, tx.Type, tx.InstrumentSymbol, tx.InstrumentName)
		}
	}

	badBuyRec := do(http.MethodPost, txPath, map[string]any{"type": "buy", "date": "2024-01-01", "quantity": "10", "price": "5"})
	if badBuyRec.Code != http.StatusBadRequest {
		t.Fatalf("buy without instrument_id status = %d, want %d; body: %s", badBuyRec.Code, http.StatusBadRequest, badBuyRec.Body.String())
	}

	// GLG-54: an instrument_id that does not exist is a client error, not a
	// foreign key failure surfacing as 500. Same on update.
	unknownBuy := map[string]any{"type": "buy", "date": "2024-01-01", "instrument_id": 99999, "quantity": "1", "price": "1"}
	unknownRec := do(http.MethodPost, txPath, unknownBuy)
	if unknownRec.Code != http.StatusBadRequest {
		t.Fatalf("buy with unknown instrument_id status = %d, want %d; body: %s", unknownRec.Code, http.StatusBadRequest, unknownRec.Body.String())
	}
	var unknownErr struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(unknownRec.Body).Decode(&unknownErr); err != nil || unknownErr.Error != "instrument_id: unknown instrument" {
		t.Fatalf("buy with unknown instrument_id error = %q (%v), want \"instrument_id: unknown instrument\"", unknownErr.Error, err)
	}
	updateRec := do(http.MethodPut, fmt.Sprintf("/api/transactions/%d", list[0].ID), unknownBuy)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("update with unknown instrument_id status = %d, want %d; body: %s", updateRec.Code, http.StatusBadRequest, updateRec.Body.String())
	}
}

// TestTransactionsUpdateAndDelete covers GLG-47: PUT and DELETE on
// /api/transactions/{id} are scoped by user_id only, so a transaction owned
// by another user must look like it does not exist (404), and update must
// re-run the per-type validation of fromRequest.
func TestTransactionsUpdateAndDelete(t *testing.T) {
	env := newTransactionTestEnv(t)
	alice := env.login("alice@example.com")
	bob := env.login("bob@example.com")
	alicePortfolio := env.createPortfolio(alice, "Alice")
	deposit := map[string]any{"type": "deposit", "date": "2024-01-01", "amount": "1000"}
	txID := env.createTransaction(alice, alicePortfolio, deposit)
	txURL := fmt.Sprintf("/api/transactions/%d", txID)

	// Cross-user isolation: bob sees alice's transaction as not found and
	// changes nothing.
	if rec := env.do(bob, http.MethodPut, txURL, map[string]any{"type": "deposit", "date": "2024-01-02", "amount": "1"}); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user update status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if rec := env.do(bob, http.MethodDelete, txURL, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	// Per-type re-validation on update: a deposit may not carry an
	// instrument, a sell must carry a quantity.
	badBodies := []map[string]any{
		{"type": "deposit", "date": "2024-01-01", "amount": "1000", "instrument_id": env.instrumentID},
		{"type": "sell", "date": "2024-01-01", "instrument_id": env.instrumentID, "price": "5"},
	}
	for _, b := range badBodies {
		if rec := env.do(alice, http.MethodPut, txURL, b); rec.Code != http.StatusBadRequest {
			t.Fatalf("update with invalid %s status = %d, want %d; body: %s", b["type"], rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	}

	// Happy path: change the type, date, and amounts in place.
	updateRec := env.do(alice, http.MethodPut, txURL, map[string]any{"type": "buy", "date": "2024-02-01", "instrument_id": env.instrumentID, "quantity": "10", "price": "5"})
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d; body: %s", updateRec.Code, http.StatusOK, updateRec.Body.String())
	}
	var updated struct {
		ID          int64  `json:"id"`
		PortfolioID int64  `json:"portfolio_id"`
		Type        string `json:"type"`
		Date        string `json:"date"`
		Amount      string `json:"amount"`
	}
	if err := json.NewDecoder(updateRec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if updated.ID != txID || updated.PortfolioID != alicePortfolio || updated.Type != "buy" || updated.Date != "2024-02-01" || updated.Amount != "50" {
		t.Fatalf("updated = %+v, want id=%d portfolio=%d type=buy date=2024-02-01 amount=50", updated, txID, alicePortfolio)
	}

	// The invalid and cross-user attempts above must not have leaked into
	// the stored row: the list reflects only the successful update.
	listPath := fmt.Sprintf("/api/portfolios/%d/transactions", alicePortfolio)
	var list []struct {
		Type string `json:"type"`
		Date string `json:"date"`
	}
	if err := json.NewDecoder(env.do(alice, http.MethodGet, listPath, nil).Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Type != "buy" || list[0].Date != "2024-02-01" {
		t.Fatalf("list after update = %+v, want one buy on 2024-02-01", list)
	}

	if rec := env.do(alice, http.MethodDelete, txURL, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	// Second delete and update of a deleted row are 404.
	if rec := env.do(alice, http.MethodDelete, txURL, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete again status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := env.do(alice, http.MethodPut, txURL, deposit); rec.Code != http.StatusNotFound {
		t.Fatalf("update deleted status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if err := json.NewDecoder(env.do(alice, http.MethodGet, listPath, nil).Body).Decode(&list); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list after delete = %+v, want empty", list)
	}
}

// TestTransactionsCurrency covers GLG-73 and GLG-136: a trade's currency is
// always the instrument's own currency, and a request body currency that
// contradicts it is rejected instead of silently overwritten, while a cash
// row (no instrument) takes its currency from the request, validated against
// transaction.ValidCurrencies and defaulting to TRY.
func TestTransactionsCurrency(t *testing.T) {
	env := newTransactionTestEnv(t)
	session := env.login("alice@example.com")
	txPath := fmt.Sprintf("/api/portfolios/%d/transactions", env.createPortfolio(session, "Test"))

	res, err := env.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('AAPL', 'US', 'Apple Inc.', 'USD')`)
	if err != nil {
		t.Fatalf("insert USD instrument: %v", err)
	}
	usdInstrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("usd instrument id: %v", err)
	}

	currencyOf := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		var resp struct {
			Currency string `json:"currency"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp.Currency
	}

	// buy on a USD instrument with no currency in the body stores USD.
	buyRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "buy", "date": "2024-01-01", "instrument_id": usdInstrumentID,
		"quantity": "1", "price": "100",
	})
	if buyRec.Code != http.StatusCreated {
		t.Fatalf("buy status = %d, want %d; body: %s", buyRec.Code, http.StatusCreated, buyRec.Body.String())
	}
	if got := currencyOf(buyRec); got != "USD" {
		t.Fatalf("buy currency = %q, want USD", got)
	}

	// buy naming a currency the instrument does not use is rejected.
	buyEURRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "buy", "date": "2024-01-01", "instrument_id": usdInstrumentID,
		"quantity": "1", "price": "100", "currency": "EUR",
	})
	if buyEURRec.Code != http.StatusBadRequest {
		t.Fatalf("buy EUR status = %d, want %d; body: %s", buyEURRec.Code, http.StatusBadRequest, buyEURRec.Body.String())
	}

	// dividend repeating the instrument's own currency is accepted.
	dividendRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "dividend", "date": "2024-01-01", "instrument_id": usdInstrumentID,
		"amount": "5", "currency": "USD",
	})
	if dividendRec.Code != http.StatusCreated {
		t.Fatalf("dividend status = %d, want %d; body: %s", dividendRec.Code, http.StatusCreated, dividendRec.Body.String())
	}
	if got := currencyOf(dividendRec); got != "USD" {
		t.Fatalf("dividend currency = %q, want USD", got)
	}

	// dividend in another currency is rejected, not stored as the
	// instrument's currency (GLG-136).
	dividendTRYRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "dividend", "date": "2024-01-01", "instrument_id": usdInstrumentID,
		"amount": "5", "currency": "TRY",
	})
	if dividendTRYRec.Code != http.StatusBadRequest {
		t.Fatalf("dividend TRY status = %d, want %d; body: %s", dividendTRYRec.Code, http.StatusBadRequest, dividendTRYRec.Body.String())
	}
	if body := dividendTRYRec.Body.String(); !strings.Contains(body, "must match instrument currency (USD)") {
		t.Fatalf("dividend TRY body = %s, want the instrument currency in the error", body)
	}

	// dividend with no instrument is interest income (GLG-533): it takes the
	// currency it names, like a deposit.
	interestRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "dividend", "date": "2024-01-01", "amount": "5", "currency": "USD",
	})
	if interestRec.Code != http.StatusCreated {
		t.Fatalf("interest status = %d, want %d; body: %s", interestRec.Code, http.StatusCreated, interestRec.Body.String())
	}
	if got := currencyOf(interestRec); got != "USD" {
		t.Fatalf("interest currency = %q, want USD", got)
	}

	// ...and an unknown currency is rejected.
	interestGBPRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "dividend", "date": "2024-01-01", "amount": "5", "currency": "GBP",
	})
	if interestGBPRec.Code != http.StatusBadRequest {
		t.Fatalf("interest GBP status = %d, want %d; body: %s", interestGBPRec.Code, http.StatusBadRequest, interestGBPRec.Body.String())
	}

	// deposit with currency: "EUR" stores EUR.
	depositEURRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "deposit", "date": "2024-01-01", "amount": "100", "currency": "EUR",
	})
	if depositEURRec.Code != http.StatusCreated {
		t.Fatalf("deposit EUR status = %d, want %d; body: %s", depositEURRec.Code, http.StatusCreated, depositEURRec.Body.String())
	}
	if got := currencyOf(depositEURRec); got != "EUR" {
		t.Fatalf("deposit EUR currency = %q, want EUR", got)
	}

	// deposit with currency: "GBP" is rejected.
	depositGBPRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "deposit", "date": "2024-01-01", "amount": "100", "currency": "GBP",
	})
	if depositGBPRec.Code != http.StatusBadRequest {
		t.Fatalf("deposit GBP status = %d, want %d; body: %s", depositGBPRec.Code, http.StatusBadRequest, depositGBPRec.Body.String())
	}

	// deposit without a currency defaults to TRY.
	depositDefaultRec := env.do(session, http.MethodPost, txPath, map[string]any{
		"type": "deposit", "date": "2024-01-01", "amount": "100",
	})
	if depositDefaultRec.Code != http.StatusCreated {
		t.Fatalf("deposit default status = %d, want %d; body: %s", depositDefaultRec.Code, http.StatusCreated, depositDefaultRec.Body.String())
	}
	if got := currencyOf(depositDefaultRec); got != "TRY" {
		t.Fatalf("deposit default currency = %q, want TRY", got)
	}
}

// TestTransactionsListPaging guards GLG-141: the list endpoint hands out one
// page at a time, sized by ?limit= and continued with the ?before= cursor
// from the previous page's X-Next-Cursor header. Several transactions share a
// date on purpose -- both the order and the cursor are (date, id), so paging
// across a shared date must neither skip nor repeat a row.
func TestTransactionsListPaging(t *testing.T) {
	env := newTransactionTestEnv(t)
	session := env.login("alice@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	txPath := fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID)

	// Dates repeated and out of order, so row ids do not line up with the
	// date order and a cursor on either half alone would misbehave.
	dates := []string{"2024-01-02", "2024-01-01", "2024-01-02", "2024-01-03", "2024-01-01", "2024-01-02", "2024-01-01"}
	ids := make([]int64, len(dates))
	for i, date := range dates {
		ids[i] = env.createTransaction(session, portfolioID, map[string]any{"type": "deposit", "date": date, "amount": "100"})
	}
	// Newest first, ties broken by descending id.
	want := []int64{ids[3], ids[5], ids[2], ids[0], ids[6], ids[4], ids[1]}

	list := func(query string) ([]int64, string) {
		t.Helper()
		rec := env.do(session, http.MethodGet, txPath+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s%s status = %d, want %d; body: %s", txPath, query, rec.Code, http.StatusOK, rec.Body.String())
		}
		var page []struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s%s: %v", txPath, query, err)
		}
		got := make([]int64, len(page))
		for i, tx := range page {
			got[i] = tx.ID
		}
		return got, rec.Header().Get("X-Next-Cursor")
	}

	var paged []int64
	query := "?limit=2"
	for page := 0; ; page++ {
		if page > len(dates) {
			t.Fatalf("paging did not end after %d pages; collected %v", page, paged)
		}
		got, cursor := list(query)
		if len(got) == 0 {
			t.Fatalf("page %d (%s) is empty but a cursor pointed at it; collected %v", page, query, paged)
		}
		paged = append(paged, got...)
		if cursor == "" {
			break
		}
		query = "?limit=2&before=" + url.QueryEscape(cursor)
	}
	if !slices.Equal(paged, want) {
		t.Errorf("paged ids = %v, want %v (every row exactly once, newest first)", paged, want)
	}

	// No params: still a plain array, the whole list under the default page
	// size, and no cursor to follow.
	all, cursor := list("")
	if !slices.Equal(all, want) {
		t.Errorf("unpaged ids = %v, want %v", all, want)
	}
	if cursor != "" {
		t.Errorf("X-Next-Cursor on a complete list = %q, want empty", cursor)
	}

	// A malformed cursor is a 400, not a silent re-read of the newest page.
	if bad := env.do(session, http.MethodGet, txPath+"?before=nonsense", nil); bad.Code != http.StatusBadRequest {
		t.Errorf("malformed cursor status = %d, want %d; body: %s", bad.Code, http.StatusBadRequest, bad.Body.String())
	}

	// Summary and history read every transaction internally, so paging must
	// not reach them: 7 deposits of 100 is 700 cash and a 7-transaction
	// history, however small a page is.
	var summary struct {
		Cash string `json:"cash"`
	}
	rec := env.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", portfolioID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Cash != "700.00" {
		t.Errorf("summary cash = %s, want 700.00 (summary must still see every transaction)", summary.Cash)
	}
}

// TestTransactionsExportCSV covers the CSV export (GLG-563): the frozen
// header with market appended last (GLG-600), a note that needs both CSV quoting and the formula-injection
// prefix, quantity and price left empty outside buy and sell, split rows
// left out, the list's type and instrument filters (GLG-613), and 404
// for another user's portfolio.
func TestTransactionsExportCSV(t *testing.T) {
	env := newTransactionTestEnv(t)
	alice := env.login("alice@example.com")
	portfolioID := env.createPortfolio(alice, "Test")
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-02", "instrument_id": env.instrumentID,
		"quantity": "10", "price": "12.5", "fee": "1.25",
	})
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "deposit", "date": "2024-01-01", "amount": "1000", "note": `=SUM(A1), "x"`,
	})
	if _, err := env.db.Exec(`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (?, '2024-01-03', '2', 'manual')`, env.instrumentID); err != nil {
		t.Fatalf("insert split: %v", err)
	}

	path := fmt.Sprintf("/api/portfolios/%d/transactions.csv", portfolioID)
	rec := env.do(alice, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d; body: %s", path, rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	want := "date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
		"2024-01-01,deposit,,,,1000,0,TRY,\"'=SUM(A1), \"\"x\"\"\",\n" +
		"2024-01-02,buy,TEST,10,12.5,125,1.25,TRY,,BIST\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}

	// The list's ?type= and ?instrument_id= filters narrow the export too
	// (GLG-613), and a bad value is the same 400 as on the list.
	for query, wantBody := range map[string]string{
		"?type=deposit": "date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
			"2024-01-01,deposit,,,,1000,0,TRY,\"'=SUM(A1), \"\"x\"\"\",\n",
		fmt.Sprintf("?instrument_id=%d", env.instrumentID): "date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
			"2024-01-02,buy,TEST,10,12.5,125,1.25,TRY,,BIST\n",
	} {
		rec := env.do(alice, http.MethodGet, path+query, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != wantBody {
			t.Errorf("GET %s = %d\n%s\nwant 200\n%s", query, rec.Code, rec.Body.String(), wantBody)
		}
	}
	for query, msg := range map[string]string{"?type=bogus": "type: invalid", "?instrument_id=0": "instrument_id: must be a positive integer"} {
		rec := env.do(alice, http.MethodGet, path+query, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), msg) {
			t.Errorf("GET %s = %d %s, want 400 %q", query, rec.Code, rec.Body.String(), msg)
		}
	}

	// A symbol gets the same formula guard as the note (GLG-623).
	if _, err := env.db.Exec(`UPDATE instruments SET symbol = '=X' WHERE id = ?`, env.instrumentID); err != nil {
		t.Fatalf("rename instrument: %v", err)
	}
	rec = env.do(alice, http.MethodGet, path+"?type=buy", nil)
	if want := "2024-01-02,buy,'=X,10,12.5,125,1.25,TRY,,BIST\n"; !strings.HasSuffix(rec.Body.String(), want) {
		t.Errorf("export with symbol =X =\n%s\nwant a row\n%s", rec.Body.String(), want)
	}

	bob := env.login("bob@example.com")
	if rec := env.do(bob, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other user's export status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestTransactionsExportCSVBonusStoredDate covers GLG-894: a zero-price
// buy that books a split's bonus shares is exported on its stored date, not
// on the ex-date List moves it to, and ?from= keeps it as the list does.
func TestTransactionsExportCSVBonusStoredDate(t *testing.T) {
	env := newTransactionTestEnv(t)
	alice := env.login("alice@example.com")
	portfolioID := env.createPortfolio(alice, "Test")
	if _, err := env.db.Exec(`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (?, '2024-03-10', '2', 'manual')`, env.instrumentID); err != nil {
		t.Fatalf("insert split: %v", err)
	}
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-03-01", "instrument_id": env.instrumentID, "quantity": "10", "price": "100",
	})
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-03-15", "instrument_id": env.instrumentID, "quantity": "10", "price": "0",
	})

	path := fmt.Sprintf("/api/portfolios/%d/transactions.csv?from=2024-03-12", portfolioID)
	rec := env.do(alice, http.MethodGet, path, nil)
	want := "date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
		"2024-03-15,buy,TEST,10,0,0,0,TRY,,BIST\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("GET %s = %d\n%s\nwant 200\n%s", path, rec.Code, rec.Body.String(), want)
	}
}

// TestTransactionsExportCSVReimports covers GLG-653: a value in thousands
// shape (amount 7.407, price 150.125, fee 1.125) is the CSV import's
// ambiguous_number, so the export writes it with a trailing zero and the
// file parses back without error rows and to the same values.
func TestTransactionsExportCSVReimports(t *testing.T) {
	env := newTransactionTestEnv(t)
	alice := env.login("alice@example.com")
	portfolioID := env.createPortfolio(alice, "Test")
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-02", "instrument_id": env.instrumentID,
		"quantity": "0.00012345", "price": "60000",
	})
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-03", "instrument_id": env.instrumentID,
		"quantity": "2", "price": "150.125", "fee": "1.125",
	})
	// quantity × price has 10 fraction digits, past the import's digit cap
	// (GLG-853).
	env.createTransaction(alice, portfolioID, map[string]any{
		"type": "buy", "date": "2024-01-04", "instrument_id": env.instrumentID,
		"quantity": "0.00123456", "price": "2345678.12",
	})

	rec := env.do(alice, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/transactions.csv", portfolioID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	st, err := statementcsv.Parse(rows)
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("parse export errors = %+v, want none", st.Errors)
	}
	if len(st.Trades) != 3 {
		t.Fatalf("parsed %d trades, want 3", len(st.Trades))
	}
	for i, want := range [][3]string{{"7.407", "60000", "0"}, {"300.25", "150.125", "1.125"}, {"2895.8803798272", "2345678.12", "0"}} {
		tr := st.Trades[i]
		if tr.Amount.String() != want[0] || tr.Price.String() != want[1] || tr.Fee.String() != want[2] {
			t.Errorf("trade %d amount, price, fee = %s, %s, %s; want %v", i, tr.Amount, tr.Price, tr.Fee, want)
		}
	}
}

// TestTransactionsDateRange covers GLG-601: ?from= and ?to= are an inclusive
// date range on both the paged list and the CSV export, a range bound that
// is not YYYY-MM-DD or from after to is a 400, and paging walks inside the
// range.
func TestTransactionsDateRange(t *testing.T) {
	env := newTransactionTestEnv(t)
	session := env.login("alice@example.com")
	portfolioID := env.createPortfolio(session, "Test")
	ids := map[string]int64{}
	for _, date := range []string{"2026-01-31", "2026-02-01", "2026-02-28", "2026-03-01"} {
		ids[date] = env.createTransaction(session, portfolioID, map[string]any{"type": "deposit", "date": date, "amount": "100"})
	}
	txPath := fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID)
	const rangeQuery = "?from=2026-02-01&to=2026-02-28"

	list := func(query string) ([]int64, string) {
		t.Helper()
		rec := env.do(session, http.MethodGet, txPath+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body: %s", query, rec.Code, http.StatusOK, rec.Body.String())
		}
		var page []struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", query, err)
		}
		got := make([]int64, len(page))
		for i, tx := range page {
			got[i] = tx.ID
		}
		return got, rec.Header().Get("X-Next-Cursor")
	}

	want := []int64{ids["2026-02-28"], ids["2026-02-01"]}
	if got, _ := list(rangeQuery); !slices.Equal(got, want) {
		t.Errorf("ranged list ids = %v, want %v (the February rows, newest first)", got, want)
	}
	// Paging stays inside the range: page two holds the other February row
	// and nothing from January.
	first, cursor := list(rangeQuery + "&limit=1")
	if cursor == "" {
		t.Fatalf("no X-Next-Cursor on the first of two ranged pages")
	}
	second, cursor := list(rangeQuery + "&limit=1&before=" + url.QueryEscape(cursor))
	if got := append(first, second...); !slices.Equal(got, want) || cursor != "" {
		t.Errorf("ranged paging ids = %v, cursor %q; want %v and no cursor", got, cursor, want)
	}

	rec := env.do(session, http.MethodGet, txPath+".csv"+rangeQuery, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ranged CSV status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	wantCSV := "date,type,symbol,quantity,price,amount,fee,currency,note,market\n" +
		"2026-02-01,deposit,,,,100,0,TRY,,\n" +
		"2026-02-28,deposit,,,,100,0,TRY,,\n"
	if got := rec.Body.String(); got != wantCSV {
		t.Errorf("ranged CSV =\n%s\nwant\n%s", got, wantCSV)
	}

	for _, path := range []string{txPath, txPath + ".csv"} {
		for query, msg := range map[string]string{
			"?from=2026-2-1":                 "from must be YYYY-MM-DD",
			"?to=nonsense":                   "to must be YYYY-MM-DD",
			"?from=2026-03-01&to=2026-02-01": "from must not be after to",
		} {
			rec := env.do(session, http.MethodGet, path+query, nil)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), msg) {
				t.Errorf("GET %s%s = %d %s, want 400 %q", path, query, rec.Code, rec.Body.String(), msg)
			}
		}
	}
}

// TestTransactionsFilterByImport covers GLG-931: ?import_id=<n> keeps one
// import's rows and ?import_id=none the hand-entered ones, on the paged list
// (paging over the filtered rows only) and the CSV export alike. Another
// user's import id is an empty list, not an error that says it exists, and
// a value that is neither a positive integer nor none is a 400.
func TestTransactionsFilterByImport(t *testing.T) {
	env := newTransactionTestEnv(t)
	alice := env.login("alice@example.com")
	portfolioID := env.createPortfolio(alice, "Test")
	bob := env.login("bob@example.com")
	bobPortfolioID := env.createPortfolio(bob, "Bob")

	// addImport records an import of the portfolio's owner and moves the
	// given transactions into it, as the commit endpoint would.
	addImport := func(portfolioID int64, txIDs ...int64) int64 {
		t.Helper()
		res, err := env.db.Exec(`INSERT INTO statement_imports (user_id, portfolio_id, broker, period_start, period_end, file_sha256, created_at)
			SELECT user_id, id, 'csv', '2024-01-01', '2024-01-31', ?, '2024-02-01T00:00:00Z' FROM portfolios WHERE id = ?`,
			fmt.Sprintf("%064d", portfolioID*1000+int64(len(txIDs))), portfolioID)
		if err != nil {
			t.Fatalf("insert import: %v", err)
		}
		importID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("import id: %v", err)
		}
		for _, id := range txIDs {
			if _, err := env.db.Exec(`UPDATE transactions SET import_id = ? WHERE id = ?`, importID, id); err != nil {
				t.Fatalf("set import_id: %v", err)
			}
		}
		return importID
	}

	deposit := func(session string, portfolioID int64, date, amount string) int64 {
		return env.createTransaction(session, portfolioID, map[string]any{"type": "deposit", "date": date, "amount": amount})
	}
	imported := []int64{
		deposit(alice, portfolioID, "2024-01-02", "1"),
		deposit(alice, portfolioID, "2024-01-03", "2"),
		deposit(alice, portfolioID, "2024-01-03", "3"),
	}
	byHand := deposit(alice, portfolioID, "2024-01-04", "4")
	other := deposit(alice, portfolioID, "2024-01-05", "5")
	importID := addImport(portfolioID, imported...)
	addImport(portfolioID, other)
	bobImportID := addImport(bobPortfolioID, deposit(bob, bobPortfolioID, "2024-01-02", "9"))

	txPath := fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID)
	list := func(query string) ([]int64, string) {
		t.Helper()
		rec := env.do(alice, http.MethodGet, txPath+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body: %s", query, rec.Code, http.StatusOK, rec.Body.String())
		}
		var page []struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", query, err)
		}
		got := []int64{}
		for _, tx := range page {
			got = append(got, tx.ID)
		}
		return got, rec.Header().Get("X-Next-Cursor")
	}

	importQuery := fmt.Sprintf("?import_id=%d", importID)
	want := []int64{imported[2], imported[1], imported[0]}
	if got, _ := list(importQuery); !slices.Equal(got, want) {
		t.Errorf("import list ids = %v, want %v (the import's rows, newest first)", got, want)
	}
	if got, _ := list("?import_id=none"); !slices.Equal(got, []int64{byHand}) {
		t.Errorf("import_id=none ids = %v, want [%d] (the hand-entered row)", got, byHand)
	}
	if got, _ := list(fmt.Sprintf("?import_id=%d", bobImportID)); len(got) != 0 {
		t.Errorf("other user's import ids = %v, want none", got)
	}

	// Paging with the filter on visits every row of the import once.
	var paged []int64
	query := importQuery + "&limit=1"
	for page := 0; ; page++ {
		if page > len(want) {
			t.Fatalf("paging did not end; collected %v", paged)
		}
		got, cursor := list(query)
		paged = append(paged, got...)
		if cursor == "" {
			break
		}
		query = importQuery + "&limit=1&before=" + url.QueryEscape(cursor)
	}
	if !slices.Equal(paged, want) {
		t.Errorf("paged import ids = %v, want %v", paged, want)
	}

	csvHead := "date,type,symbol,quantity,price,amount,fee,currency,note,market\n"
	for query, wantCSV := range map[string]string{
		importQuery: csvHead +
			"2024-01-02,deposit,,,,1,0,TRY,,\n" +
			"2024-01-03,deposit,,,,2,0,TRY,,\n" +
			"2024-01-03,deposit,,,,3,0,TRY,,\n",
		"?import_id=none":                         csvHead + "2024-01-04,deposit,,,,4,0,TRY,,\n",
		fmt.Sprintf("?import_id=%d", bobImportID): csvHead,
	} {
		rec := env.do(alice, http.MethodGet, txPath+".csv"+query, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != wantCSV {
			t.Errorf("GET .csv%s = %d\n%s\nwant 200\n%s", query, rec.Code, rec.Body.String(), wantCSV)
		}
	}

	for _, path := range []string{txPath, txPath + ".csv"} {
		for _, query := range []string{"?import_id=abc", "?import_id=0", "?import_id=-1", "?import_id=NONE"} {
			rec := env.do(alice, http.MethodGet, path+query, nil)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "import_id: must be a positive integer or none") {
				t.Errorf("GET %s%s = %d %s, want 400", path, query, rec.Code, rec.Body.String())
			}
		}
	}
}
