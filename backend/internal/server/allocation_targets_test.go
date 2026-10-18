package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func targetsBody(pairs ...string) allocationTargetsBody {
	b := allocationTargetsBody{Targets: []allocationTarget{}}
	for i := 0; i < len(pairs); i += 2 {
		b.Targets = append(b.Targets, allocationTarget{AssetClass: pairs[i], Weight: pairs[i+1]})
	}
	return b
}

func targetsPath(id any) string { return fmt.Sprintf("/api/portfolios/%v/allocation-targets", id) }

// TestAllocationTargetsRoundTrip checks that a put is answered and read back
// in class order, that a second put replaces the first and that an empty put
// clears the set.
func TestAllocationTargetsRoundTrip(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := targetsPath(e.createPortfolio(session, "P"))

	rec := e.do(session, http.MethodGet, path, nil)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"targets":[]}` {
		t.Fatalf("empty get = %d %s, want 200 with no targets", rec.Code, got)
	}

	want := targetsBody("equity", "40", "fund", "30.5", "cash", "29.5")
	rec = e.do(session, http.MethodPut, path, targetsBody("cash", "29.5", "equity", "40", "fund", "30.5"))
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s, want 200", rec.Code, rec.Body.String())
	}
	for _, got := range []allocationTargetsBody{
		decodeJSON[allocationTargetsBody](t, rec.Body.Bytes()),
		decodeJSON[allocationTargetsBody](t, e.do(session, http.MethodGet, path, nil).Body.Bytes()),
	} {
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("targets = %v, want %v", got, want)
		}
	}

	if rec := e.do(session, http.MethodPut, path, targetsBody("crypto", "100")); rec.Code != http.StatusOK {
		t.Fatalf("second put = %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(session, http.MethodGet, path, nil)
	if got := fmt.Sprint(decodeJSON[allocationTargetsBody](t, rec.Body.Bytes())); got != fmt.Sprint(targetsBody("crypto", "100")) {
		t.Errorf("after replace = %s, want crypto 100 only", got)
	}

	rec = e.do(session, http.MethodPut, path, targetsBody())
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"targets":[]}` {
		t.Errorf("empty put = %d %s, want 200 with no targets", rec.Code, got)
	}
	rec = e.do(session, http.MethodGet, path, nil)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"targets":[]}` {
		t.Errorf("get after clear = %s, want no targets", got)
	}
}

// TestAllocationTargetsValidation checks each refusal and that it stores
// nothing.
func TestAllocationTargetsValidation(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := targetsPath(e.createPortfolio(session, "P"))
	tests := []struct {
		name string
		body allocationTargetsBody
		want string
	}{
		{"unknown class", targetsBody("bist", "100"), "asset_class: unknown class"},
		{"duplicate", targetsBody("equity", "50", "equity", "50"), "asset_class: duplicate"},
		{"above 100", targetsBody("equity", "100.5"), "weight: must be between 0 and 100"},
		{"negative", targetsBody("equity", "-1", "fund", "101"), "weight: must be between 0 and 100"},
		{"not a number", targetsBody("equity", "abc"), "weight: must be between 0 and 100"},
		{"empty weight", targetsBody("equity", ""), "weight: must be between 0 and 100"},
		{"sum below 100", targetsBody("equity", "40", "fund", "59.9"), "weight: must sum to 100"},
		{"sum above 100", targetsBody("equity", "60", "fund", "50"), "weight: must sum to 100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := e.do(session, http.MethodPut, path, tt.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("put = %d %s, want 400 %q", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM allocation_targets`).Scan(&n); err != nil || n != 0 {
		t.Errorf("targets stored = %d (err %v), want 0", n, err)
	}
}

// TestAllocationTargetsAreScopedToTheirUser checks that another user's get and
// put of the same portfolio id are 404, as is an id that is not a number, and
// that the other user's put leaves the set alone.
func TestAllocationTargetsAreScopedToTheirUser(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	pathA := targetsPath(e.createPortfolio(a, "A"))
	if rec := e.do(a, http.MethodPut, pathA, targetsBody("equity", "100")); rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body.String())
	}

	for _, tt := range []struct {
		session, method, path string
	}{
		{b, http.MethodGet, pathA},
		{b, http.MethodPut, pathA},
		{a, http.MethodGet, targetsPath("nope")},
		{a, http.MethodPut, targetsPath("nope")},
	} {
		rec := e.do(tt.session, tt.method, tt.path, targetsBody("fund", "100"))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "portfolio not found") {
			t.Errorf("%s %s = %d %s, want 404 portfolio not found", tt.method, tt.path, rec.Code, rec.Body.String())
		}
	}
	rec := e.do(a, http.MethodGet, pathA, nil)
	if got := fmt.Sprint(decodeJSON[allocationTargetsBody](t, rec.Body.Bytes())); got != fmt.Sprint(targetsBody("equity", "100")) {
		t.Errorf("A's set = %s, want equity 100", got)
	}
}

// TestAllocationTargetsAllScope checks that the all-portfolios set is stored
// and read apart from a portfolio's set, and apart from another user's.
func TestAllocationTargetsAllScope(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	own := targetsPath(e.createPortfolio(a, "A"))
	all := targetsPath("all")

	for path, body := range map[string]allocationTargetsBody{
		own: targetsBody("equity", "100"),
		all: targetsBody("fund", "70", "deposit", "30"),
	} {
		if rec := e.do(a, http.MethodPut, path, body); rec.Code != http.StatusOK {
			t.Fatalf("put %s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := e.do(a, http.MethodGet, own, nil)
	if got := fmt.Sprint(decodeJSON[allocationTargetsBody](t, rec.Body.Bytes())); got != fmt.Sprint(targetsBody("equity", "100")) {
		t.Errorf("portfolio set = %s, want equity 100", got)
	}
	rec = e.do(a, http.MethodGet, all, nil)
	if got := fmt.Sprint(decodeJSON[allocationTargetsBody](t, rec.Body.Bytes())); got != fmt.Sprint(targetsBody("fund", "70", "deposit", "30")) {
		t.Errorf("all set = %s, want fund 70 and deposit 30", got)
	}
	rec = e.do(b, http.MethodGet, all, nil)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"targets":[]}` {
		t.Errorf("B's all set = %s, want no targets", got)
	}
	// Clearing the all set leaves the portfolio's set.
	if rec := e.do(a, http.MethodPut, all, targetsBody()); rec.Code != http.StatusOK {
		t.Fatalf("clear all = %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(a, http.MethodGet, own, nil)
	if got := fmt.Sprint(decodeJSON[allocationTargetsBody](t, rec.Body.Bytes())); got != fmt.Sprint(targetsBody("equity", "100")) {
		t.Errorf("portfolio set after clearing all = %s, want equity 100", got)
	}
}
