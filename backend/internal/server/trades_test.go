package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// sell records a sell of qty at px with fee in currency.
func (e *periodsEnv) sell(t *testing.T, userID int64, l lot, fee, currency string) {
	t.Helper()
	q, p := decimal.RequireFromString(l.qty), decimal.RequireFromString(l.px)
	tx := transaction.Transaction{PortfolioID: l.portfolioID, InstrumentID: &l.instrumentID, Type: transaction.TypeSell, Date: l.date, Quantity: q, Price: p, Amount: q.Mul(p), Fee: decimal.RequireFromString(fee), Currency: currency}
	if _, err := transaction.NewRepo(e.conn).Create(context.Background(), userID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
}

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

// TestTrades checks the ADR 0034 worked example: a buy of 10 at 100, a sell
// of 4 at 120 with a fee of 2 and a sell of 6 at 90 answer two rows newest
// first, the filter keeps one instrument's sells and a bad one is a 400.
func TestTrades(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "trades@example.com")
	pf := e.portfolio(t, userID)
	a, b := e.instrument(t, "AAA"), e.instrument(t, "BBB")
	e.buy(t, userID, lot{pf, a, "2026-09-01", "10", "100"})
	e.sell(t, userID, lot{pf, a, "2026-09-02", "4", "120"}, "2", "TRY")
	e.sell(t, userID, lot{pf, a, "2026-09-03", "6", "90"}, "0", "TRY")

	var got tradesResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/trades", pf), http.StatusOK, &got)
	want := []tradeResponse{
		{PortfolioID: pf, Date: "2026-09-03", InstrumentID: a, Symbol: "AAA", Name: "AAA", Market: "BIST", Currency: "TRY", Quantity: "6", Proceeds: "540.00", Cost: "600.00", Fee: "0.00", Realized: "-60.00", RealizedPct: str("-0.1000"), OpenedAt: str("2026-09-01"), Days: num(2), RealizedReporting: str("-60.00")},
		{PortfolioID: pf, Date: "2026-09-02", InstrumentID: a, Symbol: "AAA", Name: "AAA", Market: "BIST", Currency: "TRY", Quantity: "4", Proceeds: "478.00", Cost: "400.00", Fee: "2.00", Realized: "78.00", RealizedPct: str("0.1950"), OpenedAt: str("2026-09-01"), Days: num(1), RealizedReporting: str("78.00")},
	}
	if got.Currency != "TRY" || asJSON(t, got.Trades) != asJSON(t, want) || len(got.MissingRates) != 0 {
		t.Errorf("trades = %s, want %s (currency %q, missing %v)", asJSON(t, got.Trades), asJSON(t, want), got.Currency, got.MissingRates)
	}

	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/trades?instrument_id=%d", pf, a), http.StatusOK, &got)
	if len(got.Trades) != 2 {
		t.Errorf("filtered to the sold instrument: %d rows, want 2", len(got.Trades))
	}
	var none tradesResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/trades?instrument_id=%d", pf, b), http.StatusOK, &none)
	if none.Trades == nil || len(none.Trades) != 0 || none.MissingRates == nil {
		t.Errorf("another instrument = %+v, want empty lists, not null", none)
	}
	for _, bad := range []string{"x", "0", "-1"} {
		e.get(t, session, fmt.Sprintf("/api/portfolios/%d/trades?instrument_id=%s", pf, bad), http.StatusBadRequest, nil)
	}
	e.get(t, session, "/api/portfolios/999999/trades", http.StatusNotFound, nil)
}

// TestTradesAllScope checks that "all" lists every portfolio's sells with
// their portfolio id, each realized against its own average cost, and that
// another user sees none of them.
func TestTradesAllScope(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "all@example.com")
	_, other := e.user(t, "other@example.com")
	p1, p2 := e.portfolio(t, userID), e.portfolio(t, userID)
	a := e.instrument(t, "AAA")
	e.buy(t, userID, lot{p1, a, "2026-09-01", "10", "100"})
	e.buy(t, userID, lot{p2, a, "2026-09-01", "10", "50"})
	e.sell(t, userID, lot{p1, a, "2026-09-02", "1", "110"}, "0", "TRY")
	e.sell(t, userID, lot{p2, a, "2026-09-03", "1", "110"}, "0", "TRY")

	var got tradesResponse
	e.get(t, session, "/api/portfolios/all/trades", http.StatusOK, &got)
	if len(got.Trades) != 2 || got.Trades[0].PortfolioID != p2 || got.Trades[0].Realized != "60.00" || got.Trades[1].PortfolioID != p1 || got.Trades[1].Realized != "10.00" {
		t.Errorf("all = %s, want p2's sell (60.00) then p1's (10.00)", asJSON(t, got.Trades))
	}
	var foreign tradesResponse
	e.get(t, other, "/api/portfolios/all/trades", http.StatusOK, &foreign)
	if foreign.Trades == nil || len(foreign.Trades) != 0 {
		t.Errorf("second user's all = %+v, want []", foreign)
	}
	e.get(t, other, fmt.Sprintf("/api/portfolios/%d/trades", p1), http.StatusNotFound, nil)
}

// TestTradesMissingRate checks a USD sell with no USD rate has no
// realized_reporting and lists USD, and with a rate of 40 on or before the sell
// date is the realized times 40.
func TestTradesMissingRate(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "missing-trades@example.com")
	pf := e.portfolio(t, userID)
	res, err := e.conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDSTK', 'US', 'USDSTK', 'USD')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	tx := transaction.Transaction{PortfolioID: pf, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2026-03-01", Quantity: dec("10"), Price: dec("10"), Amount: dec("100"), Currency: "USD"}
	if _, err := transaction.NewRepo(e.conn).Create(context.Background(), userID, tx); err != nil {
		t.Fatal(err)
	}
	e.sell(t, userID, lot{pf, id, "2026-03-05", "10", "12"}, "0", "USD")

	var got tradesResponse
	path := fmt.Sprintf("/api/portfolios/%d/trades", pf)
	e.get(t, session, path, http.StatusOK, &got)
	if len(got.Trades) != 1 || got.Trades[0].Realized != "20.00" || got.Trades[0].RealizedReporting != nil || fmt.Sprint(got.MissingRates) != "[USD]" {
		t.Errorf("no rate: %+v, want realized 20.00, null reporting, [USD]", got)
	}

	if err := fx.NewRepo(e.conn).Upsert(context.Background(), "USD", "2026-03-04", "40", "manual"); err != nil {
		t.Fatal(err)
	}
	e.get(t, session, path, http.StatusOK, &got)
	if len(got.Trades) != 1 || got.Trades[0].RealizedReporting == nil || *got.Trades[0].RealizedReporting != "800.00" || len(got.MissingRates) != 0 {
		t.Errorf("rate 40: %+v, want reporting 800.00 and no missing rate", got)
	}
}

// TestTradesSkipsDeposit checks that a term deposit's maturity sell is not a
// trade.
func TestTradesSkipsDeposit(t *testing.T) {
	e := newTransactionTestEnv(t)
	a := e.login("deposit-trades@example.com")
	portfolioID := e.createPortfolio(a, "P")
	path := fmt.Sprintf("/api/portfolios/%d", portfolioID)
	e.createTransaction(a, portfolioID, map[string]any{"type": "deposit", "date": "2025-12-31", "amount": "100000"})
	rec := e.do(a, http.MethodPost, path+"/deposits", depositBody("2026-01-01", "2026-02-02"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	id := decodeJSON[depositResponse](t, rec.Body.Bytes()).ID
	if rec := e.do(a, http.MethodPost, fmt.Sprintf("/api/deposits/%d/mature", id), nil); rec.Code != http.StatusOK {
		t.Fatalf("mature = %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE type = 'sell'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("sell rows = %d (err %v), want the maturity sell", n, err)
	}
	got := decodeJSON[tradesResponse](t, e.do(a, http.MethodGet, path+"/trades", nil).Body.Bytes())
	if len(got.Trades) != 0 {
		t.Errorf("trades = %+v, want none", got.Trades)
	}
}
