package server

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/split"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
	"github.com/shopspring/decimal"
)

// BenchmarkHistorySweep times the history sweep over 7 years and 25
// instruments in five markets, with 2,000 transactions (GLG-1363): a
// close every weekday (every day for crypto and gold), USD and EUR rates
// every weekday.
func BenchmarkHistorySweep(b *testing.B) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	d := &deps{
		users: user.NewRepo(conn), portfolios: portfolio.NewRepo(conn), instruments: instrument.NewRepo(conn),
		prices: price.NewRepo(conn), manualPrices: price.NewManualRepo(conn), transactions: transaction.NewRepo(conn),
		fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn),
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
	if err := sqlTx.Commit(); err != nil {
		b.Fatal(err)
	}

	days := int(to.Sub(from).Hours()/24) + 1
	txs := make([]transaction.Transaction, 0, 2000)
	for i := range 2000 {
		date := from.AddDate(0, 0, i*days/2000).Format(time.DateOnly)
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

	h := &historyHandler{d}
	for _, reporting := range []string{"TRY", "USD"} {
		b.Run(reporting, func(b *testing.B) {
			if err := d.users.UpdateReportingCurrency(ctx, usr.ID, reporting); err != nil {
				b.Fatal(err)
			}
			for b.Loop() {
				lists, err := scopeTransactions(ctx, d.portfolios, d.transactions, usr.ID, fmt.Sprint(pf.ID))
				if err != nil {
					b.Fatal(err)
				}
				if _, err := h.sweep(ctx, usr.ID, flatByDate(lists), from, to, benchmarkRef{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
