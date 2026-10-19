package portfolio

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
)

// TestValidNameCountsRunes checks that the limit is 100 characters, not
// 100 bytes, so 100 two-byte Turkish letters must still be accepted.
func TestValidNameCountsRunes(t *testing.T) {
	if !validName(strings.Repeat("ş", maxNameLen)) {
		t.Errorf("%d multibyte runes: want valid", maxNameLen)
	}
	if validName(strings.Repeat("ş", maxNameLen+1)) {
		t.Errorf("%d multibyte runes: want invalid", maxNameLen+1)
	}
	if validName("") {
		t.Error("empty name: want invalid")
	}
}

// TestCheckNameRejectsFormatChars checks that checkName answers 400 for a
// name that would mislead or look blank: a zero-width space renders as nothing
// and an RTL override reverses the text after it; Hangul fillers, the Braille
// blank and bare combining marks render as a blank name; the line and
// paragraph separators are line breaks like newline, not whitespace to
// collapse.
func TestCheckNameRejectsFormatChars(t *testing.T) {
	for _, name := range []string{
		"\u200b", "Emeklilik\u202e",
		"\u3164", "\u115f", "\u1160", "\uffa0", "\u2800", "Döviz\u3164",
		"\u0301\u0301", "\u0301 \u20dd",
		"x\ny", "x\u2028y", "x\u2029y",
	} {
		w := httptest.NewRecorder()
		if _, ok := checkName(w, name); ok || w.Code != http.StatusBadRequest {
			t.Errorf("%q: ok=%v code=%d, want 400", name, ok, w.Code)
		}
	}
}

// TestCheckNameAcceptsMarksOnLetters checks that the blank-name check does not
// go too far: a combining mark on a base letter (NFD "Döviz", Devanagari) is
// visible.
func TestCheckNameAcceptsMarksOnLetters(t *testing.T) {
	for _, name := range []string{"Do\u0308viz", "हिंदी", "-"} {
		w := httptest.NewRecorder()
		if _, ok := checkName(w, name); !ok {
			t.Errorf("%q: rejected with %d, want accepted", name, w.Code)
		}
	}
}

// TestCheckNameLimitsStoredLength checks that the 100-character limit
// applies to the name after inner whitespace runs collapse, not before.
func TestCheckNameLimitsStoredLength(t *testing.T) {
	w := httptest.NewRecorder()
	got, ok := checkName(w, "a"+strings.Repeat(" ", 120)+"b")
	if !ok || got != "a b" {
		t.Errorf("got %q ok=%v code=%d, want \"a b\" accepted", got, ok, w.Code)
	}
}

// TestOwnedRejectsForeignPortfolio checks that Owned answers 404 for
// another user's portfolio id, as the handler preambles it replaced did, and
// hands the caller's own portfolio through.
func TestOwnedRejectsForeignPortfolio(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES
		 (1, 'a@example.com', 'x', '2026-01-01T00:00:00Z'),
		 (2, 'b@example.com', 'x', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := NewRepo(conn)
	ctx := context.Background()
	own, err := repo.Create(ctx, 1, "Mine")
	if err != nil {
		t.Fatalf("create own: %v", err)
	}
	foreign, err := repo.Create(ctx, 2, "Theirs")
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}
	sessions := auth.NewRepo(conn)
	s, err := sessions.CreateSession(ctx, 1)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, id, ok := Owned(w, r, repo); ok {
			fmt.Fprint(w, id)
		}
	})
	h := sessions.RequireUser(mux)

	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{fmt.Sprintf("/p/%d", own.ID), http.StatusOK, fmt.Sprint(own.ID)},
		{fmt.Sprintf("/p/%d", foreign.ID), http.StatusNotFound, `{"error":"portfolio not found"}`},
		{"/p/x", http.StatusNotFound, `{"error":"portfolio not found"}`},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: s.ID})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%s: %d %q, want %d %q", tc.path, w.Code, w.Body.String(), tc.code, tc.body)
		}
	}

	// A failing lookup is a 500, not a 404 that tells the user the portfolio
	// is gone. Dropping the table, not closing conn, keeps auth working.
	if _, err := conn.Exec(`DROP TABLE portfolios`); err != nil {
		t.Fatalf("drop portfolios: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/p/%d", own.ID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: s.ID})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError || strings.TrimSpace(w.Body.String()) != `{"error":"internal error"}` {
		t.Errorf("database error: %d %q, want 500 internal error", w.Code, w.Body.String())
	}
}
