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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/dividend"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// TestExpectedDividends covers the expected dividends of a portfolio:
//
//	THYAO (BIST): buy 10 on 2024-03-01, buy 5 on the ex-date 2024-05-10
//	  => quantity 10 (the ex-date buy does not count), 10 x 1.2345 = 12.345
//	  => gross 12.35, stopaj 15% = 1.8525 -> 1.85, net 10.50
//	AAPL (US): buy 4 on 2024-01-02, dividend 0.5 on 2024-02-01 already
//	  recorded as a transaction on 2024-03-01 (within +45 days) => hidden;
//	  dividend 0.5 on 2024-06-01 => gross 2.00, stopaj 20% 0.40, net 1.60
//	a dividend dated after today is not expected yet;
//	VOO (US): 0.001528463 x 1.8724 = 0.0029 rounds to 0.00 => dropped;
//	another user's portfolio is 404.
//
// In a second portfolio, one recorded dividend hides one ex-date.
//
//	O: ex-dates 01-30, 02-28, 03-28, only 02-28 recorded => 01-30 and
//	  03-28 are still expected;
//	MAIN: ex-dates 01-30, 02-28, a Midas row paid 02-22 for 01-30 =>
//	  only 02-28 is expected.
func TestExpectedDividends(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

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
	thyao := instrumentID("THYAO", "BIST", "TRY")
	aapl := instrumentID("AAPL", "US", "USD")
	voo := instrumentID("VOO", "US", "USD")
	o := instrumentID("O", "US", "USD")
	mainID := instrumentID("MAIN", "US", "USD")

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
	intruder, err := users.Create(context.Background(), "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create intruder: %v", err)
	}
	intruderSession, err := sessions.CreateSession(context.Background(), intruder.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession intruder: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}
	monthly, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Monthly")
	if err != nil {
		t.Fatalf("portfolios.Create monthly: %v", err)
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{InstrumentID: &thyao, Type: transaction.TypeBuy, Date: "2024-03-01", Quantity: d("10"), Price: d("1"), Amount: d("10"), Currency: "TRY"},
		{InstrumentID: &thyao, Type: transaction.TypeBuy, Date: "2024-05-10", Quantity: d("5"), Price: d("1"), Amount: d("5"), Currency: "TRY"},
		{InstrumentID: &aapl, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("4"), Price: d("1"), Amount: d("4"), Currency: "USD"},
		{InstrumentID: &aapl, Type: transaction.TypeDividend, Date: "2024-03-01", Amount: d("2"), Fee: d("0.4"), Currency: "USD"},
		{InstrumentID: &voo, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("0.001528463"), Price: d("400"), Amount: d("0.61"), Currency: "USD"},
		{PortfolioID: monthly.ID, InstrumentID: &o, Type: transaction.TypeBuy, Date: "2024-01-05", Quantity: d("10"), Price: d("1"), Amount: d("10"), Currency: "USD"},
		{PortfolioID: monthly.ID, InstrumentID: &o, Type: transaction.TypeDividend, Date: "2024-02-28", Amount: d("2.70"), Fee: d("0.54"), Currency: "USD"},
		{PortfolioID: monthly.ID, InstrumentID: &mainID, Type: transaction.TypeBuy, Date: "2024-01-05", Quantity: d("10"), Price: d("1"), Amount: d("10"), Currency: "USD"},
		{PortfolioID: monthly.ID, InstrumentID: &mainID, Type: transaction.TypeDividend, Date: "2024-02-22", Amount: d("2.50"), Fee: d("0.50"), Currency: "USD"},
	} {
		if tx.PortfolioID == 0 {
			tx.PortfolioID = pf.ID
		}
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	dividends := dividend.NewRepo(conn)
	for _, dv := range []struct {
		id       int64
		date     string
		amount   string
		currency string
	}{
		{thyao, "2024-05-10", "1.2345", "TRY"},
		{thyao, "2099-01-01", "1", "TRY"},
		{aapl, "2024-02-01", "0.5", "USD"},
		{aapl, "2024-06-01", "0.5", "USD"},
		{voo, "2024-03-27", "1.8724", "USD"},
		{o, "2024-01-30", "0.27", "USD"},
		{o, "2024-02-28", "0.27", "USD"},
		{o, "2024-03-28", "0.27", "USD"},
		{mainID, "2024-01-30", "0.25", "USD"},
		{mainID, "2024-02-28", "0.25", "USD"},
	} {
		if err := dividends.Upsert(ctx, dv.id, dv.date, d(dv.amount), dv.currency, "test"); err != nil {
			t.Fatalf("dividends.Upsert: %v", err)
		}
	}

	getFor := func(sessionID string, portfolioID int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/portfolios/%d/dividends/expected", portfolioID), nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	get := func(sessionID string) *httptest.ResponseRecorder { return getFor(sessionID, pf.ID) }

	if rec := get(intruderSession.ID); rec.Code != http.StatusNotFound {
		t.Errorf("intruder status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec := get(session.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []expectedDividendResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := []expectedDividendResponse{
		{InstrumentID: thyao, Symbol: "THYAO", Market: "BIST", Date: "2024-05-10", Quantity: "10", PerShare: "1.2345", Gross: "12.35", Stopaj: "1.85", Net: "10.50", Currency: "TRY"},
		{InstrumentID: aapl, Symbol: "AAPL", Market: "US", Date: "2024-06-01", Quantity: "4", PerShare: "0.5", Gross: "2.00", Stopaj: "0.40", Net: "1.60", Currency: "USD"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// Dismissing THYAO 2024-05-10 drops it from the GET; the
	// intruder gets 404 and dismisses nothing; a repeat dismiss is a no-op.
	dismiss := func(sessionID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"instrument_id":%d,"date":"2024-05-10"}`, thyao)
		r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/portfolios/%d/dividends/expected/dismiss", pf.ID), strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	if rec := dismiss(intruderSession.ID); rec.Code != http.StatusNotFound {
		t.Errorf("intruder dismiss status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	rec = get(session.ID)
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after intruder dismiss got %+v\nwant %+v", got, want)
	}
	for i := 0; i < 2; i++ {
		if rec := dismiss(session.ID); rec.Code != http.StatusNoContent {
			t.Fatalf("dismiss #%d status = %d, want %d; body: %s", i+1, rec.Code, http.StatusNoContent, rec.Body.String())
		}
	}
	rec = get(session.ID)
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(got, want[1:]) {
		t.Errorf("after dismiss got %+v\nwant %+v", got, want[1:])
	}

	rec = getFor(session.ID, monthly.ID)
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode monthly response: %v", err)
	}
	wantMonthly := []expectedDividendResponse{
		{InstrumentID: o, Symbol: "O", Market: "US", Date: "2024-01-30", Quantity: "10", PerShare: "0.27", Gross: "2.70", Stopaj: "0.54", Net: "2.16", Currency: "USD"},
		{InstrumentID: mainID, Symbol: "MAIN", Market: "US", Date: "2024-02-28", Quantity: "10", PerShare: "0.25", Gross: "2.50", Stopaj: "0.50", Net: "2.00", Currency: "USD"},
		{InstrumentID: o, Symbol: "O", Market: "US", Date: "2024-03-28", Quantity: "10", PerShare: "0.27", Gross: "2.70", Stopaj: "0.54", Net: "2.16", Currency: "USD"},
	}
	if !reflect.DeepEqual(got, wantMonthly) {
		t.Errorf("monthly got  %+v\nwant %+v", got, wantMonthly)
	}

	// The pre-filled stopaj follows the stopaj_rates table: US 20% -> 10%.
	if _, err := conn.Exec("UPDATE stopaj_rates SET rate = '0.10' WHERE market = 'US'"); err != nil {
		t.Fatalf("update stopaj rate: %v", err)
	}
	rec = get(session.ID)
	got = nil
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "AAPL" || got[0].Stopaj != "0.20" || got[0].Net != "1.80" {
		t.Errorf("after rate change got %+v, want AAPL stopaj 0.20 net 1.80", got)
	}

	// Deleting the portfolio leaves no orphan dismissal.
	if _, err := portfolio.NewRepo(conn).Delete(ctx, usr.ID, pf.ID, true); err != nil {
		t.Fatalf("portfolios.Delete: %v", err)
	}
	var left int
	if err := conn.QueryRow("SELECT COUNT(*) FROM dividend_dismissals").Scan(&left); err != nil {
		t.Fatalf("count dismissals: %v", err)
	}
	if left != 0 {
		t.Errorf("dismissals left after portfolio delete = %d, want 0", left)
	}
}

// TestDividendTimeline covers the dividend timeline.
// Reporting currency TRY; USD rates 30 on 2025-03-01, 36 on 2025-03-20, 40 on
// 2025-04-01.
//
//	THYAO 2025-03-05 100 TRY, fee 15   => net 85
//	ASELS 2025-03-10 50 TRY, fee 7.5   => net 42.50
//	AAPL  2025-03-20 2 USD, fee 0.3 at its own date's 36 => 72, 10.80, 61.20
//	MSFT  2025-02-10 1 USD, before any rate => left out, missing_rates [USD]
//	interest 2025-03-15 5 TRY, fee 1   => interest 4.00 only
func TestDividendTimeline(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	instrumentID := func(symbol, currency string) int64 {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, ?)`, symbol, symbol+" name", currency)
		if err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("instrument id: %v", err)
		}
		return id
	}
	thyao, asels := instrumentID("THYAO", "TRY"), instrumentID("ASELS", "TRY")
	aapl, msft := instrumentID("AAPL", "USD"), instrumentID("MSFT", "USD")

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
	intruder, err := users.Create(context.Background(), "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create intruder: %v", err)
	}
	intruderSession, err := sessions.CreateSession(context.Background(), intruder.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession intruder: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	rates := fx.NewRepo(conn)
	for date, rate := range map[string]string{"2025-03-01": "30", "2025-03-20": "36", "2025-04-01": "40"} {
		if err := rates.Upsert(ctx, "USD", date, rate, "manual"); err != nil {
			t.Fatalf("fx.Upsert: %v", err)
		}
	}

	d := decimal.RequireFromString
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{InstrumentID: &msft, Type: transaction.TypeDividend, Date: "2025-02-10", Amount: d("1"), Currency: "USD"},
		{InstrumentID: &thyao, Type: transaction.TypeDividend, Date: "2025-03-05", Amount: d("100"), Fee: d("15"), Currency: "TRY"},
		{InstrumentID: &asels, Type: transaction.TypeDividend, Date: "2025-03-10", Amount: d("50"), Fee: d("7.5"), Currency: "TRY"},
		{Type: transaction.TypeDividend, Date: "2025-03-15", Amount: d("5"), Fee: d("1"), Currency: "TRY"},
		{InstrumentID: &aapl, Type: transaction.TypeDividend, Date: "2025-03-20", Amount: d("2"), Fee: d("0.3"), Currency: "USD"},
	} {
		tx.PortfolioID = pf.ID
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	get := func(sessionID, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/dividends/timeline", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	id := fmt.Sprint(pf.ID)
	if rec := get(intruderSession.ID, id); rec.Code != http.StatusNotFound {
		t.Errorf("intruder status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := get(session.ID, "all"); rec.Code != http.StatusNotFound {
		t.Errorf("all status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec := get(session.ID, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got dividendTimelineResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := dividendTimelineResponse{
		Currency: "TRY",
		Months:   []dividendBucket{{Period: "2025-03", Gross: "222.00", Tax: "33.30", Net: "188.70", Interest: "4.00"}},
		Years:    []dividendBucket{{Period: "2025", Gross: "222.00", Tax: "33.30", Net: "188.70", Interest: "4.00"}},
		ByInstrument: []dividendInstrumentTotal{
			{InstrumentID: thyao, Symbol: "THYAO", Name: "THYAO name", Count: 1, Net: "85.00", LastDate: "2025-03-05"},
			{InstrumentID: aapl, Symbol: "AAPL", Name: "AAPL name", Count: 1, Net: "61.20", LastDate: "2025-03-20"},
			{InstrumentID: asels, Symbol: "ASELS", Name: "ASELS name", Count: 1, Net: "42.50", LastDate: "2025-03-10"},
		},
		MissingRates: []string{"USD"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// TestDividendForecast covers the dividend forecast. A buy of 10 BIST shares
// a year ago and a recorded dividend of 25.00 eight months ago project 25.00
// gross four months from now: stopaj 15% (stopaj_rates) 3.75, net 21.25, per
// share 2.5, in a month of the twelve and 0.00 in the others.
func TestDividendForecast(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()

	res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('THYAO', 'BIST', 'THYAO name', 'TRY')`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	thyao, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("instrument id: %v", err)
	}

	handler := New(logger, conn, false, settings.NewRepo(conn, nil), nil)
	users := user.NewRepo(conn)
	sessions := auth.NewRepo(conn)
	usr, err := users.Create(ctx, "alice@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}
	session, err := sessions.CreateSession(ctx, usr.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession: %v", err)
	}
	intruder, err := users.Create(ctx, "intruder@example.com", "hash")
	if err != nil {
		t.Fatalf("users.Create intruder: %v", err)
	}
	intruderSession, err := sessions.CreateSession(ctx, intruder.ID)
	if err != nil {
		t.Fatalf("sessions.CreateSession intruder: %v", err)
	}
	pf, err := portfolio.NewRepo(conn).Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatalf("portfolios.Create: %v", err)
	}

	d := decimal.RequireFromString
	now := time.Now().In(price.Istanbul)
	monthsAgo := func(n int) string {
		return time.Date(now.Year(), now.Month()-time.Month(n), 10, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	wantPeriod := monthsAgo(-4)[:7]
	transactions := transaction.NewRepo(conn)
	for _, tx := range []transaction.Transaction{
		{InstrumentID: &thyao, Type: transaction.TypeBuy, Date: monthsAgo(12), Quantity: d("10"), Price: d("1"), Amount: d("10"), Currency: "TRY"},
		{InstrumentID: &thyao, Type: transaction.TypeDividend, Date: monthsAgo(8), Amount: d("25.00"), Fee: d("3.75"), Currency: "TRY"},
	} {
		tx.PortfolioID = pf.ID
		if _, err := transactions.Create(ctx, usr.ID, tx); err != nil {
			t.Fatalf("transactions.Create: %v", err)
		}
	}

	get := func(sessionID, id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/portfolios/"+id+"/dividends/forecast", nil)
		r.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	id := fmt.Sprint(pf.ID)
	if rec := get(intruderSession.ID, id); rec.Code != http.StatusNotFound {
		t.Errorf("intruder status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := get(session.ID, "all"); rec.Code != http.StatusNotFound {
		t.Errorf("all status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec := get(session.ID, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got dividendForecastResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Months) != 12 {
		t.Fatalf("months = %+v, want 12", got.Months)
	}
	for _, m := range got.Months {
		want := "0.00"
		if m.Period == wantPeriod {
			want = "21.25"
		}
		if m.Net != want {
			t.Errorf("month %s net = %s, want %s", m.Period, m.Net, want)
		}
	}
	net := "21.25"
	wantRows := []dividendForecastRow{
		{InstrumentID: thyao, Symbol: "THYAO", Market: "BIST", Period: wantPeriod, Quantity: "10", PerShare: "2.5", Gross: "25.00", Stopaj: "3.75", Net: net, Currency: "TRY", NetReporting: &net},
	}
	if !reflect.DeepEqual(got.Rows, wantRows) {
		t.Errorf("rows got  %+v\nwant %+v", got.Rows, wantRows)
	}
	if len(got.MissingRates) != 0 || got.Currency != "TRY" {
		t.Errorf("currency = %q, missing_rates = %v, want TRY and none", got.Currency, got.MissingRates)
	}
}
