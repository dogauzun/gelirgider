package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/price"
)

func goalPath(id any) string { return fmt.Sprintf("/api/portfolios/%v/goal", id) }

func futureDate() string { return time.Now().In(price.Istanbul).AddDate(5, 0, 0).Format(time.DateOnly) }

func goalReq(amount, unit, date string) goalBody {
	return goalBody{Amount: amount, Unit: unit, TargetDate: date}
}

func (e *transactionTestEnv) setReportingCurrency(session, currency string) {
	e.t.Helper()
	if err := e.users.UpdateReportingCurrency(context.Background(), e.userID(session), currency); err != nil {
		e.t.Fatalf("set reporting currency: %v", err)
	}
}

// TestGoalRoundTrip checks that a put is answered and read back, that a
// second put replaces the first and that a deleted goal is gone.
func TestGoalRoundTrip(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := goalPath(e.createPortfolio(session, "P"))
	date := futureDate()

	rec := e.do(session, http.MethodGet, path, nil)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusNotFound || got != `{"error":"goal not found"}` {
		t.Fatalf("get without goal = %d %s, want 404 goal not found", rec.Code, got)
	}

	want := goalReq("1000000.5", "real", date)
	rec = e.do(session, http.MethodPut, path, want)
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := decodeJSON[goalBody](t, rec.Body.Bytes()); got != want {
		t.Errorf("put answered %v, want %v", got, want)
	}
	rec = e.do(session, http.MethodGet, path, nil)
	if got := decodeJSON[goalBody](t, rec.Body.Bytes()); rec.Code != http.StatusOK || got != want {
		t.Errorf("get = %d %v, want 200 %v", rec.Code, got, want)
	}

	want = goalReq("2000000", "nominal", date)
	if rec := e.do(session, http.MethodPut, path, want); rec.Code != http.StatusOK {
		t.Fatalf("second put = %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(session, http.MethodGet, path, nil)
	if got := decodeJSON[goalBody](t, rec.Body.Bytes()); got != want {
		t.Errorf("after replace = %v, want %v", got, want)
	}
	if n := e.countRows("goals"); n != 1 {
		t.Errorf("goals stored = %d, want 1", n)
	}

	if rec := e.do(session, http.MethodDelete, path, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", rec.Code, rec.Body.String())
	}
	if rec := e.do(session, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
	rec = e.do(session, http.MethodDelete, path, nil)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusNotFound || got != `{"error":"goal not found"}` {
		t.Errorf("second delete = %d %s, want 404 goal not found", rec.Code, got)
	}
}

// TestGoalValidation checks each refusal and that it stores nothing.
func TestGoalValidation(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := goalPath(e.createPortfolio(session, "P"))
	date := futureDate()
	today := price.Today()
	tests := []struct {
		name     string
		currency string
		body     goalBody
		want     string
	}{
		{"zero amount", "TRY", goalReq("0", "nominal", date), "amount: must be a positive number"},
		{"negative amount", "TRY", goalReq("-5", "nominal", date), "amount: must be a positive number"},
		{"not a number", "TRY", goalReq("abc", "nominal", date), "amount: must be a positive number"},
		{"unknown unit", "TRY", goalReq("100", "eur", date), "unit: must be one of nominal, real, usd"},
		{"real in USD", "USD", goalReq("100", "real", date), "unit: real needs a TRY reporting currency"},
		{"real in EUR", "EUR", goalReq("100", "real", date), "unit: real needs a TRY reporting currency"},
		{"usd in USD", "USD", goalReq("100", "usd", date), "unit: usd needs a non-USD reporting currency"},
		{"today", "TRY", goalReq("100", "nominal", today), "target_date: must be after today"},
		{"past", "TRY", goalReq("100", "nominal", "2020-01-01"), "target_date: must be after today"},
		{"not a date", "TRY", goalReq("100", "nominal", "soon"), "target_date: must be after today"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.setReportingCurrency(session, tt.currency)
			rec := e.do(session, http.MethodPut, path, tt.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("put = %d %s, want 400 %q", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
	if n := e.countRows("goals"); n != 0 {
		t.Errorf("goals stored = %d, want 0", n)
	}

	e.setReportingCurrency(session, "USD")
	if rec := e.do(session, http.MethodPut, path, goalReq("100", "nominal", date)); rec.Code != http.StatusOK {
		t.Errorf("nominal in USD = %d %s, want 200", rec.Code, rec.Body.String())
	}
	e.setReportingCurrency(session, "EUR")
	if rec := e.do(session, http.MethodPut, path, goalReq("100", "usd", date)); rec.Code != http.StatusOK {
		t.Errorf("usd in EUR = %d %s, want 200", rec.Code, rec.Body.String())
	}
	e.setReportingCurrency(session, "TRY")
	if rec := e.do(session, http.MethodPut, path, goalReq("100", "real", date)); rec.Code != http.StatusOK {
		t.Errorf("real in TRY = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

// TestGoalsAreScopedToTheirUser checks that another user's id is 404 on every
// verb without touching the goal.
func TestGoalsAreScopedToTheirUser(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	pathA := goalPath(e.createPortfolio(a, "A"))
	want := goalReq("100", "nominal", futureDate())
	if rec := e.do(a, http.MethodPut, pathA, want); rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body.String())
	}

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		var body any
		if method == http.MethodPut {
			body = want
		}
		rec := e.do(b, method, pathA, body)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusNotFound || got != `{"error":"portfolio not found"}` {
			t.Errorf("B %s on A's portfolio = %d %s, want 404 portfolio not found", method, rec.Code, got)
		}
	}
	if rec := e.do(b, http.MethodGet, goalPath("all"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("B's all scope = %d, want 404 goal not found", rec.Code)
	}
	rec := e.do(a, http.MethodGet, pathA, nil)
	if got := decodeJSON[goalBody](t, rec.Body.Bytes()); got != want {
		t.Errorf("A's goal = %v, want %v", got, want)
	}
}

// TestGoalAllScopeIsIndependent checks that the all scope and a portfolio's
// goal are stored apart.
func TestGoalAllScopeIsIndependent(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := goalPath(e.createPortfolio(session, "P"))
	all := goalPath("all")
	date := futureDate()
	one, two := goalReq("100", "nominal", date), goalReq("200", "usd", date)

	for p, g := range map[string]goalBody{path: one, all: two} {
		if rec := e.do(session, http.MethodPut, p, g); rec.Code != http.StatusOK {
			t.Fatalf("put %s = %d %s", p, rec.Code, rec.Body.String())
		}
	}
	// A second put on the all scope replaces only that scope's row.
	two = goalReq("300", "nominal", date)
	if rec := e.do(session, http.MethodPut, all, two); rec.Code != http.StatusOK {
		t.Fatalf("second put all = %d %s", rec.Code, rec.Body.String())
	}
	for p, want := range map[string]goalBody{path: one, all: two} {
		rec := e.do(session, http.MethodGet, p, nil)
		if got := decodeJSON[goalBody](t, rec.Body.Bytes()); got != want {
			t.Errorf("get %s = %v, want %v", p, got, want)
		}
	}

	if rec := e.do(session, http.MethodDelete, all, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete all = %d", rec.Code)
	}
	if rec := e.do(session, http.MethodGet, path, nil); rec.Code != http.StatusOK {
		t.Errorf("portfolio goal after deleting all = %d, want 200", rec.Code)
	}
	if rec := e.do(session, http.MethodGet, all, nil); rec.Code != http.StatusNotFound {
		t.Errorf("all goal after delete = %d, want 404", rec.Code)
	}
}

// TestGoalProjection checks the projection route: 404 without a goal, a fan
// from 8 full months of history, an empty fan from one transaction and an
// empty 200 for a real goal without a TUFE level.
func TestGoalProjection(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	now := time.Now().In(price.Istanbul)
	monthStart := func(ago int) string {
		return time.Date(now.Year(), now.Month()-time.Month(ago), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	project := func(portfolioID int64) projectionResponse {
		t.Helper()
		rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/projection", portfolioID), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("projection = %d %s, want 200", rec.Code, rec.Body.String())
		}
		return decodeJSON[projectionResponse](t, rec.Body.Bytes())
	}

	long, short := e.createPortfolio(session, "Long"), e.createPortfolio(session, "Short")
	rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/projection", long), nil)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusNotFound || got != `{"error":"goal not found"}` {
		t.Fatalf("projection without goal = %d %s, want 404 goal not found", rec.Code, got)
	}

	for ago := 9; ago >= 1; ago-- {
		e.createTransaction(session, long, map[string]any{"type": "deposit", "date": monthStart(ago), "amount": "1000"})
	}
	e.createTransaction(session, short, map[string]any{"type": "deposit", "date": monthStart(1), "amount": "1000"})
	for _, id := range []int64{long, short} {
		if rec := e.do(session, http.MethodPut, goalPath(id), goalReq("100000", "nominal", futureDate())); rec.Code != http.StatusOK {
			t.Fatalf("put goal = %d %s", rec.Code, rec.Body.String())
		}
	}

	got := project(long)
	if got.HistoryMonths < 8 || len(got.Months) == 0 || got.Unit != "nominal" || got.Currency != "TRY" || got.Amount != "100000" {
		t.Errorf("long history = %+v, want 8 or more history months and a fan", got)
	}
	if len(got.Months) > 0 && math.Abs(got.Months[0].Mid-10000) > 0.01 {
		t.Errorf("first month mid = %v, want 10000: the 9000 held plus the 1000 deposited on each month's first day", got.Months[0].Mid)
	}
	if got.MonthlyGap == nil || got.ReachedAt != nil || got.Progress <= 0 {
		t.Errorf("long history = %+v, want a gap and no reached_at", got)
	}
	if got := project(short); got.HistoryMonths != 0 || len(got.Months) != 0 || got.Months == nil {
		t.Errorf("one transaction = %+v, want an empty fan", got)
	}

	if rec := e.do(session, http.MethodPut, goalPath(long), goalReq("100000", "real", futureDate())); rec.Code != http.StatusOK {
		t.Fatalf("put real goal = %d %s", rec.Code, rec.Body.String())
	}
	if got := project(long); got.HistoryMonths != 0 || len(got.Months) != 0 || got.Months == nil {
		t.Errorf("real without TUFE = %+v, want an empty fan", got)
	}
}

// TestGoalProjectionWindowsFullMonths checks that the 36-month window is cut
// over the full calendar months before the months without a return are
// dropped: a scope with returns from 39 to 30 months ago, empty from 29 to 21
// and funded again from 20 to 1 has 27 months with a return inside the last
// 36 full months (36 to 30 and 20 to 1), and the 3 older ones must not count.
func TestGoalProjectionWindowsFullMonths(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	now := time.Now().In(price.Istanbul)
	monthStart := func(ago int) string {
		return time.Date(now.Year(), now.Month()-time.Month(ago), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	id := e.createPortfolio(session, "Gap")
	for ago := 40; ago >= 1; ago-- {
		switch {
		case ago == 29:
			e.createTransaction(session, id, map[string]any{"type": "withdrawal", "date": monthStart(ago), "amount": "11000"})
		case ago > 29 || ago <= 20:
			e.createTransaction(session, id, map[string]any{"type": "deposit", "date": monthStart(ago), "amount": "1000"})
		}
	}
	if rec := e.do(session, http.MethodPut, goalPath(id), goalReq("100000", "nominal", futureDate())); rec.Code != http.StatusOK {
		t.Fatalf("put goal = %d %s", rec.Code, rec.Body.String())
	}
	rec := e.do(session, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/projection", id), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("projection = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := decodeJSON[projectionResponse](t, rec.Body.Bytes()); got.HistoryMonths != 27 || len(got.Months) == 0 {
		t.Errorf("history_months = %d, want 27: empty months and months before the 36-month window do not count", got.HistoryMonths)
	}
}
