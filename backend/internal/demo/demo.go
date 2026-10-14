// Package demo fills an empty database with a synthetic demo account
// (GLG-1380): one admin user, one portfolio with two years of trades and
// dividends, and daily prices and FX rates for the same two years. The
// numbers come from a fixed-seed random walk, so the seed needs no network
// and two runs give the same values.
package demo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"

	"github.com/dogauzun/gelirgider/backend/internal/fx"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/dogauzun/gelirgider/backend/internal/user"
)

// The demo login, and the source of every price and FX row the seed
// writes: no scheduler source owns "demo", so a later fetch does not
// count these rows as its own.
const (
	Email    = "demo@example.com"
	Password = "demo-password"
	Source   = "demo"
)

// ErrNotEmpty is Seed's refusal on a database that already has a user.
var ErrNotEmpty = errors.New("database is not empty: -demo only fills a database with no users")

// walk is one random-walk series: an instrument's daily close, or, with an
// empty market, the TRY rate of currency symbol. Each day multiplies the
// value by exp(drift + vol × N(0,1)).
type walk struct {
	market, symbol    string
	start, drift, vol float64
	places            int32
}

var walks = []walk{
	{"BIST", "THYAO", 280, 0.0008, 0.022, 2},
	{"BIST", "ASELS", 60, 0.0010, 0.025, 2},
	{"BIST", "TUPRS", 160, 0.0006, 0.020, 2},
	{"US", "AAPL", 190, 0.0004, 0.016, 2},
	{"TEFAS", "TTE", 2.5, 0.0009, 0.012, 6},
	{"CRYPTO", "BTC-TRY", 2_000_000, 0.0012, 0.030, 2},
	{"GOLD", "GRAM", 2400, 0.0010, 0.010, 2},
	{"", "USD", 32, 0.0004, 0.004, 4},
	{"", "EUR", 35, 0.0005, 0.005, 4},
}

// step is one demo transaction, day days after the first day. n is the
// quantity of a buy or sell and the amount of a deposit or dividend;
// symbol names the walk of a trade or dividend, currency a deposit's.
type step struct {
	day                 int
	typ                 string
	symbol, n, currency string
}

var steps = []step{
	{0, transaction.TypeDeposit, "", "1000000", "TRY"},
	{0, transaction.TypeDeposit, "", "10000", "USD"},
	{1, transaction.TypeBuy, "THYAO", "1000", ""},
	{1, transaction.TypeBuy, "ASELS", "2000", ""},
	{5, transaction.TypeBuy, "AAPL", "30", ""},
	{10, transaction.TypeBuy, "TTE", "20000", ""},
	{20, transaction.TypeBuy, "BTC-TRY", "0.1", ""},
	{40, transaction.TypeBuy, "GRAM", "50", ""},
	{110, transaction.TypeDeposit, "", "500000", "TRY"},
	{120, transaction.TypeBuy, "TUPRS", "1500", ""},
	{200, transaction.TypeSell, "ASELS", "800", ""},
	{300, transaction.TypeBuy, "THYAO", "300", ""},
	{330, transaction.TypeDividend, "THYAO", "3900", ""},
	{380, transaction.TypeDividend, "AAPL", "7.20", ""},
	{400, transaction.TypeSell, "TUPRS", "500", ""},
	{450, transaction.TypeBuy, "AAPL", "10", ""},
	{500, transaction.TypeSell, "BTC-TRY", "0.04", ""},
	{600, transaction.TypeBuy, "GRAM", "30", ""},
}

// Seed fills conn, which must have no user, with the demo account and two
// years of history ending on today (YYYY-MM-DD). Instruments must already
// be seeded. It returns ErrNotEmpty when conn has a user. Prices and rates
// go in before the user, so a seed that fails halfway can be run again.
func Seed(ctx context.Context, conn *sql.DB, today string) error {
	users := user.NewRepo(conn)
	existing, err := users.List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ErrNotEmpty
	}
	end, err := price.ParseDate(today)
	if err != nil {
		return fmt.Errorf("today %q: %w", today, err)
	}
	var dates []string
	for d := end.AddDate(-2, 0, 0); !d.After(end); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format(time.DateOnly))
	}
	instruments, closes, err := writePrices(ctx, conn, dates)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u, err := users.Create(ctx, Email, string(hash))
	if err != nil {
		return err
	}
	if err := users.SetAdmin(ctx, u.ID, true); err != nil {
		return err
	}
	p, err := portfolio.NewRepo(conn).Create(ctx, u.ID, "Demo")
	if err != nil {
		return err
	}
	txs := make([]transaction.Transaction, 0, len(steps))
	for _, s := range steps {
		t := transaction.Transaction{PortfolioID: p.ID, Type: s.typ, Date: dates[s.day], Amount: decimal.RequireFromString(s.n), Currency: s.currency}
		if in, ok := instruments[s.symbol]; ok {
			t.InstrumentID, t.Currency = &in.ID, in.Currency
		}
		if s.typ == transaction.TypeBuy || s.typ == transaction.TypeSell {
			t.Quantity, t.Price = t.Amount, closes[s.symbol][s.day]
			t.Amount = t.Quantity.Mul(t.Price)
		}
		txs = append(txs, t)
	}
	_, err = transaction.NewRepo(conn).CreateAll(ctx, u.ID, txs)
	return err
}

// writePrices stores every walk's value on each of dates and returns the
// walked instruments and every walk's values, both keyed by symbol.
func writePrices(ctx context.Context, conn *sql.DB, dates []string) (map[string]instrument.Instrument, map[string][]decimal.Decimal, error) {
	instruments := map[string]instrument.Instrument{}
	closes := map[string][]decimal.Decimal{}
	r := rand.New(rand.NewPCG(1380, 1))
	prices, rates := price.NewRepo(conn), fx.NewRepo(conn)
	for _, w := range walks {
		closes[w.symbol] = w.series(r, len(dates))
		upsert := func(date, value string) error { return rates.Upsert(ctx, w.symbol, date, value, Source) }
		if w.market != "" {
			in, err := instrument.NewRepo(conn).BySymbol(ctx, w.market, w.symbol)
			if err != nil {
				return nil, nil, fmt.Errorf("%s:%s: %w", w.market, w.symbol, err)
			}
			instruments[w.symbol] = in
			upsert = func(date, value string) error { return prices.Upsert(ctx, in.ID, date, value, Source) }
		}
		for i, c := range closes[w.symbol] {
			if err := upsert(dates[i], c.String()); err != nil {
				return nil, nil, err
			}
		}
	}
	return instruments, closes, nil
}

// series returns n daily values of w drawn from r, each rounded once to
// w.places.
func (w walk) series(r *rand.Rand, n int) []decimal.Decimal {
	out := make([]decimal.Decimal, n)
	v := w.start
	for i := range out {
		out[i] = decimal.NewFromFloat(v).Round(w.places)
		v *= math.Exp(w.drift + w.vol*r.NormFloat64())
	}
	return out
}
