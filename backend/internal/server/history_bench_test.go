package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/settings"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// benchSeed is a database seeded by seedBench with the repos, the one user
// and portfolio, the date range of the data, the full HTTP handler and a
// session id for it, and the id of the first seeded instrument.
type benchSeed struct {
	ctx        context.Context
	d          *deps
	usr        user.User
	pf         portfolio.Portfolio
	from, to   time.Time
	handler    http.Handler
	session    string
	instrument int64
}

// seedBench seeds 7 years and 25 instruments in five markets with n
// transactions of one portfolio: a close every weekday (every day for
// crypto and gold), USD and EUR rates every weekday, monthly TUFE levels.
// The data is the same on every run.
func seedBench(b *testing.B, n int) benchSeed {
	b.Helper()
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	d := &deps{
		users: user.NewRepo(conn), portfolios: portfolio.NewRepo(conn), instruments: instrument.NewRepo(conn),
		prices: price.NewRepo(conn), manualPrices: price.NewManualRepo(conn), transactions: transaction.NewRepo(conn),
		fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn), deposits: deposit.NewRepo(conn),
	}
	usr, err := d.users.Create(ctx, "bench@example.com", "hash")
	if err != nil {
		b.Fatal(err)
	}
	pf, err := d.portfolios.Create(ctx, usr.ID, "Bench")
	if err != nil {
		b.Fatal(err)
	}

	from := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewPCG(1363, 1))
	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	markets := []struct {
		market, currency string
		daily            bool
		places           int32
	}{{"BIST", "TRY", false, 2}, {"US", "USD", false, 2}, {"TEFAS", "TRY", false, 6}, {"CRYPTO", "USD", true, 2}, {"GOLD", "TRY", true, 2}}
	type instr struct {
		id       int64
		currency string
		places   int32
	}
	var instruments []instr
	for i := range 25 {
		m := markets[i%len(markets)]
		res, err := sqlTx.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, fmt.Sprintf("S%d", i), m.market, fmt.Sprintf("Instrument %d", i), m.currency)
		if err != nil {
			b.Fatal(err)
		}
		id, _ := res.LastInsertId()
		instruments = append(instruments, instr{id, m.currency, m.places})
		close := 10 + rng.Float64()*100
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			if !m.daily && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
				continue
			}
			close *= 1 + (rng.Float64()-0.5)*0.04
			if _, err := sqlTx.Exec(`INSERT INTO prices (instrument_id, date, close, source) VALUES (?, ?, ?, 'bench')`, id, day.Format(time.DateOnly), decimal.NewFromFloat(close).StringFixed(m.places)); err != nil {
				b.Fatal(err)
			}
		}
	}
	rates := map[string]float64{"USD": 5.3, "EUR": 6.1}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		for _, c := range []string{"USD", "EUR"} {
			rates[c] *= 1.0012 + (rng.Float64()-0.5)*0.01
			if _, err := sqlTx.Exec(`INSERT INTO fx_rates (currency, date, rate, source) VALUES (?, ?, ?, 'bench')`, c, day.Format(time.DateOnly), decimal.NewFromFloat(rates[c]).StringFixed(4)); err != nil {
				b.Fatal(err)
			}
		}
	}
	// TUFE levels from a year before the first transaction, about 2% a
	// month, running to the end of the prices; the history extrapolates
	// from there to today.
	tufe := 100.0
	for m := from.AddDate(-1, 0, 0); !m.After(to); m = m.AddDate(0, 1, 0) {
		tufe *= 1.02 + (rng.Float64()-0.5)*0.01
		if _, err := sqlTx.Exec(`INSERT INTO benchmark_values (code, date, value, source) VALUES (?, ?, ?, 'bench')`, benchmark.CodeTUFE, m.Format(time.DateOnly), decimal.NewFromFloat(tufe).StringFixed(2)); err != nil {
			b.Fatal(err)
		}
	}
	if err := sqlTx.Commit(); err != nil {
		b.Fatal(err)
	}

	days := int(to.Sub(from).Hours()/24) + 1
	txs := make([]transaction.Transaction, 0, n)
	for i := range n {
		date := from.AddDate(0, 0, i*days/n).Format(time.DateOnly)
		in := instruments[rng.IntN(len(instruments))]
		qty := decimal.NewFromFloat(1 + rng.Float64()*20).Round(4)
		px := decimal.NewFromFloat(10 + rng.Float64()*100).Round(in.places)
		fee := decimal.NewFromFloat(rng.Float64()).Round(2)
		t := transaction.Transaction{PortfolioID: pf.ID, Date: date, Fee: decimal.Zero, Currency: in.currency}
		switch i % 10 {
		case 0, 1:
			t.Type, t.Currency, t.Amount = transaction.TypeDeposit, []string{"TRY", "USD", "EUR"}[i%3], decimal.NewFromInt(int64(1000+rng.IntN(9000)))
		case 2:
			t.Type, t.Currency, t.Amount = transaction.TypeWithdrawal, []string{"TRY", "USD", "EUR"}[i%3], decimal.NewFromInt(int64(100+rng.IntN(900)))
		case 3:
			t.Type, t.InstrumentID, t.Quantity, t.Price, t.Amount, t.Fee = transaction.TypeSell, &in.id, qty.Div(decimal.NewFromInt(4)).Round(4), px, qty.Div(decimal.NewFromInt(4)).Round(4).Mul(px), fee
		case 4:
			t.Type, t.InstrumentID, t.Amount = transaction.TypeDividend, &in.id, decimal.NewFromFloat(rng.Float64()*50).Round(2)
		case 5:
			t.Type, t.Currency, t.Amount = transaction.TypeFee, "TRY", fee
		default:
			t.Type, t.InstrumentID, t.Quantity, t.Price, t.Amount, t.Fee = transaction.TypeBuy, &in.id, qty, px, qty.Mul(px), fee
		}
		txs = append(txs, t)
	}
	if _, err := d.transactions.CreateAll(ctx, usr.ID, txs); err != nil {
		b.Fatal(err)
	}

	handler := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), conn, false, settings.NewRepo(conn, nil), nil)
	session, err := auth.NewRepo(conn).CreateSession(ctx, usr.ID)
	if err != nil {
		b.Fatal(err)
	}
	return benchSeed{ctx, d, usr, pf, from, to, handler, session.ID, instruments[0].id}
}

// BenchmarkHistorySweep times the history sweep over the seedBench data.
func BenchmarkHistorySweep(b *testing.B) {
	for _, n := range []int{2000, 15000} {
		b.Run(fmt.Sprintf("txs=%d", n), func(b *testing.B) {
			s := seedBench(b, n)
			h := &historyHandler{s.d}
			for _, reporting := range []string{"TRY", "USD"} {
				b.Run(reporting, func(b *testing.B) {
					if err := s.d.users.UpdateReportingCurrency(s.ctx, s.usr.ID, reporting); err != nil {
						b.Fatal(err)
					}
					for b.Loop() {
						lists, err := scopeTransactions(s.ctx, s.d.portfolios, s.d.transactions, s.usr.ID, fmt.Sprint(s.pf.ID))
						if err != nil {
							b.Fatal(err)
						}
						if _, err := h.sweep(s.ctx, s.usr.ID, flatByDate(lists), s.from, s.to, sweepOpts{}); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// BenchmarkPortfolioRoutes times each portfolio read route through the full
// handler, with a session cookie, over the seedBench data.
func BenchmarkPortfolioRoutes(b *testing.B) {
	for _, n := range []int{2000, 15000} {
		b.Run(fmt.Sprintf("txs=%d", n), func(b *testing.B) {
			s := seedBench(b, n)
			p := fmt.Sprintf("/api/portfolios/%d", s.pf.ID)
			routes := []struct{ name, path string }{
				{"summary", p + "/summary"},
				{"history", p + "/history"},
				{"history_tufe", p + "/history?benchmark=TUFE"},
				{"returns_periods", p + "/returns/periods"},
				{"transactions", p + "/transactions"},
				{"dividends_timeline", p + "/dividends/timeline"},
				{"dividends_expected", p + "/dividends/expected"},
				{"instrument_history", fmt.Sprintf("%s/instruments/%d/history", p, s.instrument)},
				{"portfolios", "/api/portfolios"},
				{"all_history", "/api/portfolios/all/history"},
			}
			for _, r := range routes {
				b.Run(r.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						getBody(b, s.handler, s.session, r.path)
					}
				})
			}
		})
	}
}
