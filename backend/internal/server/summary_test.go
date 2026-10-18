package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// TestPortfolioSummaryCostAndPositionsValue checks that the positions'
// value and cost are summed unrounded and rounded once, not left for the
// page to derive from totals already rounded on their own:
//
//	deposit 10, buy 1 @ 1.005, price 1.005
//	=> cash 8.995 (9.00), total 10.000 (10.00), unrealized 0
//	=> 10.00 - 9.00 - 0.00 = 1.00, but value and cost are 1.005 => 1.01
//
// Both the portfolio and the all scope answer the same.
func TestPortfolioSummaryCostAndPositionsValue(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TTE', 'TEFAS', 'Test Fund', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2026-09-01", Amount: decimal.RequireFromString("10"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-02", Quantity: decimal.RequireFromString("1"), Price: decimal.RequireFromString("1.005"), Amount: decimal.RequireFromString("1.005"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := price.NewRepo(conn).Upsert(context.Background(), instrumentID, "2026-09-02", "1.005", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	for _, id := range []string{fmt.Sprint(pf.ID), "all"} {
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/summary", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body: %s", id, rec.Code, rec.Body.String())
		}
		// A map, not summaryResponse, so a missing field fails here rather
		// than at compile time.
		var got map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got["total_value"] != "10.00" || got["cash"] != "9.00" || got["unrealized_pl"] != "0.00" {
			t.Errorf("%s: {total_value, cash, unrealized_pl} = {%v, %v, %v}, want {10.00, 9.00, 0.00}", id, got["total_value"], got["cash"], got["unrealized_pl"])
		}
		if got["positions_value"] != "1.01" {
			t.Errorf("%s: positions_value = %v, want 1.01", id, got["positions_value"])
		}
		if got["cost_basis"] != "1.01" {
			t.Errorf("%s: cost_basis = %v, want 1.01", id, got["cost_basis"])
		}
	}
}

// TestPortfolioSummaryDerivedTotals checks that a position's total
// P/L and the stats' net contribution and net dividends are worked out
// unrounded and rounded once, not left for the page to add or subtract from
// parts the API already rounded on their own:
//
//	deposit 50000, buy 40 @ 270 fee 5, sell 15 @ 300 fee 2, price 331.20,
//	dividend 101 fee 15.15, deposit 101, withdrawal 15.15
//	=> unrealized 1526.875 (1526.88) + realized 531.975 (531.98) = 2058.85
//	USD 41.50: deposits 1207.25 - withdrawals 0.37 = 1206.88, but
//	(50101 - 15.15) / 41.50 = 1206.89; gross 2.43 - tax 0.37 = 2.06, but
//	(101 - 15.15) / 41.50 = 2.07
func TestPortfolioSummaryDerivedTotals(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('THYAO', 'BIST', 'THY', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	thyao, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	ctx := context.Background()
	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	users := user.NewRepo(conn)
	usr, err := users.Create(ctx, "derived@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2026-02-01", Amount: d("50000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &thyao, Type: transaction.TypeBuy, Date: "2026-02-03", Quantity: d("40"), Price: d("270"), Amount: d("10800"), Fee: d("5"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &thyao, Type: transaction.TypeSell, Date: "2026-08-20", Quantity: d("15"), Price: d("300"), Amount: d("4500"), Fee: d("2"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &thyao, Type: transaction.TypeDividend, Date: "2026-09-01", Amount: d("101"), Fee: d("15.15"), Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2026-09-02", Amount: d("101"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, Type: transaction.TypeWithdrawal, Date: "2026-09-03", Amount: d("15.15"), Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := price.NewRepo(conn).Upsert(ctx, thyao, "2026-10-12", "331.20", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := fx.NewRepo(conn).Upsert(ctx, "USD", "2026-01-02", "41.50", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	get := func() summaryResponse {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}
	str := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}

	got := get()
	if len(got.Positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(got.Positions))
	}
	p := got.Positions[0]
	if str(p.UnrealizedPLReporting) != "1526.88" || str(p.RealizedPLReporting) != "531.98" {
		t.Errorf("{unrealized, realized}_pl_reporting = {%s, %s}, want {1526.88, 531.98}", str(p.UnrealizedPLReporting), str(p.RealizedPLReporting))
	}
	if str(p.TotalPLReporting) != "2058.85" {
		t.Errorf("total_pl_reporting = %s, want 2058.85", str(p.TotalPLReporting))
	}

	if err := users.UpdateReportingCurrency(ctx, usr.ID, "USD"); err != nil {
		t.Fatalf("users.UpdateReportingCurrency: %v", err)
	}
	r := get().Stats.Reporting
	if r == nil {
		t.Fatal("stats.reporting = null, want USD totals")
	}
	if r.Deposits != "1207.25" || r.Withdrawals != "0.37" || r.NetContribution != "1206.89" {
		t.Errorf("{deposits, withdrawals, net_contribution} = {%s, %s, %s}, want {1207.25, 0.37, 1206.89}", r.Deposits, r.Withdrawals, r.NetContribution)
	}
	if r.DividendsGross != "2.43" || r.DividendsTax != "0.37" || r.DividendsNet != "2.07" {
		t.Errorf("{dividends_gross, dividends_tax, dividends_net} = {%s, %s, %s}, want {2.43, 0.37, 2.07}", r.DividendsGross, r.DividendsTax, r.DividendsNet)
	}
}

// TestPortfolioSummaryDayChangeTotal checks that the summary's
// day_change is summed unrounded and rounded once, not totalled from the rows
// already rounded on their own:
//
//	deposit 10, buy 1 A @ 1 and 1 B @ 1, each closing 1 then 1.005
//	=> each row's day_change_reporting 0.005 (0.01), total 0.010 => 0.01
//
// Before the second close no position has a previous close, so day_change
// is null rather than a zero. Both the portfolio and the all scope answer
// the same.
func TestPortfolioSummaryDayChangeTotal(t *testing.T) {
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
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	ctx := context.Background()
	transactions := transaction.NewRepo(conn)
	if _, err := transactions.Create(ctx, usr.ID, transaction.Transaction{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: "2026-09-01", Amount: decimal.RequireFromString("10"), Fee: decimal.Zero, Currency: "TRY"}); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
	prices := price.NewRepo(conn)
	var ids []int64
	for _, symbol := range []string{"TTA", "TTB"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', ?, 'TRY')`, symbol, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		ids = append(ids, id)
		tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2026-09-02", Quantity: decimal.RequireFromString("1"), Price: decimal.RequireFromString("1"), Amount: decimal.RequireFromString("1"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
		if err := prices.Upsert(ctx, id, "2026-09-02", "1", "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}

	dayChange := func(id string) any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/summary", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body: %s", id, rec.Code, rec.Body.String())
		}
		// A map, not summaryResponse, so a missing field reads as absent
		// rather than failing to compile.
		var got map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		v, ok := got["day_change"]
		if !ok {
			return "absent"
		}
		return v
	}

	for _, id := range []string{fmt.Sprint(pf.ID), "all"} {
		if got := dayChange(id); got != nil {
			t.Errorf("%s: day_change with no previous close = %v, want null", id, got)
		}
	}
	for _, id := range ids {
		if err := prices.Upsert(ctx, id, "2026-10-08", "1.005", "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	for _, id := range []string{fmt.Sprint(pf.ID), "all"} {
		if got := dayChange(id); got != "0.01" {
			t.Errorf("%s: day_change = %v, want 0.01", id, got)
		}
	}
}

// TestPortfolioSummary seeds two buys and a fetched price directly through
// the repos, then checks the numbers the holdings engine would compute by
// hand:
//
//	buy 10 @ 5 (fee 0): cost basis 50
//	buy 10 @ 7 (fee 0): cost basis 70
//	=> quantity 20, cost basis 120, avg cost 6
//	fetched price 8 => market value 20*8 = 160
//	unrealized_pl = 160 - 120 = 40
//	cash = -50 - 70 = -120
//	total_value = cash + market value = -120 + 160 = 40
func TestPortfolioSummary(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	buys := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("7"), Amount: decimal.RequireFromString("70"), Fee: decimal.Zero, Currency: "TRY"},
	}
	for _, tx := range buys {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), instrumentID, "2024-01-03", "8", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.TotalValue != "40.00" {
		t.Errorf("total_value = %s, want 40.00", got.TotalValue)
	}
	if got.Cash != "-120.00" {
		t.Errorf("cash = %s, want -120.00", got.Cash)
	}
	if got.FXPL != nil {
		t.Errorf("fx_pl = %s, want null (TRY only)", *got.FXPL)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("len(positions) = %d, want 1", len(got.Positions))
	}
	pos := got.Positions[0]
	if pos.UnrealizedPL != "40.00" {
		t.Errorf("position unrealized_pl = %s, want 40.00", pos.UnrealizedPL)
	}
	if !pos.HasPrice {
		t.Errorf("has_price = false, want true")
	}
	if pos.LastPrice == nil || *pos.LastPrice != "8" {
		t.Errorf("last_price = %v, want 8", pos.LastPrice)
	}
	if pos.Symbol != "TEST" {
		t.Errorf("symbol = %s, want TEST", pos.Symbol)
	}
	if pos.Quantity != "20" {
		t.Errorf("quantity = %s, want 20", pos.Quantity)
	}
}

// TestPortfolioSummaryQuantityKeepsScale checks that a fractional-share
// quantity comes back with its exact scale, not rounded to 6 fraction
// digits the way the money fields are.
func TestPortfolioSummaryQuantityKeepsScale(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('VTI', 'US', 'Vanguard Total Stock Market ETF', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "frac@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("0.006218342"), Price: decimal.RequireFromString("331.82"), Amount: decimal.RequireFromString("2.06"), Fee: decimal.Zero, Currency: "USD"}
	if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("len(positions) = %d, want 1", len(got.Positions))
	}
	if q := got.Positions[0].Quantity; q != "0.006218342" {
		t.Errorf("quantity = %s, want 0.006218342", q)
	}
}

// TestPortfolioSummaryAssetClass checks that a position carries its
// instrument's stored asset class, and null when the instrument has none. It
// also checks the sector key: mapped from the raw provider string, beaten by
// an override, and unknown without data.
func TestPortfolioSummaryAssetClass(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(ctx, "class@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	for _, in := range []struct {
		symbol, market string
		class          any
		sector         any
		override       any
	}{
		{"GLD", "BIST", "commodity", nil, nil},
		{"PLAIN", "BIST", nil, nil, nil},
		{"BANK", "US", nil, "Banking", nil},
		{"OVER", "US", nil, "Banking", "energy"},
		{"NOSEC", "US", nil, "", nil},
	} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency, asset_class, sector, sector_override) VALUES (?, ?, ?, 'TRY', ?, ?, ?)`, in.symbol, in.market, in.symbol, in.class, in.sector, in.override)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("1"), Price: decimal.RequireFromString("10"), Amount: decimal.RequireFromString("10"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	bySymbol := map[string]positionResponse{}
	for _, p := range got.Positions {
		bySymbol[p.Symbol] = p
	}
	if c := bySymbol["GLD"].AssetClass; c == nil || *c != "commodity" {
		t.Errorf("GLD asset_class = %v, want commodity", c)
	}
	if c := bySymbol["PLAIN"].AssetClass; c != nil {
		t.Errorf("PLAIN asset_class = %q, want null", *c)
	}
	for symbol, want := range map[string]string{"BANK": "financials", "OVER": "energy", "NOSEC": "unknown", "PLAIN": "unknown"} {
		if got := bySymbol[symbol].Sector; got != want {
			t.Errorf("%s sector = %q, want %q", symbol, got, want)
		}
	}
}

// TestPortfolioSummaryUnitPricesKeepScale checks that a TEFAS NAV with 6
// decimals comes back exact instead of rounded to 2, and an avg cost that
// divides into a repeating decimal (10 / 3) is capped at 9 fraction digits.
func TestPortfolioSummaryUnitPricesKeepScale(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('MAC', 'TEFAS', 'Test Fund', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	usr, err := user.NewRepo(conn).Create(context.Background(), "nav@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("3"), Price: decimal.RequireFromString("3.333333"), Amount: decimal.RequireFromString("10"), Fee: decimal.Zero, Currency: "TRY"}
	if _, err := transaction.NewRepo(conn).Create(context.Background(), usr.ID, tx); err != nil {
		t.Fatalf("transactions.Create: %v", err)
	}
	if err := price.NewRepo(conn).Upsert(context.Background(), instrumentID, "2024-01-02", "0.712194", "tefas"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("len(positions) = %d, want 1", len(got.Positions))
	}
	pos := got.Positions[0]
	if pos.LastPrice == nil || *pos.LastPrice != "0.712194" {
		t.Errorf("last_price = %v, want 0.712194", pos.LastPrice)
	}
	if pos.AvgCost != "3.333333333" {
		t.Errorf("avg_cost = %s, want 3.333333333", pos.AvgCost)
	}
	if pos.MarketValue != "2.14" {
		t.Errorf("market_value = %s, want 2.14 (money stays 2dp)", pos.MarketValue)
	}
}

// TestPortfolioSummaryManualPriceIsPerUser checks that, at the
// summary endpoint, a manual price overrides the fetched price for the
// same date, but only for the user who entered it -- a second user with an
// identical portfolio still sees the fetched price.
func TestPortfolioSummaryManualPriceIsPerUser(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	portfolios := portfolio.NewRepo(conn)
	transactions := transaction.NewRepo(conn)

	newUserWithBuy := func(email string) (*http.Cookie, int64) {
		usr, err := users.Create(context.Background(), email, "hash")
		if err != nil {
			t.Fatalf("users.Create: %v", err)
		}
		session, err := sessions.CreateSession(context.Background(), usr.ID)
		if err != nil {
			t.Fatalf("sessions.CreateSession: %v", err)
		}
		pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
		if err != nil {
			t.Fatalf("portfolios.Create: %v", err)
		}
		tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
		return &http.Cookie{Name: "session", Value: session.ID}, pf.ID
	}

	aliceCookie, alicePfID := newUserWithBuy("alice2@example.com")
	bobCookie, bobPfID := newUserWithBuy("bob2@example.com")

	prices := price.NewRepo(conn)
	// "bist", not "manual": the shared prices table only ever
	// holds fetched rows, and price_source below must tell the two apart.
	if err := prices.Upsert(context.Background(), instrumentID, "2024-01-03", "8", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	manual := price.NewManualRepo(conn)
	usr, err := users.GetByEmail(context.Background(), "alice2@example.com")
	if err != nil {
		t.Fatalf("users.GetByEmail: %v", err)
	}
	if err := manual.Upsert(context.Background(), usr.ID, instrumentID, "2024-01-03", "50"); err != nil {
		t.Fatalf("manual.Upsert: %v", err)
	}

	getPosition := func(cookie *http.Cookie, pfID int64) positionResponse {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pfID), nil)
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got.Positions) != 1 {
			t.Fatalf("len(positions) = %d, want 1", len(got.Positions))
		}
		return got.Positions[0]
	}

	alice := getPosition(aliceCookie, alicePfID)
	if alice.LastPrice == nil || *alice.LastPrice != "50" {
		t.Errorf("alice last_price = %v, want 50 (her manual entry overrides the fetched price)", alice.LastPrice)
	}
	// The page tags a manual price and deletes it by this date.
	if alice.PriceSource == nil || *alice.PriceSource != "manual" {
		t.Errorf("alice price_source = %v, want manual", alice.PriceSource)
	}
	if alice.PriceDate == nil || *alice.PriceDate != "2024-01-03" {
		t.Errorf("alice price_date = %v, want 2024-01-03", alice.PriceDate)
	}

	bob := getPosition(bobCookie, bobPfID)
	if bob.LastPrice == nil || *bob.LastPrice != "8" {
		t.Errorf("bob last_price = %v, want 8 (no manual entry of his own, sees fetched price)", bob.LastPrice)
	}
	if bob.PriceSource == nil || *bob.PriceSource != "bist" {
		t.Errorf("bob price_source = %v, want bist (nothing of his to tag or delete)", bob.PriceSource)
	}
}

// TestPortfolioSummaryMultiCurrency covers converting a mixed-currency
// portfolio into the user's (default TRY) reporting currency: a TRY
// position plus a USD position. Before any USD fx_rates row exists, the USD
// cash and position are excluded from totals and reported in missing_rates;
// once a rate is stored, they convert in using rate(USD)/rate(TRY).
func TestPortfolioSummaryMultiCurrency(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TRYX', 'BIST', 'TRY Inc', 'TRY')`)
	if err != nil {
		t.Fatalf("insert TRY instrument: %v", err)
	}
	tryID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("try instrument id: %v", err)
	}
	res, err = conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDX', 'US', 'USD Inc', 'USD')`)
	if err != nil {
		t.Fatalf("insert USD instrument: %v", err)
	}
	usdID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("usd instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "bob@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	txs := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &tryID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("5"), Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &usdID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("2"), Amount: decimal.RequireFromString("20"), Fee: decimal.Zero, Currency: "USD"},
	}
	for _, tx := range txs {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), tryID, "2024-01-03", "8", "test"); err != nil {
		t.Fatalf("prices.Upsert try: %v", err)
	}
	if err := prices.Upsert(context.Background(), usdID, "2024-01-03", "3", "test"); err != nil {
		t.Fatalf("prices.Upsert usd: %v", err)
	}

	get := func() summaryResponse {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}

	// Before any USD rate exists: USD cash/position excluded from totals.
	// TRY only: cash -50 + market value (10 * 8) = 30.
	got := get()
	if got.Currency != "TRY" {
		t.Errorf("currency = %s, want TRY", got.Currency)
	}
	if _, ok := got.CashByCurrencyReporting["USD"]; ok || got.CashByCurrencyReporting["TRY"] != "-50.00" {
		t.Errorf("cash_by_currency_reporting = %v, want only TRY -50.00 without a USD rate", got.CashByCurrencyReporting)
	}
	if len(got.MissingRates) != 1 || got.MissingRates[0] != "USD" {
		t.Errorf("missing_rates = %v, want [USD]", got.MissingRates)
	}
	if got.TotalValue != "30.00" {
		t.Errorf("total_value = %s, want 30.00", got.TotalValue)
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	// With USD = 40: TRY side unchanged (30.00); USD side (cash -20 +
	// market value 10*3=30) * 40 = 400.00. Total 430.00.
	got = get()
	if len(got.MissingRates) != 0 {
		t.Errorf("missing_rates = %v, want empty", got.MissingRates)
	}
	if got.CashByCurrency["TRY"] != "-50.00" || got.CashByCurrency["USD"] != "-20.00" {
		t.Errorf("cash_by_currency = %v, want TRY -50.00, USD -20.00", got.CashByCurrency)
	}
	if got.CashByCurrencyReporting["TRY"] != "-50.00" || got.CashByCurrencyReporting["USD"] != "-800.00" {
		t.Errorf("cash_by_currency_reporting = %v, want TRY -50.00, USD -800.00", got.CashByCurrencyReporting)
	}
	if got.Cash != "-850.00" { // -50*1 + -20*40
		t.Errorf("cash = %s, want -850.00", got.Cash)
	}
	if got.TotalValue != "430.00" {
		t.Errorf("total_value = %s, want 430.00", got.TotalValue)
	}

	var usdPos *positionResponse
	for i := range got.Positions {
		if got.Positions[i].Symbol == "USDX" {
			usdPos = &got.Positions[i]
		}
	}
	if usdPos == nil {
		t.Fatal("USDX position not found in response")
	}
	if usdPos.Currency != "USD" {
		t.Errorf("USDX currency = %s, want USD", usdPos.Currency)
	}
	if usdPos.Market != "US" {
		t.Errorf("USDX market = %s, want US", usdPos.Market)
	}
	if usdPos.MarketValueReporting == nil || *usdPos.MarketValueReporting != "1200.00" { // 30 * 40
		t.Errorf("USDX market_value_reporting = %v, want 1200.00", usdPos.MarketValueReporting)
	}

	// Splits per position. Seeded last so the value assertions
	// above stay valid once splits change quantities. Only the
	// split after the first buy (2024-01-01) and not after today counts;
	// one on the buy's own date is already priced in.
	splits := split.NewRepo(conn)
	for _, s := range []struct{ date, ratio string }{
		{"2023-06-01", "4"},
		{"2024-01-01", "3"},
		{"2024-01-02", "2"},
		{"2099-01-01", "5"},
	} {
		if err := splits.Upsert(context.Background(), usdID, s.date, decimal.RequireFromString(s.ratio), "manual"); err != nil {
			t.Fatalf("splits.Upsert %s: %v", s.date, err)
		}
	}
	got = get()
	for _, p := range got.Positions {
		switch p.Symbol {
		case "USDX":
			if len(p.Splits) != 1 || p.Splits[0] != (splitResponse{Date: "2024-01-02", Ratio: "2"}) {
				t.Errorf("USDX splits = %+v, want [{2024-01-02 2}]", p.Splits)
			}
		case "TRYX":
			if p.Splits == nil || len(p.Splits) != 0 {
				t.Errorf("TRYX splits = %#v, want empty non-null array", p.Splits)
			}
		}
	}
}

// TestPortfolioSummaryNotOwned covers that a portfolio belonging to another
// user 404s instead of leaking its summary.
func TestPortfolioSummaryNotOwned(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	owner, err := users.Create(context.Background(), "owner@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create owner: %v", err)
	}
	intruder, err := users.Create(context.Background(), "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create intruder: %v", err)
	}
	intruderSession, err := sessions.CreateSession(context.Background(), intruder.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), owner.ID, "Owner's portfolio")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: intruderSession.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestPortfolioSummaryClosedPositionRealizedPL checks that a
// position sold down to zero keeps its realized P/L, and a dividend on the
// instrument afterwards, in the portfolio's realized_pl -- and is listed in
// positions with zero quantity and value so the table adds up to that same
// figure.
//
//	buy 10 @ 100 fee 5  => cost basis 1005, avg cost 100.5
//	sell 10 @ 120 fee 5 => realized 1200 - 5 - 1005 = 190
//	dividend 50         => realized 190 + 50 = 240
//	cash = -1005 + 1195 + 50 = 240
func TestPortfolioSummaryClosedPositionRealizedPL(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('TEST', 'BIST', 'Test A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "closed@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	txs := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("100"), Amount: decimal.RequireFromString("1000"), Fee: decimal.RequireFromString("5"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-01-02", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("120"), Amount: decimal.RequireFromString("1200"), Fee: decimal.RequireFromString("5"), Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeDividend, Date: "2024-01-03", Amount: decimal.RequireFromString("50"), Fee: decimal.Zero, Currency: "TRY"},
	}
	for _, tx := range txs {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	// A split after the full sell changes no quantity, so it is not listed
	// on the closed position.
	if err := split.NewRepo(conn).Upsert(context.Background(), instrumentID, "2024-01-05", decimal.RequireFromString("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// The closed position is listed with zero quantity and value but its
	// realized P/L set, so the positions table can total the same figure as
	// the realized_pl card.
	if len(got.Positions) != 1 {
		t.Fatalf("len(positions) = %d, want 1 (the closed position)", len(got.Positions))
	}
	closed := got.Positions[0]
	if closed.Quantity != "0" || closed.MarketValue != "0.00" || closed.RealizedPL != "240.00" {
		t.Errorf("closed position = {quantity %s, market_value %s, realized_pl %s}, want {0, 0.00, 240.00}", closed.Quantity, closed.MarketValue, closed.RealizedPL)
	}
	if !closed.Closed {
		t.Errorf("closed position closed = false, want true")
	}
	if closed.Splits == nil || len(closed.Splits) != 0 {
		t.Errorf("closed splits = %#v, want empty non-null array", closed.Splits)
	}
	if closed.RealizedPLReporting == nil || *closed.RealizedPLReporting != "240.00" {
		t.Errorf("closed realized_pl_reporting = %v, want 240.00", closed.RealizedPLReporting)
	}
	if got.RealizedPL == nil || *got.RealizedPL != "240.00" {
		t.Errorf("realized_pl = %v, want 240.00", got.RealizedPL)
	}
	if got.Cash != "240.00" {
		t.Errorf("cash = %s, want 240.00", got.Cash)
	}
}

// TestPortfolioSummaryPLAllCurrenciesExcluded checks that with a USD
// position and no USD rate, every amount contributing to unrealized and
// realized P/L is excluded from the reporting-currency totals, so both are
// null rather than a 0.00 that reads as "no profit". Once the rate exists
// they are numbers again.
//
//	buy 10 @ 2 => cost basis 20, avg cost 2
//	sell 5 @ 4 => realized 20 - 10 = 10
//	price 3    => market value 15, unrealized 15 - 10 = 5
func TestPortfolioSummaryPLAllCurrenciesExcluded(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDX', 'US', 'USD Inc', 'USD')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	usdID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "norate@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	pf, err := portfolios.Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	txs := []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &usdID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("2"), Amount: decimal.RequireFromString("20"), Fee: decimal.Zero, Currency: "USD"},
		{PortfolioID: pf.ID, InstrumentID: &usdID, Type: transaction.TypeSell, Date: "2024-01-02", Quantity: decimal.RequireFromString("5"), Price: decimal.RequireFromString("4"), Amount: decimal.RequireFromString("20"), Fee: decimal.Zero, Currency: "USD"},
	}
	for _, tx := range txs {
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	prices := price.NewRepo(conn)
	if err := prices.Upsert(context.Background(), usdID, "2024-01-03", "3", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	get := func() summaryResponse {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}

	got := get()
	if got.UnrealizedPL != nil {
		t.Errorf("unrealized_pl = %s, want null (USD excluded for lack of a rate)", *got.UnrealizedPL)
	}
	if got.RealizedPL != nil {
		t.Errorf("realized_pl = %s, want null (USD excluded for lack of a rate)", *got.RealizedPL)
	}

	fxRates := fx.NewRepo(conn)
	if err := fxRates.Upsert(context.Background(), "USD", "2024-01-03", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}

	got = get()
	if got.UnrealizedPL == nil || *got.UnrealizedPL != "200.00" {
		t.Errorf("unrealized_pl = %v, want 200.00", got.UnrealizedPL)
	}
	if got.RealizedPL == nil || *got.RealizedPL != "400.00" {
		t.Errorf("realized_pl = %v, want 400.00", got.RealizedPL)
	}
}

// TestPortfolioSummaryFXPL checks that a $1,000 deposit made at 40 and
// valued at 48 today has no unrealized or realized P/L, and its ₺8,000 gain
// comes back as fx_pl. A $10 fee paid at 44 is a cost, not a currency move:
// it leaves only its own ₺40 currency loss behind (10 x (48 - 44)).
func TestPortfolioSummaryFXPL(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)

	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(context.Background(), "fx@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}

	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "USD cash")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	fxRates := fx.NewRepo(conn)
	for date, rate := range map[string]string{"2024-01-01": "40", "2024-03-01": "44", "2024-06-01": "48"} {
		if err := fxRates.Upsert(context.Background(), "USD", date, rate, "tcmb"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	transactions := transaction.NewRepo(conn)
	add := func(tx transaction.Transaction) {
		tx.PortfolioID = pf.ID
		tx.Currency = "USD"
		if _, err := transactions.Create(context.Background(), usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	get := func() summaryResponse {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got
	}

	add(transaction.Transaction{Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero})
	got := get()
	if got.TotalValue != "48000.00" {
		t.Errorf("total_value = %s, want 48000.00", got.TotalValue)
	}
	if got.FXPL == nil || *got.FXPL != "8000.00" {
		t.Errorf("fx_pl = %v, want 8000.00", got.FXPL)
	}

	add(transaction.Transaction{Type: transaction.TypeFee, Date: "2024-03-01", Amount: decimal.RequireFromString("10"), Fee: decimal.Zero})
	got = get()
	if got.FXPL == nil || *got.FXPL != "7960.00" {
		t.Errorf("fx_pl after fee = %v, want 7960.00", got.FXPL)
	}
}

// TestPortfolioSummaryPositionFXPL covers the FX P/L of a position.
// Reporting TRY, USD 40 on day 1 and 48 today:
//
//	deposit $1,000, buy 10 AAPL @ $50 on day 1, AAPL $60 today
//	=> unrealized_pl_reporting 100 x 48 = 4800
//	   fx_pl_reporting 500 x 48 - 500 x 40 = 4000
//	   portfolio fx_pl = 4000 (position) + 500 x (48 - 40) (cash) = 8000
//	buy 10 THYAO @ 10 TRY => fx_pl_reporting null
func TestPortfolioSummaryPositionFXPL(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "posfx@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	ctx := context.Background()
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Mixed")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	fxRates := fx.NewRepo(conn)
	for date, rate := range map[string]string{"2024-01-01": "40", "2024-06-01": "48"} {
		if err := fxRates.Upsert(ctx, "USD", date, rate, "tcmb"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	instrumentID := func(symbol, market, currency string) int64 {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, symbol, market, symbol, currency)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		return id
	}
	aapl := instrumentID("AAPL", "US", "USD")
	thyao := instrumentID("THYAO", "BIST", "TRY")
	if err := price.NewRepo(conn).Upsert(ctx, aapl, "2024-06-01", "60", "test"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}

	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: decimal.RequireFromString("1000"), Currency: "USD"},
		{Type: transaction.TypeBuy, InstrumentID: &aapl, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("50"), Amount: decimal.RequireFromString("500"), Currency: "USD"},
		{Type: transaction.TypeBuy, InstrumentID: &thyao, Date: "2024-01-01", Quantity: decimal.RequireFromString("10"), Price: decimal.RequireFromString("10"), Amount: decimal.RequireFromString("100"), Currency: "TRY"},
	} {
		tx.PortfolioID = pf.ID
		tx.Fee = decimal.Zero
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	if len(got.Positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(got.Positions))
	}
	for _, p := range got.Positions {
		switch p.Symbol {
		case "AAPL":
			if str(p.UnrealizedPLReporting) != "4800.00" {
				t.Errorf("AAPL unrealized_pl_reporting = %s, want 4800.00", str(p.UnrealizedPLReporting))
			}
			if str(p.FXPLReporting) != "4000.00" {
				t.Errorf("AAPL fx_pl_reporting = %s, want 4000.00", str(p.FXPLReporting))
			}
		case "THYAO":
			if p.FXPLReporting != nil {
				t.Errorf("THYAO fx_pl_reporting = %s, want null", *p.FXPLReporting)
			}
		}
	}
	if str(got.FXPL) != "8000.00" {
		t.Errorf("fx_pl = %s, want 8000.00", str(got.FXPL))
	}
}

// TestPortfolioSummaryDayChange covers the day change.
// 2024-01-05 is a Friday and 2024-01-08 the Monday after it; each position but
// NEW holds 10 bought earlier.
//
//	DAY:  fetched 100 Fri, fetched 103 + manual 105 Mon
//	      => prev_close 100 (Fri), day_change 10 x (105 - 100) = 50
//	ONCE: priced only on Mon => all four fields null
//	SPLT: fetched 100 Fri, 52 Mon, 2-for-1 split Mon => quantity 20,
//	      prev_close 100 / 2 = 50, day_change 20 x (52 - 50) = 40
//	NEW:  as SPLT, but 20 first bought on the Mon ex-date at 50, so the split
//	      never touches the position => still prev_close 50, day_change 40
//	GAP:  fetched 98 Thu, 100 Fri, 2-for-1 split Mon with no close on or
//	      after it => quantity 20 valued at 100 / 2 = 50, market_value 1000,
//	      prev_close 98 / 2 = 49, day_change 20 x (50 - 49) = 20
//	LATE: as GAP, but 20 first bought on the Mon ex-date, so the split never
//	      touches the position => the same values but day_change 0: the
//	      shares were not held on the Fri price date
func TestPortfolioSummaryDayChange(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "day@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	ctx := context.Background()
	ids := map[string]int64{}
	for _, b := range []struct{ symbol, date, quantity, price string }{
		{"DAY", "2024-01-02", "10", "90"}, {"ONCE", "2024-01-02", "10", "90"}, {"SPLT", "2024-01-02", "10", "90"}, {"NEW", "2024-01-08", "20", "50"}, {"GAP", "2024-01-02", "10", "90"}, {"LATE", "2024-01-08", "20", "90"},
	} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, b.symbol, b.symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		ids[b.symbol] = id
		tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &id, Type: transaction.TypeBuy, Date: b.date, Quantity: decimal.RequireFromString(b.quantity), Price: decimal.RequireFromString(b.price), Amount: decimal.RequireFromString("900"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, p := range []struct{ symbol, date, close string }{
		{"DAY", "2024-01-05", "100"}, {"DAY", "2024-01-08", "103"},
		{"ONCE", "2024-01-08", "20"},
		{"SPLT", "2024-01-05", "100"}, {"SPLT", "2024-01-08", "52"},
		{"NEW", "2024-01-05", "100"}, {"NEW", "2024-01-08", "52"},
		{"GAP", "2024-01-04", "98"}, {"GAP", "2024-01-05", "100"},
		{"LATE", "2024-01-04", "98"}, {"LATE", "2024-01-05", "100"},
	} {
		if err := prices.Upsert(ctx, ids[p.symbol], p.date, p.close, "bist"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := price.NewManualRepo(conn).Upsert(ctx, usr.ID, ids["DAY"], "2024-01-08", "105"); err != nil {
		t.Fatalf("manual.Upsert: %v", err)
	}
	for _, symbol := range []string{"SPLT", "NEW", "GAP", "LATE"} {
		if err := split.NewRepo(conn).Upsert(ctx, ids[symbol], "2024-01-08", decimal.RequireFromString("2"), "manual"); err != nil {
			t.Fatalf("splits.Upsert: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	bySymbol := map[string]positionResponse{}
	for _, p := range got.Positions {
		bySymbol[p.Symbol] = p
	}

	str := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	for symbol, want := range map[string][5]string{
		"DAY":  {"100", "2024-01-05", "50.00", "50.00", "1000.00"},
		"ONCE": {"null", "null", "null", "null", "null"},
		"SPLT": {"50", "2024-01-05", "40.00", "40.00", "1000.00"},
		"NEW":  {"50", "2024-01-05", "40.00", "40.00", "1000.00"},
		"GAP":  {"49", "2024-01-04", "20.00", "20.00", "980.00"},
		"LATE": {"49", "2024-01-04", "0.00", "0.00", "0.00"},
	} {
		p := bySymbol[symbol]
		if fields := [5]string{str(p.PrevClose), str(p.PrevPriceDate), str(p.DayChange), str(p.DayChangeReporting), str(p.DayChangeBase)}; fields != want {
			t.Errorf("%s {prev_close, prev_price_date, day_change, day_change_reporting, day_change_base} = %v, want %v", symbol, fields, want)
		}
	}
	for _, symbol := range []string{"GAP", "LATE"} {
		if p := bySymbol[symbol]; str(p.LastPrice) != "50" || p.MarketValue != "1000.00" || p.UnrealizedPL != "100.00" {
			t.Errorf("%s {last_price, market_value, unrealized_pl} = {%s, %s, %s}, want {50, 1000.00, 100.00}", symbol, str(p.LastPrice), p.MarketValue, p.UnrealizedPL)
		}
	}
}

// TestPortfolioSummaryDayChangeSameDayTrades checks that shares traded on the
// price date move from their own price, not from prev_close: held before
// that date they move from prev_close, a buy moves from its price, a sell
// gives up the move after its price. 2024-01-05 is a Friday, 2024-01-08
// the Monday after it; each position's earlier buy is 10 @ 90 on 01-02.
//
//	ONLY: no earlier buy; buy 10 @ 102 Mon; 100 Fri, 105 Mon
//	      => day_change 10 x (105 - 102) = 30
//	MIX:  buy 10 @ 102 Mon, same prices => 10 x 5 + 10 x 3 = 80
//	SELL: sell 4 @ 104 Mon, same prices => 10 x 5 - 4 x (105 - 104) = 46
//	SPLT: buy 5 @ 52 Mon, 2-for-1 split Mon, 100 Fri (50), 53 Mon
//	      => 20 x (53 - 50) + 5 x (53 - 52) = 65
//	GAP:  buy 5 @ 99 Fri, 2-for-1 split Mon with no close after it, 98 Thu
//	      (49), 100 Fri (50) => 20 x (50 - 49) + 10 x (50 - 99 / 2) = 25
//	POSB: buy 5 @ 108 Tue, after the price date, same prices as ONLY
//	      => the 5 never moved with it, 10 x (105 - 100) = 50
//	POSS: sell 4 @ 110 Tue, same prices => the 4 sold still moved Mon,
//	      10 x (105 - 100) = 50
//	OUT:  sell all 10 @ 104 Mon, same prices => closed, row day_change null,
//	      but the total takes 10 x (104 - 100) = 40
//	OUTL: sell all 10 @ 110 Tue => closed, total takes 10 x (105 - 100) = 50
//	OLD:  sell all 10 @ 95 on 01-03, before the price date => closed, adds 0
//
// Each row's day_change_base is the value at the previous close of what day_change counts: ONLY 1020, MIX 2020, SELL 1000 (the 4 sold were held), SPLT 1260, GAP 1475, POSB 1000 (the 5 bought later were not), POSS 1000.
func TestPortfolioSummaryDayChangeSameDayTrades(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "sameday@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	ctx := context.Background()
	ids := map[string]int64{}
	for _, symbol := range []string{"ONLY", "MIX", "SELL", "SPLT", "GAP", "POSB", "POSS", "OUT", "OUTL", "OLD"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, 'TRY')`, symbol, symbol)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
	}
	for _, tr := range []struct{ symbol, typ, date, quantity, price string }{
		{"MIX", "buy", "2024-01-02", "10", "90"}, {"SELL", "buy", "2024-01-02", "10", "90"},
		{"SPLT", "buy", "2024-01-02", "10", "90"}, {"GAP", "buy", "2024-01-02", "10", "90"},
		{"ONLY", "buy", "2024-01-08", "10", "102"}, {"MIX", "buy", "2024-01-08", "10", "102"},
		{"SELL", "sell", "2024-01-08", "4", "104"}, {"SPLT", "buy", "2024-01-08", "5", "52"},
		{"GAP", "buy", "2024-01-05", "5", "99"},
		{"POSB", "buy", "2024-01-02", "10", "90"}, {"POSS", "buy", "2024-01-02", "10", "90"},
		{"POSB", "buy", "2024-01-09", "5", "108"}, {"POSS", "sell", "2024-01-09", "4", "110"},
		{"OUT", "buy", "2024-01-02", "10", "90"}, {"OUTL", "buy", "2024-01-02", "10", "90"}, {"OLD", "buy", "2024-01-02", "10", "90"},
		{"OLD", "sell", "2024-01-03", "10", "95"}, {"OUT", "sell", "2024-01-08", "10", "104"}, {"OUTL", "sell", "2024-01-09", "10", "110"},
	} {
		qty, px, id := decimal.RequireFromString(tr.quantity), decimal.RequireFromString(tr.price), ids[tr.symbol]
		tx := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &id, Type: tr.typ, Date: tr.date, Quantity: qty, Price: px, Amount: qty.Mul(px), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	prices := price.NewRepo(conn)
	for _, p := range []struct{ symbol, date, close string }{
		{"ONLY", "2024-01-05", "100"}, {"ONLY", "2024-01-08", "105"},
		{"MIX", "2024-01-05", "100"}, {"MIX", "2024-01-08", "105"},
		{"SELL", "2024-01-05", "100"}, {"SELL", "2024-01-08", "105"},
		{"SPLT", "2024-01-05", "100"}, {"SPLT", "2024-01-08", "53"},
		{"GAP", "2024-01-04", "98"}, {"GAP", "2024-01-05", "100"},
		{"POSB", "2024-01-05", "100"}, {"POSB", "2024-01-08", "105"},
		{"POSS", "2024-01-05", "100"}, {"POSS", "2024-01-08", "105"},
		{"OUT", "2024-01-05", "100"}, {"OUT", "2024-01-08", "105"},
		{"OUTL", "2024-01-05", "100"}, {"OUTL", "2024-01-08", "105"},
		{"OLD", "2024-01-05", "100"}, {"OLD", "2024-01-08", "105"},
	} {
		if err := prices.Upsert(ctx, ids[p.symbol], p.date, p.close, "bist"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	for _, symbol := range []string{"SPLT", "GAP"} {
		if err := split.NewRepo(conn).Upsert(ctx, ids[symbol], "2024-01-08", decimal.RequireFromString("2"), "manual"); err != nil {
			t.Fatalf("splits.Upsert: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	str := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	for _, p := range got.Positions {
		if p.Closed {
			if p.DayChange != nil || p.DayChangeReporting != nil || p.DayChangeBase != nil {
				t.Errorf("%s closed: day_change = %s, reporting %s, base %s, want null", p.Symbol, str(p.DayChange), str(p.DayChangeReporting), str(p.DayChangeBase))
			}
			continue
		}
		want := map[string]string{"ONLY": "30.00", "MIX": "80.00", "SELL": "46.00", "SPLT": "65.00", "GAP": "25.00", "POSB": "50.00", "POSS": "50.00"}[p.Symbol]
		if p.DayChange == nil || *p.DayChange != want || p.DayChangeReporting == nil || *p.DayChangeReporting != want {
			t.Errorf("%s day_change = %s, reporting %s, want %s", p.Symbol, str(p.DayChange), str(p.DayChangeReporting), want)
		}
		wantBase := map[string]string{"ONLY": "1020.00", "MIX": "2020.00", "SELL": "1000.00", "SPLT": "1260.00", "GAP": "1475.00", "POSB": "1000.00", "POSS": "1000.00"}[p.Symbol]
		if p.DayChangeBase == nil || *p.DayChangeBase != wantBase {
			t.Errorf("%s day_change_base = %s, want %s", p.Symbol, str(p.DayChangeBase), wantBase)
		}
	}
	if got.DayChange == nil || *got.DayChange != "436.00" {
		t.Errorf("total day_change = %s, want 436.00", str(got.DayChange))
	}
}

// TestPortfolioSummaryDayChangeBaseAll checks that day_change_base on
// /api/portfolios/all/summary counts the trades of every portfolio, and that
// it stays in the position's currency. 2024-01-05 is a Friday, 2024-01-08 the
// Monday after it; reporting TRY, USD 40, each close 100 Fri and 105 Mon.
//
//	XYZ:  A holds 10 @ 90, B holds 5 @ 90 and sells 5 @ 110 on Tue
//	      => quantity 10, day_change 15 x (105 - 100) = 75 (the 5 sold still
//	      moved Mon), day_change_base 15 x 100 = 1500
//	AAPL: A holds 10 @ $90 and sells 4 @ $110 on Tue (USD)
//	      => day_change $50 = 2000.00 TRY, day_change_base 10 x $100 = 1000.00
//	      in dollars, not 40000.00
func TestPortfolioSummaryDayChangeBaseAll(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	ctx := context.Background()
	usr, err := user.NewRepo(conn).Create(ctx, "baseall@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	portfolios := portfolio.NewRepo(conn)
	pfA, err := portfolios.Create(ctx, usr.ID, "A")
	if err != nil {
		t.Fatalf("portfolios.Create A: %v", err)
	}
	pfB, err := portfolios.Create(ctx, usr.ID, "B")
	if err != nil {
		t.Fatalf("portfolios.Create B: %v", err)
	}
	if err := fx.NewRepo(conn).Upsert(ctx, "USD", "2024-01-01", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}
	ids := map[string]int64{}
	prices := price.NewRepo(conn)
	for _, in := range []struct{ symbol, market, currency string }{{"XYZ", "BIST", "TRY"}, {"AAPL", "US", "USD"}} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, in.symbol, in.market, in.symbol, in.currency)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		if ids[in.symbol], err = res.LastInsertId(); err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		for date, close := range map[string]string{"2024-01-05": "100", "2024-01-08": "105"} {
			if err := prices.Upsert(ctx, ids[in.symbol], date, close, "test"); err != nil {
				t.Fatalf("prices.Upsert: %v", err)
			}
		}
	}
	d := decimal.RequireFromString
	xyz, aapl := ids["XYZ"], ids["AAPL"]
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pfA.ID, Type: transaction.TypeBuy, InstrumentID: &xyz, Date: "2024-01-02", Quantity: d("10"), Price: d("90"), Amount: d("900"), Currency: "TRY"},
		{PortfolioID: pfB.ID, Type: transaction.TypeBuy, InstrumentID: &xyz, Date: "2024-01-02", Quantity: d("5"), Price: d("90"), Amount: d("450"), Currency: "TRY"},
		{PortfolioID: pfB.ID, Type: transaction.TypeSell, InstrumentID: &xyz, Date: "2024-01-09", Quantity: d("5"), Price: d("110"), Amount: d("550"), Currency: "TRY"},
		{PortfolioID: pfA.ID, Type: transaction.TypeBuy, InstrumentID: &aapl, Date: "2024-01-02", Quantity: d("10"), Price: d("90"), Amount: d("900"), Currency: "USD"},
		{PortfolioID: pfA.ID, Type: transaction.TypeSell, InstrumentID: &aapl, Date: "2024-01-09", Quantity: d("4"), Price: d("110"), Amount: d("440"), Currency: "USD"},
	} {
		tx.Fee = decimal.Zero
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/api/portfolios/all/summary", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	str := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	want := map[string][3]string{"XYZ": {"75.00", "75.00", "1500.00"}, "AAPL": {"50.00", "2000.00", "1000.00"}}
	for _, p := range got.Positions {
		if fields := [3]string{str(p.DayChange), str(p.DayChangeReporting), str(p.DayChangeBase)}; fields != want[p.Symbol] {
			t.Errorf("%s {day_change, day_change_reporting, day_change_base} = %v, want %v", p.Symbol, fields, want[p.Symbol])
		}
	}
}

// TestPortfolioSummaryAll checks that /api/portfolios/all/summary sums
// the per-portfolio summaries. Reporting TRY, USD 40 / 44 / 48 on Jan 1 /
// Feb 1 / Jun 1; SPL splits 2-for-1 on Mar 1.
//
//	A: buy 10 SPL @ 10, buy 5 AAPL @ $50 (Jan 1), sell 4 SPL @ 15 (Apr 1)
//	   => SPL 10 x 2 - 4 = 16
//	B: buy 6 SPL @ 12, buy 5 AAPL @ $50 (Feb 1) => SPL 12
//	A: sell 3 CXL @ 20 (oversold), B: buy 3 CXL @ 20 (Feb 1), no price
//	all => SPL 28 (a split applied once per portfolio, not twice to each),
//	       AAPL fx_pl_reporting 250 x (48 - 40) + 250 x (48 - 44) = 3000,
//	       CXL quantity 0 but open, its cost 60 in total_value
func TestPortfolioSummaryAll(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	users := user.NewRepo(conn)
	usr, err := users.Create(context.Background(), "all@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	other, err := users.Create(context.Background(), "other@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create other: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	ctx := context.Background()
	portfolios := portfolio.NewRepo(conn)
	pfA, err := portfolios.Create(ctx, usr.ID, "A")
	if err != nil {
		t.Fatalf("portfolios.Create A: %v", err)
	}
	pfB, err := portfolios.Create(ctx, usr.ID, "B")
	if err != nil {
		t.Fatalf("portfolios.Create B: %v", err)
	}
	foreign, err := portfolios.Create(ctx, other.ID, "Other")
	if err != nil {
		t.Fatalf("portfolios.Create other: %v", err)
	}

	fxRates := fx.NewRepo(conn)
	for date, rate := range map[string]string{"2024-01-01": "40", "2024-02-01": "44", "2024-06-01": "48"} {
		if err := fxRates.Upsert(ctx, "USD", date, rate, "tcmb"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}
	instrumentID := func(symbol, market, currency string) int64 {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, symbol, market, symbol, currency)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		return id
	}
	spl := instrumentID("SPL", "BIST", "TRY")
	aapl := instrumentID("AAPL", "US", "USD")
	cxl := instrumentID("CXL", "BIST", "TRY")
	prices := price.NewRepo(conn)
	for id, close := range map[int64]string{spl: "8", aapl: "60"} {
		if err := prices.Upsert(ctx, id, "2024-06-01", close, "test"); err != nil {
			t.Fatalf("prices.Upsert: %v", err)
		}
	}
	if err := split.NewRepo(conn).Upsert(ctx, spl, "2024-03-01", decimal.RequireFromString("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pfA.ID, Type: transaction.TypeBuy, InstrumentID: &spl, Date: "2024-01-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{PortfolioID: pfA.ID, Type: transaction.TypeBuy, InstrumentID: &aapl, Date: "2024-01-01", Quantity: d("5"), Price: d("50"), Amount: d("250"), Currency: "USD"},
		{PortfolioID: pfA.ID, Type: transaction.TypeSell, InstrumentID: &spl, Date: "2024-04-01", Quantity: d("4"), Price: d("15"), Amount: d("60"), Currency: "TRY"},
		{PortfolioID: pfB.ID, Type: transaction.TypeBuy, InstrumentID: &spl, Date: "2024-02-01", Quantity: d("6"), Price: d("12"), Amount: d("72"), Currency: "TRY"},
		{PortfolioID: pfB.ID, Type: transaction.TypeBuy, InstrumentID: &aapl, Date: "2024-02-01", Quantity: d("5"), Price: d("50"), Amount: d("250"), Currency: "USD"},
		{PortfolioID: pfA.ID, Type: transaction.TypeSell, InstrumentID: &cxl, Date: "2024-02-01", Quantity: d("3"), Price: d("20"), Amount: d("60"), Currency: "TRY"},
		{PortfolioID: pfB.ID, Type: transaction.TypeBuy, InstrumentID: &cxl, Date: "2024-02-01", Quantity: d("3"), Price: d("20"), Amount: d("60"), Currency: "TRY"},
	} {
		tx.Fee = decimal.Zero
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	get := func(id string) (int, summaryResponse) {
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/summary", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		var got summaryResponse
		if rec.Code == http.StatusOK {
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
		}
		return rec.Code, got
	}
	if code, _ := get(fmt.Sprint(foreign.ID)); code != http.StatusNotFound {
		t.Errorf("other user's portfolio: status = %d, want %d", code, http.StatusNotFound)
	}
	var parts []summaryResponse
	for _, id := range []string{fmt.Sprint(pfA.ID), fmt.Sprint(pfB.ID), "all"} {
		code, got := get(id)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", id, code, http.StatusOK)
		}
		parts = append(parts, got)
	}
	a, b, all := parts[0], parts[1], parts[2]

	num := func(s *string) decimal.Decimal {
		if s == nil {
			t.Fatalf("unexpected null")
		}
		return d(*s)
	}
	pos := func(s summaryResponse, symbol string) positionResponse {
		for _, p := range s.Positions {
			if p.Symbol == symbol {
				return p
			}
		}
		t.Fatalf("no %s position in %+v", symbol, s.Positions)
		return positionResponse{}
	}

	if q := pos(all, "SPL").Quantity; !d(q).Equal(d("28")) || !d(q).Equal(d(pos(a, "SPL").Quantity).Add(d(pos(b, "SPL").Quantity))) {
		t.Errorf("all SPL quantity = %s, want 28 = %s + %s", q, pos(a, "SPL").Quantity, pos(b, "SPL").Quantity)
	}
	if got, want := num(all.RealizedPL), num(a.RealizedPL).Add(num(b.RealizedPL)); !got.Equal(want) {
		t.Errorf("all realized_pl = %s, want %s", got, want)
	}
	if len(all.CashByCurrency) != 2 {
		t.Errorf("all cash_by_currency = %v, want TRY and USD", all.CashByCurrency)
	}
	for c, v := range all.CashByCurrency {
		want := decimal.Zero
		for _, s := range []summaryResponse{a, b} {
			if w, ok := s.CashByCurrency[c]; ok {
				want = want.Add(d(w))
			}
		}
		if !d(v).Equal(want) {
			t.Errorf("all cash_by_currency[%s] = %s, want %s", c, v, want)
		}
	}
	fxA, fxB := num(pos(a, "AAPL").FXPLReporting), num(pos(b, "AAPL").FXPLReporting)
	if got := num(pos(all, "AAPL").FXPLReporting); !got.Equal(d("3000")) || !got.Equal(fxA.Add(fxB)) {
		t.Errorf("all AAPL fx_pl_reporting = %s, want 3000 = %s + %s", got, fxA, fxB)
	}
	if c := pos(all, "CXL"); c.Quantity != "0" || c.Closed || c.CostBasis != "60.00" {
		t.Errorf("all CXL = {quantity %s, closed %v, cost_basis %s}, want {0, false, 60.00}", c.Quantity, c.Closed, c.CostBasis)
	}
	if got, want := d(all.TotalValue), d(a.TotalValue).Add(d(b.TotalValue)); !got.Equal(want) {
		t.Errorf("all total_value = %s, want %s", got, want)
	}
}

// TestPortfolioSummaryStats covers stats per currency, null in the
// reporting currency while a nonzero currency has no rate, summed over
// portfolios for "all", and the earliest transaction date across them.
func TestPortfolioSummaryStats(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "stats@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	ctx := context.Background()
	portfolios := portfolio.NewRepo(conn)
	var pfs []portfolio.Portfolio
	for _, name := range []string{"A", "B", "Empty"} {
		pf, err := portfolios.Create(ctx, usr.ID, name)
		if err != nil {
			t.Fatalf("portfolios.Create %s: %v", name, err)
		}
		pfs = append(pfs, pf)
	}
	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('THYAO', 'BIST', 'THY', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	thyao, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pfs[0].ID, Type: transaction.TypeDeposit, Date: "2024-02-01", Amount: d("1000"), Fee: d("1"), Currency: "TRY"},
		{PortfolioID: pfs[0].ID, Type: transaction.TypeDeposit, Date: "2024-03-01", Amount: d("100"), Fee: d("0"), Currency: "USD"},
		{PortfolioID: pfs[1].ID, Type: transaction.TypeDeposit, Date: "2024-01-15", Amount: d("500"), Fee: d("0"), Currency: "TRY"},
		{PortfolioID: pfs[1].ID, Type: transaction.TypeDividend, InstrumentID: &thyao, Date: "2024-04-01", Amount: d("100"), Fee: d("15"), Currency: "TRY"},
		{PortfolioID: pfs[1].ID, Type: transaction.TypeFee, Date: "2024-04-02", Amount: d("10"), Fee: d("0"), Currency: "TRY"},
	} {
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	get := func(id string) statsResponse {
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/summary", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d; body: %s", id, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return got.Stats
	}

	all := get("all")
	if all.Reporting != nil {
		t.Errorf("reporting = %+v, want null (USD has no rate)", *all.Reporting)
	}
	if got := all.ByCurrency["USD"].Deposits; got != "100.00" {
		t.Errorf("by_currency.USD.deposits = %q, want 100.00", got)
	}
	want := statsFields{Deposits: "1500.00", Withdrawals: "0.00", NetContribution: "1500.00", Fees: "11.00", DividendsGross: "100.00", DividendsTax: "15.00", DividendsNet: "85.00", Interest: "0.00"}
	if got := all.ByCurrency["TRY"]; got != want {
		t.Errorf("all by_currency.TRY = %+v, want %+v", got, want)
	}
	if all.FirstTransactionDate == nil || *all.FirstTransactionDate != "2024-01-15" {
		t.Errorf("all first_transaction_date = %v, want 2024-01-15", all.FirstTransactionDate)
	}
	if a := get(fmt.Sprint(pfs[0].ID)); a.FirstTransactionDate == nil || *a.FirstTransactionDate != "2024-02-01" {
		t.Errorf("A first_transaction_date = %v, want 2024-02-01", a.FirstTransactionDate)
	}
	if e := get(fmt.Sprint(pfs[2].ID)); e.FirstTransactionDate != nil || len(e.ByCurrency) != 0 {
		t.Errorf("empty portfolio stats = %+v, want no date and no currencies", e)
	}

	if err := fx.NewRepo(conn).Upsert(ctx, "USD", "2024-03-01", "40", "tcmb"); err != nil {
		t.Fatalf("fx.Upsert: %v", err)
	}
	all = get("all")
	a, b := get(fmt.Sprint(pfs[0].ID)), get(fmt.Sprint(pfs[1].ID))
	if all.Reporting == nil || a.Reporting == nil || b.Reporting == nil {
		t.Fatalf("reporting null with every rate present: all %v, A %v, B %v", all.Reporting, a.Reporting, b.Reporting)
	}
	if got := all.Reporting.Deposits; got != "5500.00" || !d(got).Equal(d(a.Reporting.Deposits).Add(d(b.Reporting.Deposits))) {
		t.Errorf("all reporting.deposits = %s, want 5500.00 = %s + %s", got, a.Reporting.Deposits, b.Reporting.Deposits)
	}
}

// TestPortfolioSummaryBookedBonusKeepsPriceScaling checks that a
// zero-price buy within 10 days of a split's ex-date books that split's
// bonus shares, so the holdings engine skips the split, but the instrument's
// price is still scaled by it: prices are market data.
//
//	buy 100 @ 10 on Jan 2, split 2 on Jan 10, buy 100 @ 0 on Jan 12 => 200
//	close 20 on Jan 5, before the split => last price 20 / 2 = 10
func TestPortfolioSummaryBookedBonusKeepsPriceScaling(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('BNS', 'BIST', 'Bonus A.S.', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "bonus@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(context.Background(), usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	ctx := context.Background()
	for _, tx := range []transaction.Transaction{
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: decimal.RequireFromString("100"), Price: decimal.RequireFromString("10"), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"},
		{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-12", Quantity: decimal.RequireFromString("100"), Price: decimal.Zero, Amount: decimal.Zero, Fee: decimal.Zero, Currency: "TRY"},
	} {
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}
	if err := price.NewRepo(conn).Upsert(ctx, instrumentID, "2024-01-05", "20", "bist"); err != nil {
		t.Fatalf("prices.Upsert: %v", err)
	}
	if err := split.NewRepo(conn).Upsert(ctx, instrumentID, "2024-01-10", decimal.RequireFromString("2"), "manual"); err != nil {
		t.Fatalf("splits.Upsert: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got summaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("len(positions) = %d, want 1", len(got.Positions))
	}
	p := got.Positions[0]
	if p.Quantity != "200" || p.LastPrice == nil || *p.LastPrice != "10" || p.MarketValue != "2000.00" || len(p.Splits) != 0 {
		t.Errorf("position = {quantity %s, last_price %v, market_value %s, splits %+v}, want {200, 10, 2000.00, []}", p.Quantity, p.LastPrice, p.MarketValue, p.Splits)
	}
}

// TestPortfolioSummaryDeposit checks that a term deposit is valued from its
// terms: 1000 TRY at 36.5% with 20% stopaj, started ten days ago, closes at
// 1 + 10 x 0.0008 = 1.008, so market_value is 1008.00, the price is dated
// today with source "deposit", and the day change is one day's accrual of
// 0.80. Another user holding the same instrument without a deposit of their
// own has no price.
func TestPortfolioSummaryDeposit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	ctx := context.Background()

	today := price.Today()
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}
	start := day.AddDate(0, 0, -10).Format(time.DateOnly)
	maturity := day.AddDate(0, 0, 20).Format(time.DateOnly)
	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('DEP-T1', 'DEPOSIT', 'Deposit', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instrumentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}
	summaryOf := func(email string, withDeposit bool) positionResponse {
		usr, err := user.NewRepo(conn).Create(ctx, email, "hash")
		if err != nil {
			t.Fatalf("users.Create: %v", err)
		}
		session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
		if err != nil {
			t.Fatalf("sessions.CreateSession: %v", err)
		}
		pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
		if err != nil {
			t.Fatalf("portfolios.Create: %v", err)
		}
		if withDeposit {
			if _, err := conn.Exec(`INSERT INTO term_deposits (user_id, portfolio_id, instrument_id, currency, principal, rate, stopaj_rate, start_date, maturity_date, created_at)
				VALUES (?, ?, ?, 'TRY', '1000', '0.365', '0.2', ?, ?, '2026-01-10T00:00:00Z')`, usr.ID, pf.ID, instrumentID, start, maturity); err != nil {
				t.Fatalf("insert term deposit: %v", err)
			}
		}
		buy := transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: start, Quantity: decimal.RequireFromString("1000"), Price: decimal.NewFromInt(1), Amount: decimal.RequireFromString("1000"), Fee: decimal.Zero, Currency: "TRY"}
		if _, err := transaction.NewRepo(conn).Create(ctx, usr.ID, buy); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/summary", pf.ID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got summaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got.Positions) != 1 {
			t.Fatalf("positions = %+v, want 1", got.Positions)
		}
		return got.Positions[0]
	}

	p := summaryOf("alice@example.com", true)
	terms := deposit.Terms{Rate: decimal.RequireFromString("0.365"), StopajRate: decimal.RequireFromString("0.2"), StartDate: start, MaturityDate: maturity}
	closeToday, _ := deposit.Close(terms, today)
	principal := decimal.RequireFromString("1000")
	if want := principal.Mul(closeToday).StringFixed(2); p.MarketValue != want || want != "1008.00" {
		t.Errorf("market_value = %s, want %s (1008.00)", p.MarketValue, want)
	}
	if !p.HasPrice || p.PriceSource == nil || *p.PriceSource != "deposit" || p.PriceDate == nil || *p.PriceDate != today {
		t.Errorf("{has_price, price_source, price_date} = {%v, %v, %v}, want {true, deposit, %s}", p.HasPrice, p.PriceSource, p.PriceDate, today)
	}
	if p.DayChange == nil || *p.DayChange != "0.80" {
		t.Errorf("day_change = %v, want 0.80", p.DayChange)
	}

	if bob := summaryOf("bob@example.com", false); bob.HasPrice || bob.PriceSource != nil {
		t.Errorf("another user's {has_price, price_source} = {%v, %v}, want {false, nil}", bob.HasPrice, bob.PriceSource)
	}
}
