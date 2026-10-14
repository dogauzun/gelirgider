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
	"path/filepath"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestPortfolioOwnership covers that a user cannot see, rename, or delete
// another user's portfolio: each of two users creates their own portfolio,
// then user A's list must not contain B's portfolio, and PATCH/DELETE by A
// on B's portfolio id must 404.
func TestPortfolioOwnership(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)

	userA, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create A: %v", err)
	}
	userB, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create B: %v", err)
	}
	sessionA, err := sessions.CreateSession(context.Background(), userA.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession A: %v", err)
	}
	sessionB, err := sessions.CreateSession(context.Background(), userB.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession B: %v", err)
	}

	do := func(sessionID, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reqBody io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reqBody = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, reqBody)
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	createRec := do(sessionB.ID, http.MethodPost, "/api/portfolios", map[string]string{"name": "Bob's portfolio"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create B status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if rec := do(sessionA.ID, http.MethodPost, "/api/portfolios", map[string]string{"name": "Alice's portfolio"}); rec.Code != http.StatusCreated {
		t.Fatalf("create A status = %d, want %d", rec.Code, http.StatusCreated)
	}

	// A's list must not include B's portfolio.
	listRec := do(sessionA.ID, http.MethodGet, "/api/portfolios", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list A status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var list []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	for _, p := range list {
		if p.ID == created.ID {
			t.Fatalf("A's portfolio list contains B's portfolio %d", created.ID)
		}
	}

	path := fmt.Sprintf("/api/portfolios/%d", created.ID)

	if rec := do(sessionA.ID, http.MethodPatch, path, map[string]string{"name": "hijacked"}); rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH other user's portfolio status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := do(sessionA.ID, http.MethodDelete, path, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE other user's portfolio status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestPortfolioDeleteWithTransactions covers that deleting a portfolio that
// still has transactions is refused with 409 and the portfolio stays.
func TestPortfolioDeleteWithTransactions(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reqBody io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reqBody = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, reqBody)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	createRec := do(http.MethodPost, "/api/portfolios", map[string]string{"name": "Test"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create portfolio status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var portfolio struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&portfolio); err != nil {
		t.Fatalf("decode portfolio: %v", err)
	}
	path := fmt.Sprintf("/api/portfolios/%d", portfolio.ID)

	if rec := do(http.MethodPost, path+"/transactions", map[string]any{"type": "deposit", "date": "2024-01-01", "amount": "100"}); rec.Code != http.StatusCreated {
		t.Fatalf("create transaction status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec := do(http.MethodDelete, path, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("DELETE status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Error            string `json:"error"`
		TransactionCount int    `json:"transaction_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error != "portfolio has transactions" {
		t.Fatalf("error = %q, want %q", body.Error, "portfolio has transactions")
	}
	// The refusal names how many transactions stand in the way, so
	// the UI can say it in the cascade confirmation.
	if body.TransactionCount != 1 {
		t.Fatalf("transaction_count = %d, want 1", body.TransactionCount)
	}

	listRec := do(http.MethodGet, "/api/portfolios", nil)
	var list []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != portfolio.ID {
		t.Fatalf("portfolio list after refused delete = %+v, want the original portfolio", list)
	}
}

// TestPortfolioListIncludesTotalValue covers that GET /api/portfolios
// reports each portfolio's total value and reporting currency, computed
// from its transactions the same way the summary endpoint does.
func TestPortfolioListIncludesTotalValue(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reqBody io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reqBody = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, reqBody)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	createRec := do(http.MethodPost, "/api/portfolios", map[string]string{"name": "Test"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create portfolio status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode portfolio: %v", err)
	}
	path := fmt.Sprintf("/api/portfolios/%d", created.ID)

	if rec := do(http.MethodPost, path+"/transactions", map[string]any{"type": "deposit", "date": "2024-01-01", "amount": "100"}); rec.Code != http.StatusCreated {
		t.Fatalf("create transaction status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	listRec := do(http.MethodGet, "/api/portfolios", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var list []struct {
		ID         int64  `json:"id"`
		TotalValue string `json:"total_value"`
		Currency   string `json:"currency"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	if list[0].TotalValue != "100.00" {
		t.Fatalf("total_value = %q, want %q", list[0].TotalValue, "100.00")
	}
	if list[0].Currency != "TRY" {
		t.Fatalf("currency = %q, want %q", list[0].Currency, "TRY")
	}
}

// TestPortfolioNameTrimmed covers that a whitespace-only name is rejected on
// create and rename, and surrounding whitespace is stripped before storing.
func TestPortfolioNameTrimmed(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	do := func(method, path, name string) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"name": name})
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	if rec := do(http.MethodPost, "/api/portfolios", "   "); rec.Code != http.StatusBadRequest {
		t.Fatalf("create whitespace name status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	createRec := do(http.MethodPost, "/api/portfolios", "  Test  ")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var portfolio struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&portfolio); err != nil {
		t.Fatalf("decode portfolio: %v", err)
	}
	if portfolio.Name != "Test" {
		t.Fatalf("created name = %q, want %q", portfolio.Name, "Test")
	}

	path := fmt.Sprintf("/api/portfolios/%d", portfolio.ID)
	if rec := do(http.MethodPatch, path, "\t\n"); rec.Code != http.StatusBadRequest {
		t.Fatalf("rename whitespace name status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestPortfolioDeleteCascade checks that DELETE with ?force=true removes
// the portfolio together with its transactions, and only ever its owner's --
// another user's id is a 404 that leaves every row in place.
func TestPortfolioDeleteCascade(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)

	userA, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create A: %v", err)
	}
	userB, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create B: %v", err)
	}
	sessionA, err := sessions.CreateSession(context.Background(), userA.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession A: %v", err)
	}
	sessionB, err := sessions.CreateSession(context.Background(), userB.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession B: %v", err)
	}

	do := func(sessionID, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reqBody io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reqBody = bytes.NewReader(b)
		}
		r := httptest.NewRequest(method, path, reqBody)
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	// Creates a portfolio with n deposits and returns its path.
	setUp := func(sessionID, name string, n int) string {
		t.Helper()
		rec := do(sessionID, http.MethodPost, "/api/portfolios", map[string]string{"name": name})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create portfolio %s status = %d, want %d", name, rec.Code, http.StatusCreated)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("decode portfolio %s: %v", name, err)
		}
		path := fmt.Sprintf("/api/portfolios/%d", created.ID)
		for i := 0; i < n; i++ {
			tx := map[string]any{"type": "deposit", "date": "2024-01-01", "amount": "100"}
			if rec := do(sessionID, http.MethodPost, path+"/transactions", tx); rec.Code != http.StatusCreated {
				t.Fatalf("create transaction status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
			}
		}
		return path
	}

	countTransactions := func(userID int64) int {
		t.Helper()
		var n int
		if err := conn.QueryRow("SELECT COUNT(*) FROM transactions WHERE user_id = ?", userID).Scan(&n); err != nil {
			t.Fatalf("count transactions: %v", err)
		}
		return n
	}

	pathA := setUp(sessionA.ID, "Alice", 3)
	pathB := setUp(sessionB.ID, "Bob", 2)

	// A cascading delete aimed at another user's portfolio must touch nothing.
	if rec := do(sessionA.ID, http.MethodDelete, pathB+"?force=true", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("force DELETE other user's portfolio status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if n := countTransactions(userB.ID); n != 2 {
		t.Fatalf("B's transactions after A's force delete = %d, want 2", n)
	}

	// Without the flag the refusal stands, even now that a cascade exists.
	if rec := do(sessionA.ID, http.MethodDelete, pathA, nil); rec.Code != http.StatusConflict {
		t.Fatalf("DELETE without force status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if n := countTransactions(userA.ID); n != 3 {
		t.Fatalf("A's transactions after refused delete = %d, want 3", n)
	}

	if rec := do(sessionA.ID, http.MethodDelete, pathA+"?force=true", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("force DELETE own portfolio status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if n := countTransactions(userA.ID); n != 0 {
		t.Fatalf("A's transactions after cascade = %d, want 0", n)
	}
	if n := countTransactions(userB.ID); n != 2 {
		t.Fatalf("B's transactions after A's cascade = %d, want 2", n)
	}

	listRec := do(sessionA.ID, http.MethodGet, "/api/portfolios", nil)
	var list []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("A's portfolio list after cascade = %+v, want empty", list)
	}
	if rec := do(sessionB.ID, http.MethodGet, "/api/portfolios", nil); rec.Code != http.StatusOK {
		t.Fatalf("B's list status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestPortfolioNameDuplicateAndControlChars checks that a name another
// portfolio of the same user already has is refused with 409 on create and
// on rename, a name holding a control character is refused with 400, and
// renaming a portfolio to the name it already has still succeeds.
func TestPortfolioNameDuplicateAndControlChars(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)

	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	other, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create other: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	otherSession, err := sessions.CreateSession(context.Background(), other.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession other: %v", err)
	}

	do := func(sessionID, method, path, name string) *httptest.ResponseRecorder {
		t.Helper()
		b, err := json.Marshal(map[string]string{"name": name})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	if rec := do(session.ID, http.MethodPost, "/api/portfolios", "Portföy"); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", rec.Code, http.StatusCreated)
	}

	rec := do(session.ID, http.MethodPost, "/api/portfolios", "Portföy")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error != "name already used" {
		t.Fatalf("error = %q, want %q", body.Error, "name already used")
	}

	// A name is only taken for the user who holds it.
	if rec := do(otherSession.ID, http.MethodPost, "/api/portfolios", "Portföy"); rec.Code != http.StatusCreated {
		t.Fatalf("other user's create status = %d, want %d", rec.Code, http.StatusCreated)
	}

	if rec := do(session.ID, http.MethodPost, "/api/portfolios", "a\nb\ac"); rec.Code != http.StatusBadRequest {
		t.Fatalf("control character create status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	secondRec := do(session.ID, http.MethodPost, "/api/portfolios", "Emeklilik")
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("second create status = %d, want %d", secondRec.Code, http.StatusCreated)
	}
	var second struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&second); err != nil {
		t.Fatalf("decode second portfolio: %v", err)
	}
	path := fmt.Sprintf("/api/portfolios/%d", second.ID)

	if rec := do(session.ID, http.MethodPatch, path, "Portföy"); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate rename status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if rec := do(session.ID, http.MethodPatch, path, "Emek\nlilik"); rec.Code != http.StatusBadRequest {
		t.Fatalf("control character rename status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	// Renaming a portfolio to the name it already has excludes itself.
	if rec := do(session.ID, http.MethodPatch, path, "Emeklilik"); rec.Code != http.StatusOK {
		t.Fatalf("unchanged rename status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	listRec := do(session.ID, http.MethodGet, "/api/portfolios", "")
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "Portföy" || list[1].Name != "Emeklilik" {
		t.Fatalf("portfolio list = %+v, want Portföy and Emeklilik only", list)
	}
}
