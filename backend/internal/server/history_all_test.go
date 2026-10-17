package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// seedHistoryAll builds ten portfolios of one user over ten years
// (2016-10-01 to 2026-09-30): one with 1,000 transactions, nine with 100,
// over TRY, USD and EUR instruments with daily closes, USD and EUR rates
// (EUR starting a year late), a split, an instrument unpriced for its
// first two years, and X sold short in one portfolio while held in
// another. It returns the handler and a session id.
func seedHistoryAll(tb testing.TB) (http.Handler, string) {
	tb.Helper()
	conn, err := db.Open(filepath.Join(tb.TempDir(), "test.db"))
	if err != nil {
		tb.Fatalf("db.Open: %v", err)
	}
	tb.Cleanup(func() { conn.Close() })
	ctx := context.Background()
	rng := rand.New(rand.NewSource(685))

	const first = "2016-10-01"
	start, _ := time.Parse(time.DateOnly, first)
	days := 3652

	type instr struct {
		id       int64
		currency string
	}
	var instrs []instr
	for i, cur := range []string{"TRY", "TRY", "TRY", "TRY", "TRY", "TRY", "USD", "USD", "USD", "EUR", "EUR", "TRY"} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'BIST', ?, ?)`, fmt.Sprintf("S%02d", i), fmt.Sprintf("S%02d", i), cur)
		if err != nil {
			tb.Fatalf("insert instrument: %v", err)
		}
		id, _ := res.LastInsertId()
		instrs = append(instrs, instr{id, cur})
	}

	sqlTx, err := conn.Begin()
	if err != nil {
		tb.Fatalf("begin: %v", err)
	}
	for k, in := range instrs {
		close := 10 + rng.Float64()*90
		for i := 0; i < days; i++ {
			day := start.AddDate(0, 0, i)
			close *= 1 + (rng.Float64()-0.5)/25
			// Weekends have no close; the last instrument has none for
			// its first two years.
			if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || (k == len(instrs)-1 && i < 730) {
				continue
			}
			if _, err := sqlTx.Exec(`INSERT INTO prices (instrument_id, date, close, source) VALUES (?, ?, ?, 'test')`, in.id, day.Format(time.DateOnly), fmt.Sprintf("%.4f", close)); err != nil {
				tb.Fatalf("insert price: %v", err)
			}
		}
	}
	usd, eur := 2.9, 3.3
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		usd *= 1 + (rng.Float64()-0.45)/200
		eur *= 1 + (rng.Float64()-0.45)/200
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		if _, err := sqlTx.Exec(`INSERT INTO fx_rates (currency, date, rate, source) VALUES ('USD', ?, ?, 'manual')`, day.Format(time.DateOnly), fmt.Sprintf("%.4f", usd)); err != nil {
			tb.Fatalf("insert fx: %v", err)
		}
		if i >= 365 {
			if _, err := sqlTx.Exec(`INSERT INTO fx_rates (currency, date, rate, source) VALUES ('EUR', ?, ?, 'manual')`, day.Format(time.DateOnly), fmt.Sprintf("%.4f", eur)); err != nil {
				tb.Fatalf("insert fx: %v", err)
			}
		}
	}
	if err := sqlTx.Commit(); err != nil {
		tb.Fatalf("commit: %v", err)
	}
	if err := split.NewRepo(conn).Upsert(ctx, instrs[1].id, "2020-03-02", decimal.RequireFromString("3"), "manual"); err != nil {
		tb.Fatalf("splits.Upsert: %v", err)
	}

	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil)
	usr, err := user.NewRepo(conn).Create(context.Background(), "all@example.com", "hash")
	if err != nil {
		tb.Fatalf("users.Create: %v", err)
	}
	session, err := auth.NewRepo(conn).CreateSession(context.Background(), usr.ID)
	if err != nil {
		tb.Fatalf("CreateSession: %v", err)
	}

	portfolios := portfolio.NewRepo(conn)
	transactions := transaction.NewRepo(conn)
	var pids []int64
	for p := 0; p < 10; p++ {
		pf, err := portfolios.Create(ctx, usr.ID, fmt.Sprintf("P%d", p))
		if err != nil {
			tb.Fatalf("portfolios.Create: %v", err)
		}
		pids = append(pids, pf.ID)
	}
	sqlTx, err = conn.Begin()
	if err != nil {
		tb.Fatalf("begin: %v", err)
	}
	add := func(t transaction.Transaction) {
		if _, err := transactions.CreateTx(ctx, sqlTx, usr.ID, t); err != nil {
			tb.Fatalf("transactions.Create: %v", err)
		}
	}
	// dec is a random amount from lo to lo+span, in units of 10^-places.
	dec := func(lo, span int, places int32) decimal.Decimal {
		return decimal.New(int64(lo+rng.Intn(span)), -places)
	}
	for p, pid := range pids {
		n := 100
		if p == 0 {
			n = 1000
		}
		for _, cur := range []string{"TRY", "USD", "EUR"} {
			add(transaction.Transaction{PortfolioID: pid, Type: transaction.TypeDeposit, Date: first, Amount: dec(500000, 500000, 2), Currency: cur})
		}
		for i := 0; i < n-3; i++ {
			date := start.AddDate(0, 0, rng.Intn(days)).Format(time.DateOnly)
			in := instrs[rng.Intn(len(instrs))]
			qty := dec(1000, 20000, 3)
			px := dec(1000, 9000, 2)
			amount := qty.Mul(px).Round(2)
			fee := dec(0, 200, 2)
			t := transaction.Transaction{PortfolioID: pid, InstrumentID: &in.id, Date: date, Quantity: qty, Price: px, Amount: amount, Fee: fee, Currency: in.currency}
			switch r := rng.Intn(20); {
			case r < 9:
				t.Type = transaction.TypeBuy
			case r < 14:
				t.Type = transaction.TypeSell
			case r < 16:
				t = transaction.Transaction{PortfolioID: pid, InstrumentID: &in.id, Type: transaction.TypeDividend, Date: date, Amount: dec(100, 5000, 2), Fee: dec(0, 100, 2), Currency: in.currency}
			case r < 18:
				t = transaction.Transaction{PortfolioID: pid, Type: transaction.TypeDeposit, Date: date, Amount: dec(100, 200000, 2), Fee: fee, Currency: in.currency}
			case r < 19:
				t = transaction.Transaction{PortfolioID: pid, Type: transaction.TypeWithdrawal, Date: date, Amount: dec(100, 100000, 2), Fee: fee, Currency: in.currency}
			default:
				t = transaction.Transaction{PortfolioID: pid, Type: transaction.TypeFee, Date: date, Amount: dec(100, 1000, 2), Currency: in.currency}
			}
			add(t)
		}
	}
	// X is sold short in P1 and held long in P2 by the same quantity, so
	// the merged quantity is zero while neither portfolio's is.
	x := instrs[0].id
	add(transaction.Transaction{PortfolioID: pids[1], InstrumentID: &x, Type: transaction.TypeSell, Date: "2025-06-02", Quantity: decimal.RequireFromString("1000"), Price: decimal.RequireFromString("20"), Amount: decimal.RequireFromString("20000"), Currency: "TRY"})
	add(transaction.Transaction{PortfolioID: pids[2], InstrumentID: &x, Type: transaction.TypeBuy, Date: "2025-06-02", Quantity: decimal.RequireFromString("1000"), Price: decimal.RequireFromString("20"), Amount: decimal.RequireFromString("20000"), Currency: "TRY"})
	if err := sqlTx.Commit(); err != nil {
		tb.Fatalf("commit: %v", err)
	}
	return handler, session.ID
}

func getHistoryBody(tb testing.TB, handler http.Handler, session, query string) []byte {
	tb.Helper()
	return getBody(tb, handler, session, "/api/portfolios/all/history?"+query)
}

// getBody GETs path with the session cookie and returns the body of a 200.
func getBody(tb testing.TB, handler http.Handler, session, path string) []byte {
	tb.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: session})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		tb.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// BenchmarkHistoryAll times the max-range `all` history
// over ten portfolios and ten years of daily closes.
func BenchmarkHistoryAll(b *testing.B) {
	handler, session := seedHistoryAll(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		getHistoryBody(b, handler, session, "to=2026-09-30")
	}
}
