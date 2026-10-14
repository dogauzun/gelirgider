package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
)

// raceBody calls before once, at the first Read, then reads body. Every
// handler under test reads its body after its ownership check and before its
// insert, so before lands exactly where a concurrent delete commits in the
// reported race, without a thread.
type raceBody struct {
	before func()
	body   io.Reader
}

func (b *raceBody) Read(p []byte) (int, error) {
	if b.before != nil {
		b.before()
		b.before = nil
	}
	return b.body.Read(p)
}

// captureErrorLogs points the default logger, which WriteInternalError
// writes to, at a buffer for the rest of the test and returns a function
// that fails the test if any ERROR line was logged.
func captureErrorLogs(t *testing.T) func() {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() {
		t.Helper()
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry map[string]any
			if line == "" || json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			if entry["level"] == "ERROR" {
				t.Errorf("logged at ERROR: %s", line)
			}
		}
	}
}

// doRacing sends body as sessionID with before run between the handler's
// ownership check and its insert.
func (e *transactionTestEnv) doRacing(sessionID, method, path string, body any, before func()) *httptest.ResponseRecorder {
	e.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		e.t.Fatalf("marshal body: %v", err)
	}
	r := httptest.NewRequest(method, path, &raceBody{before: before, body: bytes.NewReader(b)})
	r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	return rec
}

func (e *transactionTestEnv) userID(sessionID string) int64 {
	e.t.Helper()
	id, err := e.sessions.GetSession(context.Background(), sessionID)
	if err != nil {
		e.t.Fatalf("GetSession: %v", err)
	}
	return id
}

func (e *transactionTestEnv) deletePortfolio(userID, portfolioID int64) {
	e.t.Helper()
	if _, err := portfolio.NewRepo(e.db).Delete(context.Background(), userID, portfolioID, true); err != nil {
		e.t.Fatalf("delete portfolio: %v", err)
	}
}

func (e *transactionTestEnv) countRows(table string) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
	want := fmt.Sprintf(`{"error":%q}`, msg)
	if body := strings.TrimSpace(rec.Body.String()); body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

// TestTransactionCreateRacingPortfolioDelete covers the case where the portfolio
// is deleted after the create's ownership check and before its insert. The
// insert's foreign key fails, and the answer is the 404 the same request
// gets a moment later, not a logged 500.
func TestTransactionCreateRacingPortfolioDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	userID := e.userID(session)
	portfolioID := e.createPortfolio(session, "Race")
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID),
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "1", "currency": "TRY"},
		func() { e.deletePortfolio(userID, portfolioID) })

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
	if n := e.countRows("transactions"); n != 0 {
		t.Errorf("%d transactions stored, want 0", n)
	}
}

// TestTransactionCreateRacingUserDelete: an admin deletes the owner, with
// every portfolio of theirs, between the check and the insert.
func TestTransactionCreateRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("gone@example.com")
	userID := e.userID(session)
	portfolioID := e.createPortfolio(session, "Race")
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID),
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "1", "currency": "TRY"},
		func() {
			if err := e.users.Delete(context.Background(), userID); err != nil {
				t.Fatalf("delete user: %v", err)
			}
		})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
}

// TestPortfolioCreateRacingUserDelete: the user is deleted after the session
// check and before the portfolio insert. The answer is the 401 the next
// request gets, not a logged 500.
func TestPortfolioCreateRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("gone@example.com")
	userID := e.userID(session)
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPost, "/api/portfolios", map[string]string{"name": "Race"},
		func() {
			if err := e.users.Delete(context.Background(), userID); err != nil {
				t.Fatalf("delete user: %v", err)
			}
		})

	checkLogs()
	wantError(t, rec, http.StatusUnauthorized, "unauthorized")
	if n := e.countRows("portfolios"); n != 0 {
		t.Errorf("%d portfolios stored, want 0", n)
	}
}

// TestDividendDismissRacingPortfolioDelete is the same race on the expected
// dividend dismiss.
func TestDividendDismissRacingPortfolioDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	userID := e.userID(session)
	portfolioID := e.createPortfolio(session, "Race")
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/dividends/expected/dismiss", portfolioID),
		map[string]any{"instrument_id": e.instrumentID, "date": "2025-05-10"},
		func() { e.deletePortfolio(userID, portfolioID) })

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
	if n := e.countRows("dividend_dismissals"); n != 0 {
		t.Errorf("%d dismissals stored, want 0", n)
	}
}

// TestImportCommitRacingPortfolioDelete is the same race on the import
// commit, whose body is read before its ownership check, so the delete runs
// from a trigger on the import insert instead. The commit answers 404 and
// leaves neither the import nor any of its rows behind.
func TestImportCommitRacingPortfolioDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	portfolioID := e.createPortfolio(session, "Race")
	if _, err := e.db.Exec(`CREATE TRIGGER race BEFORE INSERT ON statement_imports
		BEGIN DELETE FROM portfolios WHERE id = NEW.portfolio_id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPost, "/api/imports", importBody(portfolioID, fakeSHA("race"),
		map[string]any{"type": "deposit", "date": "2025-12-12", "amount": "10", "fee": "0", "currency": "TRY"},
	))

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
	for _, table := range []string{"statement_imports", "transactions", "import_aliases", "import_forgotten_names"} {
		if n := e.countRows(table); n != 0 {
			t.Errorf("%d rows in %s, want 0", n, table)
		}
	}
}

// TestManualPriceSaveRacingUserDelete covers the case where an admin deletes the
// user after the session check and before the manual price insert. The
// answer is the 401 the next request gets, not a logged 500.
func TestManualPriceSaveRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("gone@example.com")
	userID := e.userID(session)
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPut, fmt.Sprintf("/api/instruments/%d/prices/2026-01-02", e.instrumentID),
		map[string]string{"close": "10.5"},
		func() {
			if err := e.users.Delete(context.Background(), userID); err != nil {
				t.Fatalf("delete user: %v", err)
			}
		})

	checkLogs()
	wantError(t, rec, http.StatusUnauthorized, "unauthorized")
	if n := e.countRows("manual_prices"); n != 0 {
		t.Errorf("%d manual prices stored, want 0", n)
	}
}

// TestLoginRacingUserDelete: the user is deleted after the password check
// and before the session insert, by a trigger on that insert, since login
// reads its body before the check. The answer is the one an unknown account
// gets, not a logged 500.
func TestLoginRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	admin, err := e.users.Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := e.users.SetAdmin(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("set admin: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if _, err := e.users.Create(context.Background(), "gone@example.com", string(hash)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := e.db.Exec(`CREATE TRIGGER race BEFORE INSERT ON sessions
		BEGIN DELETE FROM users WHERE id = NEW.user_id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do("", http.MethodPost, "/api/auth/login",
		map[string]string{"email": "gone@example.com", "password": "password123"})

	checkLogs()
	wantError(t, rec, http.StatusUnauthorized, "invalid email or password")
	if n := e.countRows("sessions"); n != 0 {
		t.Errorf("%d sessions stored, want 0", n)
	}
}

// TestTransactionCreateRacingForcedPortfolioDelete: a forced delete of the
// portfolio commits between the transaction's insert and its re-read, so the
// re-read finds nothing. A trigger on the insert does that delete. The
// answer is the 404 of TestTransactionCreateRacingPortfolioDelete, not a
// logged 500.
func TestTransactionCreateRacingForcedPortfolioDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	portfolioID := e.createPortfolio(session, "Race")
	if _, err := e.db.Exec(`CREATE TRIGGER race AFTER INSERT ON transactions
		BEGIN
			DELETE FROM transactions WHERE portfolio_id = NEW.portfolio_id;
			DELETE FROM portfolios WHERE id = NEW.portfolio_id;
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/transactions", portfolioID),
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "1", "currency": "TRY"})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
	if n := e.countRows("transactions"); n != 0 {
		t.Errorf("%d transactions stored, want 0", n)
	}
}

// TestTransactionUpdateRacingForcedPortfolioDelete covers the case where a forced
// delete of the portfolio, or of its user, commits between the transaction's
// update and its re-read, so the re-read finds nothing. A trigger on the
// update does that delete. The answer is the 404 a missing transaction gets,
// not a logged 500.
func TestTransactionUpdateRacingForcedPortfolioDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	portfolioID := e.createPortfolio(session, "Race")
	txID := e.createTransaction(session, portfolioID,
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "1", "currency": "TRY"})
	if _, err := e.db.Exec(`CREATE TRIGGER race AFTER UPDATE ON transactions
		BEGIN
			DELETE FROM transactions WHERE portfolio_id = NEW.portfolio_id;
			DELETE FROM portfolios WHERE id = NEW.portfolio_id;
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPut, fmt.Sprintf("/api/transactions/%d", txID),
		map[string]any{"type": "deposit", "date": "2026-01-03", "amount": "2", "currency": "TRY"})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "transaction not found")
	if n := e.countRows("transactions"); n != 0 {
		t.Errorf("%d transactions stored, want 0", n)
	}
}

// TestReportingCurrencyRacingUserDelete: an admin deletes the user between
// the reporting currency update and its re-read, done by a trigger on the
// update. The answer is the 401 the next request gets, not a logged 500.
func TestReportingCurrencyRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("gone@example.com")
	if _, err := e.db.Exec(`CREATE TRIGGER race AFTER UPDATE ON users
		BEGIN
			DELETE FROM sessions WHERE user_id = NEW.id;
			DELETE FROM users WHERE id = NEW.id;
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPut, "/api/me/reporting-currency", map[string]string{"reporting_currency": "USD"})

	checkLogs()
	wantError(t, rec, http.StatusUnauthorized, "unauthorized")
}

// deleteUserRow deletes the users row of userID alone, keeping its session,
// portfolios and transactions. That is what a handler sees when an admin's
// delete of the user commits after the session check and every ownership
// check the handler makes, and before its own lookup of the user: no hook
// sits between those reads, so the test sets that state up front. Foreign
// keys are off on one connection for this delete only.
func (e *transactionTestEnv) deleteUserRow(userID int64) {
	e.t.Helper()
	ctx := context.Background()
	conn, err := e.db.Conn(ctx)
	if err != nil {
		e.t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		e.t.Fatalf("foreign keys off: %v", err)
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	if _, err := conn.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID); err != nil {
		e.t.Fatalf("delete user row: %v", err)
	}
}

// TestSignedInUserLookupRacingUserDelete checks that a request that
// looks the signed-in user up by id after the session check, when an admin
// deleted the user in between, answers the 401 the user's next request
// gets, not a logged 500.
func TestSignedInUserLookupRacingUserDelete(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"me", http.MethodGet, "/api/me", nil},
		{"password change", http.MethodPost, "/api/account/password",
			map[string]string{"current_password": "password123", "new_password": "password456"}},
		{"portfolio list", http.MethodGet, "/api/portfolios", nil},
		{"summary", http.MethodGet, "/api/portfolios/%d/summary", nil},
		{"summary of all", http.MethodGet, "/api/portfolios/all/summary", nil},
		{"dividend timeline", http.MethodGet, "/api/portfolios/%d/dividends/timeline", nil},
		{"history", http.MethodGet, "/api/portfolios/%d/history", nil},
		{"returns periods", http.MethodGet, "/api/portfolios/%d/returns/periods", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTransactionTestEnv(t)
			session := e.login("gone@example.com")
			portfolioID := e.createPortfolio(session, "Race")
			path := tc.path
			if strings.Contains(path, "%d") {
				path = fmt.Sprintf(path, portfolioID)
			}
			e.deleteUserRow(e.userID(session))
			checkLogs := captureErrorLogs(t)

			rec := e.do(session, tc.method, path, tc.body)

			checkLogs()
			wantError(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// TestReturnsPeriodsSweepRacingUserDelete: with a transaction in the
// portfolio, returns periods looks the user up inside the history sweep
// instead, after its price and instrument reads.
func TestReturnsPeriodsSweepRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("gone@example.com")
	portfolioID := e.createPortfolio(session, "Race")
	e.createTransaction(session, portfolioID,
		map[string]any{"type": "deposit", "date": "2026-01-02", "amount": "1", "currency": "TRY"})
	e.deleteUserRow(e.userID(session))
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/returns/periods", portfolioID), nil)

	checkLogs()
	wantError(t, rec, http.StatusUnauthorized, "unauthorized")
}

// TestDividendDismissRacingPortfolioDeleteAfterUniqueFailure covers the case
// where the dismissal is already there, so the insert fails on the primary key,
// and a forced portfolio delete commits before Dismiss counts the rows. A
// trigger on the insert does that delete and then fails the insert with the
// duplicate's message; RAISE(FAIL) keeps the delete. The answer is the 404 of
// TestDividendDismissRacingPortfolioDelete.
func TestDividendDismissRacingPortfolioDeleteAfterUniqueFailure(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("race@example.com")
	portfolioID := e.createPortfolio(session, "Race")
	path := fmt.Sprintf("/api/portfolios/%d/dividends/expected/dismiss", portfolioID)
	body := map[string]any{"instrument_id": e.instrumentID, "date": "2025-05-10"}
	if rec := e.do(session, http.MethodPost, path, body); rec.Code != http.StatusNoContent {
		t.Fatalf("first dismiss: status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := e.db.Exec(`CREATE TRIGGER race BEFORE INSERT ON dividend_dismissals
		BEGIN
			DELETE FROM dividend_dismissals WHERE portfolio_id = NEW.portfolio_id;
			DELETE FROM portfolios WHERE id = NEW.portfolio_id;
			SELECT RAISE(FAIL, 'UNIQUE constraint failed: dividend_dismissals.portfolio_id, dividend_dismissals.instrument_id, dividend_dismissals.date');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPost, path, body)

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "portfolio not found")
	if n := e.countRows("portfolios"); n != 0 {
		t.Errorf("%d portfolios left, want 0", n)
	}
}

// loginAdmin is login for an admin.
func (e *transactionTestEnv) loginAdmin(email string) string {
	e.t.Helper()
	session := e.login(email)
	if err := e.users.SetAdmin(context.Background(), e.userID(session), true); err != nil {
		e.t.Fatalf("set admin: %v", err)
	}
	return session
}

// TestAdminSetPasswordRacingUserDelete: a second admin deletes the target
// user after the first admin's request loaded it and before the password
// reset looks it up again. That is another user, so the answer is the
// route's 404, not the 401 given for the signed-in user.
func TestAdminSetPasswordRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.loginAdmin("admin@example.com")
	target, err := e.users.Create(context.Background(), "gone@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.doRacing(session, http.MethodPost, fmt.Sprintf("/api/admin/users/%d/password", target.ID),
		map[string]string{"password": "password456"},
		func() {
			if err := e.users.Delete(context.Background(), target.ID); err != nil {
				t.Fatalf("delete user: %v", err)
			}
		})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "user not found")
}

// TestAdminCreateUserRacingUserDelete: the new user is deleted before its
// default portfolio is inserted, by a trigger on that insert. The answer is
// the 404 the other admin routes give for a user deleted under them, not the
// signed-in admin's 401 and not a logged 500.
func TestAdminCreateUserRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.loginAdmin("admin@example.com")
	if _, err := e.db.Exec(`CREATE TRIGGER race BEFORE INSERT ON portfolios
		BEGIN DELETE FROM users WHERE id = NEW.user_id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do(session, http.MethodPost, "/api/admin/users",
		map[string]string{"email": "new@example.com", "password": "password123"})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "user not found")
}

// An admin deletes a user who just signed up, before its default portfolio
// is seeded: the public sign-up answers 404 like the admin's create-user
// route above, not 500.
func TestRegisterRacingUserDelete(t *testing.T) {
	e := newTransactionTestEnv(t)
	e.loginAdmin("admin@example.com") // setup done
	openRegistration(t, e.db)
	if _, err := e.db.Exec(`CREATE TRIGGER race BEFORE INSERT ON portfolios
		BEGIN DELETE FROM users WHERE id = NEW.user_id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	checkLogs := captureErrorLogs(t)

	rec := e.do("", http.MethodPost, "/api/auth/register",
		map[string]string{"email": "new@example.com", "password": "password123"})

	checkLogs()
	wantError(t, rec, http.StatusNotFound, "user not found")
}
