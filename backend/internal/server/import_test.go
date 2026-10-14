package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The period the tests in this file import unless they set their own.
const (
	importPeriodStart = "2025-12-01"
	importPeriodEnd   = "2025-12-31"
)

// fakeSHA is a well-formed file hash for a test that has no file to hash.
func fakeSHA(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

// importBody builds a commit request for portfolioID with the given rows.
func importBody(portfolioID int64, sha string, rows ...map[string]any) map[string]any {
	return map[string]any{
		"broker":       "midas",
		"portfolio_id": portfolioID,
		"sha256":       sha,
		"currency":     "TRY",
		"period_start": importPeriodStart,
		"period_end":   importPeriodEnd,
		"rows":         rows,
	}
}

// importedTransaction is the part of a transaction the import tests read back.
type importedTransaction struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Amount   string  `json:"amount"`
	Fee      string  `json:"fee"`
	Note     *string `json:"note"`
	ImportID *int64  `json:"import_id"`
}

// listTransactions returns the portfolio's transactions as the API reports
// them.
func listTransactions(e *transactionTestEnv, sessionID string, portfolioID int64) []importedTransaction {
	e.t.Helper()
	rec := e.do(sessionID, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID), nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("list transactions status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var txs []importedTransaction
	if err := json.NewDecoder(rec.Body).Decode(&txs); err != nil {
		e.t.Fatalf("decode transactions: %v", err)
	}
	return txs
}

// listImports returns the user's imports as the API reports them.
func listImports(e *transactionTestEnv, sessionID string) []map[string]any {
	e.t.Helper()
	rec := e.do(sessionID, http.MethodGet, "/api/imports", nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("list imports status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var imports []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&imports); err != nil {
		e.t.Fatalf("decode imports: %v", err)
	}
	return imports
}

// TestImportCommitCreatesTransactions checks that every
// committed row becomes a transaction carrying the new import's id and the
// generic period note, and the import is listed with that row count.
func TestImportCommitCreatesTransactions(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("importer@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	// The buy carries the 9 decimals a real Midas fractional-share row
	// prints.
	body := importBody(portfolioID, fakeSHA("ok"),
		map[string]any{"type": "buy", "date": "2025-09-26", "instrument_id": e.instrumentID, "quantity": "0.006218342", "price": "331.82", "fee": "0", "currency": "TRY"},
		map[string]any{"type": "dividend", "date": "2025-09-26", "instrument_id": e.instrumentID, "amount": "2.65", "fee": "0.53", "currency": "TRY"},
		map[string]any{"type": "deposit", "date": "2025-09-11", "amount": "8237.46", "fee": "0", "currency": "TRY"},
	)
	body["period_start"], body["period_end"] = "2025-09-01", "2025-09-30"
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var result struct {
		ID               int64  `json:"id"`
		TransactionCount int    `json:"transaction_count"`
		PeriodStart      string `json:"period_start"`
		PeriodEnd        string `json:"period_end"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ID == 0 || result.TransactionCount != 3 {
		t.Fatalf("result = %+v, want a non-zero id and 3 transactions", result)
	}
	if result.PeriodStart != "2025-09-01" || result.PeriodEnd != "2025-09-30" {
		t.Errorf("period = %s..%s, want 2025-09-01..2025-09-30", result.PeriodStart, result.PeriodEnd)
	}

	txs := listTransactions(e, session, portfolioID)
	if len(txs) != 3 {
		t.Fatalf("got %d transactions, want 3", len(txs))
	}
	for _, tx := range txs {
		if tx.ImportID == nil || *tx.ImportID != result.ID {
			t.Errorf("%s import_id = %v, want %d", tx.Type, tx.ImportID, result.ID)
		}
		if want := "Midas 2025-09-01 – 2025-09-30"; tx.Note == nil || *tx.Note != want {
			t.Errorf("%s note = %v, want %q", tx.Type, tx.Note, want)
		}
	}

	imports := listImports(e, session)
	if len(imports) != 1 {
		t.Fatalf("got %d imports, want 1", len(imports))
	}
	if imports[0]["transaction_count"] != float64(3) {
		t.Errorf("transaction_count = %v, want 3", imports[0]["transaction_count"])
	}
	if imports[0]["broker"] != "midas" {
		t.Errorf("broker = %v, want midas", imports[0]["broker"])
	}
}

// TestImportCommitRecordsEmptyStatement checks that a month whose every
// section is empty commits with no rows, is listed with 0 transactions so a
// re-upload of the period is caught, and can still be undone.
func TestImportCommitRecordsEmptyStatement(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("empty@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("empty"))
	body["rows"] = []map[string]any{}
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var result struct {
		ID               int64 `json:"id"`
		TransactionCount int   `json:"transaction_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ID == 0 || result.TransactionCount != 0 {
		t.Fatalf("result = %+v, want a non-zero id and 0 transactions", result)
	}

	imports := listImports(e, session)
	if len(imports) != 1 || imports[0]["transaction_count"] != float64(0) {
		t.Fatalf("imports = %v, want one import with transaction_count 0", imports)
	}

	path := fmt.Sprintf("/api/imports/%d", result.ID)
	if rec := e.do(session, http.MethodDelete, path, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports after the undo, want none", len(imports))
	}
}

// TestImportCommitRollsBackOnInvalidRow covers the all-or-nothing rule: the
// second row is rejected and neither the first row's transaction nor the
// import row survives.
func TestImportCommitRollsBackOnInvalidRow(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("rollback@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("bad"),
		map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"},
		map[string]any{"type": "buy", "date": "2025-12-24", "instrument_id": e.instrumentID, "price": "10", "fee": "0", "currency": "TRY"},
	)
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if want := "rows[1].quantity: "; len(errBody.Error) < len(want) || errBody.Error[:len(want)] != want {
		t.Errorf("error = %q, want it to start with %q", errBody.Error, want)
	}

	if txs := listTransactions(e, session, portfolioID); len(txs) != 0 {
		t.Errorf("got %d transactions, want none after a rolled-back import", len(txs))
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports, want none after a rolled-back import", len(imports))
	}
}

// TestImportCommitRejectsDuplicateFile covers idempotency: the same file
// imported twice into the same portfolio is a 409, not a second copy of the
// month's trades.
func TestImportCommitRejectsDuplicateFile(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("duplicate@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	row := map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"}
	body := importBody(portfolioID, fakeSHA("same"), row)

	if rec := e.do(session, http.MethodPost, "/api/imports", body); rec.Code != http.StatusCreated {
		t.Fatalf("first commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second commit status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errBody.Error != "already_imported" {
		t.Errorf("error = %q, want %q", errBody.Error, "already_imported")
	}

	if txs := listTransactions(e, session, portfolioID); len(txs) != 1 {
		t.Errorf("got %d transactions, want the 1 from the first import", len(txs))
	}
}

// TestImportRefusesFileOfAnotherPortfolio checks that a statement file
// belongs to one account, so a file already imported into one of the user's
// portfolios is refused by parse and commit in another, naming the portfolio
// that holds it. Another user's copy of the same file is theirs to import.
func TestImportRefusesFileOfAnotherPortfolio(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("two-portfolios@example.com")
	fundsID := e.createPortfolio(session, "Fonlar")
	brokerageID := e.createPortfolio(session, "Aracı")

	file := []byte("date,type,amount,fee,currency\n2025-01-02,deposit,100,0,TRY\n")
	commitBody := func(portfolioID int64, p preview) map[string]any {
		body := importBody(portfolioID, p.SHA256, map[string]any{"type": "deposit", "date": "2025-01-02", "amount": "100", "fee": "0", "currency": "TRY"})
		body["broker"], body["period_start"], body["period_end"] = "csv", p.PeriodStart, p.PeriodEnd
		return body
	}
	p := parsePreview(t, upload(e, session, "csv", fundsID, file))
	rec := e.do(session, http.MethodPost, "/api/imports", commitBody(fundsID, p))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var first struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&first); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	refused := func(what string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s status = %d, want %d; body: %s", what, rec.Code, http.StatusConflict, rec.Body.String())
		}
		var conflict struct {
			Error         string `json:"error"`
			ImportID      int64  `json:"import_id"`
			PortfolioID   int64  `json:"portfolio_id"`
			PortfolioName string `json:"portfolio_name"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
			t.Fatalf("decode conflict: %v", err)
		}
		if conflict.Error != "already_imported" || conflict.ImportID != first.ID ||
			conflict.PortfolioID != fundsID || conflict.PortfolioName != "Fonlar" {
			t.Errorf("%s conflict = %+v, want already_imported naming import %d in Fonlar (%d)", what, conflict, first.ID, fundsID)
		}
	}
	refused("parse", upload(e, session, "csv", brokerageID, file))
	refused("commit", e.do(session, http.MethodPost, "/api/imports", commitBody(brokerageID, p)))
	if txs := listTransactions(e, session, brokerageID); len(txs) != 0 {
		t.Errorf("got %d transactions in the second portfolio, want none", len(txs))
	}

	other := e.login("other-user@example.com")
	otherID := e.createPortfolio(other, "Fonlar")
	p = parsePreview(t, upload(e, other, "csv", otherID, file))
	if rec := e.do(other, http.MethodPost, "/api/imports", commitBody(otherID, p)); rec.Code != http.StatusCreated {
		t.Errorf("other user's commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

// TestImportCommitRefusesSameStatement checks that a month downloaded again is
// refused. Midas regenerates the PDF on every download, so it passes the hash
// check. The same broker, portfolio, currency and period is refused while the
// earlier import exists, naming it; the other currency's statement of the
// month is a different account and imports, and so does the month once undone.
// An import with no known currency matches either.
func TestImportCommitRefusesSameStatement(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("redownload@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	commit := func(sha, currency string, rows ...map[string]any) *httptest.ResponseRecorder {
		body := importBody(portfolioID, fakeSHA(sha), rows...)
		body["currency"] = currency
		return e.do(session, http.MethodPost, "/api/imports", body)
	}
	created := func(rec *httptest.ResponseRecorder) int64 {
		t.Helper()
		if rec.Code != http.StatusCreated {
			t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var result struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		return result.ID
	}
	refused := func(rec *httptest.ResponseRecorder, importID int64) {
		t.Helper()
		if rec.Code != http.StatusConflict {
			t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
		}
		var conflict struct {
			Error       string `json:"error"`
			ImportID    int64  `json:"import_id"`
			PeriodStart string `json:"period_start"`
			PeriodEnd   string `json:"period_end"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
			t.Fatalf("decode conflict: %v", err)
		}
		if conflict.Error != "already_imported" || conflict.ImportID != importID ||
			conflict.PeriodStart != importPeriodStart || conflict.PeriodEnd != importPeriodEnd {
			t.Errorf("conflict = %+v, want already_imported naming import %d", conflict, importID)
		}
	}
	try := map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"}
	usd := map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "5", "fee": "0", "currency": "USD"}

	first := created(commit("try", "TRY", try))
	refused(commit("try-again", "TRY", try), first)
	if txs := listTransactions(e, session, portfolioID); len(txs) != 1 {
		t.Fatalf("got %d transactions after the refused commit, want 1", len(txs))
	}

	created(commit("usd", "USD", usd))

	// The two statements of the month are told apart by their currency in
	// the list, newest first.
	imports := listImports(e, session)
	if len(imports) != 2 || imports[0]["currency"] != "USD" || imports[1]["currency"] != "TRY" {
		t.Fatalf("imports = %v, want the USD then the TRY statement, each with its currency", imports)
	}

	if rec := e.do(session, http.MethodDelete, fmt.Sprintf("/api/imports/%d", first), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	created(commit("try-again", "TRY", try))

	// Another portfolio, so only the currency-less import is in the way.
	other := e.createPortfolio(session, "Legacy")
	body := importBody(other, fakeSHA("unknown"))
	body["currency"] = ""
	legacy := created(e.do(session, http.MethodPost, "/api/imports", body))
	body = importBody(other, fakeSHA("known"))
	refused(e.do(session, http.MethodPost, "/api/imports", body), legacy)
}

// commitError posts body to the commit endpoint and returns the error of the
// 400 it must answer.
func commitError(e *transactionTestEnv, sessionID string, body map[string]any) string {
	e.t.Helper()
	rec := e.do(sessionID, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusBadRequest {
		e.t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		e.t.Fatalf("decode error: %v", err)
	}
	return errBody.Error
}

// TestImportCommitRejectsMalformedSHA256 checks that the duplicate check
// keys on the hash, so anything but the 64 lowercase hex characters parse
// returns is a 400.
func TestImportCommitRejectsMalformedSHA256(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("badsha@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	row := map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"}
	for _, sha := range []string{"abc", strings.ToUpper(fakeSHA("upper"))} {
		if got := commitError(e, session, importBody(portfolioID, sha, row)); !strings.HasPrefix(got, "sha256: ") {
			t.Errorf("sha256 %q: error = %q, want a sha256 field error", sha, got)
		}
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports, want none", len(imports))
	}
}

// TestImportCommitRejectsRowOutsidePeriod checks that a row dated outside
// the statement period is a 400 naming the row, and the whole import rolls
// back.
func TestImportCommitRejectsRowOutsidePeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("outside@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("outside"),
		map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"},
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "100", "fee": "0", "currency": "TRY"},
	)
	if got := commitError(e, session, body); !strings.HasPrefix(got, "rows[1].date: ") {
		t.Errorf("error = %q, want a rows[1].date field error", got)
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 0 {
		t.Errorf("got %d transactions, want none after a rejected import", len(txs))
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports, want none after a rejected import", len(imports))
	}
}

// TestImportCommitAcceptsInterestDayAfterPeriod checks that a row dated the
// day after period_end commits, because Midas books a month's interest on the
// 1st of the next month and prints it in the month it accrued; two days after
// is still refused.
func TestImportCommitAcceptsInterestDayAfterPeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("interest@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	late := importBody(portfolioID, fakeSHA("interest-late"),
		map[string]any{"type": "dividend", "date": "2026-01-02", "instrument_id": e.instrumentID, "amount": "12.34", "fee": "0", "currency": "TRY"},
	)
	if got := commitError(e, session, late); !strings.HasPrefix(got, "rows[0].date: must be within ") {
		t.Errorf("error = %q, want a rows[0].date period error", got)
	}

	body := importBody(portfolioID, fakeSHA("interest"),
		map[string]any{"type": "dividend", "date": "2026-01-01", "instrument_id": e.instrumentID, "amount": "12.34", "fee": "0", "currency": "TRY"},
	)
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 1 {
		t.Errorf("got %d transactions, want 1", len(txs))
	}
}

// TestImportCommitRejectsBogusPeriod checks that an empty statement with
// an out-of-range period is a 400, not an import that overlaps every later
// statement of the account.
func TestImportCommitRejectsBogusPeriod(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bogusperiod@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	for _, tc := range []struct{ start, end, field string }{
		{"0001-01-01", "9999-12-31", "period_start: "},
		{"1989-12-31", "1990-01-31", "period_start: "},
		{"2025-12-01", "9999-12-31", "period_end: "},
		{"2025-01-01", "2026-01-01", "period_end: "},
	} {
		body := importBody(portfolioID, fakeSHA(tc.start+tc.end))
		body["period_start"], body["period_end"] = tc.start, tc.end
		if got := commitError(e, session, body); !strings.HasPrefix(got, tc.field) {
			t.Errorf("period %s – %s: error = %q, want prefix %q", tc.start, tc.end, got, tc.field)
		}
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports, want none", len(imports))
	}
}

// TestImportDeleteIsUndoAndUserScoped covers the undo button: deleting an
// import removes its transactions too, and another user's delete is a 404
// that changes nothing.
func TestImportDeleteIsUndoAndUserScoped(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("owner@example.com")
	other := e.login("intruder@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	body := importBody(portfolioID, fakeSHA("undo"),
		map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "100", "fee": "0", "currency": "TRY"},
		map[string]any{"type": "withdrawal", "date": "2025-12-13", "amount": "20", "fee": "0", "currency": "TRY"},
	)
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	path := fmt.Sprintf("/api/imports/%d", result.ID)
	if rec := e.do(other, http.MethodDelete, path, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("other user delete status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 2 {
		t.Fatalf("got %d transactions after a rejected delete, want 2", len(txs))
	}

	if rec := e.do(session, http.MethodDelete, path, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 0 {
		t.Errorf("got %d transactions after the undo, want none", len(txs))
	}
	if imports := listImports(e, session); len(imports) != 0 {
		t.Errorf("got %d imports after the undo, want none", len(imports))
	}
}

// TestImportCommitAcceptsBodyOverGlobalLimit checks that the commit route is
// exempt from the global 64 KB body limit and caps itself at 2 MB, answering
// 413 -- not a generic 400 -- above that, because the reviewed rows of a real
// statement run past 64 KB.
func TestImportCommitAcceptsBodyOverGlobalLimit(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("bigimport@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	rows := make([]map[string]any, 0, 1000)
	for i := 0; i < 1000; i++ {
		rows = append(rows, map[string]any{"type": "deposit", "date": "2025-09-11", "amount": "8237.46", "fee": "0", "currency": "TRY"})
	}
	body := importBody(portfolioID, fakeSHA("big"), rows...)
	body["period_start"], body["period_end"] = "2025-09-01", "2025-09-30"
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if len(encoded) <= maxBodyBytes {
		t.Fatalf("body is %d bytes, want more than the %d byte global limit", len(encoded), maxBodyBytes)
	}

	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	oversized := json.RawMessage(`{"pad":"` + strings.Repeat("a", 2<<20) + `"}`)
	rec = e.do(session, http.MethodPost, "/api/imports", oversized)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized commit status = %d, want %d; body: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

// TestImportCommitRejectsTooManyInstrumentIDs checks that a hand-crafted
// commit naming more distinct instrument ids than sqlite allows variables
// (32766) is the 400 of its first unknown instrument, not a 500 "too many SQL
// variables".
func TestImportCommitRejectsTooManyInstrumentIDs(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("manyrows@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	rows := make([]map[string]any, 0, 32767)
	for i := range 32767 {
		rows = append(rows, map[string]any{"type": "buy", "date": "2025-12-12", "instrument_id": 100000 + i})
	}
	// Only the first row is otherwise valid, so it is the one that answers;
	// the rest stay short to fit the 2 MB body.
	for k, v := range map[string]any{"quantity": "1", "price": "1", "fee": "0", "currency": "TRY"} {
		rows[0][k] = v
	}
	if got := commitError(e, session, importBody(portfolioID, fakeSHA("many"), rows...)); got != "rows[0].instrument_id: unknown instrument" {
		t.Errorf("error = %q, want rows[0].instrument_id: unknown instrument", got)
	}
}

// TestImportCommitCSVFeeRows checks that a CSV's fee row and a deposit
// carrying a fee go from the preview to a fee transaction and a deposit with
// that fee, both part of the import and noted as a CSV one, and the undo
// removes both. The same file again is already imported. The file spans more
// than a year, which a CSV may and a broker statement may not.
func TestImportCommitCSVFeeRows(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("csv-commit@example.com")
	portfolioID := e.createPortfolio(session, "Import")

	file := []byte("date,type,amount,fee,currency\n" +
		"2025-01-02,deposit,100,1,TRY\n" +
		"2026-03-01,fee,2.5,,TRY\n")
	p := parsePreview(t, upload(e, session, "csv", portfolioID, file))
	if len(p.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", p.Warnings)
	}
	rows := []map[string]any{}
	for _, row := range p.Rows {
		rows = append(rows, map[string]any{"type": row.Type, "date": row.Date, "amount": row.Amount, "fee": row.Fee, "currency": row.Currency})
	}
	body := importBody(portfolioID, p.SHA256, rows...)
	body["broker"], body["period_start"], body["period_end"] = "csv", p.PeriodStart, p.PeriodEnd
	rec := e.do(session, http.MethodPost, "/api/imports", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	got := map[string]string{}
	for _, tx := range listTransactions(e, session, portfolioID) {
		if tx.ImportID == nil || *tx.ImportID != result.ID || tx.Note == nil || *tx.Note != "CSV 2025-01-02 – 2026-03-01" {
			t.Errorf("%s: import_id = %v, note = %v, want import %d noted as CSV", tx.Type, tx.ImportID, tx.Note, result.ID)
		}
		got[tx.Type] = tx.Amount + "/" + tx.Fee
	}
	if len(got) != 2 || got["deposit"] != "100/1" || got["fee"] != "2.5/0" {
		t.Errorf("transactions = %v, want a deposit 100 with fee 1 and a fee 2.5", got)
	}

	if rec := upload(e, session, "csv", portfolioID, file); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already_imported") {
		t.Errorf("same file again: status = %d, body %s; want 409 already_imported", rec.Code, rec.Body.String())
	}

	if rec := e.do(session, http.MethodDelete, fmt.Sprintf("/api/imports/%d", result.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if txs := listTransactions(e, session, portfolioID); len(txs) != 0 {
		t.Errorf("got %d transactions after the undo, want none", len(txs))
	}
}
