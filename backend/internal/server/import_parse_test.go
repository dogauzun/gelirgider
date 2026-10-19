package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/scheduler"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// The parse endpoint runs a real PDF through pdftext before it reaches a
// parser, so these tests upload a real (if empty) PDF and register a stub
// parser that ignores the extracted rows and returns a fixed statement. No
// broker sample is needed, and none may enter the repository (ADR 0006).
func init() {
	statement.Parsers["test"] = func([][]string) (statement.Statement, error) { return stubStatement, nil }
	statement.Parsers["test-fund"] = func([][]string) (statement.Statement, error) { return fundStatement, nil }
	statement.Parsers["test-lookup"] = func([][]string) (statement.Statement, error) { return lookupStatement, nil }
	statement.Parsers["test-bonus"] = func([][]string) (statement.Statement, error) { return bonusStatement, nil }
	statement.Parsers["test-alias"] = func([][]string) (statement.Statement, error) { return aliasStatement, nil }
}

// aliasStatement holds and buys ZZZZZ, a symbol no instrument has, which
// the alias test maps to TEST with a stored pick.
var aliasStatement = statement.Statement{
	Broker: "test-alias", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", Currency: "TRY",
	Trades: []statement.Trade{
		{Date: "2026-01-10", Symbol: "ZZZZZ", Side: "buy", Quantity: dec("5"), Price: dec("20"), Amount: dec("100"), Fee: dec("0"), Currency: "TRY"},
	},
	Holdings: []statement.Holding{
		{Symbol: "ZZZZZ", Quantity: dec("15"), AvgCost: dec("20"), Price: dec("20"), Currency: "TRY"},
	},
}

// bonusStatement is a bonus issue as the garanti_yatirim parser books it: a
// buy at price 0 and amount 0 with a fractional quantity.
var bonusStatement = statement.Statement{
	Broker: "test-bonus", PeriodStart: "2023-08-01", PeriodEnd: "2023-08-31", Currency: "TRY",
	Trades: []statement.Trade{
		{Date: "2023-08-22", Symbol: "TEST", Side: "buy", Quantity: dec("1500.75"), Price: decimal.Zero, Amount: decimal.Zero, Currency: "TRY"},
	},
}

// lookupStatement names funds no instrument exists for: GJH twice,
// NOP that TEFAS does not know, a fund name that is no code, A01..A08 and
// then A09, the 11th distinct code, and XYZ without a market hint.
var lookupStatement = func() statement.Statement {
	st := statement.Statement{Broker: "test-lookup", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS"}
	trade := func(symbol, market string) statement.Trade {
		return statement.Trade{Date: "2025-12-10", Symbol: symbol, Side: "buy", Quantity: dec("10"), Price: dec("1.5"), Amount: dec("15"), Fee: dec("0"), Currency: "TRY", Market: market}
	}
	for _, symbol := range []string{"GJH", "GJH", "NOP", "AZİMUT PYŞ DEĞİŞKEN FON"} {
		st.Trades = append(st.Trades, trade(symbol, "TEFAS"))
	}
	for i := 1; i <= 9; i++ {
		st.Trades = append(st.Trades, trade(fmt.Sprintf("A%02d", i), "TEFAS"))
	}
	st.Trades = append(st.Trades, trade("XYZ", ""))
	return st
}()

// fundStatement is a TRY statement whose holdings are TEFAS funds, which its
// Market says: TRY alone would read them as BIST shares.
var fundStatement = statement.Statement{
	Broker:      "test-fund",
	PeriodStart: "2025-12-01",
	PeriodEnd:   "2025-12-31",
	Currency:    "TRY",
	Market:      "TEFAS",
	Holdings: []statement.Holding{
		{Symbol: "GJH", Quantity: dec("100"), AvgCost: dec("1.5"), Currency: "TRY"},
	},
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// stubStatement covers one row of every kind the preview has to report. The
// test environment seeds a single instrument: TEST on BIST, priced in TRY.
var stubStatement = statement.Statement{
	Broker:      "test",
	PeriodStart: "2025-09-01",
	PeriodEnd:   "2025-09-30",
	Currency:    "TRY",
	Trades: []statement.Trade{
		{Date: "2025-09-26", Symbol: "TEST", Side: "buy", Quantity: dec("0.006218342"), Price: dec("331.82"), Amount: dec("2.06"), Fee: dec("0"), Currency: "TRY"},
		{Date: "2025-09-27", Symbol: "NOSUCH", Side: "sell", Quantity: dec("2"), Price: dec("10"), Amount: dec("20"), Fee: dec("0.1"), Currency: "TRY"},
		{Date: "2025-09-28", Symbol: "TEST", Side: "buy", Quantity: dec("1"), Price: dec("5"), Amount: dec("5"), Fee: dec("0"), Currency: "EUR"},
	},
	Dividends: []statement.Dividend{
		{Date: "2025-09-26", Symbol: "TEST", Gross: dec("2.65"), Tax: dec("0.53"), Currency: "TRY"},
	},
	Cash: []statement.CashMovement{
		{Date: "2025-09-11", Kind: "deposit", Amount: dec("8237.46"), Currency: "TRY", Description: "Bank - TR000000000000000000001234"},
	},
	// The quantity a portfolio that already held 1 TEST ends the period on,
	// once the buy above is applied: the reconciliation test builds exactly
	// that history and expects a match, while a portfolio with no history at
	// all is short of it.
	Holdings: []statement.Holding{
		{Symbol: "TEST", Quantity: dec("1.006218342"), AvgCost: dec("587.42"), Currency: "TRY"},
	},
	Skipped: []statement.Skipped{
		{Section: "HESAP İŞLEMLERİ", Reason: "not executed", Row: []string{"11/09/25 10:41:12", "11/09/25 10:41:15", "Para Yatırma", "Bank - TR000000000000000000001234", "İptal Edildi", "10,00 TRY"}},
	},
	Errors: []statement.ParseError{
		{Section: "Hesap Ekstresi", Code: "unknown_row", Value: "1234-6000001 Nolu Hesabın Banka Yeni İşlemi", Message: "unknown ledger row", Date: "2025-09-29", Row: []string{"29/09/2025", "1234-6000001 Nolu Hesabın Banka Yeni İşlemi", "+1.00"}},
	},
}

// emptyPDF returns a syntactically valid one-page PDF with no text, padded to
// at least size bytes with a comment line so a test can exceed a body limit.
func emptyPDF(size int) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	if size > 0 {
		buf.WriteByte('%')
		buf.Write(bytes.Repeat([]byte("p"), size))
		buf.WriteByte('\n')
	}
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// importDeps are the import routes' repos on e's database, with no fetcher;
// a test that registers the routes itself sets the ones it needs.
func (e *transactionTestEnv) importDeps() statement.Deps {
	return statement.Deps{Imports: statement.NewImportRepo(e.db), Transactions: transaction.NewRepo(e.db), Splits: split.NewRepo(e.db), Portfolios: portfolio.NewRepo(e.db), Instruments: instrument.NewRepo(e.db), PriceSources: holdings.PriceSources{Prices: price.NewRepo(e.db), Manual: price.NewManualRepo(e.db)}}
}

// upload posts file to the parse endpoint as the given session.
func upload(e *transactionTestEnv, sessionID, broker string, portfolioID int64, file []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	return uploadPicks(e, sessionID, broker, portfolioID, file, "")
}

// uploadPicks is upload with the picks form field, sent when not "".
func uploadPicks(e *transactionTestEnv, sessionID, broker string, portfolioID int64, file []byte, picks string) *httptest.ResponseRecorder {
	e.t.Helper()
	return uploadFields(e, sessionID, broker, portfolioID, file, map[string]string{"picks": picks})
}

// uploadFields is upload with extra form fields, each sent when not "".
func uploadFields(e *transactionTestEnv, sessionID, broker string, portfolioID int64, file []byte, fields map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("broker", broker); err != nil {
		e.t.Fatalf("write broker: %v", err)
	}
	if err := form.WriteField("portfolio_id", strconv.FormatInt(portfolioID, 10)); err != nil {
		e.t.Fatalf("write portfolio_id: %v", err)
	}
	for name, value := range fields {
		if value == "" {
			continue
		}
		if err := form.WriteField(name, value); err != nil {
			e.t.Fatalf("write %s: %v", name, err)
		}
	}
	part, err := form.CreateFormFile("file", "statement.pdf")
	if err != nil {
		e.t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(file); err != nil {
		e.t.Fatalf("write file part: %v", err)
	}
	if err := form.Close(); err != nil {
		e.t.Fatalf("close form: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/imports/parse", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

// preview is the parse response, as the import page reads it.
type preview struct {
	Broker      string `json:"broker"`
	SHA256      string `json:"sha256"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Currency    string `json:"currency"`
	Warnings    []struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		ImportID    int64  `json:"import_id"`
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	} `json:"warnings"`
	Rows []struct {
		Key          string   `json:"key"`
		Status       string   `json:"status"`
		Type         string   `json:"type"`
		Date         string   `json:"date"`
		Symbol       string   `json:"symbol"`
		Market       string   `json:"market"`
		InstrumentID *int64   `json:"instrument_id"`
		Quantity     string   `json:"quantity"`
		Price        string   `json:"price"`
		Amount       string   `json:"amount"`
		Fee          string   `json:"fee"`
		Currency     string   `json:"currency"`
		Fallback     bool     `json:"currency_fallback"`
		Description  string   `json:"description"`
		Reason       string   `json:"reason"`
		Code         string   `json:"code"`
		Value        string   `json:"value"`
		Message      string   `json:"message"`
		Unknown      bool     `json:"unknown_instrument"`
		Raw          []string `json:"raw"`
		DuplicateOf  string   `json:"duplicate_of"`
		ResolvedBy   string   `json:"resolved_by"`
		InstrSymbol  string   `json:"instrument_symbol"`
		InstrName    string   `json:"instrument_name"`
		Opening      bool     `json:"opening"`
		Approximate  bool     `json:"approximate_price"`
		CostPrice    bool     `json:"cost_price"`
		Closing      bool     `json:"closing"`
	} `json:"rows"`
	Holdings       []map[string]string `json:"holdings"`
	Reconciliation []struct {
		Symbol            string  `json:"symbol"`
		InstrumentID      int64   `json:"instrument_id"`
		StatementQuantity *string `json:"statement_quantity"`
		PortfolioQuantity string  `json:"portfolio_quantity"`
		Match             bool    `json:"match"`
	} `json:"reconciliation"`
	CashReconciliation *struct {
		Currency        string `json:"currency"`
		StatementAmount string `json:"statement_amount"`
		PortfolioAmount string `json:"portfolio_amount"`
		Match           bool   `json:"match"`
	} `json:"cash_reconciliation"`
	Names []map[string]any `json:"names"`
}

func parsePreview(t *testing.T, rec *httptest.ResponseRecorder) preview {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("parse status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var p preview
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return p
}

// TestParseStatementPreview covers the preview: a known
// symbol resolves to an instrument, an unknown one and an unsupported
// currency are reported per row, and skipped and unreadable rows come back
// with their reason, message and raw cells. The upload is deliberately larger
// than the global 64 KB body limit, which this route is exempt from.
func TestParseStatementPreview(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("parser@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	file := emptyPDF(100 << 10)
	if len(file) <= maxBodyBytes {
		t.Fatalf("test file is %d bytes, want more than the %d byte global limit", len(file), maxBodyBytes)
	}
	p := parsePreview(t, upload(e, session, "test", portfolioID, file))

	sum := sha256.Sum256(file)
	if p.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want the file's hash", p.SHA256)
	}
	if p.Broker != "test" || p.Currency != "TRY" || p.PeriodStart != "2025-09-01" || p.PeriodEnd != "2025-09-30" {
		t.Errorf("preview envelope = %+v, want the stub statement's broker, currency and period", p)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("warnings = %v, want none without an earlier import", p.Warnings)
	}
	if len(p.Holdings) != 1 || p.Holdings[0]["symbol"] != "TEST" || p.Holdings[0]["avg_cost"] != "587.42" {
		t.Errorf("holdings = %v, want the statement's snapshot passed through", p.Holdings)
	}

	// This portfolio has no history, so the preview's own buy is everything
	// it holds and the statement's position is short of it.
	if len(p.Reconciliation) != 1 {
		t.Fatalf("reconciliation = %+v, want one entry for the statement's only holding", p.Reconciliation)
	}
	if r := p.Reconciliation[0]; r.Symbol != "TEST" || r.Match ||
		r.StatementQuantity == nil || *r.StatementQuantity != "1.006218342" ||
		r.PortfolioQuantity != "0.006218342" {
		t.Errorf("reconciliation[0] = %+v, want TEST mismatched against the preview's buy alone", r)
	}

	byKey := map[string]int{}
	for i, row := range p.Rows {
		byKey[row.Key] = i
	}
	if len(p.Rows) != 7 {
		t.Fatalf("got %d rows, want 7; keys: %v", len(p.Rows), byKey)
	}

	ok := p.Rows[byKey["trade-0"]]
	if ok.Status != "ok" || ok.InstrumentID == nil || *ok.InstrumentID != e.instrumentID {
		t.Errorf("trade-0 = %+v, want ok with instrument %d", ok, e.instrumentID)
	}
	if ok.Quantity != "0.006218342" || ok.Type != "buy" || ok.Date != "2025-09-26" {
		t.Errorf("trade-0 = %+v, want the statement's buy with its 9 decimals", ok)
	}

	if unknown := p.Rows[byKey["trade-1"]]; unknown.Status != "unknown_instrument" || unknown.InstrumentID != nil || unknown.Symbol != "NOSUCH" {
		t.Errorf("trade-1 = %+v, want unknown_instrument and no instrument id", unknown)
	}
	if unsupported := p.Rows[byKey["trade-2"]]; unsupported.Status != "unsupported_currency" || unsupported.Currency != "EUR" {
		t.Errorf("trade-2 = %+v, want unsupported_currency", unsupported)
	}
	if div := p.Rows[byKey["div-0"]]; div.Status != "ok" || div.Type != "dividend" || div.Amount != "2.65" || div.Fee != "0.53" {
		t.Errorf("div-0 = %+v, want the gross dividend with the tax as its fee", div)
	}
	if cash := p.Rows[byKey["cash-0"]]; cash.Status != "ok" || cash.Type != "deposit" || cash.Amount != "8237.46" || cash.Description != "Bank - TR** **** 1234" {
		t.Errorf("cash-0 = %+v, want an ok deposit with its description, the IBAN masked", cash)
	}
	if skipped := p.Rows[byKey["skip-0"]]; skipped.Status != "skipped" || skipped.Reason != "not executed" ||
		len(skipped.Raw) != 6 || skipped.Raw[3] != "Bank - TR** **** 1234" {
		t.Errorf("skip-0 = %+v, want skipped with the parser's reason and its cells, the IBAN masked", skipped)
	}
	failed := p.Rows[byKey["err-0"]]
	if failed.Status != "error" || failed.Code != "unknown_row" || failed.Message != "unknown ledger row" || failed.Date != "2025-09-29" {
		t.Errorf("err-0 = %+v, want an error row with its message and date", failed)
	}
	if masked := "***0001 Nolu Hesabın Banka Yeni İşlemi"; len(failed.Raw) != 3 || failed.Raw[1] != masked || failed.Value != masked {
		t.Errorf("err-0 raw = %v, value = %q, want the row's cells so the user can fix it, the account number masked", failed.Raw, failed.Value)
	}

	// Nothing is written: the preview is a proposal (ADR 0006).
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports after a parse, want none", len(imports))
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 0 {
		t.Errorf("got %d transactions after a parse, want none", len(txs))
	}
}

// TestParseStatementPassesBonusBuy checks that a bonus issue's buy at
// price 0 and amount 0 passes the commit's validation in the preview.
func TestParseStatementPassesBonusBuy(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bonus@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	p := parsePreview(t, upload(e, session, "test-bonus", portfolioID, emptyPDF(0)))
	if len(p.Rows) != 1 {
		t.Fatalf("rows = %+v, want the bonus buy", p.Rows)
	}
	if r := p.Rows[0]; r.Status != "ok" || r.Type != "buy" || r.Quantity != "1500.75" || r.Price != "0" || r.Amount != "0" {
		t.Errorf("bonus row = %+v, want an ok buy of 1500.75 at price 0, amount 0", r)
	}
}

// TestParseStatementFlagsDuplicateBookedBonus checks that the preview
// reads List's rows, which move a stored bonus buy to the ex-date of the
// split it books, and the duplicate check still pairs it on its own date.
func TestParseStatementFlagsDuplicateBookedBonus(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bonus-dup@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	e.createTransaction(session, portfolioID, map[string]any{"type": "buy", "date": "2023-08-01", "instrument_id": e.instrumentID, "quantity": "1000", "price": "10"})
	e.createTransaction(session, portfolioID, map[string]any{"type": "buy", "date": "2023-08-22", "instrument_id": e.instrumentID, "quantity": "1500.75", "price": "0"})
	if _, err := e.db.Exec(`INSERT INTO splits (instrument_id, date, ratio, source) VALUES (?, '2023-08-15', '2.50075', 'manual')`, e.instrumentID); err != nil {
		t.Fatalf("insert split: %v", err)
	}

	p := parsePreview(t, upload(e, session, "test-bonus", portfolioID, emptyPDF(0)))
	if len(p.Rows) != 1 || p.Rows[0].DuplicateOf != "2023-08-22" {
		t.Errorf("rows = %+v, want the bonus buy flagged as a duplicate of 2023-08-22", p.Rows)
	}
}

// TestParseStatementReconciliation checks that the statement's period-end
// snapshot is compared against the positions the portfolio would hold once
// the preview's ok rows are committed, and a position the statement never
// mentions is reported with no statement quantity at all.
func TestParseStatementReconciliation(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("reconcile@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	// A position the statement does not list: the same market, so it is the
	// user's to explain -- a sale the statement is missing, or a typo.
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('OTHER', 'BIST', 'Other A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	otherID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	// 1 TEST before the period, which the statement's buy of 0.006218342
	// tops up to exactly what the snapshot claims.
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2025-08-10", "instrument_id": e.instrumentID, "quantity": "1", "price": "500",
	})
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2025-08-10", "instrument_id": otherID, "quantity": "3", "price": "10",
	})
	// After the period, so the snapshot cannot know about it.
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2025-10-05", "instrument_id": e.instrumentID, "quantity": "5", "price": "500",
	})

	p := parsePreview(t, upload(e, session, "test", portfolioID, emptyPDF(0)))
	if len(p.Reconciliation) != 2 {
		t.Fatalf("reconciliation = %+v, want the statement's holding plus the position it lacks", p.Reconciliation)
	}

	matched := p.Reconciliation[0]
	if matched.Symbol != "TEST" || matched.InstrumentID != e.instrumentID || !matched.Match || matched.PortfolioQuantity != "1.006218342" ||
		matched.StatementQuantity == nil || *matched.StatementQuantity != "1.006218342" {
		t.Errorf("reconciliation[0] = %+v, want TEST matching as of the period end", matched)
	}

	unmentioned := p.Reconciliation[1]
	if unmentioned.Symbol != "OTHER" || unmentioned.InstrumentID != otherID || unmentioned.Match || unmentioned.StatementQuantity != nil ||
		unmentioned.PortfolioQuantity != "3" {
		t.Errorf("reconciliation[1] = %+v, want OTHER held but absent from the statement", unmentioned)
	}
}

// TestParseStatementReconcilesMarketHint checks that a statement's
// Market, not its currency, picks the market of its holdings and of the
// positions it is silent about, so a TRY fund statement matches the TEFAS
// fund and says nothing about a BIST share.
func TestParseStatementReconcilesMarketHint(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("reconcile-fund@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	for _, in := range []struct{ symbol, market, quantity string }{{"GJH", "TEFAS", "100"}, {"ASELS", "BIST", "10"}} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, 'x', 'TRY')`, in.symbol, in.market)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		e.createTransaction(session, portfolioID, map[string]any{
			"type": "buy", "date": "2025-11-10", "instrument_id": id, "quantity": in.quantity, "price": "1",
		})
	}

	p := parsePreview(t, upload(e, session, "test-fund", portfolioID, emptyPDF(0)))
	if len(p.Reconciliation) != 1 {
		t.Fatalf("reconciliation = %+v, want only the fund", p.Reconciliation)
	}
	got := p.Reconciliation[0]
	if got.Symbol != "GJH" || !got.Match || got.PortfolioQuantity != "100" ||
		got.StatementQuantity == nil || *got.StatementQuantity != "100" {
		t.Errorf("reconciliation[0] = %+v, want GJH matching the TEFAS position", got)
	}
}

// TestParseStatementReconcilesCash checks that the statement's cash
// balance is compared against the portfolio's cash in the statement currency
// once the preview's ok rows are applied. The stub's ok rows leave TRY cash
// at 8237.46 - 2.06 + (2.65 - 0.53) = 8237.52; the EUR buy and the unknown
// symbol's sell are not ok, so they move nothing.
func TestParseStatementReconcilesCash(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("reconcile-cash@example.com")
	portfolioID := e.createPortfolio(session, "Import")
	t.Cleanup(func() { delete(statement.Parsers, "cash") })

	for i, c := range []struct {
		balance string
		match   bool
	}{{"8238.52", false}, {"8237.52", true}} {
		st := stubStatement
		balance := dec(c.balance)
		st.CashBalance = &balance
		statement.Parsers["cash"] = func([][]string) (statement.Statement, error) { return st, nil }
		p := parsePreview(t, upload(e, session, "cash", portfolioID, emptyPDF(i)))
		got := p.CashReconciliation
		if got == nil || got.Currency != "TRY" || got.StatementAmount != c.balance ||
			got.PortfolioAmount != "8237.52" || got.Match != c.match {
			t.Errorf("balance %s: cash_reconciliation = %+v, want TRY %s against 8237.52, match %v", c.balance, got, c.balance, c.match)
		}
	}
}

// TestParseStatementWithoutRowsReturnsEmptyRows checks that a quiet
// month's statement, a cash balance and nothing else, proposes no opening
// or closing, and its rows are an empty array, not null, which the import
// page maps over.
func TestParseStatementWithoutRowsReturnsEmptyRows(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("quiet-month@example.com")
	portfolioID := e.createPortfolio(session, "Import")
	t.Cleanup(func() { delete(statement.Parsers, "quiet") })
	balance := dec("0")
	statement.Parsers["quiet"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{Broker: "quiet", PeriodStart: "2026-07-01", PeriodEnd: "2026-07-31", Currency: "TRY", CashBalance: &balance}, nil
	}

	rec := upload(e, session, "quiet", portfolioID, emptyPDF(0))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if got := string(body["rows"]); got != "[]" {
		t.Errorf("rows = %s, want []", got)
	}
}

// TestParseStatementReconcilesCashWithoutDuplicateDividend checks that
// a statement dividend flagged as repeating a hand-entered one starts
// unchecked, so the cash comparison leaves it out. The hand-entered dividend
// already put its net 2.12 in cash, so the stub's deposit and buy end on
// 8237.52, not 8239.64.
func TestParseStatementReconcilesCashWithoutDuplicateDividend(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("reconcile-dup@example.com")
	portfolioID := e.createPortfolio(session, "Import")
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "dividend", "date": "2025-09-22", "instrument_id": e.instrumentID, "amount": "2.65", "fee": "0.53",
	})
	t.Cleanup(func() { delete(statement.Parsers, "cash-dup") })
	st := stubStatement
	balance := dec("8237.52")
	st.CashBalance = &balance
	statement.Parsers["cash-dup"] = func([][]string) (statement.Statement, error) { return st, nil }

	got := parsePreview(t, upload(e, session, "cash-dup", portfolioID, emptyPDF(0))).CashReconciliation
	if got == nil || got.PortfolioAmount != "8237.52" || !got.Match {
		t.Errorf("cash_reconciliation = %+v, want 8237.52 and a match", got)
	}
}

// TestParseStatementRejectsDuplicateAndOverlap covers the checks against
// importing the same trades twice: the identical file is a 409 carrying the
// import to undo, a different file of the same statement is a same_period
// warning, a period that only overlaps is an overlap warning, and
// the other account's statement is neither.
func TestParseStatementRejectsDuplicateAndOverlap(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("duplicate-parse@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	file := emptyPDF(0)
	sum := sha256.Sum256(file)
	sha := hex.EncodeToString(sum[:])

	body := importBody(portfolioID, sha,
		map[string]any{"type": "deposit", "date": "2025-09-11", "amount": "100", "fee": "0", "currency": "TRY"},
	)
	body["period_start"], body["period_end"] = stubStatement.PeriodStart, stubStatement.PeriodEnd
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var committed struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&committed); err != nil {
		t.Fatalf("decode commit: %v", err)
	}

	rec = upload(e, session, "test", portfolioID, file)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate parse status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var conflict struct {
		Error    string `json:"error"`
		ImportID int64  `json:"import_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Error != "already_imported" || conflict.ImportID != committed.ID {
		t.Errorf("conflict = %+v, want already_imported with import %d", conflict, committed.ID)
	}

	// A quarterly statement over the month: it overlaps, it is not the same
	// statement, so it commits.
	quarter := importBody(portfolioID, fakeSHA("quarter"))
	quarter["period_start"], quarter["period_end"] = "2025-07-01", "2025-09-30"
	rec = e.do(session, http.MethodPost, "/api/imports", quarter)
	if rec.Code != http.StatusCreated {
		t.Fatalf("quarter commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var quarterly struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&quarterly); err != nil {
		t.Fatalf("decode quarter commit: %v", err)
	}

	// A different file over the periods already imported warns: the earlier
	// imports are "midas" ones, so the warning needs the same broker.
	p := parsePreview(t, upload(e, session, "test", portfolioID, emptyPDF(16)))
	if len(p.Warnings) != 0 {
		t.Errorf("warnings = %v, want none for another broker's period", p.Warnings)
	}
	// "midas" is a real parser server.go registers: put it back afterwards.
	realMidas := statement.Parsers["midas"]
	t.Cleanup(func() { statement.Parsers["midas"] = realMidas })
	statement.Parsers["midas"] = statement.Parsers["test"]
	p = parsePreview(t, upload(e, session, "midas", portfolioID, emptyPDF(16)))
	if len(p.Warnings) != 2 {
		t.Fatalf("warnings = %v, want an overlap and a same_period warning", p.Warnings)
	}
	if w := p.Warnings[0]; w.Code != "overlap" || w.ImportID != quarterly.ID || w.PeriodStart != "2025-07-01" || w.PeriodEnd != "2025-09-30" {
		t.Errorf("warnings[0] = %+v, want overlap with import %d over 2025-07-01 – 2025-09-30", w, quarterly.ID)
	}
	if w := p.Warnings[1]; w.Code != "same_period" || w.ImportID != committed.ID || w.PeriodStart != "2025-09-01" || w.PeriodEnd != "2025-09-30" {
		t.Errorf("warnings[1] = %+v, want same_period with import %d over 2025-09-01 – 2025-09-30", w, committed.ID)
	}

	// Both earlier imports are of the TRY account. The USD account's
	// statement of the month is the other account and warns about neither;
	// a statement that printed no currency may be either account,
	// so it still gets both.
	for _, c := range []struct {
		currency string
		want     int
	}{{"USD", 0}, {"", 2}} {
		st := stubStatement
		st.Currency = c.currency
		statement.Parsers["midas"] = func([][]string) (statement.Statement, error) { return st, nil }
		p = parsePreview(t, upload(e, session, "midas", portfolioID, emptyPDF(16)))
		if len(p.Warnings) != c.want {
			t.Errorf("currency %q: warnings = %v, want %d", c.currency, p.Warnings, c.want)
		}
	}
}

// TestParseStatementKeepsMidasParser checks that the overlap test, which swaps
// the real Midas parser for a stub, puts it back when it is done.
func TestParseStatementKeepsMidasParser(t *testing.T) {
	t.Run("overlap", TestParseStatementRejectsDuplicateAndOverlap)
	if statement.Parsers["midas"] == nil {
		t.Fatal(`statement.Parsers["midas"] is nil after the overlap test`)
	}
}

// TestParseStatementFlagsDuplicateDividend checks that a statement
// dividend inside the expected-dividends matching window of a hand-entered
// dividend of the same instrument carries that dividend's date, which the
// page shows as a warning and starts the row unchecked on. One imported with
// an earlier statement is dated on the payment date like the row,
// so only the same payment repeated counts, not last month's.
func TestParseStatementFlagsDuplicateDividend(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("dup-dividend@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	divRow := func(p preview) string {
		for _, row := range p.Rows {
			if row.Key == "div-0" {
				return row.DuplicateOf
			}
		}
		t.Fatalf("no div-0 row in %+v", p.Rows)
		return ""
	}

	// Added from the expected dividends panel: dated on the ex-date, four
	// days before the statement's payment date.
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "dividend", "date": "2025-09-22", "instrument_id": e.instrumentID, "amount": "2.65", "fee": "0.53",
	})
	if got := divRow(parsePreview(t, upload(e, session, "test", portfolioID, emptyPDF(0)))); got != "2025-09-22" {
		t.Errorf("duplicate_of = %q, want the hand-entered dividend of 2025-09-22", got)
	}

	// The same payment imported before takes the row instead.
	body := importBody(portfolioID, fakeSHA("earlier"),
		map[string]any{"type": "dividend", "date": "2025-09-26", "instrument_id": e.instrumentID, "amount": "2.65", "fee": "0.53", "currency": "TRY"},
	)
	body["period_start"], body["period_end"] = stubStatement.PeriodStart, stubStatement.PeriodEnd
	if rec := e.do(session, http.MethodPost, "/api/imports", body); rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := divRow(parsePreview(t, upload(e, session, "test", portfolioID, emptyPDF(16)))); got != "2025-09-26" {
		t.Errorf("duplicate_of = %q, want the imported dividend of 2025-09-26", got)
	}

	// A monthly payer: last month's statement booked the August payment,
	// 30 days before this one.
	monthly := e.createPortfolio(session, "Monthly")
	body = importBody(monthly, fakeSHA("november"),
		map[string]any{"type": "dividend", "date": "2025-08-27", "instrument_id": e.instrumentID, "amount": "2.65", "fee": "0.53", "currency": "TRY"},
	)
	body["period_start"], body["period_end"] = "2025-08-01", "2025-08-31"
	if rec := e.do(session, http.MethodPost, "/api/imports", body); rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := divRow(parsePreview(t, upload(e, session, "test", monthly, emptyPDF(0)))); got != "" {
		t.Errorf("duplicate_of = %q, want none: the imported 2025-08-27 dividend is last month's payment", got)
	}
}

// TestParseStatementFlagsDuplicateTradesAndCash checks that a statement
// trade repeating a hand-entered one of the same instrument, side, date and
// quantity, and a cash row repeating one of the same type, date, currency and
// amount, carry that transaction's date. Each earlier transaction, hand-entered
// or imported, accounts for one row only.
func TestParseStatementFlagsDuplicateTradesAndCash(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("dup-trade@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("earlier"),
		map[string]any{"type": "sell", "date": "2025-12-11", "instrument_id": e.instrumentID, "quantity": "1", "price": "10", "amount": "10", "fee": "0", "currency": "TRY"},
	)
	if rec := e.do(session, http.MethodPost, "/api/imports", body); rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	for _, tx := range []map[string]any{
		{"type": "buy", "date": "2025-12-10", "instrument_id": e.instrumentID, "quantity": "2", "price": "10"},
		{"type": "deposit", "date": "2025-12-05", "amount": "100", "currency": "TRY"},
		{"type": "dividend", "date": "2025-12-31", "amount": "3.5", "currency": "TRY"},
	} {
		e.createTransaction(session, portfolioID, tx)
	}

	st := statement.Statement{
		Broker: "test", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY",
		Trades: []statement.Trade{
			{Date: "2025-12-10", Symbol: "TEST", Side: "buy", Quantity: dec("2.0"), Price: dec("10"), Amount: dec("20"), Fee: dec("0"), Currency: "TRY"},
			{Date: "2025-12-10", Symbol: "TEST", Side: "buy", Quantity: dec("2"), Price: dec("10"), Amount: dec("20"), Fee: dec("0"), Currency: "TRY"},
			{Date: "2025-12-10", Symbol: "TEST", Side: "sell", Quantity: dec("2"), Price: dec("10"), Amount: dec("20"), Fee: dec("0"), Currency: "TRY"},
			{Date: "2025-12-11", Symbol: "TEST", Side: "sell", Quantity: dec("1"), Price: dec("10"), Amount: dec("10"), Fee: dec("0"), Currency: "TRY"},
		},
		Cash: []statement.CashMovement{
			{Date: "2025-12-05", Kind: "deposit", Amount: dec("100.00"), Currency: "TRY"},
			{Date: "2025-12-05", Kind: "withdrawal", Amount: dec("100"), Currency: "TRY"},
			{Date: "2025-12-06", Kind: "deposit", Amount: dec("100"), Currency: "TRY"},
			{Date: "2025-12-31", Kind: "dividend", Amount: dec("3.5"), Currency: "TRY"},
		},
	}
	statement.Parsers["dup-trade"] = func([][]string) (statement.Statement, error) { return st, nil }
	t.Cleanup(func() { delete(statement.Parsers, "dup-trade") })

	want := map[string]string{
		"trade-0": "2025-12-10", // same buy, quantity printed differently
		"trade-1": "",           // the hand-entered buy is already taken
		"trade-2": "",           // other side
		"trade-3": "2025-12-11", // repeats an imported sell
		"cash-0":  "2025-12-05",
		"cash-1":  "", // other type
		"cash-2":  "", // other date
		"cash-3":  "2025-12-31",
	}
	p := parsePreview(t, upload(e, session, "dup-trade", portfolioID, emptyPDF(0)))
	for _, row := range p.Rows {
		if row.DuplicateOf != want[row.Key] {
			t.Errorf("%s: duplicate_of = %q, want %q", row.Key, row.DuplicateOf, want[row.Key])
		}
	}
	if len(p.Rows) != len(want) {
		t.Errorf("rows = %d, want %d", len(p.Rows), len(want))
	}
}

// TestParseCSVAgainstEarlierImports checks that a CSV is one file, not an
// account, so its import is recorded with no account currency. A second CSV
// over the same dates or part of them overlaps, whatever its majority
// currency, and never blocks the commit: its rows are checked one by one. A
// row an earlier import already booked comes back with that transaction's
// date.
func TestParseCSVAgainstEarlierImports(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-repeat@example.com")
	portfolioID := e.createPortfolio(session, "Import")
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USX', 'US', 'x', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	usx, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	commit := func(p preview, rows ...map[string]any) *httptest.ResponseRecorder {
		return e.do(session, http.MethodPost, "/api/imports", map[string]any{
			"broker": "csv", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": p.Currency,
			"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": rows,
		})
	}
	const header = "date,type,symbol,quantity,price,amount,fee,currency\n"
	usxBuy := "2025-05-02,buy,USX,1,100,,,USD\n"
	usxSell := "2025-05-05,sell,USX,1,110,,,USD\n"

	// Mostly TRY, with two USD trades.
	first := parsePreview(t, upload(e, session, "csv", portfolioID, []byte(header+
		"2025-05-02,deposit,,,,1000,,TRY\n2025-05-03,deposit,,,,500,,TRY\n2025-05-04,deposit,,,,250,,TRY\n"+usxBuy+usxSell)))
	if first.Currency != "TRY" {
		t.Fatalf("first CSV currency = %q, want the TRY majority", first.Currency)
	}
	rec := commit(first,
		map[string]any{"type": "deposit", "date": "2025-05-02", "amount": "1000", "currency": "TRY"},
		map[string]any{"type": "deposit", "date": "2025-05-03", "amount": "500", "currency": "TRY"},
		map[string]any{"type": "deposit", "date": "2025-05-04", "amount": "250", "currency": "TRY"},
		map[string]any{"type": "buy", "date": "2025-05-02", "instrument_id": usx, "quantity": "1", "price": "100", "currency": "USD"},
		map[string]any{"type": "sell", "date": "2025-05-05", "instrument_id": usx, "quantity": "1", "price": "110", "currency": "USD"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var imported struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&imported); err != nil {
		t.Fatalf("decode commit: %v", err)
	}

	for _, c := range []struct {
		name, file, code string
		want             map[string]string
	}{
		// One of them again and a new one after the first CSV's period.
		{"partial", usxSell + "2025-05-08,buy,USX,2,105,,,USD\n", "overlap", map[string]string{"trade-0": "2025-05-05", "trade-1": ""}},
		// Only the USD trades, over the same dates. Last: its commit adds an import.
		{"same dates", usxBuy + usxSell, "overlap", map[string]string{"trade-0": "2025-05-02", "trade-1": "2025-05-05"}},
	} {
		p := parsePreview(t, upload(e, session, "csv", portfolioID, []byte(header+c.file)))
		if p.Currency != "USD" || len(p.Warnings) != 1 || p.Warnings[0].Code != c.code || p.Warnings[0].ImportID != imported.ID {
			t.Errorf("%s: currency %q, warnings %+v; want USD and one %s warning naming import %d", c.name, p.Currency, p.Warnings, c.code, imported.ID)
		}
		if len(p.Rows) != len(c.want) {
			t.Fatalf("%s: rows = %+v, want %d", c.name, p.Rows, len(c.want))
		}
		for _, row := range p.Rows {
			if row.DuplicateOf != c.want[row.Key] {
				t.Errorf("%s: %s duplicate_of = %q, want %q", c.name, row.Key, row.DuplicateOf, c.want[row.Key])
			}
		}
		if c.name != "same dates" {
			continue
		}
		// Both rows: a CSV's period is the dates it commits. Not refused.
		rec := commit(p,
			map[string]any{"type": "buy", "date": "2025-05-02", "instrument_id": usx, "quantity": "1", "price": "100", "currency": "USD"},
			map[string]any{"type": "sell", "date": "2025-05-05", "instrument_id": usx, "quantity": "1", "price": "110", "currency": "USD"},
		)
		if rec.Code != http.StatusCreated {
			t.Errorf("%s: commit status = %d, body %s; want %d", c.name, rec.Code, rec.Body.String(), http.StatusCreated)
		}
	}
}

// TestParseStatementWarnsAboutRefusedPeriod checks that a period the
// commit refuses (ending after today, starting before 1990, a year or longer)
// comes back from the parse step as a "period" warning carrying the commit's
// error, so the preview can block İçe aktar before the user clicks it.
func TestParseStatementWarnsAboutRefusedPeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("period@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	today, err := time.Parse(time.DateOnly, price.Today())
	if err != nil {
		t.Fatal(err)
	}
	tomorrow := today.AddDate(0, 0, 1).Format(time.DateOnly)
	t.Cleanup(func() { delete(statement.Parsers, "period") })
	for _, c := range []struct{ start, end, message string }{
		{today.AddDate(0, 0, -20).Format(time.DateOnly), tomorrow, "period_end: must not be in the future"},
		{"1989-12-01", "1989-12-31", "period_start: must be on or after 1990-01-01"},
		{"2024-01-01", "2025-01-01", "period_end: period must be shorter than a year"},
	} {
		st := stubStatement
		st.PeriodStart, st.PeriodEnd = c.start, c.end
		statement.Parsers["period"] = func([][]string) (statement.Statement, error) { return st, nil }
		p := parsePreview(t, upload(e, session, "period", portfolioID, emptyPDF(16)))
		if len(p.Warnings) != 1 || p.Warnings[0].Code != "period" || p.Warnings[0].Message != c.message {
			t.Errorf("period %s – %s: warnings = %+v, want one period warning %q", c.start, c.end, p.Warnings, c.message)
		}
	}
}

// TestParseStatementRejectsBadUploads covers the guards around the file
// itself: too large, not a PDF, and a portfolio that is not the caller's.
func TestParseStatementRejectsBadUploads(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("guards@example.com")
	other := e.login("stranger@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	if rec := upload(e, session, "test", portfolioID, emptyPDF(6<<20)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized upload status = %d, want %d; body: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if rec := upload(e, session, "test", portfolioID, []byte("not a pdf at all")); rec.Code != http.StatusBadRequest {
		t.Errorf("non-PDF upload status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if rec := upload(e, session, "nosuchbroker", portfolioID, emptyPDF(0)); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown broker status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	// A broker whose parser does not recognise the file.
	if rec := upload(e, session, "garanti", portfolioID, emptyPDF(0)); rec.Code != http.StatusBadRequest {
		t.Errorf("unrecognised garanti file status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if rec := upload(e, other, "test", portfolioID, emptyPDF(0)); rec.Code != http.StatusNotFound {
		t.Errorf("other user's portfolio status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestParseStatementDetectsBroker covers broker detection at the endpoint: under
// broker auto every real PDF parser rejects the empty PDF and the first stub
// in key order, test, accepts it, so the preview is test's, byte for byte.
// With the stubs gone no parser accepts it, and a CSV is not a PDF. Each
// parser's own tests detect their synthetic statement (statement.Detect).
func TestParseStatementDetectsBroker(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("detect@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	explicit := upload(e, session, "test", portfolioID, emptyPDF(0))
	detected := upload(e, session, "auto", portfolioID, emptyPDF(0))
	if detected.Code != http.StatusOK || detected.Body.String() != explicit.Body.String() {
		t.Fatalf("auto = %d %s\nwant %d %s", detected.Code, detected.Body, explicit.Code, explicit.Body)
	}

	saved := maps.Clone(statement.Parsers)
	t.Cleanup(func() { maps.Copy(statement.Parsers, saved) })
	for key := range statement.Parsers {
		if strings.HasPrefix(key, "test") {
			delete(statement.Parsers, key)
		}
	}
	for _, c := range []struct {
		name string
		file []byte
		want string
	}{
		{"unknown PDF", emptyPDF(0), "file: not a supported statement"},
		{"CSV", []byte("date;type\n2025-12-01;buy\n"), "file: not a PDF"},
	} {
		rec := upload(e, session, "auto", portfolioID, c.file)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"`+c.want+`"`) {
			t.Errorf("%s: %d %s, want 400 %q", c.name, rec.Code, rec.Body, c.want)
		}
	}
}

// TestParseCSVStatement checks that a ";"-separated CSV saved with a BOM
// goes through the real csv parser. A TRY symbol BIST does not know resolves
// in the one other TRY market that has it, one that two of them have needs a
// market hint, a hint is looked up directly, and a fee row previews as a fee.
// A CSV has no snapshot, so nothing is reconciled, not even the preview's own
// BIST buy. A PDF sent as a CSV is refused.
func TestParseCSVStatement(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	ids := map[string]int64{}
	for _, in := range []struct{ symbol, market, currency string }{
		{"FUND", "TEFAS", "TRY"}, {"DUP", "TEFAS", "TRY"}, {"DUP", "GOLD", "TRY"}, {"USX", "US", "USD"}, {"COIN", "CRYPTO", "USD"},
	} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, 'x', ?)`, in.symbol, in.market, in.currency)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[in.market+":"+in.symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}

	// An open BIST position before the period: a CSV has no snapshot, so it
	// is not reported as missing from one.
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2025-11-10", "instrument_id": e.instrumentID, "quantity": "1", "price": "500",
	})

	file := []byte("\xef\xbb\xbfTarih;İşlem;Sembol;Adet;Fiyat;Tutar;Komisyon;Para Birimi;Piyasa\n" +
		"2025-12-02;alış;TEST;1;10,5;;0;TRY;\n" +
		"2025-12-03;alış;FUND;2;1,25;;;TRY;\n" +
		"2025-12-04;alış;DUP;1;100;;;TRY;\n" +
		"2025-12-05;alış;DUP;1;100;;;TRY;TEFAS\n" +
		"2025-12-05;alış;USX;1;100;;;TRY;US\n" +
		"2025-12-05;alış;COIN;1;100;;;TRY;\n" +
		"2025-12-06;ücret;;;;2,5;;TRY;\n" +
		"2025-12-06;ücret;;;;2,5;1;TRY;\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))

	if p.Broker != "csv" || p.PeriodStart != "2025-12-02" || p.PeriodEnd != "2025-12-06" {
		t.Errorf("preview envelope = %+v, want broker csv over 2025-12-02 – 2025-12-06", p)
	}
	if len(p.Reconciliation) != 0 || p.CashReconciliation != nil {
		t.Errorf("reconciliation = %+v, cash = %+v, want none for a CSV", p.Reconciliation, p.CashReconciliation)
	}
	want := map[string]struct {
		status, typ, market string
		instrumentID        int64
	}{
		"trade-0": {"ok", "buy", "", e.instrumentID},
		"trade-1": {"ok", "buy", "", ids["TEFAS:FUND"]},
		"trade-2": {"unknown_instrument", "buy", "", 0},
		"trade-3": {"ok", "buy", "TEFAS", ids["TEFAS:DUP"]},
		// Quoted in USD, so the TRY cell contradicts the hinted instrument:
		// the row says so rather than unknown_instrument.
		"trade-4": {"error", "buy", "US", ids["US:USX"]},
		"trade-5": {"unknown_instrument", "buy", "", 0},
		"cash-0":  {"ok", "fee", "", 0},
		// A fee on a fee row is what the commit refuses.
		"err-0": {"error", "", "", 0},
	}
	if len(p.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", p.Rows, len(want))
	}
	for _, row := range p.Rows {
		w := want[row.Key]
		var id int64
		if row.InstrumentID != nil {
			id = *row.InstrumentID
		}
		if row.Status != w.status || row.Type != w.typ || row.Market != w.market || id != w.instrumentID {
			t.Errorf("%s = %+v, want %+v", row.Key, row, w)
		}
		if row.Key == "trade-4" && row.Message != "currency: must match instrument currency (USD)" {
			t.Errorf("trade-4 message = %q, want the currency mismatch", row.Message)
		}
	}
	if fee := p.Rows[len(p.Rows)-2]; fee.Amount != "2.5" || fee.Currency != "TRY" {
		t.Errorf("fee row = %+v, want 2.5 TRY", fee)
	}
	if bad := p.Rows[len(p.Rows)-1]; bad.Code != "fee_on_fee" || bad.Message == "" {
		t.Errorf("fee row with a fee = %+v, want error fee_on_fee with a message", bad)
	}

	// A statement that prints a balance but no positions still reports the
	// position it is silent about: that is a sold-out account, not a CSV.
	zero := dec("0")
	st := statement.Statement{Broker: "test", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", CashBalance: &zero}
	statement.Parsers["no-holdings"] = func([][]string) (statement.Statement, error) { return st, nil }
	t.Cleanup(func() { delete(statement.Parsers, "no-holdings") })
	if r := parsePreview(t, upload(e, session, "no-holdings", portfolioID, emptyPDF(0))).Reconciliation; len(r) != 1 || r[0].Symbol != "TEST" || r[0].Match {
		t.Errorf("reconciliation without holdings = %+v, want the TEST position flagged", r)
	}

	rec := upload(e, session, "csv", portfolioID, emptyPDF(0))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file: not a CSV") {
		t.Errorf("PDF as CSV: status = %d, body %s; want 400 file: not a CSV", rec.Code, rec.Body.String())
	}
}

// TestParseCSVCurrencyFromInstrument checks that a CSV without a currency
// column resolves a hinted row to the instrument and takes its currency, TRY
// only when nothing matches, and the resolved rows commit. Each row says its
// currency is that fallback, so a pick in the page may switch it.
func TestParseCSVCurrencyFromInstrument(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-currency@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	ids := map[string]int64{"TEST": e.instrumentID}
	for _, in := range []struct{ symbol, market string }{{"AAPL", "US"}, {"BTC-USD", "CRYPTO"}} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, 'x', 'USD')`, in.symbol, in.market)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[in.symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}

	file := []byte("date,type,symbol,quantity,price,market\n" +
		"2025-05-01,buy,AAPL,1,2,US\n" +
		"2025-05-01,buy,BTC-USD,1,2,CRYPTO\n" +
		"2025-05-02,buy,TEST,1,2,\n" +
		"2025-05-03,buy,NOPE,1,2,US\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))

	want := map[string]struct{ status, currency, symbol string }{
		"trade-0": {"ok", "USD", "AAPL"},
		"trade-1": {"ok", "USD", "BTC-USD"},
		"trade-2": {"ok", "TRY", "TEST"},
		"trade-3": {"unknown_instrument", "TRY", ""},
	}
	if len(p.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", p.Rows, len(want))
	}
	var ok []map[string]any
	for _, row := range p.Rows {
		w := want[row.Key]
		var id int64
		if row.InstrumentID != nil {
			id = *row.InstrumentID
		}
		if row.Status != w.status || row.Currency != w.currency || !row.Fallback || id != ids[w.symbol] {
			t.Errorf("%s = %+v, want %+v", row.Key, row, w)
		}
		if row.Status == "ok" {
			ok = append(ok, map[string]any{
				"type": row.Type, "date": row.Date, "instrument_id": id, "quantity": row.Quantity,
				"price": row.Price, "amount": row.Amount, "fee": row.Fee, "currency": row.Currency,
			})
		}
	}

	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": p.Currency,
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": ok,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("commit of the ok rows: status = %d, body %s; want %d", rec.Code, rec.Body.String(), http.StatusCreated)
	}
}

// TestParseCSVWithoutAPeriod checks that a header-only CSV is refused,
// and one whose every row is an error row, so that no row gives it a period,
// previews those rows without a warning about that empty period.
func TestParseCSVWithoutAPeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-no-period@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	rec := upload(e, session, "csv", portfolioID, []byte("date,type,amount\n"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file: no rows") {
		t.Errorf("header only: status = %d, body %s; want 400 file: no rows", rec.Code, rec.Body.String())
	}

	file := []byte("date,type,amount\nyesterday,deposit,1\n1989-12-31,deposit,5\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))
	if p.PeriodStart != "" || len(p.Warnings) != 0 || len(p.Rows) != 2 || p.Rows[0].Status != "error" || p.Rows[1].Status != "error" {
		t.Errorf("preview = %+v; want two error rows, no period and no warning", p)
	}
}

// TestParseCSVWindows1254 covers Turkish Excel's plain "CSV" save, which
// is Windows-1254, not UTF-8. Its "İşlem" header was not found, so the file
// was refused as not a csv statement, and a type word read as "Yat\uFFFDrma".
// Such a file is decoded as Windows-1254, its 0x80–0x9F range included.
func TestParseCSVWindows1254(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-1254@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	for _, header := range []string{"Tarih;\xdd\xfelem;Tutar;Para Birimi", "date;type;amount;currency"} {
		file := []byte(header + "\r\n05.09.2026;Yat\xfdrma;10,5;TRY\r\n05.09.2026;\x93\x80\x94;1;TRY\r\n")
		p := parsePreview(t, upload(e, session, "csv", portfolioID, file))
		if len(p.Rows) != 2 {
			t.Fatalf("%s: rows = %+v, want 2", header, p.Rows)
		}
		if r := p.Rows[0]; r.Status != "ok" || r.Type != "deposit" || r.Amount != "10.5" || r.Currency != "TRY" {
			t.Errorf("%s: row 0 = %+v, want an ok 10.5 TRY deposit", header, r)
		}
		if r := p.Rows[1]; r.Code != "unknown_type" || r.Value != "\u201c\u20ac\u201d" {
			t.Errorf("%s: row 1 = %+v, want unknown_type with value \u201c\u20ac\u201d", header, r)
		}
	}
}

// TestParseCSVRefusesTooManyRows covers a CSV under the 5 MB cap
// with millions of short rows, which drove the preview to gigabytes: the parse
// reads at most 5000 records under the header and refuses the file past that.
func TestParseCSVRefusesTooManyRows(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-too-many-rows@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	file := func(n int) []byte { return []byte("date,type\n" + strings.Repeat("x\n", n)) }
	if p := parsePreview(t, upload(e, session, "csv", portfolioID, file(5000))); len(p.Rows) != 5000 {
		t.Errorf("5000 rows: preview has %d rows, want 5000", len(p.Rows))
	}
	rec := upload(e, session, "csv", portfolioID, file(5001))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file: more than 5000 rows") {
		t.Errorf("5001 rows: status = %d, body %.200s; want 400 file: more than 5000 rows", rec.Code, rec.Body.String())
	}
}

// TestParseStatementRefusesTooManyRows covers a PDF statement
// under the 5 MB cap that parsed to tens of thousands of rows, so the preview
// spent minutes of CPU on them. Every parser's statement is held to the CSV
// cap of 5000 rows, holdings, skipped and error rows counted too.
func TestParseStatementRefusesTooManyRows(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("pdf-too-many-rows@example.com")
	portfolioID := e.createPortfolio(session, "Import")
	t.Cleanup(func() { delete(statement.Parsers, "many") })

	for i, c := range []struct {
		rows int
		ok   bool
	}{{5000, true}, {5001, false}} {
		st := stubStatement
		st.Skipped = []statement.Skipped{{Section: "x", Reason: "x"}}
		st.Errors = []statement.ParseError{{Section: "x", Code: "x"}}
		n := len(st.Trades) + len(st.Dividends) + len(st.Cash) + len(st.Holdings) + len(st.Skipped) + len(st.Errors)
		st.Cash = slices.Clone(st.Cash)
		for range c.rows - n {
			st.Cash = append(st.Cash, statement.CashMovement{Date: "2025-09-12", Kind: "deposit", Amount: dec("1"), Currency: "TRY"})
		}
		statement.Parsers["many"] = func([][]string) (statement.Statement, error) { return st, nil }
		rec := upload(e, session, "many", portfolioID, emptyPDF(i))
		if c.ok {
			parsePreview(t, rec)
		} else if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "file: more than 5000 rows") {
			t.Errorf("%d rows: status = %d, body %.200s; want 400 file: more than 5000 rows", c.rows, rec.Code, rec.Body.String())
		}
	}
}

// TestCommitCSVTakesItsRowsPeriod checks that, since a CSV prints no period,
// a row fixed in the preview commits even when the preview had none, and the
// import records the committed rows' dates, not the period sent.
func TestCommitCSVTakesItsRowsPeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-fixed-row@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	p := parsePreview(t, upload(e, session, "csv", portfolioID, []byte("date,type,amount\nyesterday,deposit,1\n")))
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": p.Currency,
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd,
		"rows": []map[string]any{{"type": "deposit", "date": "2025-01-02", "amount": "1", "currency": "TRY"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	imports := listImports(e, session)
	if len(imports) != 1 || imports[0]["period_start"] != "2025-01-02" || imports[0]["period_end"] != "2025-01-02" || imports[0]["currency"] != "" {
		t.Errorf("imports = %+v, want one over 2025-01-02 – 2025-01-02 with no currency", imports)
	}
}

// TestCommitCSVOfRefusedDatesReturnsTheRowError checks that a CSV commit
// whose every row date is refused has no period, and answers with the row's
// own date error, not the empty period's.
func TestCommitCSVOfRefusedDatesReturnsTheRowError(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-refused-dates@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": portfolioID, "sha256": strings.Repeat("a", 64),
		"period_start": "", "period_end": "",
		"rows": []map[string]any{{"type": "deposit", "date": "1989-12-31", "amount": "5", "currency": "TRY"}},
	})
	want := `{"error":"rows[0].date: must be on or after 1990-01-01"}`
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("commit = %d %s, want 400 %s", rec.Code, rec.Body.String(), want)
	}
}

// TestCSVPeriodSameInParseAndCommit checks that the preview and the
// commit leave the same dates out of a CSV's period, so a row whose date only
// sorts like one widens neither.
func TestCSVPeriodSameInParseAndCommit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-same-period@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	original := statement.Parsers["csv"]
	t.Cleanup(func() { statement.Parsers["csv"] = original })
	statement.Parsers["csv"] = func([][]string) (statement.Statement, error) {
		deposit := func(date string) statement.CashMovement {
			return statement.CashMovement{Date: date, Kind: "deposit", Amount: dec("1"), Currency: "TRY"}
		}
		return statement.Statement{Broker: "csv", Currency: "TRY", Cash: []statement.CashMovement{deposit("2025-05-01"), deposit("2025-05-03"), deposit("2025-05-1x")}}, nil
	}
	p := parsePreview(t, upload(e, session, "csv", portfolioID, []byte("date,type,amount\n2025-05-01,deposit,1\n")))

	var ok []map[string]any
	for _, row := range p.Rows {
		if row.Status == "ok" {
			ok = append(ok, map[string]any{"type": row.Type, "date": row.Date, "amount": row.Amount, "currency": row.Currency})
		}
	}
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": p.Currency,
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": ok,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var committed struct {
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &committed); err != nil {
		t.Fatalf("decode commit: %v", err)
	}
	if p.PeriodStart != committed.PeriodStart || p.PeriodEnd != committed.PeriodEnd || committed.PeriodEnd != "2025-05-03" {
		t.Errorf("preview period %s – %s, commit period %s – %s; want both 2025-05-01 – 2025-05-03", p.PeriodStart, p.PeriodEnd, committed.PeriodStart, committed.PeriodEnd)
	}
}

// TestParseCSVFlagsRowsTheCommitRefuses checks that a row the commit's
// validation refuses is an error row in the preview, a cash row in a currency
// the app does not support is unsupported_currency, and the rest of the file
// still commits, even with an error row dated before 1990 or in the future.
// A row of an unknown symbol is checked the same way, short of its instrument,
// and still says its instrument is unknown.
func TestParseCSVFlagsRowsTheCommitRefuses(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-invalid@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	file := []byte("date,type,symbol,quantity,price,amount,fee,currency\n" +
		"2025-05-01,deposit,,,,1000,,TRY\n" +
		"2025-05-02,withdrawal,,,,-500,,TRY\n" +
		"2025-05-03,buy,TEST,0,10,,,TRY\n" +
		"2025-05-04,sell,TEST,1,0,,,TRY\n" +
		"2025-05-04,sell,XXXXX,5,0,,,TRY\n" +
		"2025-05-04,buy,XXXXX,1,10,,,TRY\n" +
		"2025-05-05,dividend,TEST,,,10,20,TRY\n" +
		"2025-05-05,dividend,XXXXX,,,10,20,TRY\n" +
		"2025-05-06,interest,,,,10,20,TRY\n" +
		"2025-05-07,deposit,,,,0,,TRY\n" +
		"2025-05-08,deposit,,,,100,,GBP\n" +
		"1989-12-31,deposit,,,,5,,TRY\n" +
		"2999-01-01,takas,,,,10,,TRY\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))

	// A date the commit refuses stays out of the period, so it does not
	// block the other rows.
	if p.PeriodStart != "2025-05-01" || p.PeriodEnd != "2025-05-08" || len(p.Warnings) != 0 {
		t.Errorf("period = %s – %s, warnings %+v; want 2025-05-01 – 2025-05-08 and none", p.PeriodStart, p.PeriodEnd, p.Warnings)
	}

	want := map[string]struct {
		status, message string
		unknown         bool
	}{
		"cash-0":  {"ok", "", false},
		"trade-0": {"error", "quantity: must be a positive number with at most 15 integer and 9 fraction digits", false},
		"trade-1": {"error", "price: must be a positive number with at most 15 integer and 9 fraction digits", false},
		"trade-2": {"error", "price: must be a positive number with at most 15 integer and 9 fraction digits", true},
		"trade-3": {"unknown_instrument", "", false},
		"div-0":   {"error", "fee: must not exceed amount", false},
		"div-1":   {"error", "fee: must not exceed amount", true},
		"cash-1":  {"error", "fee: must not exceed amount", false},
		"cash-2":  {"error", "amount: must be a positive number with at most 15 integer and 9 fraction digits", false},
		"cash-3":  {"unsupported_currency", "", false},
		"cash-4":  {"error", "date: must be on or after 1990-01-01", false},
		// The negative withdrawal is already unreadable to the parser.
		"err-0": {"error", "unreadable amount", false},
		"err-1": {"error", "unknown type", false},
	}
	if len(p.Rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", p.Rows, len(want))
	}
	var ok []map[string]any
	for _, row := range p.Rows {
		if w := want[row.Key]; row.Status != w.status || row.Message != w.message || row.Unknown != w.unknown {
			t.Errorf("%s = %+v, want %+v", row.Key, row, w)
		}
		if row.Status == "ok" {
			ok = append(ok, map[string]any{"type": row.Type, "date": row.Date, "amount": row.Amount, "fee": row.Fee, "currency": row.Currency})
		}
	}

	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": p.Currency,
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": ok,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("commit of the ok rows: status = %d, body %s; want %d", rec.Code, rec.Body.String(), http.StatusCreated)
	}
}

// TestParseStatementCreatesConfirmedFund checks that a TEFAS row's
// unknown fund code is created as a TRY TEFAS fund named as the lookup names
// it, once per code per request and for at most 10 codes; a code the lookup
// does not know, a symbol that is no code, and a row without the TEFAS hint
// stay unknown_instrument; a nil lookup creates nothing.
func TestParseStatementCreatesConfirmedFund(t *testing.T) {
	for _, withLookup := range []bool{true, false} {
		t.Run(fmt.Sprint("lookup=", withLookup), func(t *testing.T) {
			e := newTransactionTestEnv(t)
			session := e.login("lookup@example.com")
			portfolioID := e.createPortfolio(session, "Import")

			calls := map[string]int{}
			var lookup statement.FundLookup
			if withLookup {
				lookup = func(_ context.Context, code string) (string, bool, error) {
					calls[code]++
					return "Fund " + code, code != "NOP", nil
				}
			}
			protected := http.NewServeMux()
			deps := e.importDeps()
			deps.LookupFund = lookup
			statement.RegisterRoutes(protected, deps)
			e.handler = e.sessions.RequireUser(protected)

			p := parsePreview(t, upload(e, session, "test-lookup", portfolioID, emptyPDF(0)))
			status := map[string]string{}
			for _, row := range p.Rows {
				s := row.Status
				if row.InstrumentID != nil {
					s += " with id"
				}
				status[row.Symbol] += s + ";"
			}

			want := map[string]string{
				"GJH": "ok with id;ok with id;", "NOP": "unknown_instrument;",
				"AZİMUT PYŞ DEĞİŞKEN FON": "unknown_instrument;",
				"A09":                     "unknown_instrument;", "XYZ": "unknown_instrument;",
			}
			wantCalls := map[string]int{"GJH": 1, "NOP": 1}
			for i := 1; i <= 8; i++ {
				want[fmt.Sprintf("A%02d", i)] = "ok with id;"
				wantCalls[fmt.Sprintf("A%02d", i)] = 1
			}
			if !withLookup {
				for code := range want {
					want[code] = strings.Repeat("unknown_instrument;", strings.Count(want[code], ";"))
				}
				wantCalls = map[string]int{}
			}
			if fmt.Sprint(status) != fmt.Sprint(want) {
				t.Errorf("row statuses = %v\nwant %v", status, want)
			}
			if fmt.Sprint(calls) != fmt.Sprint(wantCalls) {
				t.Errorf("lookups = %v, want %v", calls, wantCalls)
			}

			var created []string
			rows, err := e.db.Query(`SELECT symbol || '/' || name || '/' || currency FROM instruments WHERE market = 'TEFAS' AND symbol IN ('GJH', 'NOP', 'A09', 'XYZ') ORDER BY symbol`)
			if err != nil {
				t.Fatalf("query instruments: %v", err)
			}
			defer rows.Close()
			for rows.Next() {
				var c string
				if err := rows.Scan(&c); err != nil {
					t.Fatalf("scan: %v", err)
				}
				created = append(created, c)
			}
			wantCreated := "[GJH/Fund GJH/TRY]"
			if !withLookup {
				wantCreated = "[]"
			}
			if fmt.Sprint(created) != wantCreated {
				t.Errorf("created TEFAS instruments = %v, want %s", created, wantCreated)
			}
		})
	}
}

// TestLookupFundCreatesConfirmedFund checks that the preview picker's
// fund lookup creates the fund TEFAS confirms, returns it again without a
// second lookup, answers 404 for a code TEFAS does not know and 400 for text
// that is no fund code.
func TestLookupFundCreatesConfirmedFund(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("fundlookup@example.com")

	calls := map[string]int{}
	lookup := func(_ context.Context, code string) (string, bool, error) {
		calls[code]++
		return "GARANTİ PORTFÖY " + code, code == "GTL", nil
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.LookupFund = lookup
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	type fund struct {
		ID       int64  `json:"id"`
		Symbol   string `json:"symbol"`
		Market   string `json:"market"`
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}
	var got [2]fund
	for i, code := range []string{" gtl ", "GTL"} {
		rec := e.do(session, http.MethodPost, "/api/imports/funds", map[string]string{"code": code})
		if rec.Code != http.StatusOK {
			t.Fatalf("lookup %q: status = %d, body %s; want 200", code, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got[i]); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	want := fund{ID: got[0].ID, Symbol: "GTL", Market: "TEFAS", Name: "GARANTİ PORTFÖY GTL", Currency: "TRY"}
	if got[0] != want || got[1] != want {
		t.Errorf("funds = %+v, want %+v twice", got, want)
	}

	if rec := e.do(session, http.MethodPost, "/api/imports/funds", map[string]string{"code": "NOP"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown code: status = %d, body %s; want 404", rec.Code, rec.Body.String())
	}
	if rec := e.do(session, http.MethodPost, "/api/imports/funds", map[string]string{"code": "GARANTİ FON"}); rec.Code != http.StatusBadRequest {
		t.Errorf("fund name: status = %d, body %s; want 400", rec.Code, rec.Body.String())
	}
	if wantCalls := map[string]int{"GTL": 1, "NOP": 1}; fmt.Sprint(calls) != fmt.Sprint(wantCalls) {
		t.Errorf("lookups = %v, want %v", calls, wantCalls)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM instruments WHERE market = 'TEFAS' AND symbol IN ('GTL', 'NOP')`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("TEFAS instruments GTL/NOP = %d, want 1", n)
	}
}

// TestFundLookupsShareClientLimit checks that the preview's TEFAS
// lookups and the fund route's count against one budget of 20 a minute per
// client IP, so repeated previews cannot crawl TEFAS; a row over the budget
// stays unknown_instrument and the fund route answers 429 once it is spent.
func TestFundLookupsShareClientLimit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("fundlimit@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	calls := 0
	lookup := func(context.Context, string) (string, bool, error) {
		calls++
		return "", false, errors.New("tefas unreachable")
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.LookupFund = lookup
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	file := "date,type,symbol,quantity,price,currency,market\n"
	for i := 0; i < 10; i++ {
		file += fmt.Sprintf("2026-09-02,buy,Q%02d,1,1,TRY,TEFAS\n", i)
	}
	for i := 0; i < 3; i++ {
		for _, row := range parsePreview(t, upload(e, session, "csv", portfolioID, []byte(file))).Rows {
			if row.Status != "unknown_instrument" {
				t.Errorf("parse %d row %s: status = %s, want unknown_instrument", i+1, row.Symbol, row.Status)
			}
		}
	}
	if calls != 20 {
		t.Errorf("lookups after 3 previews of 10 codes = %d, want 20", calls)
	}
	rec := e.do(session, http.MethodPost, "/api/imports/funds", map[string]string{"code": "QC1"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("fund route after the budget is spent: status = %d, Retry-After %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if calls != 20 {
		t.Errorf("lookups after the fund route = %d, want 20", calls)
	}
}

// TestFundLookupsTefasOffSpendNoBudget checks that with the tefas
// source off, the server's fund lookup asks TEFAS nothing, so the fund
// route answers 404 past the client's budget of 20 lookups a minute, not
// 429.
func TestFundLookupsTefasOffSpendNoBudget(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("fundoff@example.com")
	if err := settings.NewRepo(e.db, nil).Set(context.Background(), scheduler.EnabledKey("tefas"), "false"); err != nil {
		t.Fatalf("turn tefas off: %v", err)
	}
	for i := 0; i < 25; i++ {
		code := fmt.Sprintf("Q%02d", i)
		if rec := e.do(session, http.MethodPost, "/api/imports/funds", map[string]string{"code": code}); rec.Code != http.StatusNotFound {
			t.Fatalf("lookup %d (%s): status = %d, body %s; want 404", i+1, code, rec.Code, rec.Body.String())
		}
	}
}

// TestImportAliasResolvesLaterPreview checks that a commit stores the
// user's pick for a symbol the preview did not know, the next preview of the
// same broker resolves that symbol with it and says so, naming the picked
// instrument, a real match wins over a stored pick, and another
// user's preview never sees it.
func TestImportAliasResolvesLaterPreview(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("alias@example.com")
	first := e.createPortfolio(session, "First")
	second := e.createPortfolio(session, "Second")
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('OTHER', 'BIST', 'x', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	otherID, _ := res.LastInsertId()

	file := []byte("date,type,symbol,quantity,price,currency\n" +
		"2025-12-02,buy,GARANTI PORTFOY PARA PIYASASI,1,10,TRY\n" +
		"2025-12-03,buy,TEST,1,10,TRY\n")
	statusOf := func(p preview) map[string]string {
		out := map[string]string{}
		for _, row := range p.Rows {
			id := int64(0)
			if row.InstrumentID != nil {
				id = *row.InstrumentID
			}
			out[row.Symbol] = strings.TrimSpace(fmt.Sprintf("%s %d %s %s %s", row.Status, id, row.ResolvedBy, row.InstrSymbol, row.InstrName))
		}
		return out
	}

	p := parsePreview(t, upload(e, session, "csv", first, file))
	if got := statusOf(p)["GARANTI PORTFOY PARA PIYASASI"]; got != "unknown_instrument 0" {
		t.Fatalf("first preview = %q, want unknown_instrument", got)
	}
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "csv", "portfolio_id": first, "sha256": p.SHA256,
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd,
		"rows": []map[string]any{
			{"type": "buy", "date": "2025-12-02", "instrument_id": e.instrumentID, "quantity": "1", "price": "10", "currency": "TRY"},
			{"type": "buy", "date": "2025-12-03", "instrument_id": e.instrumentID, "quantity": "1", "price": "10", "currency": "TRY"},
		},
		"aliases": []map[string]any{
			{"symbol": "GARANTI PORTFOY PARA PIYASASI", "instrument_id": e.instrumentID},
			{"symbol": "TEST", "instrument_id": otherID},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	// Another file of the same rows: the same file is refused in a second
	// portfolio.
	got := statusOf(parsePreview(t, upload(e, session, "csv", second, append(file, '\n'))))
	want := map[string]string{
		"GARANTI PORTFOY PARA PIYASASI": fmt.Sprintf("ok %d alias TEST Test A.S.", e.instrumentID),
		"TEST":                          fmt.Sprintf("ok %d", e.instrumentID),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("second preview = %v, want %v", got, want)
	}

	other := e.login("alias-other@example.com")
	otherPortfolio := e.createPortfolio(other, "Other")
	if got := statusOf(parsePreview(t, upload(e, other, "csv", otherPortfolio, file)))["GARANTI PORTFOY PARA PIYASASI"]; got != "unknown_instrument 0" {
		t.Errorf("other user's preview = %q, want unknown_instrument", got)
	}
}

// TestImportAliasResolvesHoldings checks that a holding of a symbol the
// user picked an instrument for resolves through that pick like the trade
// rows do, so it reconciles against the picked instrument's position with
// no separate entry for it, and an empty portfolio gets an opening for it.
func TestImportAliasResolvesHoldings(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("alias-holding@example.com")
	held := e.createPortfolio(session, "Held")
	empty := e.createPortfolio(session, "Empty")

	// The commit's own storing of a pick is TestImportAliasResolvesLaterPreview's;
	// it accepts only real brokers, so the pick is stored directly here.
	e.createTransaction(session, held, map[string]any{
		"type": "buy", "date": "2025-12-05", "instrument_id": e.instrumentID, "quantity": "10", "price": "20",
	})
	if _, err := e.db.Exec(`INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at)
		SELECT id, 'test-alias', 'ZZZZZ', ?, '2026-01-01T00:00:00Z' FROM users WHERE email = 'alias-holding@example.com'`, e.instrumentID); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	p := parsePreview(t, upload(e, session, "test-alias", held, emptyPDF(0)))
	if len(p.Reconciliation) != 1 {
		t.Fatalf("reconciliation = %+v, want only ZZZZZ", p.Reconciliation)
	}
	if r := p.Reconciliation[0]; r.Symbol != "ZZZZZ" || !r.Match || r.PortfolioQuantity != "15" {
		t.Errorf("reconciliation[0] = %+v, want ZZZZZ 15 matching TEST's position", r)
	}

	p = parsePreview(t, upload(e, session, "test-alias", empty, emptyPDF(0)))
	var opening []string
	for _, row := range p.Rows {
		if row.Opening && row.Type == "buy" && row.InstrumentID != nil {
			opening = append(opening, fmt.Sprintf("%d %s", *row.InstrumentID, row.Quantity))
		}
	}
	if want := fmt.Sprintf("%d 10", e.instrumentID); len(opening) != 1 || opening[0] != want {
		t.Errorf("opening buys = %v, want [%s]", opening, want)
	}
}

// TestImportCommitRejectsUnknownAliasInstrument checks that an alias of
// an instrument that does not exist is a 400 naming the field, and nothing
// is written.
func TestImportCommitRejectsUnknownAliasInstrument(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("alias-bad@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("alias-bad"),
		map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "5", "fee": "0", "currency": "TRY"})
	body["aliases"] = []map[string]any{{"symbol": "SOME FUND", "instrument_id": 999999}}
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	want := `{"error":"aliases[0].instrument_id: unknown instrument"}`
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("commit = %d %s, want 400 %s", rec.Code, rec.Body.String(), want)
	}
	var n int
	if err := e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM import_aliases) + (SELECT COUNT(*) FROM statement_imports) + (SELECT COUNT(*) FROM transactions)`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rows written = %d, want 0", n)
	}
}

// TestParseStatementProposesOpenings checks that an instrument the
// statement holds more of than the portfolio can explain, and one sold below
// zero, get an opening buy dated the day before period_start, priced with
// the stored price on or before that date, else after an on-demand fetch,
// else with the statement's own price (approximate_price), else left as an
// opening_price error; a deposit pays for the priced buys, the
// reconciliation matches with them, and the commit takes them. An
// instrument the portfolio already traded before period_start gets none.
func TestParseStatementProposesOpenings(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening@example.com")
	empty := e.createPortfolio(session, "Empty")
	traded := e.createPortfolio(session, "Traded")

	ids := map[string]int64{}
	for _, symbol := range []string{"OPA", "OPB", "OPC", "OPD"} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', 'x', 'TRY')`, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	e.createTransaction(session, traded, map[string]any{
		"type": "buy", "date": "2025-11-20", "instrument_id": ids["OPA"], "quantity": "10", "price": "1",
	})

	prices := price.NewRepo(e.db)
	ctx := context.Background()
	// OPA's price on the day before the period, and a later one that must
	// not be used.
	for date, close := range map[string]string{"2025-11-28": "1.5", "2025-12-05": "9"} {
		if err := prices.Upsert(ctx, ids["OPA"], date, close, "tefas"); err != nil {
			t.Fatalf("upsert price: %v", err)
		}
	}
	// The fetch finds a price for OPB only.
	var fetched []string
	fetch := func(ctx context.Context, in instrument.Instrument, date string) {
		fetched = append(fetched, in.Symbol+"@"+date)
		if in.Symbol == "OPB" {
			if err := prices.Upsert(ctx, in.ID, date, "3.25", "tefas"); err != nil {
				t.Errorf("upsert fetched price: %v", err)
			}
		}
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.PriceSources.Prices = prices
	deps.OnDemandFetch = fetch
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "opening") })
	statement.Parsers["opening"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS",
			Trades: []statement.Trade{
				{Date: "2025-12-10", Symbol: "OPA", Side: "buy", Quantity: dec("30"), Price: dec("2"), Amount: dec("60"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"},
				{Date: "2025-12-15", Symbol: "OPB", Side: "sell", Quantity: dec("40"), Price: dec("3"), Amount: dec("120"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"},
			},
			Holdings: []statement.Holding{
				{Symbol: "OPA", Quantity: dec("100"), Currency: "TRY", Price: dec("2.5")},
				{Symbol: "OPC", Quantity: dec("10"), Currency: "TRY", Price: dec("4")},
				{Symbol: "OPD", Quantity: dec("5"), Currency: "TRY"},
			},
		}, nil
	}

	type opening struct{ key, status, typ, symbol, quantity, price, amount, code string }
	summarize := func(p preview) (open []opening, approximate []string, reconciled map[string]bool) {
		for _, row := range p.Rows {
			if row.Opening {
				open = append(open, opening{row.Key, row.Status, row.Type, row.Symbol, row.Quantity, row.Price, row.Amount, row.Code})
				if row.Date != "2025-11-30" {
					t.Errorf("%s date = %s, want 2025-11-30", row.Key, row.Date)
				}
			}
			if row.Approximate {
				approximate = append(approximate, row.Symbol)
			}
		}
		reconciled = map[string]bool{}
		for _, r := range p.Reconciliation {
			reconciled[r.Symbol] = r.Match
		}
		return open, approximate, reconciled
	}

	p := parsePreview(t, upload(e, session, "opening", empty, emptyPDF(0)))
	open, approximate, reconciled := summarize(p)
	// Keyed by instrument and currency, so a re-preview keeps them.
	key := func(symbol string) string { return fmt.Sprintf("open-%d", ids[symbol]) }
	want := []opening{
		{"open-cash-TRY", "ok", "deposit", "", "", "", "275", ""},
		{key("OPA"), "ok", "buy", "OPA", "70", "1.5", "105", ""},
		{key("OPC"), "ok", "buy", "OPC", "10", "4", "40", ""},
		{key("OPD"), "error", "buy", "OPD", "5", "", "", "opening_price"},
		{key("OPB"), "ok", "buy", "OPB", "40", "3.25", "130", ""},
	}
	if fmt.Sprint(open) != fmt.Sprint(want) {
		t.Errorf("opening rows = %v\nwant %v", open, want)
	}
	if fmt.Sprint(approximate) != "[OPC]" {
		t.Errorf("approximate_price on %v, want [OPC]", approximate)
	}
	if fmt.Sprint(fetched) != "[OPC@2025-11-30 OPD@2025-11-30 OPB@2025-11-30]" {
		t.Errorf("fetches = %v, want OPC, OPD and OPB on 2025-11-30", fetched)
	}
	if fmt.Sprint(reconciled) != "map[OPA:true OPC:true OPD:false]" {
		t.Errorf("reconciliation matches = %v, want OPA and OPC matched, OPD short", reconciled)
	}

	var rows []map[string]any
	for _, row := range p.Rows {
		if row.Status == "ok" {
			rows = append(rows, map[string]any{
				"instrument_id": row.InstrumentID, "type": row.Type, "date": row.Date, "quantity": row.Quantity,
				"price": row.Price, "amount": row.Amount, "fee": row.Fee, "currency": row.Currency,
			})
		}
	}
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "garanti", "portfolio_id": empty, "sha256": p.SHA256, "currency": "TRY",
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": rows,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("commit with the opening rows: status = %d, body %s; want 201", rec.Code, rec.Body.String())
	}

	// Another file: the same file is refused in a second portfolio.
	open, _, reconciled = summarize(parsePreview(t, upload(e, session, "opening", traded, emptyPDF(1))))
	for _, o := range open {
		if o.symbol == "OPA" {
			t.Errorf("traded portfolio got opening row %v for OPA, which it traded before the period", o)
		}
	}
	if reconciled["OPA"] {
		t.Errorf("OPA matched in the traded portfolio, want 10 + 30 against 100 mismatched")
	}
}

// TestImportPriceFetchesShareOneDeadline checks that with every price
// fetch hanging until its context ends, neither the preview's opening
// fetches nor the commit's fetches take OnDemandFetchTimeout per instrument;
// both answer within about one timeout in all.
func TestImportPriceFetchesShareOneDeadline(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("hung-fetch@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	symbols := []string{"HNA", "HNB", "HNC"}
	ids := map[string]int64{}
	for _, symbol := range symbols {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', 'x', 'TRY')`, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	var calls atomic.Int32
	fetch := func(ctx context.Context, in instrument.Instrument, date string) {
		calls.Add(1)
		<-ctx.Done()
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.OnDemandFetch = fetch
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "hung") })
	statement.Parsers["hung"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "hung", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "BIST"}
		for _, symbol := range symbols {
			st.Holdings = append(st.Holdings, statement.Holding{Symbol: symbol, Quantity: dec("1"), Currency: "TRY"})
		}
		return st, nil
	}
	const limit = transaction.OnDemandFetchTimeout + time.Second

	start := time.Now()
	parsePreview(t, upload(e, session, "hung", portfolioID, emptyPDF(0)))
	if took := time.Since(start); took > limit {
		t.Errorf("preview took %v with %d hung fetches, want under %v", took, calls.Load(), limit)
	}

	var rows []map[string]any
	for _, symbol := range symbols {
		rows = append(rows, map[string]any{"type": "buy", "date": "2025-12-10", "instrument_id": ids[symbol], "quantity": "1", "price": "1", "fee": "0", "currency": "TRY"})
	}
	calls.Store(0)
	start = time.Now()
	rec := e.do(session, http.MethodPost, "/api/imports", importBody(portfolioID, fakeSHA("hung"), rows...))
	took := time.Since(start)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, body %s; want 201", rec.Code, rec.Body.String())
	}
	if took > limit {
		t.Errorf("commit took %v, want under %v", took, limit)
	}
	if calls.Load() == 0 {
		t.Errorf("commit ran no price fetch, want one per instrument")
	}
}

// TestConcurrentParsesFetchOneAtATime checks that parses running at once
// make their opening price fetches one after another, so they do not compete
// for SQLite's one writer, and that each parse's fetch deadline starts when
// its fetches do, not when the parse queued behind the others.
func TestConcurrentParsesFetchOneAtATime(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("concurrent-parse@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	symbols := []string{"CPA", "CPB"}
	for _, symbol := range symbols {
		if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', 'x', 'TRY')`, symbol); err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
	}
	var inFlight, peak atomic.Int32
	var mu sync.Mutex
	shortest := transaction.OnDemandFetchTimeout
	fetch := func(ctx context.Context, in instrument.Instrument, date string) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if deadline, ok := ctx.Deadline(); ok {
			mu.Lock()
			shortest = min(shortest, time.Until(deadline))
			mu.Unlock()
		}
		time.Sleep(200 * time.Millisecond)
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.OnDemandFetch = fetch
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "concurrent") })
	statement.Parsers["concurrent"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "concurrent", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS"}
		for _, symbol := range symbols {
			st.Holdings = append(st.Holdings, statement.Holding{Symbol: symbol, Quantity: dec("1"), Currency: "TRY"})
		}
		return st, nil
	}

	const parses = 5
	var wg sync.WaitGroup
	for i := range parses {
		wg.Go(func() {
			if rec := upload(e, session, "concurrent", portfolioID, emptyPDF(i)); rec.Code != http.StatusOK {
				t.Errorf("parse %d status = %d, want 200; body: %s", i, rec.Code, rec.Body.String())
			}
		})
	}
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Errorf("%d price fetches ran at once, want 1", got)
	}
	// The last parse queues behind 8 fetches of 200ms; the fetch deadline it
	// gets must not have shrunk by that wait.
	if shortest < transaction.OnDemandFetchTimeout-time.Second {
		t.Errorf("shortest fetch deadline left = %v, want about %v", shortest, transaction.OnDemandFetchTimeout)
	}
}

// TestQueuedParseStillFetchesAfterTheHolderRunsOut checks that a parse queued
// behind one whose fetches run to their deadline gets the slot when that
// deadline ends, not before and not never, and then fetches all its
// candidates.
func TestQueuedParseStillFetchesAfterTheHolderRunsOut(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("queued-parse@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	for _, symbol := range []string{"QA1", "QA2", "QB1", "QB2", "QB3"} {
		if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', 'x', 'TRY')`, symbol); err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
	}
	started := make(chan struct{})
	var once sync.Once
	var fetchedB atomic.Int32
	fetch := func(ctx context.Context, in instrument.Instrument, date string) {
		if strings.HasPrefix(in.Symbol, "QA") {
			once.Do(func() { close(started) })
			<-ctx.Done()            // a hung host: runs to the deadline
			time.Sleep(time.Second) // and the parse goes on a while after it
			return
		}
		fetchedB.Add(1)
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.OnDemandFetch = fetch
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	for broker, prefix := range map[string]string{"queued-a": "QA", "queued-b": "QB"} {
		t.Cleanup(func() { delete(statement.Parsers, broker) })
		n := map[string]int{"QA": 2, "QB": 3}[prefix]
		statement.Parsers[broker] = func([][]string) (statement.Statement, error) {
			st := statement.Statement{Broker: broker, PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS"}
			for i := 1; i <= n; i++ {
				st.Holdings = append(st.Holdings, statement.Holding{Symbol: fmt.Sprintf("%s%d", prefix, i), Quantity: dec("1"), Currency: "TRY"})
			}
			return st, nil
		}
	}

	var wg sync.WaitGroup
	wg.Go(func() { upload(e, session, "queued-a", portfolioID, emptyPDF(1)) })
	<-started
	wg.Go(func() {
		if rec := upload(e, session, "queued-b", portfolioID, emptyPDF(2)); rec.Code != http.StatusOK {
			t.Errorf("queued parse status = %d, want 200", rec.Code)
		}
	})
	wg.Wait()

	if got := fetchedB.Load(); got != 3 {
		t.Errorf("queued parse fetched %d of its 3 candidates, want 3", got)
	}
}

// TestImportLookupsShareOneDeadline checks that with TEFAS and the
// bulletin host hanging until the context ends, a preview naming three
// unknown fund codes and an unknown BIST symbol on two dates answers within
// about one PreviewLookupTimeout in all, not one per lookup, and the rows
// whose lookup was skipped stay unknown_instrument.
func TestImportLookupsShareOneDeadline(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("hung-lookup@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	// Each stub also gives up after one timeout, so a run without the shared
	// deadline fails on time instead of hanging.
	var lookups, syncs atomic.Int32
	hang := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(statement.PreviewLookupTimeout):
			return context.DeadlineExceeded
		}
	}
	lookupFund := func(ctx context.Context, code string) (string, bool, error) {
		lookups.Add(1)
		return "", false, hang(ctx)
	}
	syncBulletin := func(ctx context.Context, date time.Time) (bool, error) {
		syncs.Add(1)
		return true, hang(ctx)
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.LookupFund = lookupFund
	deps.SyncBulletin = syncBulletin
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "hung-lookup") })
	statement.Parsers["hung-lookup"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "hung-lookup", PeriodStart: "2026-09-01", PeriodEnd: "2026-09-30", Currency: "TRY"}
		trade := func(date, symbol, market string) statement.Trade {
			return statement.Trade{Date: date, Symbol: symbol, Side: "buy", Quantity: dec("10"), Price: dec("1.5"), Amount: dec("15"), Fee: dec("0"), Currency: "TRY", Market: market}
		}
		for _, code := range []string{"QZ1", "QZ2", "QZ3"} {
			st.Trades = append(st.Trades, trade("2026-09-10", code, "TEFAS"))
		}
		st.Trades = append(st.Trades, trade("2026-09-11", "NOSUCHX", "BIST"), trade("2026-09-14", "NOSUCHX", "BIST"))
		return st, nil
	}
	const limit = statement.PreviewLookupTimeout + 2*time.Second

	start := time.Now()
	p := parsePreview(t, upload(e, session, "hung-lookup", portfolioID, emptyPDF(0)))
	if took := time.Since(start); took > limit {
		t.Errorf("preview took %v with %d hung fund lookups and %d hung bulletin syncs, want under %v", took, lookups.Load(), syncs.Load(), limit)
	}
	if len(p.Rows) != 5 {
		t.Errorf("got %d rows, want 5", len(p.Rows))
	}
	for _, row := range p.Rows {
		if row.Status != "unknown_instrument" {
			t.Errorf("row %s status = %q, want unknown_instrument", row.Symbol, row.Status)
		}
	}
}

// TestParseMidasStatementPricesOpeningsAtAverageCost checks that the
// first Midas statement of an empty portfolio prices its
// opening buys with the holding's printed Ortalama Maliyet (cost_price), not
// with Toplam Değeri / Adet nor a stored price of the day before, and a
// deposit pays for them. Synthetic values.
func TestParseMidasStatementPricesOpeningsAtAverageCost(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("midas-opening@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	ids := map[string]int64{}
	for _, symbol := range []string{"QQQ", "VTI"} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'US', 'x', 'USD')`, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	prices := price.NewRepo(e.db)
	// A stored price of the day before the period, which the average cost
	// wins over.
	if err := prices.Upsert(context.Background(), ids["VTI"], "2025-08-29", "210", "us"); err != nil {
		t.Fatalf("upsert price: %v", err)
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.PriceSources.Prices = prices
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	// The real Midas parser on a synthetic PORTFÖY ÖZETİ.
	parse := statement.Parsers["midas"]
	t.Cleanup(func() { delete(statement.Parsers, "midas-opening") })
	statement.Parsers["midas-opening"] = func([][]string) (statement.Statement, error) {
		return parse([][]string{
			{"01/09/25", "-", "30/09/25", "HESAP EKSTRESİ"},
			{"Nakit Bakiye", ":", "0.74 USD"},
			{"PORTFÖY ÖZETİ (", "30/09/25", ")"},
			{"QQQ - NASDAQ 100 Endeks Fo...", "3", "500,00 USD", "360,00 USD", "1.860,00 USD"},
			{"VTI - ABD Tüm Borsa Endeks...", "2", "200,50 USD", "49,00 USD", "450,00 USD"},
		})
	}

	var got []string
	for _, row := range parsePreview(t, upload(e, session, "midas-opening", portfolioID, emptyPDF(0))).Rows {
		if row.Opening {
			got = append(got, fmt.Sprintf("%s %s %s %s %s %s %v %v", row.Status, row.Type, row.Symbol, row.Quantity, row.Price, row.Amount, row.Approximate, row.CostPrice))
		}
	}
	want := []string{
		"ok deposit    1901 false false",
		"ok buy QQQ 3 500 1500 false true",
		"ok buy VTI 2 200.5 401 false true",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("opening rows = %q\nwant %q", got, want)
	}
}

// TestParseStatementOpeningSkipsAverageCostAfterInPeriodBuy checks that, since
// the period-end average cost includes the period's own buys, an instrument
// bought in the period prices its opening without it, here with the period-end
// price. Synthetic values.
func TestParseStatementOpeningSkipsAverageCostAfterInPeriodBuy(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening-bought@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('QQQ', 'US', 'x', 'USD')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "opening-bought") })
	statement.Parsers["opening-bought"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening-bought", PeriodStart: "2025-09-01", PeriodEnd: "2025-09-30", Currency: "USD", Market: "US",
			Trades: []statement.Trade{
				{Date: "2025-09-10", Symbol: "QQQ", Side: "buy", Quantity: dec("2"), Price: dec("550"), Amount: dec("1100"), Fee: dec("0"), Currency: "USD", Market: "US"},
			},
			Holdings: []statement.Holding{{Symbol: "QQQ", Quantity: dec("3"), AvgCost: dec("500"), Currency: "USD", Price: dec("620")}},
		}, nil
	}

	var got []string
	for _, row := range parsePreview(t, upload(e, session, "opening-bought", portfolioID, emptyPDF(0))).Rows {
		if row.Opening && row.Type == "buy" {
			got = append(got, fmt.Sprintf("%s %s %s %s %v %v", row.Status, row.Symbol, row.Quantity, row.Price, row.Approximate, row.CostPrice))
		}
	}
	if want := []string{"ok QQQ 1 620 true false"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("opening rows = %q\nwant %q", got, want)
	}
}

// TestParseStatementOpeningBeforeInPeriodSplit covers the case where the
// statement lists its period-end holding after a 2:1 split dated inside the
// period, and the opening buy, dated before the split, is scaled by it once
// committed. So the opening is the pre-split quantity, priced at the
// period-end price times the ratio, and the reconciliation, which applies
// the split as the commit's holdings will, matches. Synthetic values.
func TestParseStatementOpeningBeforeInPeriodSplit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening-split@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('SPLT', 'US', 'x', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	if err := split.NewRepo(e.db).Upsert(context.Background(), id, "2026-03-15", dec("2"), "manual"); err != nil {
		t.Fatalf("upsert split: %v", err)
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "opening-split") })
	statement.Parsers["opening-split"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening-split", PeriodStart: "2026-03-01", PeriodEnd: "2026-03-31", Currency: "USD", Market: "US",
			Holdings: []statement.Holding{{Symbol: "SPLT", Quantity: dec("20"), Currency: "USD", Price: dec("30")}},
		}, nil
	}

	p := parsePreview(t, upload(e, session, "opening-split", portfolioID, emptyPDF(0)))
	var got []string
	for _, row := range p.Rows {
		if row.Opening && row.Type == "buy" {
			got = append(got, fmt.Sprintf("%s %s %s %s %s", row.Status, row.Date, row.Symbol, row.Quantity, row.Price))
		}
	}
	if want := []string{"ok 2026-02-28 SPLT 10 60"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("opening rows = %q\nwant %q", got, want)
	}
	for _, r := range p.Reconciliation {
		if !r.Match {
			t.Errorf("reconciliation %s: portfolio %s, statement %v", r.Symbol, r.PortfolioQuantity, *r.StatementQuantity)
		}
	}
}

// TestParseStatementClosingBeforeInPeriodSplit covers the case where the
// portfolio holds 10 bought before the period, a 2:1 split is dated inside
// it, and the statement lists 10 at period end. Held at period end is 20, so
// the excess is 10 post-split, but the closing sell is dated before the
// split, which doubles what it leaves: it sells 5, priced at the period-end
// price times the ratio. Synthetic values.
func TestParseStatementClosingBeforeInPeriodSplit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("closing-split@example.com")
	portfolioID := e.createPortfolio(session, "Held")
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('SPLT', 'US', 'x', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	e.createTransaction(session, portfolioID, map[string]any{
		"type": "buy", "date": "2026-01-10", "instrument_id": id, "quantity": "10", "price": "1", "currency": "USD",
	})
	if err := split.NewRepo(e.db).Upsert(context.Background(), id, "2026-03-15", dec("2"), "manual"); err != nil {
		t.Fatalf("upsert split: %v", err)
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "closing-split") })
	statement.Parsers["closing-split"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "closing-split", PeriodStart: "2026-03-01", PeriodEnd: "2026-03-31", Currency: "USD", Market: "US",
			Holdings: []statement.Holding{{Symbol: "SPLT", Quantity: dec("10"), Currency: "USD", Price: dec("30")}},
		}, nil
	}

	p := parsePreview(t, upload(e, session, "closing-split", portfolioID, emptyPDF(0)))
	var got []string
	for _, row := range p.Rows {
		if row.Closing || row.Opening {
			got = append(got, fmt.Sprintf("%s %s %s %s %s %s", row.Status, row.Type, row.Date, row.Symbol, row.Quantity, row.Price))
		}
	}
	want := []string{"ok sell 2026-02-28 SPLT 5 60", "ok withdrawal 2026-02-28   "}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("proposed rows = %q\nwant %q", got, want)
	}
}

// TestParseStatementProposesClosings checks that a position the
// portfolio has held since before the period and the statement's holdings
// omit gets one closing sell of the held quantity, dated the day before the
// period at the stored price of that day, and a withdrawal of its proceeds
// rounded down to the cent; a position the statement lists at the held
// quantity gets none, and the reconciliation leaves both out, as the page
// starts the sell unchecked.
func TestParseStatementProposesClosings(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("closing@example.com")
	portfolioID := e.createPortfolio(session, "Held")
	ids := map[string]int64{}
	for _, symbol := range []string{"OLD", "KEEP"} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'US', 'x', 'USD')`, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		e.createTransaction(session, portfolioID, map[string]any{
			"type": "buy", "date": "2019-01-10", "instrument_id": ids[symbol], "quantity": "79", "price": "1", "currency": "USD",
		})
	}
	prices := price.NewRepo(e.db)
	for date, close := range map[string]string{"2025-08-29": "12.345", "2025-09-05": "99"} {
		if err := prices.Upsert(context.Background(), ids["OLD"], date, close, "us"); err != nil {
			t.Fatalf("upsert price: %v", err)
		}
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.PriceSources.Prices = prices
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "closing") })
	statement.Parsers["closing"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "closing", PeriodStart: "2025-09-01", PeriodEnd: "2025-09-30", Currency: "USD", Market: "US",
			Holdings: []statement.Holding{{Symbol: "KEEP", Quantity: dec("79"), Currency: "USD", Price: dec("2")}},
		}, nil
	}

	p := parsePreview(t, upload(e, session, "closing", portfolioID, emptyPDF(0)))
	var got []string
	for _, row := range p.Rows {
		if row.Closing || row.Opening {
			got = append(got, fmt.Sprintf("%s %s %s %s %s %s %s %s %v", row.Key, row.Status, row.Type, row.Date, row.Symbol, row.Quantity, row.Price, row.Amount, row.Closing))
		}
	}
	want := []string{
		fmt.Sprintf("close-%d ok sell 2025-08-31 OLD 79 12.345 975.255 true", ids["OLD"]),
		"close-cash-USD ok withdrawal 2025-08-31    975.25 true",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("proposed rows = %q\nwant %q", got, want)
	}
	matched := map[string]bool{}
	for _, r := range p.Reconciliation {
		matched[r.Symbol] = r.Match
	}
	if fmt.Sprint(matched) != "map[KEEP:true OLD:false]" {
		t.Errorf("reconciliation matches = %v, want KEEP matched and OLD still held", matched)
	}
}

// TestParseStatementOpeningIgnoresForeignCurrencyPrice covers a
// Garanti foreign-currency fund that prints a USD unit price for a TRY fund:
// with no stored price the opening is an opening_price error, not a TRY buy
// at the USD price.
func TestParseStatementOpeningIgnoresForeignCurrencyPrice(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening-usd@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('GPA', 'TEFAS', 'x', 'TRY')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "opening-usd") })
	statement.Parsers["opening-usd"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening-usd", PeriodStart: "2026-05-01", PeriodEnd: "2026-05-31", Currency: "TRY", Market: "TEFAS",
			Holdings: []statement.Holding{{Symbol: "GPA", Quantity: dec("100"), Currency: "USD", Price: dec("0.05")}},
		}, nil
	}

	var got []string
	for _, row := range parsePreview(t, upload(e, session, "opening-usd", portfolioID, emptyPDF(0))).Rows {
		if row.Opening {
			got = append(got, fmt.Sprintf("%s %s %s %s %q %s", row.Status, row.Type, row.Symbol, row.Quantity, row.Price, row.Code))
		}
	}
	if want := []string{`error buy GPA 100 "" opening_price`}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("opening rows = %q\nwant %q", got, want)
	}
}

// TestParseStatementProposesNoOpeningsWithUnknownTrades checks that, since a
// buy of an unknown instrument may belong to a holding that resolves, a
// preview with one proposes no opening rows at all. Unknown rows whose units
// net to zero, a fund bought and sold within the month, cannot change a
// holding and do not block them.
func TestParseStatementProposesNoOpeningsWithUnknownTrades(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening-unknown@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('OPA', 'TEFAS', 'x', 'TRY')`); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	trade := func(date, symbol, side, quantity string) statement.Trade {
		return statement.Trade{Date: date, Symbol: symbol, Side: side, Quantity: dec(quantity), Price: dec("2"), Amount: dec(quantity).Mul(dec("2")), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"}
	}
	var trades []statement.Trade
	t.Cleanup(func() { delete(statement.Parsers, "opening-unknown") })
	statement.Parsers["opening-unknown"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening-unknown", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS",
			Trades:   trades,
			Holdings: []statement.Holding{{Symbol: "OPA", Quantity: dec("100"), Currency: "TRY", Price: dec("2.5")}},
		}, nil
	}
	preview := func() (openings []string, unknown int) {
		for _, row := range parsePreview(t, upload(e, session, "opening-unknown", portfolioID, emptyPDF(0))).Rows {
			if row.Opening {
				openings = append(openings, row.Type+" "+row.Symbol+" "+row.Quantity+" "+row.Amount)
			}
			if row.Status == "unknown_instrument" {
				unknown++
			}
		}
		return openings, unknown
	}

	opa := trade("2025-12-10", "OPA", "buy", "30")
	trades = []statement.Trade{opa, trade("2025-12-11", "OPA FUND BY NAME", "buy", "20")}
	if openings, unknown := preview(); len(openings) != 0 || unknown != 1 {
		t.Errorf("buy of an unknown fund: openings %q, %d unknown_instrument rows; want none and 1", openings, unknown)
	}

	sweep := "GPY 1. PARA PIYASASI (TL) FONU"
	trades = []statement.Trade{opa, trade("2025-12-12", sweep, "buy", "7.5"), trade("2025-12-15", sweep, "sell", "5"), trade("2025-12-20", sweep, "sell", "2.5")}
	openings, unknown := preview()
	if fmt.Sprint(openings) != "[deposit   175 buy OPA 70 175]" || unknown != 3 {
		t.Errorf("unknown fund netting to zero: openings %q, %d unknown_instrument rows; want the OPA opening and its deposit, and 3", openings, unknown)
	}

	trades = trades[:3]
	if openings, unknown := preview(); len(openings) != 0 || unknown != 2 {
		t.Errorf("unknown fund not netting to zero: openings %q, %d unknown_instrument rows; want none and 2", openings, unknown)
	}
}

// TestParseStatementSyncsBulletinForUnknownBIST checks that a BIST row
// whose symbol is no instrument, such as a share delisted since, resolves
// once the bulletin of its date is synced, and the preview proposes opening
// positions again. A symbol that bulletin does not list stays
// unknown_instrument, each date is synced once, and at most five bulletins
// are fetched; a date whose bulletin is already stored does not count.
func TestParseStatementSyncsBulletinForUnknownBIST(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bulletin-sync@example.com")
	portfolioID := e.createPortfolio(session, "Empty")

	instruments := instrument.NewRepo(e.db)
	// The stubbed bulletins: 2018-03-05 lists ZZOLD, 2018-04-09 ZZNEW, no
	// date lists ZZGONE, and those of 2018-04-02 to 2018-04-06 are already
	// stored, so syncing them fetches nothing.
	lists := map[string]string{"2018-03-05": "ZZOLD", "2018-04-09": "ZZNEW"}
	var synced []string
	syncBulletin := func(ctx context.Context, date time.Time) (bool, error) {
		d := date.Format(time.DateOnly)
		synced = append(synced, d)
		if d >= "2018-04-02" && d <= "2018-04-06" {
			return false, nil
		}
		if symbol, ok := lists[d]; ok {
			_, err := instruments.Insert(ctx, "BIST", symbol, symbol+" SYNTHETIC", "TRY")
			return true, err
		}
		return true, nil
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.Instruments = instruments
	deps.SyncBulletin = syncBulletin
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	trade := func(date, symbol, side string) statement.Trade {
		return statement.Trade{Date: date, Symbol: symbol, Side: side, Quantity: dec("10"), Price: dec("2"), Amount: dec("20"), Fee: dec("0"), Currency: "TRY", Market: "BIST"}
	}
	t.Cleanup(func() {
		for _, key := range []string{"bulletin-sync", "bulletin-gone", "bulletin-stored"} {
			delete(statement.Parsers, key)
		}
	})
	statement.Parsers["bulletin-sync"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "bulletin-sync", PeriodStart: "2018-03-01", PeriodEnd: "2018-03-31", Currency: "TRY", Market: "BIST",
			Trades:   []statement.Trade{trade("2018-03-05", "ZZOLD", "buy"), trade("2018-03-06", "ZZOLD", "sell")},
			Holdings: []statement.Holding{{Symbol: "ZZOLD", Quantity: dec("100"), Currency: "TRY", Price: dec("2.5")}},
		}, nil
	}
	p := parsePreview(t, upload(e, session, "bulletin-sync", portfolioID, emptyPDF(0)))
	var trades, openings []string
	for _, row := range p.Rows {
		switch {
		case row.Opening:
			openings = append(openings, row.Type+":"+row.Symbol+":"+row.Quantity)
		case row.Symbol != "":
			trades = append(trades, row.Date+" "+row.Status)
			if row.InstrumentID == nil {
				t.Errorf("%s: no instrument_id, want ZZOLD's", row.Key)
			}
		}
	}
	if want := "[2018-03-05 ok 2018-03-06 ok]"; fmt.Sprint(trades) != want {
		t.Errorf("trade rows = %v, want %s", trades, want)
	}
	if want := "[deposit:: buy:ZZOLD:100]"; fmt.Sprint(openings) != want {
		t.Errorf("opening rows = %v, want %s", openings, want)
	}
	// Then the opening's, of the day before the period.
	if want := "[2018-03-05 2018-02-28]"; fmt.Sprint(synced) != want {
		t.Errorf("synced bulletins = %v, want %s", synced, want)
	}

	synced = nil
	statement.Parsers["bulletin-gone"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "bulletin-gone", PeriodStart: "2018-03-01", PeriodEnd: "2018-03-31", Currency: "TRY", Market: "BIST"}
		for day := 12; day <= 18; day++ {
			st.Trades = append(st.Trades, trade(fmt.Sprintf("2018-03-%02d", day), "ZZGONE", "buy"))
		}
		st.Trades = append(st.Trades, trade("2018-03-12", "ZZGONE", "sell"))
		return st, nil
	}
	unknown := 0
	for _, row := range parsePreview(t, upload(e, session, "bulletin-gone", portfolioID, emptyPDF(0))).Rows {
		if row.Status == "unknown_instrument" {
			unknown++
		}
	}
	if unknown != 8 {
		t.Errorf("unknown_instrument rows = %d, want 8", unknown)
	}
	if want := "[2018-03-12 2018-03-13 2018-03-14 2018-03-15 2018-03-16]"; fmt.Sprint(synced) != want {
		t.Errorf("synced bulletins = %v, want the first five dates once each, %s", synced, want)
	}

	// Five dates whose bulletin is already stored come first; the sixth
	// date's bulletin is still fetched and resolves ZZNEW.
	synced = nil
	statement.Parsers["bulletin-stored"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "bulletin-stored", PeriodStart: "2018-04-01", PeriodEnd: "2018-04-30", Currency: "TRY", Market: "BIST"}
		for day := 2; day <= 6; day++ {
			st.Trades = append(st.Trades, trade(fmt.Sprintf("2018-04-%02d", day), "ZZGONE", "buy"))
		}
		st.Trades = append(st.Trades, trade("2018-04-09", "ZZNEW", "buy"))
		return st, nil
	}
	statuses := map[string]string{}
	for _, row := range parsePreview(t, upload(e, session, "bulletin-stored", portfolioID, emptyPDF(0))).Rows {
		statuses[row.Symbol] = row.Status
	}
	if want := "map[ZZGONE:unknown_instrument ZZNEW:ok]"; fmt.Sprint(statuses) != want {
		t.Errorf("statuses = %v, want %s", statuses, want)
	}
	if len(synced) != 6 {
		t.Errorf("synced bulletins = %v, want the six dates", synced)
	}
}

// TestBulletinSyncsShareClientLimit checks that the preview's bulletin
// fetches count against a budget of 10 a minute per client IP, so repeated
// previews whose bulletins keep failing cannot hammer the bulletin host; a
// row over the budget stays unknown_instrument.
func TestBulletinSyncsShareClientLimit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bulletinlimit@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	calls := 0
	syncBulletin := func(context.Context, time.Time) (bool, error) {
		calls++
		return true, errors.New("bulletin host unreachable")
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.SyncBulletin = syncBulletin
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	file := "date,type,symbol,quantity,price,currency,market\n"
	for day := 1; day <= 5; day++ {
		file += fmt.Sprintf("2026-09-%02d,buy,ZZQ%d,1,1,TRY,BIST\n", day, day)
	}
	for i := 0; i < 3; i++ {
		for _, row := range parsePreview(t, upload(e, session, "csv", portfolioID, []byte(file))).Rows {
			if row.Status != "unknown_instrument" {
				t.Errorf("parse %d row %s: status = %s, want unknown_instrument", i+1, row.Symbol, row.Status)
			}
		}
	}
	if calls != 10 {
		t.Errorf("bulletin syncs after 3 previews of 5 dates = %d, want 10", calls)
	}
}

// TestParseStatementSyncsBulletinForOpenings checks that BIST opening
// rows with no stored price for the day before the period sync that day's
// bulletin once, the last weekday on or before it, and take its closes; a
// symbol the bulletin lacks stays an opening_price error. With the bist
// source off the sync stores nothing and every row stays an error.
func TestParseStatementSyncsBulletinForOpenings(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bulletin-opening@example.com")
	portfolioID := e.createPortfolio(session, "Empty")

	instruments := instrument.NewRepo(e.db)
	prices := price.NewRepo(e.db)
	ctx := context.Background()
	ids := map[string]int64{}
	for _, symbol := range []string{"ZZA", "ZZB", "ZZC"} {
		id, err := instruments.Insert(ctx, "BIST", symbol, symbol+" SYNTHETIC", "TRY")
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		ids[symbol] = id
	}
	// The stubbed bulletin of 2018-06-29 lists ZZA and ZZB, not ZZC; with
	// bist off the sync asks nothing, as the server's does.
	bistOn := false
	var synced []string
	syncBulletin := func(ctx context.Context, date time.Time) (bool, error) {
		d := date.Format(time.DateOnly)
		synced = append(synced, d)
		if !bistOn {
			return false, nil
		}
		for symbol, close := range map[string]string{"ZZA": "12.34", "ZZB": "5.6"} {
			if err := prices.Upsert(ctx, ids[symbol], d, close, "bist_bulletin"); err != nil {
				return true, err
			}
		}
		return true, nil
	}
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.Instruments = instruments
	deps.PriceSources.Prices = prices
	deps.SyncBulletin = syncBulletin
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)

	// The day before the period, 2018-06-30, is a Saturday.
	t.Cleanup(func() { delete(statement.Parsers, "bulletin-opening") })
	statement.Parsers["bulletin-opening"] = func([][]string) (statement.Statement, error) {
		st := statement.Statement{Broker: "bulletin-opening", PeriodStart: "2018-07-01", PeriodEnd: "2018-07-31", Currency: "TRY", Market: "BIST"}
		for _, symbol := range []string{"ZZA", "ZZB", "ZZC"} {
			st.Holdings = append(st.Holdings, statement.Holding{Symbol: symbol, Quantity: dec("10"), Currency: "TRY"})
		}
		return st, nil
	}
	openings := func() []string {
		var got []string
		for _, row := range parsePreview(t, upload(e, session, "bulletin-opening", portfolioID, emptyPDF(0))).Rows {
			if row.Opening && row.Type == "buy" {
				got = append(got, fmt.Sprintf("%s %s %s %q %s", row.Date, row.Status, row.Symbol, row.Price, row.Code))
			}
		}
		return got
	}

	got := openings()
	want := []string{`2018-06-30 error ZZA "" opening_price`, `2018-06-30 error ZZB "" opening_price`, `2018-06-30 error ZZC "" opening_price`}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("bist off: opening rows = %v\nwant %v", got, want)
	}
	if want := "[2018-06-29]"; fmt.Sprint(synced) != want {
		t.Errorf("bist off: synced bulletins = %v, want %s", synced, want)
	}

	bistOn, synced = true, nil
	got = openings()
	want = []string{`2018-06-30 ok ZZA "12.34" `, `2018-06-30 ok ZZB "5.6" `, `2018-06-30 error ZZC "" opening_price`}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("bist on: opening rows = %v\nwant %v", got, want)
	}
	if want := "[2018-06-29]"; fmt.Sprint(synced) != want {
		t.Errorf("bist on: synced bulletins = %v, want %s", synced, want)
	}
}

// TestParseStatementFollowsPicks checks that a sell of a fund named by
// its name blocks every opening row, and the same preview requested again
// with the user's pick for it resolves the sell, so the held fund gets its
// opening buy and reconciles. The pick wins over a stored one for the same
// symbol, whose instrument then gets no opening row, an unknown instrument
// is a 400, and parse stores nothing.
func TestParseStatementFollowsPicks(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("opening-pick@example.com")
	portfolioID := e.createPortfolio(session, "Empty")
	ids := map[string]int64{}
	for _, symbol := range []string{"OPA", "OPB"} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', 'x', 'TRY')`, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	protected := http.NewServeMux()
	statement.RegisterRoutes(protected, e.importDeps())
	e.handler = e.sessions.RequireUser(protected)

	t.Cleanup(func() { delete(statement.Parsers, "opening-pick") })
	statement.Parsers["opening-pick"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "opening-pick", PeriodStart: "2025-12-01", PeriodEnd: "2025-12-31", Currency: "TRY", Market: "TEFAS",
			Trades: []statement.Trade{
				{Date: "2025-12-11", Symbol: "OPA FUND BY NAME", Side: "sell", Quantity: dec("20"), Price: dec("2"), Amount: dec("40"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"},
			},
			Holdings: []statement.Holding{{Symbol: "OPA", Quantity: dec("100"), Currency: "TRY", Price: dec("2.5")}},
		}, nil
	}
	openings := func(p preview) (out []string) {
		for _, row := range p.Rows {
			if row.Opening && row.Type == "buy" {
				out = append(out, row.Symbol+" "+row.Quantity)
			}
		}
		return out
	}

	p := parsePreview(t, upload(e, session, "opening-pick", portfolioID, emptyPDF(0)))
	if got := openings(p); len(got) != 0 {
		t.Fatalf("opening buys without a pick = %v, want none", got)
	}

	// A stored pick names OPB, so without this preview's pick the sell is
	// OPB's, and OPB gets the opening that sell needs.
	if _, err := e.db.Exec(`INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at)
		SELECT id, 'opening-pick', 'OPA FUND BY NAME', ?, '2026-01-01T00:00:00Z' FROM users WHERE email = 'opening-pick@example.com'`, ids["OPB"]); err != nil {
		t.Fatalf("insert alias: %v", err)
	}
	if got := fmt.Sprint(openings(parsePreview(t, upload(e, session, "opening-pick", portfolioID, emptyPDF(0))))); got != "[OPA 100 OPB 20]" {
		t.Errorf("opening buys with the stored pick = %s, want [OPA 100 OPB 20]", got)
	}

	picks := fmt.Sprintf(`[{"symbol":"OPA FUND BY NAME","instrument_id":%d}]`, ids["OPA"])
	p = parsePreview(t, uploadPicks(e, session, "opening-pick", portfolioID, emptyPDF(0), picks))
	if got := fmt.Sprint(openings(p)); got != "[OPA 120]" {
		t.Errorf("opening buys with the pick = %s, want [OPA 120]", got)
	}
	for _, row := range p.Rows {
		if row.Key == "trade-0" && (row.InstrumentID == nil || *row.InstrumentID != ids["OPA"] || row.ResolvedBy != "alias") {
			t.Errorf("picked sell = %+v, want OPA resolved_by alias", row)
		}
	}
	if len(p.Reconciliation) != 1 || !p.Reconciliation[0].Match {
		t.Errorf("reconciliation = %+v, want OPA matching", p.Reconciliation)
	}

	rec := uploadPicks(e, session, "opening-pick", portfolioID, emptyPDF(0), `[{"symbol":"OPA FUND BY NAME","instrument_id":999999}]`)
	if want := `{"error":"picks[0].instrument_id: unknown instrument"}`; rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("unknown pick = %d %s, want 400 %s", rec.Code, rec.Body.String(), want)
	}
	var stored int64
	if err := e.db.QueryRow(`SELECT instrument_id FROM import_aliases WHERE symbol = 'OPA FUND BY NAME'`).Scan(&stored); err != nil || stored != ids["OPB"] {
		t.Errorf("stored pick = %d (%v), want OPB's %d unchanged", stored, err, ids["OPB"])
	}
}

// TestImportLearnsFundNames checks that a committed garanti statement's
// balance names, sent back as the commit's names, resolve a later
// statement's sells named by a cut name with resolved_by "name", which lets
// the preview propose openings again; a name of two instruments stays
// unknown, a name whose code resolves to nothing or that is printed or sent
// for two funds is learned for neither, a later statement printing a learned
// name for two funds makes it unknown in its own preview and forgets it on
// commit, a holding's code never resolves by a learned name, and the user's
// pick for the same name wins.
func TestImportLearnsFundNames(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("learn@example.com")
	portfolioID := e.createPortfolio(session, "Funds")
	ids := map[string]int64{}
	for _, symbol := range []string{"FNA", "FNB", "FNC", "FND", "FNE"} {
		res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', ?, 'TRY')`, symbol, "Fund "+symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	// The commit accepts real brokers only, so the garanti parser is stubbed.
	original := statement.Parsers["garanti"]
	t.Cleanup(func() { statement.Parsers["garanti"] = original })
	var current statement.Statement
	statement.Parsers["garanti"] = func([][]string) (statement.Statement, error) { return current, nil }
	month := func(start, end string, trades []statement.Trade, holdings ...statement.Holding) statement.Statement {
		return statement.Statement{Broker: "garanti", PeriodStart: start, PeriodEnd: end, Currency: "TRY", Market: "TEFAS", Trades: trades, Holdings: holdings}
	}
	sell := func(name string) statement.Trade {
		return statement.Trade{Date: "2025-12-10", Symbol: name, Side: "sell", Quantity: dec("4"), Price: dec("2.5"), Amount: dec("10"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"}
	}
	hold := func(symbol, name string) statement.Holding {
		return statement.Holding{Symbol: symbol, Name: name, Quantity: dec("6"), Currency: "TRY", Price: dec("2.5")}
	}

	current = month("2025-11-01", "2025-11-30", nil,
		hold("FNA", "ALFA PORTFÖY DENGELİ DEĞİŞKEN FON"),
		hold("FNB", "BETA PORTFÖY KISA VADELİ BORÇLANMA"),
		hold("FNC", "BETA PORTFÖY KISA VADELİ SERBEST FON"),
		hold("ZZQ", "GAMA PORTFÖY HİSSE FON"),
		// One cut name printed for two funds is learned for neither.
		hold("FND", "DELTA PORTFÖY SERBEST"),
		hold("FNE", "DELTA PORTFÖY SERBEST"),
	)
	p := parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(1)))
	want := fmt.Sprintf("[map[instrument_id:%d symbol:ALFA PORTFÖY DENGELİ DEĞİŞKEN FON] map[instrument_id:%d symbol:BETA PORTFÖY KISA VADELİ BORÇLANMA] map[instrument_id:%d symbol:BETA PORTFÖY KISA VADELİ SERBEST FON] map[instrument_id:0 symbol:GAMA PORTFÖY HİSSE FON] map[instrument_id:0 symbol:DELTA PORTFÖY SERBEST]]", ids["FNA"], ids["FNB"], ids["FNC"])
	if got := fmt.Sprint(p.Names); got != want {
		t.Fatalf("names = %s\nwant %s", got, want)
	}
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "garanti", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": "TRY",
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": []any{},
		// The commit guards the same way: a name sent with two instruments
		// is stored for neither.
		"names": append(p.Names,
			map[string]any{"symbol": "EPSİLON PORTFÖY FON", "instrument_id": ids["FND"]},
			map[string]any{"symbol": "EPSİLON PORTFÖY FON", "instrument_id": ids["FNE"]},
		),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	// December sells FNA by a name cut shorter than its balance name and
	// lists no balance of it.
	tradeRow := func(p preview, key string) string {
		for _, row := range p.Rows {
			if row.Key == key {
				id := int64(0)
				if row.InstrumentID != nil {
					id = *row.InstrumentID
				}
				return strings.TrimSpace(fmt.Sprintf("%s %d %s %s", row.Status, id, row.ResolvedBy, row.InstrSymbol))
			}
		}
		return ""
	}
	trade := func(p preview) string { return tradeRow(p, "trade-0") }
	current = month("2025-12-01", "2025-12-31", []statement.Trade{sell("ALFA PORTFÖY DENGELİ")}, hold("FNB", ""))
	p = parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(2)))
	if got, want := trade(p), fmt.Sprintf("ok %d name FNA", ids["FNA"]); got != want {
		t.Errorf("sell by a learned name = %q, want %q", got, want)
	}
	var openings []string
	for _, row := range p.Rows {
		if row.Opening && row.Type == "buy" {
			openings = append(openings, row.Symbol+" "+row.Quantity)
		}
	}
	if fmt.Sprint(openings) != "[FNB 6 FNA 4]" {
		t.Errorf("opening buys = %v, want [FNB 6 FNA 4]", openings)
	}

	current = month("2025-12-01", "2025-12-31", []statement.Trade{sell("BETA PORTFÖY KISA VADELİ")})
	if got := trade(parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(2)))); got != "unknown_instrument 0" {
		t.Errorf("sell by a name of two instruments = %q, want unknown_instrument", got)
	}
	for _, name := range []string{"DELTA PORTFÖY SERBEST", "EPSİLON PORTFÖY FON"} {
		current = month("2025-12-01", "2025-12-31", []statement.Trade{sell(name)})
		if got := trade(parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(2)))); got != "unknown_instrument 0" {
			t.Errorf("sell by %s, printed or sent for two funds = %q, want unknown_instrument", name, got)
		}
	}

	current = month("2025-12-01", "2025-12-31", []statement.Trade{sell("ALFA PORTFÖY DENGELİ")})
	picks := fmt.Sprintf(`[{"symbol":"ALFA PORTFÖY DENGELİ","instrument_id":%d}]`, ids["FNC"])
	if got, want := trade(parsePreview(t, uploadPicks(e, session, "garanti", portfolioID, emptyPDF(2), picks))), fmt.Sprintf("ok %d alias FNC", ids["FNC"]); got != want {
		t.Errorf("sell picked in this preview = %q, want %q", got, want)
	}
	if _, err := e.db.Exec(`INSERT INTO import_aliases (user_id, broker, symbol, instrument_id, created_at)
		SELECT id, 'garanti', 'ALFA PORTFÖY DENGELİ', ?, '2026-01-01T00:00:00Z' FROM users WHERE email = 'learn@example.com'`, ids["FNB"]); err != nil {
		t.Fatalf("insert pick: %v", err)
	}
	if got, want := trade(parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(2)))), fmt.Sprintf("ok %d alias FNB", ids["FNB"]); got != want {
		t.Errorf("sell with a stored pick = %q, want %q", got, want)
	}

	// January learns OMEGA as FNA, and a name that starts with the code ZZQ.
	// February prints OMEGA for FNA and FNB and sells OMEGA by name.
	later := e.createPortfolio(session, "Later")
	sellOn := func(date, name string) statement.Trade { s := sell(name); s.Date = date; return s }
	omegaPick := fmt.Sprintf(`[{"symbol":"OMEGA PORTFÖY FON","instrument_id":%d}]`, ids["FNB"])
	commitNames := func(p preview) {
		t.Helper()
		rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
			"broker": "garanti", "portfolio_id": later, "sha256": p.SHA256, "currency": "TRY",
			"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "rows": []any{}, "names": p.Names,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("commit %s status = %d, want 201; body: %s", p.PeriodStart, rec.Code, rec.Body.String())
		}
	}
	current = month("2026-01-01", "2026-01-31", nil, hold("FNA", "OMEGA PORTFÖY FON"), hold("FNC", "ZZQ PORTFÖY FON"))
	commitNames(parsePreview(t, upload(e, session, "garanti", later, emptyPDF(3))))
	march := month("2026-03-01", "2026-03-31", []statement.Trade{sellOn("2026-03-10", "OMEGA PORTFÖY FON"), sellOn("2026-03-11", "ZZQ")}, hold("ZZQ", ""))
	current = march
	p = parsePreview(t, upload(e, session, "garanti", later, emptyPDF(5)))
	if got, want := trade(p), fmt.Sprintf("ok %d name FNA", ids["FNA"]); got != want {
		t.Errorf("OMEGA after January = %q, want %q", got, want)
	}
	if got := tradeRow(p, "trade-1"); got != "unknown_instrument 0" {
		t.Errorf("sell of the holding code ZZQ = %q, want unknown_instrument, not a learned name", got)
	}
	current = month("2026-02-01", "2026-02-28", []statement.Trade{sellOn("2026-02-10", "OMEGA PORTFÖY FON")},
		hold("FNA", "OMEGA PORTFÖY FON"), hold("FNB", "OMEGA PORTFÖY FON"))
	p = parsePreview(t, upload(e, session, "garanti", later, emptyPDF(4)))
	if got := trade(p); got != "unknown_instrument 0" {
		t.Errorf("OMEGA in February, printed for two funds = %q, want unknown_instrument", got)
	}
	if got, want := trade(parsePreview(t, uploadPicks(e, session, "garanti", later, emptyPDF(4), omegaPick))), fmt.Sprintf("ok %d alias FNB", ids["FNB"]); got != want {
		t.Errorf("OMEGA picked in February = %q, want %q", got, want)
	}
	commitNames(p)
	current = march
	if got := trade(parsePreview(t, upload(e, session, "garanti", later, emptyPDF(5)))); got != "unknown_instrument 0" {
		t.Errorf("OMEGA after February = %q, want unknown_instrument", got)
	}
	if got, want := trade(parsePreview(t, uploadPicks(e, session, "garanti", later, emptyPDF(5), omegaPick))), fmt.Sprintf("ok %d alias FNB", ids["FNB"]); got != want {
		t.Errorf("OMEGA picked after February = %q, want %q", got, want)
	}
}

// TestImportLearnsCreatedFundName checks that a fund that
// TEFAS confirms while the preview resolves this month's buy is the
// holding's instrument too, so it reconciles once and matching, its name is
// offered with that instrument, and a later sell by its cut name resolves.
func TestImportLearnsCreatedFundName(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("learn-created@example.com")
	portfolioID := e.createPortfolio(session, "Funds")
	lookup := func(_ context.Context, code string) (string, bool, error) { return "Fund " + code, true, nil }
	protected := http.NewServeMux()
	deps := e.importDeps()
	deps.LookupFund = lookup
	statement.RegisterRoutes(protected, deps)
	e.handler = e.sessions.RequireUser(protected)
	original := statement.Parsers["garanti"]
	t.Cleanup(func() { statement.Parsers["garanti"] = original })
	var current statement.Statement
	statement.Parsers["garanti"] = func([][]string) (statement.Statement, error) { return current, nil }
	trade := func(date, symbol, side string) statement.Trade {
		return statement.Trade{Date: date, Symbol: symbol, Side: side, Quantity: dec("6"), Price: dec("2.5"), Amount: dec("15"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"}
	}

	current = statement.Statement{
		Broker: "garanti", PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31", Currency: "TRY", Market: "TEFAS",
		Trades:   []statement.Trade{trade("2026-01-10", "ZNF", "buy")},
		Holdings: []statement.Holding{{Symbol: "ZNF", Name: "ZETA PORTFÖY YENİ FON", Quantity: dec("6"), Currency: "TRY", Price: dec("2.5")}},
	}
	p := parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(1)))
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM instruments WHERE market = 'TEFAS' AND symbol = 'ZNF'`).Scan(&id); err != nil {
		t.Fatalf("created fund: %v", err)
	}
	if len(p.Reconciliation) != 1 || p.Reconciliation[0].Symbol != "ZNF" || !p.Reconciliation[0].Match {
		t.Errorf("reconciliation = %+v, want ZNF once, matching", p.Reconciliation)
	}
	if got, want := fmt.Sprint(p.Names), fmt.Sprintf("[map[instrument_id:%d symbol:ZETA PORTFÖY YENİ FON]]", id); got != want {
		t.Errorf("names = %s, want %s", got, want)
	}
	rec := e.do(session, http.MethodPost, "/api/imports", map[string]any{
		"broker": "garanti", "portfolio_id": portfolioID, "sha256": p.SHA256, "currency": "TRY",
		"period_start": p.PeriodStart, "period_end": p.PeriodEnd, "names": p.Names,
		"rows": []map[string]any{{"type": "buy", "date": "2026-01-10", "instrument_id": id, "quantity": "6", "price": "2.5", "amount": "15", "fee": "0", "currency": "TRY"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	current = statement.Statement{
		Broker: "garanti", PeriodStart: "2026-02-01", PeriodEnd: "2026-02-28", Currency: "TRY", Market: "TEFAS",
		Trades: []statement.Trade{trade("2026-02-10", "ZETA PORTFÖY YENİ", "sell")},
	}
	for _, row := range parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(2))).Rows {
		if row.Key == "trade-0" && (row.Status != "ok" || row.InstrumentID == nil || *row.InstrumentID != id || row.ResolvedBy != "name") {
			t.Errorf("sell by the learned name = %+v, want ok ZNF resolved_by name", row)
		}
	}
}

// TestParseStatementTakesBatchNames covers a fund TEFAS renamed. It is
// named in an early statement only by its old name, which no instrument
// carries, and the statement holds no balance of it. A later statement of
// the same batch prints that name next to the code; sent as names, it
// resolves the early row like a learned name, before anything is committed.
func TestParseStatementTakesBatchNames(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("batch-names@example.com")
	portfolioID := e.createPortfolio(session, "Funds")
	res, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('IIX', 'TEFAS', 'BULLS PORTFÖY İKİNCİ SERBEST FON', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	original := statement.Parsers["garanti"]
	t.Cleanup(func() { statement.Parsers["garanti"] = original })
	statement.Parsers["garanti"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{
			Broker: "garanti", PeriodStart: "2025-04-01", PeriodEnd: "2025-04-30", Currency: "TRY", Market: "TEFAS",
			Trades: []statement.Trade{{Date: "2025-04-10", Symbol: "İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST", Side: "buy", Quantity: dec("4"), Price: dec("2.5"), Amount: dec("10"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"}},
		}, nil
	}

	if row := parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(1))).Rows[0]; row.Status != "unknown_instrument" {
		t.Fatalf("row without names = %s, want unknown_instrument", row.Status)
	}
	names := fmt.Sprintf(`[{"symbol":"İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST FON","instrument_id":%d}]`, id)
	row := parsePreview(t, uploadFields(e, session, "garanti", portfolioID, emptyPDF(1), map[string]string{"names": names})).Rows[0]
	if row.Status != "ok" || row.InstrumentID == nil || *row.InstrumentID != id || row.ResolvedBy != "name" || row.InstrSymbol != "IIX" {
		t.Errorf("row with the batch's names = %+v, want ok IIX resolved_by name", row)
	}
	rec := uploadFields(e, session, "garanti", portfolioID, emptyPDF(1), map[string]string{"names": `[{"symbol":"X","instrument_id":999999}]`})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "names[0].instrument_id: unknown instrument") {
		t.Errorf("unknown names instrument = %d %s, want 400 naming names[0]", rec.Code, rec.Body.String())
	}
}
