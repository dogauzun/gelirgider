package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// TestRegisterSameEmailRace: concurrent sign-ups with one email all try the
// insert; the email unique constraint lets one win, and the others answer
// 409 like a known duplicate, not 500 (GLG-369).
func TestRegisterSameEmailRace(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	users := user.NewRepo(conn)
	admin, err := users.Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.SetAdmin(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	h := &handler{users: users, sessions: NewRepo(conn), settings: settings.NewRepo(conn, nil)}
	if err := h.settings.Set(context.Background(), registrationOpenKey, "true"); err != nil {
		t.Fatalf("open registration: %v", err)
	}

	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"ada@example.com","password":"password123"}`))
			rec := httptest.NewRecorder()
			h.register(rec, req)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()

	sort.Ints(codes)
	want := []int{http.StatusCreated, http.StatusConflict, http.StatusConflict, http.StatusConflict}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("status codes = %v, want %v", codes, want)
		}
	}
}

// TestValidateEmailRejectsBlankLookingCharacters guards GLG-978: a Hangul or
// Braille blank filler or a line or paragraph separator renders as a gap or a
// line break, so the address would pass for another one in the admin user
// list, like the zero-width characters of GLG-900. Ordinary and IDN
// addresses still pass.
func TestValidateEmailRejectsBlankLookingCharacters(t *testing.T) {
	for _, email := range []string{
		"eㅤ@example.com",
		"f⠀@example.com",
		"g h@example.com",
		"g h@example.com",
		"hᅟ@example.com",
		"hᅠ@example.com",
		"hﾠ@example.com",
		"c​d@example.com",
		// Names let these through as one visible emoji (GLG-901, GLG-907);
		// in an address the joiner and the tags are still hidden characters.
		"a\U0001F355‍\U0001F355@example.com",
		"a\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F@example.com",
	} {
		if got := validateEmail(email); got != "invalid email" {
			t.Errorf("validateEmail(%q) = %q, want %q", email, got, "invalid email")
		}
	}
	for _, email := range []string{"alice@example.com", "ali.veli+x@example.com.tr", "user@bücher.example", "çağla@örnek.com.tr"} {
		if got := validateEmail(email); got != "" {
			t.Errorf("validateEmail(%q) = %q, want accepted", email, got)
		}
	}
}

// TestRegisterRejectsInvisibleEmailCharacters guards GLG-1010: a space
// separator (U+00A0, U+2003, U+3000) or a default-ignorable mark (U+034F,
// U+17B4, U+17B5, variation selectors) makes the address render like
// another one, so registration answers 400 and creates no user. Accented
// and IDN addresses, and a visible combining accent, still register.
func TestRegisterRejectsInvisibleEmailCharacters(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	users := user.NewRepo(conn)
	admin, err := users.Create(context.Background(), "admin@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	if err := users.SetAdmin(context.Background(), admin.ID, true); err != nil {
		t.Fatalf("SetAdmin: %v", err)
	}
	h := &handler{users: users, sessions: NewRepo(conn), settings: settings.NewRepo(conn, nil)}
	if err := h.settings.Set(context.Background(), registrationOpenKey, "true"); err != nil {
		t.Fatalf("open registration: %v", err)
	}
	register := func(email string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"email": email, "password": "Passw0rd!x"})
		rec := httptest.NewRecorder()
		h.register(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body)))
		return rec
	}

	for _, email := range []string{
		"a b@example.com",
		"a b@example.com",
		"a　b@example.com",
		"admin͏@example.com",
		"a឴@example.com",
		"a឵@example.com",
		"a️@example.com",
		"a\U000E0100@example.com",
	} {
		rec := register(email)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid email") {
			t.Errorf("register(%q) = %d %s, want 400 invalid email", email, rec.Code, rec.Body)
		}
		if _, err := users.GetByEmail(context.Background(), email); err == nil {
			t.Errorf("register(%q) created a user", email)
		}
	}
	for _, email := range []string{"kullanıcı@örnek.com", "çağrı@example.com", "é@example.com"} {
		if rec := register(email); rec.Code != http.StatusCreated {
			t.Errorf("register(%q) = %d %s, want 201", email, rec.Code, rec.Body)
		}
	}
}
