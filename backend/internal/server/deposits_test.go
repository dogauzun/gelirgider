package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// daysFromToday returns the Istanbul date n days from today, YYYY-MM-DD.
func daysFromToday(t *testing.T, n int) string {
	t.Helper()
	today, err := time.Parse(time.DateOnly, price.Today())
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}
	return today.AddDate(0, 0, n).Format(time.DateOnly)
}

func depositBody(start, maturity string) map[string]any {
	return map[string]any{
		"currency": "TRY", "principal": "100000", "rate": "0.45", "stopaj_rate": "0.175",
		"start_date": start, "maturity_date": maturity,
	}
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

// TestCreateDeposit checks the opening of a 100000 TRY deposit that started 40
// days ago: without fund the cash goes 100000 negative and cash-gaps lists
// the gap, with fund the cash is unchanged, there is no gap and no day of the
// history has negative cash. Both show the deposit position valued from its
// terms and list the deposit as active.
func TestCreateDeposit(t *testing.T) {
	start, maturity := daysFromToday(t, -40), daysFromToday(t, 60)
	terms := deposit.Terms{Rate: decimal.RequireFromString("0.45"), StopajRate: decimal.RequireFromString("0.175"), StartDate: start, MaturityDate: maturity}
	close, _ := deposit.Close(terms, price.Today())
	wantValue := decimal.NewFromInt(100000).Mul(close).Round(2)

	for _, fund := range []bool{false, true} {
		t.Run(fmt.Sprintf("fund=%v", fund), func(t *testing.T) {
			e := newTransactionTestEnv(t)
			session := e.login("a@example.com")
			portfolioID := e.createPortfolio(session, "P")
			path := fmt.Sprintf("/api/portfolios/%d", portfolioID)

			body := depositBody(start, maturity)
			body["fund"] = fund
			body["note"] = " 6 month "
			rec := e.do(session, http.MethodPost, path+"/deposits", body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create = %d %s, want 201", rec.Code, rec.Body.String())
			}
			created := decodeJSON[depositResponse](t, rec.Body.Bytes())
			if created.Status != "active" || created.Value != wantValue.StringFixed(2) {
				t.Errorf("created = %+v, want active with value %s", created, wantValue)
			}

			type summaryBody struct {
				Cash      string `json:"cash"`
				Positions []struct {
					Market      string `json:"market"`
					MarketValue string `json:"market_value"`
				} `json:"positions"`
			}
			summary := decodeJSON[summaryBody](t, e.do(session, http.MethodGet, path+"/summary", nil).Body.Bytes())
			if len(summary.Positions) != 1 || summary.Positions[0].Market != instrument.MarketDeposit {
				t.Fatalf("positions = %+v, want one DEPOSIT position", summary.Positions)
			}
			if got := decimal.RequireFromString(summary.Positions[0].MarketValue); !got.Round(2).Equal(wantValue) {
				t.Errorf("market_value = %s, want %s", got, wantValue)
			}
			wantCash := "-100000"
			if fund {
				wantCash = "0"
			}
			if got := decimal.RequireFromString(summary.Cash); !got.Equal(decimal.RequireFromString(wantCash)) {
				t.Errorf("cash = %s, want %s", got, wantCash)
			}

			gaps := decodeJSON[[]cashGap](t, e.do(session, http.MethodGet, path+"/cash-gaps", nil).Body.Bytes())
			if fund && len(gaps) != 0 {
				t.Errorf("gaps = %+v, want none", gaps)
			}
			if !fund && (len(gaps) != 1 || gaps[0].Date != start || !decimal.RequireFromString(gaps[0].Amount).Equal(decimal.NewFromInt(100000))) {
				t.Errorf("gaps = %+v, want one of 100000 on %s", gaps, start)
			}

			hist := decodeJSON[historyResponse](t, e.do(session, http.MethodGet, path+"/history?from="+start+"&to="+price.Today(), nil).Body.Bytes())
			negative := false
			for _, p := range hist.Points {
				negative = negative || strings.HasPrefix(p.Cash, "-")
			}
			if negative == fund {
				t.Errorf("history has negative cash = %v, want %v", negative, !fund)
			}
			if last := hist.Points[len(hist.Points)-1]; (last.TWR == nil) == fund {
				t.Errorf("last twr = %v, want null only without fund", last.TWR)
			}

			list := decodeJSON[[]depositResponse](t, e.do(session, http.MethodGet, path+"/deposits", nil).Body.Bytes())
			if len(list) != 1 || list[0] != created {
				t.Errorf("list = %+v, want just %+v", list, created)
			}
			var note string
			if err := e.db.QueryRow(`SELECT note FROM transactions WHERE type = 'buy'`).Scan(&note); err != nil || note != "6 month" {
				t.Errorf("buy note = %q (err %v), want %q", note, err, "6 month")
			}
			var funding any
			if err := e.db.QueryRow(`SELECT funding_transaction_id FROM term_deposits`).Scan(&funding); err != nil || (funding != nil) != fund {
				t.Errorf("funding_transaction_id = %v (err %v), want set only with fund", funding, err)
			}
		})
	}
}

// TestListDepositsPayoutAndStatus checks a deposit of 100000 from 2026-01-01
// to 2026-02-02 at 0.45 with stopaj 0.175: net interest at maturity 3254.80,
// payout 103254.80. It is due once the maturity date has passed and matured
// once a sell is recorded on it, and the newest start comes first.
func TestListDepositsPayoutAndStatus(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	portfolioID := e.createPortfolio(session, "P")
	path := fmt.Sprintf("/api/portfolios/%d/deposits", portfolioID)

	if rec := e.do(session, http.MethodPost, path, depositBody("2026-01-01", "2026-02-02")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(session, http.MethodPost, path, depositBody("2026-01-05", "2026-02-02")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	list := decodeJSON[[]depositResponse](t, e.do(session, http.MethodGet, path, nil).Body.Bytes())
	if len(list) != 2 || list[0].StartDate != "2026-01-05" {
		t.Fatalf("list = %+v, want two, newest start first", list)
	}
	older := list[1]
	if older.NetInterestAtMaturity != "3254.80" || older.Payout != "103254.80" || older.Status != "due" ||
		older.Value != "103254.79" || older.AccruedInterest != "3254.79" {
		t.Errorf("older = %+v, want net 3254.80, payout 103254.80, due, value 103254.79 (unrounded interest)", older)
	}

	if _, err := e.db.Exec(`INSERT INTO transactions
		(user_id, portfolio_id, instrument_id, type, date, quantity, price, amount, fee, currency, created_at)
		SELECT user_id, portfolio_id, instrument_id, 'sell', '2026-02-02', '100000', '1.03', '103254.80', '0', 'TRY', '2026-02-02T00:00:00Z'
		FROM term_deposits WHERE id = ?`, older.ID); err != nil {
		t.Fatalf("insert sell: %v", err)
	}
	list = decodeJSON[[]depositResponse](t, e.do(session, http.MethodGet, path, nil).Body.Bytes())
	if list[0].Status != "due" || list[1].Status != "matured" {
		t.Errorf("statuses = %s, %s, want due, matured", list[0].Status, list[1].Status)
	}
}

// TestDepositsAreScopedToTheirUser checks that another user's list and create
// of the same portfolio id are 404 and that their own portfolio lists nothing.
func TestDepositsAreScopedToTheirUser(t *testing.T) {
	e := newTransactionTestEnv(t)
	a, b := e.login("a@example.com"), e.login("b@example.com")
	portfolioA := e.createPortfolio(a, "A")
	portfolioB := e.createPortfolio(b, "B")
	pathA := fmt.Sprintf("/api/portfolios/%d/deposits", portfolioA)
	if rec := e.do(a, http.MethodPost, pathA, depositBody("2026-01-01", "2026-02-02")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var body any
		if method == http.MethodPost {
			body = depositBody("2026-01-01", "2026-02-02")
		}
		if rec := e.do(b, method, pathA, body); rec.Code != http.StatusNotFound {
			t.Errorf("B %s on A's portfolio = %d, want 404", method, rec.Code)
		}
	}
	rec := e.do(b, http.MethodGet, fmt.Sprintf("/api/portfolios/%d/deposits", portfolioB), nil)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != "[]" {
		t.Errorf("B's own list = %d %s, want 200 []", rec.Code, got)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM term_deposits`).Scan(&n); err != nil || n != 1 {
		t.Errorf("deposits stored = %d (err %v), want 1", n, err)
	}
}

// TestCreateDepositValidation checks each refusal and that it stores nothing.
func TestCreateDepositValidation(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	path := fmt.Sprintf("/api/portfolios/%d/deposits", e.createPortfolio(session, "P"))
	tests := []struct {
		name  string
		field string
		value any
		want  string
	}{
		{"currency", "currency", "GBP", "currency: must be one of"},
		{"zero principal", "principal", "0", "principal: must be a positive number"},
		{"too many digits", "principal", "1234567890123456", "principal: must have at most 15 integer"},
		{"missing principal", "principal", "", "principal: required"},
		{"zero rate", "rate", "0", "rate: must be greater than 0 and at most 10"},
		{"rate above 10", "rate", "10.01", "rate: must be greater than 0 and at most 10"},
		{"stopaj 1", "stopaj_rate", "1", "stopaj_rate: must be at least 0 and below 1"},
		{"future start", "start_date", daysFromToday(t, 1), "start_date: must not be in the future"},
		{"bad start", "start_date", "2026-13-01", "start_date: must be YYYY-MM-DD"},
		{"old start", "start_date", "1989-12-31", "start_date: must be on or after 1990-01-01"},
		{"bad maturity", "maturity_date", "soon", "maturity_date: must be YYYY-MM-DD"},
		{"maturity on start", "maturity_date", "2026-01-01", "maturity_date: must be after start_date"},
		{"maturity after 10 years", "maturity_date", "2036-01-02", "maturity_date: must be at most 10 years after start_date"},
		{"note", "note", "a\nb", "note: must not contain control characters"},
	}
	for _, tt := range tests {
		body := depositBody("2026-01-01", "2026-02-02")
		body[tt.field] = tt.value
		rec := e.do(session, http.MethodPost, path, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
			t.Errorf("%s: %d %s, want 400 containing %q", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}
	body := depositBody("2026-01-01", "2036-01-01")
	if rec := e.do(session, http.MethodPost, path, body); rec.Code != http.StatusCreated {
		t.Errorf("maturity exactly 10 years out = %d %s, want 201", rec.Code, rec.Body.String())
	}
	var n int
	if err := e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM term_deposits) + (SELECT COUNT(*) FROM instruments WHERE market = 'DEPOSIT')`).Scan(&n); err != nil || n != 2 {
		t.Errorf("rows stored = %d (err %v), want 2 from the one accepted request", n, err)
	}
}

// TestCreateDepositCallsOnDemandHookOnce checks that a USD deposit with fund
// runs the on-demand hook once, for the DEPOSIT instrument and the start date.
func TestCreateDepositCallsOnDemandHookOnce(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("a@example.com")
	portfolioID := e.createPortfolio(session, "P")
	var calls []string
	fetch := func(_ context.Context, in instrument.Instrument, date string) {
		calls = append(calls, in.Market+" "+in.Currency+" "+date)
	}
	protected := http.NewServeMux()
	registerDepositRoutes(protected, &deps{
		conn: e.db, portfolios: portfolio.NewRepo(e.db), instruments: instrument.NewRepo(e.db),
		transactions: transaction.NewRepo(e.db), deposits: deposit.NewRepo(e.db),
	}, fetch)
	e.handler = e.sessions.RequireUser(protected)

	body := depositBody("2026-01-01", "2026-02-02")
	body["currency"] = "USD"
	body["fund"] = true
	rec := e.do(session, http.MethodPost, fmt.Sprintf("/api/portfolios/%d/deposits", portfolioID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 1 || calls[0] != "DEPOSIT USD 2026-01-01" {
		t.Errorf("hook calls = %v, want one for the DEPOSIT USD instrument on 2026-01-01", calls)
	}
}
