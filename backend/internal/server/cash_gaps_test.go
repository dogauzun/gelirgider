package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

func (e *periodsEnv) post(t *testing.T, session, path, body string, wantStatus int, out any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "session", Value: session})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, r)
	if rec.Code != wantStatus {
		t.Fatalf("POST %s: status = %d, want %d; body: %s", path, rec.Code, wantStatus, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}

// TestCashGaps lists the cash gaps of portfolios with buys and no deposits,
// writes them as deposits, and checks the history's returns are then
// defined.
func TestCashGaps(t *testing.T) {
	e := newPeriodsEnv(t)
	ctx := context.Background()
	userID, session := e.user(t, "gaps@example.com")
	p1, p2 := e.portfolio(t, userID), e.portfolio(t, userID)
	_, other := e.user(t, "other@example.com")
	inst := e.instrument(t, "GAP")
	e.buy(t, userID, lot{portfolioID: p1, instrumentID: inst, date: "2024-01-02", qty: "10", px: "100"})
	e.buy(t, userID, lot{portfolioID: p1, instrumentID: inst, date: "2024-01-04", qty: "5", px: "100"})
	e.buy(t, userID, lot{portfolioID: p2, instrumentID: inst, date: "2024-01-03", qty: "1", px: "50"})
	prices := price.NewRepo(e.conn)
	for date, close := range map[string]string{"2024-01-02": "100", "2024-01-03": "105", "2024-01-04": "100", "2024-01-05": "110"} {
		if err := prices.Upsert(ctx, inst, date, close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	history := fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-02&to=2024-01-05", p1)
	var before historyResponse
	e.get(t, session, history, http.StatusOK, &before)
	if before.Returns.Simple != nil {
		t.Fatalf("returns before = %s, want null with negative cash", asJSON(t, before.Returns))
	}

	path1 := fmt.Sprintf("/api/portfolios/%d/cash-gaps", p1)
	var gaps []cashGap
	e.get(t, session, path1, http.StatusOK, &gaps)
	want := fmt.Sprintf(`[{"portfolio_id":%d,"date":"2024-01-02","currency":"TRY","amount":"1000"},{"portfolio_id":%d,"date":"2024-01-04","currency":"TRY","amount":"500"}]`, p1, p1)
	if got := asJSON(t, gaps); got != want {
		t.Fatalf("gaps = %s, want %s", got, want)
	}

	e.post(t, session, path1, `{"note":"a\nb"}`, http.StatusBadRequest, nil)
	e.get(t, other, path1, http.StatusNotFound, nil)
	e.post(t, other, path1, `{"note":"x"}`, http.StatusNotFound, nil)

	var ids []int64
	e.post(t, session, path1, `{"note":" Missing deposit "}`, http.StatusOK, &ids)
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2", ids)
	}
	txs, err := transaction.NewRepo(e.conn).List(ctx, userID, p1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	deposits := 0
	for _, tx := range txs {
		if tx.Type == transaction.TypeDeposit {
			deposits++
			if tx.Note != "Missing deposit" {
				t.Errorf("deposit %d note = %q, want %q", tx.ID, tx.Note, "Missing deposit")
			}
		}
	}
	if deposits != 2 {
		t.Errorf("deposits in p1 = %d, want 2", deposits)
	}

	var after historyResponse
	e.get(t, session, history, http.StatusOK, &after)
	for _, p := range after.Points {
		if strings.HasPrefix(p.Cash, "-") {
			t.Errorf("%s: cash = %s, want not negative", p.Date, p.Cash)
		}
	}
	if after.Returns.Simple == nil || after.Returns.TWR == nil || after.Returns.MWR == nil {
		t.Errorf("returns after = %s, want defined", asJSON(t, after.Returns))
	}

	e.post(t, session, path1, `{"note":"again"}`, http.StatusOK, &ids)
	if len(ids) != 0 {
		t.Errorf("second POST ids = %v, want none", ids)
	}

	e.post(t, session, "/api/portfolios/all/cash-gaps", `{"note":"all"}`, http.StatusOK, &ids)
	if len(ids) != 1 {
		t.Fatalf("all ids = %v, want p2's one gap", ids)
	}
	txs, err = transaction.NewRepo(e.conn).List(ctx, userID, p2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := txs[len(txs)-1]; got.ID != ids[0] || got.Type != transaction.TypeDeposit || got.Amount.String() != "50" {
		t.Errorf("p2's last row = %+v, want the deposit %d of 50", got, ids[0])
	}
	e.get(t, session, "/api/portfolios/all/cash-gaps", http.StatusOK, &gaps)
	if len(gaps) != 0 {
		t.Errorf("gaps after = %s, want none", asJSON(t, gaps))
	}
}

// TestCashGapsFractionDigits fills the gap of a manual buy whose amount,
// quantity × price, has more fraction digits than a deposit accepts:
// the POST must still write it, and the cash stay non-negative.
func TestCashGapsFractionDigits(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "digits@example.com")
	p := e.portfolio(t, userID)
	inst := e.instrument(t, "DIG")
	e.buy(t, userID, lot{portfolioID: p, instrumentID: inst, date: "2024-01-02", qty: "0.5", px: "33.333333333"})
	if err := price.NewRepo(e.conn).Upsert(context.Background(), inst, "2024-01-02", "34", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	var ids []int64
	e.post(t, session, fmt.Sprintf("/api/portfolios/%d/cash-gaps", p), `{"note":"x"}`, http.StatusOK, &ids)
	if len(ids) != 1 {
		t.Fatalf("ids = %v, want 1", ids)
	}
	var h historyResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/history?from=2024-01-02&to=2024-01-03", p), http.StatusOK, &h)
	for _, pt := range h.Points {
		if strings.HasPrefix(pt.Cash, "-") {
			t.Errorf("%s: cash = %s, want not negative", pt.Date, pt.Cash)
		}
	}
	if h.Returns.Simple == nil {
		t.Errorf("returns = %s, want defined", asJSON(t, h.Returns))
	}
}
