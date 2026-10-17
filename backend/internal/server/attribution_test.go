package server

import (
	"context"
	"fmt"
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

func TestAttributeDay(t *testing.T) {
	const usdStock, tryStock = 1, 2
	currencyOf := map[int64]string{usdStock: "USD", tryStock: "TRY"}
	flat := map[string]decimal.Decimal{"USD": dec("30")}
	up10 := map[string]decimal.Decimal{"USD": dec("33")}
	usd := func(v string) map[int64]decimal.Decimal { return map[int64]decimal.Decimal{usdStock: dec(v)} }

	tests := []struct {
		name string
		in   dayInput
		// want holds the USD stock's price/income/fx, then the scope-level
		// interest, cash_fx and costs.
		wantStock                          [3]string
		wantInterest, wantCashFX, wantCost string
		wantMissing                        []string
	}{
		{
			name:      "held flat price, USD/TRY up 10%: the gain is all fx",
			in:        dayInput{prevValue: usd("1000"), value: usd("1000"), prevRates: flat, rates: up10},
			wantStock: [3]string{"0", "0", "3000"},
		},
		{
			name:      "sold on the day USD/TRY is up 10%: the gain is all fx, price is 0",
			in:        dayInput{prevValue: usd("1000"), tradeCash: usd("-1000"), prevRates: flat, rates: up10},
			wantStock: [3]string{"0", "0", "3000"},
		},
		{
			name:      "bought at a fee: the fee is price",
			in:        dayInput{value: usd("1000"), tradeCash: usd("1005"), prevRates: flat, rates: flat},
			wantStock: [3]string{"-150", "0", "0"},
		},
		{
			name:      "price up 5% at a flat rate",
			in:        dayInput{prevValue: usd("1000"), value: usd("1050"), prevRates: flat, rates: flat},
			wantStock: [3]string{"1500", "0", "0"},
		},
		{
			name:       "standalone fee row, USD cash held while USD/TRY moves",
			in:         dayInput{costs: map[string]decimal.Decimal{"USD": dec("5")}, prevCash: map[string]decimal.Decimal{"USD": dec("100")}, prevRates: flat, rates: up10},
			wantCashFX: "300",
			wantCost:   "-165",
		},
		{
			name:     "fee on a deposit",
			in:       dayInput{costs: map[string]decimal.Decimal{"TRY": dec("2.5")}, prevRates: flat, rates: flat},
			wantCost: "-2.5",
		},
		{
			name:         "interest is income at scope level",
			in:           dayInput{interest: map[string]decimal.Decimal{"USD": dec("2")}, prevRates: flat, rates: up10},
			wantInterest: "66",
		},
		{
			name:      "dividend on an instrument no longer held",
			in:        dayInput{dividend: usd("10"), prevRates: flat, rates: up10},
			wantStock: [3]string{"0", "330", "0"},
		},
		{
			name:        "no rate on d-1: left out and listed",
			in:          dayInput{prevValue: usd("1000"), value: usd("1100"), prevRates: map[string]decimal.Decimal{}, rates: up10},
			wantStock:   [3]string{"0", "0", "0"},
			wantMissing: []string{"USD"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.currencyOf, tc.in.reporting = currencyOf, "TRY"
			got := attributeDay(tc.in)
			r := got.instruments[usdStock]
			for i, w := range tc.wantStock {
				if w == "" {
					tc.wantStock[i] = "0"
				}
			}
			if r.price.Cmp(dec(tc.wantStock[0])) != 0 || r.income.Cmp(dec(tc.wantStock[1])) != 0 || r.fx.Cmp(dec(tc.wantStock[2])) != 0 {
				t.Errorf("price/income/fx = %s/%s/%s, want %v", r.price, r.income, r.fx, tc.wantStock)
			}
			if tc.wantMissing != nil && r.ok {
				t.Error("instrument without a factor is ok")
			}
			for _, c := range []struct {
				name      string
				got, want string
			}{{"interest", got.interest.String(), tc.wantInterest}, {"cash_fx", got.cashFX.String(), tc.wantCashFX}, {"costs", got.costs.String(), tc.wantCost}} {
				if c.want == "" {
					c.want = "0"
				}
				if !dec(c.got).Equal(dec(c.want)) {
					t.Errorf("%s = %s, want %s", c.name, c.got, c.want)
				}
			}
			if fmt.Sprint(got.missing) != fmt.Sprint(tc.wantMissing) {
				t.Errorf("missing = %v, want %v", got.missing, tc.wantMissing)
			}
		})
	}
}

// TestSweepAttributionAddsUp runs the sweep with attribution over a portfolio
// with a mid-range buy, a partial sell, a dividend, interest, USD cash, a 2:1
// split, a standalone fee row and a deposit with a fee, and checks on exact
// decimals that, with the costs counted as price, every day's price + income +
// fx + cash_fx equals the change in total_value less the day's flows.
func TestSweepAttributionAddsUp(t *testing.T) {
	for _, reporting := range []string{"TRY", "USD"} {
		t.Run(reporting, func(t *testing.T) { testSweepAttribution(t, reporting) })
	}
}

func testSweepAttribution(t *testing.T, reporting string) {
	ctx := context.Background()
	conn, err := db.Open(filepath.Join(t.TempDir(), "attribution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	d := &deps{
		users: user.NewRepo(conn), portfolios: portfolio.NewRepo(conn), instruments: instrument.NewRepo(conn),
		prices: price.NewRepo(conn), manualPrices: price.NewManualRepo(conn), transactions: transaction.NewRepo(conn),
		fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn),
	}
	usr, err := d.users.Create(ctx, "attribution@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.users.UpdateReportingCurrency(ctx, usr.ID, reporting); err != nil {
		t.Fatal(err)
	}
	pf, err := d.portfolios.Create(ctx, usr.ID, "Test")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, in := range []struct{ symbol, market, currency string }{{"TRYSTK", "BIST", "TRY"}, {"USDSTK", "US", "USD"}} {
		res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, in.symbol, in.market, in.symbol, in.currency)
		if err != nil {
			t.Fatal(err)
		}
		ids[in.symbol], _ = res.LastInsertId()
	}
	tryStock, usdStock := ids["TRYSTK"], ids["USDSTK"]

	day := func(n int) string { return time.Date(2024, 1, n, 0, 0, 0, 0, time.UTC).Format(time.DateOnly) }
	for n, rate := range map[int]string{1: "30", 2: "30.5", 3: "31", 4: "31", 5: "32.25", 6: "32", 7: "33.5", 8: "33", 9: "34", 10: "35", 11: "34.5", 12: "36"} {
		if err := d.fx.Upsert(ctx, "USD", day(n), rate, "test"); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		id      int64
		n       int
		closing string
	}{
		{tryStock, 1, "100"}, {tryStock, 3, "100"}, {tryStock, 4, "104"}, {tryStock, 6, "110"}, {tryStock, 7, "120"}, {tryStock, 8, "60"}, {tryStock, 10, "66"},
		{usdStock, 1, "100"}, {usdStock, 3, "100"}, {usdStock, 5, "105"}, {usdStock, 7, "103"}, {usdStock, 9, "110"}, {usdStock, 12, "108"},
	} {
		if err := d.prices.Upsert(ctx, p.id, day(p.n), p.closing, "test"); err != nil {
			t.Fatal(err)
		}
	}
	// The 2:1 split of the TRY stock is on day 8 (its close halves).
	if err := d.splits.Upsert(ctx, tryStock, day(8), dec("2"), "manual"); err != nil {
		t.Fatal(err)
	}
	trade := func(n int, typ string, id int64, qty, px, fee string) transaction.Transaction {
		cur := "TRY"
		if id == usdStock {
			cur = "USD"
		}
		return transaction.Transaction{PortfolioID: pf.ID, InstrumentID: &id, Type: typ, Date: day(n), Quantity: dec(qty), Price: dec(px), Amount: dec(qty).Mul(dec(px)), Fee: dec(fee), Currency: cur}
	}
	cash := func(n int, typ, currency, amount, fee string) transaction.Transaction {
		return transaction.Transaction{PortfolioID: pf.ID, Type: typ, Date: day(n), Amount: dec(amount), Fee: dec(fee), Currency: currency}
	}
	dividend := func(n int, id *int64, currency, amount, stopaj string) transaction.Transaction {
		return transaction.Transaction{PortfolioID: pf.ID, InstrumentID: id, Type: transaction.TypeDividend, Date: day(n), Amount: dec(amount), Fee: dec(stopaj), Currency: currency}
	}
	txs := []transaction.Transaction{
		cash(1, transaction.TypeDeposit, "TRY", "10000", "0"),
		cash(1, transaction.TypeDeposit, "USD", "2000", "0"),
		trade(3, transaction.TypeBuy, tryStock, "10", "100", "2"),
		trade(3, transaction.TypeBuy, usdStock, "5", "100", "1.5"),
		dividend(4, &tryStock, "TRY", "20", "2"),
		dividend(4, nil, "USD", "3", "0"),
		cash(5, transaction.TypeFee, "TRY", "3", "1"),
		cash(5, transaction.TypeDeposit, "TRY", "1000", "5"),
		trade(6, transaction.TypeSell, tryStock, "4", "110", "2"),
		cash(6, transaction.TypeWithdrawal, "USD", "100", "1"),
		trade(9, transaction.TypeSell, usdStock, "5", "110", "1"),
		dividend(11, &usdStock, "USD", "4", "0.5"),
	}
	if _, err := d.transactions.CreateAll(ctx, usr.ID, txs); err != nil {
		t.Fatal(err)
	}

	lists, err := scopeTransactions(ctx, d.portfolios, d.transactions, usr.ID, fmt.Sprint(pf.ID))
	if err != nil {
		t.Fatal(err)
	}
	from, to := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)
	s, err := (&historyHandler{d}).sweep(ctx, usr.ID, flatByDate(lists), from, to, sweepOpts{attribution: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.missingRates) != 0 {
		t.Fatalf("missing rates %v", s.missingRates)
	}

	n := len(s.totals) - 1
	if len(s.attribution) != n+1 {
		t.Fatalf("%d attribution days, want %d", len(s.attribution), n+1)
	}
	if a := s.attribution[0]; len(a.instruments) != 0 || !a.cashFX.IsZero() || !a.costs.IsZero() {
		t.Errorf("day 0 has a result: %+v", a)
	}
	sum := decimal.Zero
	for i := 1; i <= n; i++ {
		a := s.attribution[i]
		parts := a.interest.Add(a.cashFX).Add(a.costs)
		for id, r := range a.instruments {
			if !r.ok {
				t.Errorf("day %d instrument %d has no factor", i, id)
			}
			parts = parts.Add(r.price).Add(r.income).Add(r.fx)
		}
		want := s.totals[i].Sub(s.totals[i-1]).Sub(s.flows[i])
		if !parts.Equal(want) {
			t.Errorf("day %d: price+income+fx+cash_fx+costs = %s, want %s", i, parts, want)
		}
		sum = sum.Add(parts)
	}
	flows := decimal.Zero
	for _, f := range s.flows[1:] {
		flows = flows.Add(f)
	}
	if want := s.totals[n].Sub(s.totals[0]).Sub(flows); !sum.Equal(want) {
		t.Errorf("sum over days 1..%d = %s, want %s", n, sum, want)
	}

	// The split is on day 8, index 7: the close halves and the shares double.
	if r := s.attribution[7].instruments[tryStock]; !r.price.IsZero() {
		t.Errorf("split day price = %s, want 0", r.price)
	}
	// Sparse: the TRY stock has no fx when TRY is the reporting currency.
	if _, ok := s.attribution[7].instruments[tryStock]; ok && reporting == "TRY" {
		t.Error("split day keeps an all-zero bucket")
	}
}

// TestSweepAttributionDepositIsIncome checks that a term deposit's close
// rising by accrued interest lands in income, not price, and that the
// currency move on it stays fx.
func TestSweepAttributionDepositIsIncome(t *testing.T) {
	for _, tc := range []struct {
		name               string
		rate               string // USD/TRY on day 3; days 1 and 2 are 30
		wantIncome, wantFX string
	}{
		{"flat rate", "30", "300", "0"},
		{"rate up 10 percent", "33", "330", "3000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			conn, err := db.Open(filepath.Join(t.TempDir(), "deposit.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			d := &deps{
				users: user.NewRepo(conn), portfolios: portfolio.NewRepo(conn), instruments: instrument.NewRepo(conn),
				prices: price.NewRepo(conn), manualPrices: price.NewManualRepo(conn), transactions: transaction.NewRepo(conn),
				fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn),
			}
			usr, err := d.users.Create(ctx, "deposit@example.com", "hash")
			if err != nil {
				t.Fatal(err)
			}
			pf, err := d.portfolios.Create(ctx, usr.ID, "Test")
			if err != nil {
				t.Fatal(err)
			}
			res, err := conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, ?, ?, ?)`, "DEP-T1", instrument.MarketDeposit, "Deposit", "USD")
			if err != nil {
				t.Fatal(err)
			}
			dep, _ := res.LastInsertId()
			day := func(n int) string { return time.Date(2024, 1, n, 0, 0, 0, 0, time.UTC).Format(time.DateOnly) }
			for n, rate := range map[int]string{1: "30", 2: "30", 3: tc.rate} {
				if err := d.fx.Upsert(ctx, "USD", day(n), rate, "test"); err != nil {
					t.Fatal(err)
				}
			}
			for n, closing := range map[int]string{1: "1", 3: "1.01"} {
				if err := d.prices.Upsert(ctx, dep, day(n), closing, "deposit"); err != nil {
					t.Fatal(err)
				}
			}
			txs := []transaction.Transaction{
				{PortfolioID: pf.ID, Type: transaction.TypeDeposit, Date: day(1), Amount: dec("1000"), Currency: "USD"},
				{PortfolioID: pf.ID, InstrumentID: &dep, Type: transaction.TypeBuy, Date: day(1), Quantity: dec("1000"), Price: dec("1"), Amount: dec("1000"), Currency: "USD"},
			}
			if _, err := d.transactions.CreateAll(ctx, usr.ID, txs); err != nil {
				t.Fatal(err)
			}
			from, to := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
			s, err := (&historyHandler{d}).sweep(ctx, usr.ID, txs, from, to, sweepOpts{attribution: true})
			if err != nil {
				t.Fatal(err)
			}
			var priceSum, income, fxSum decimal.Decimal
			for _, a := range s.attribution {
				r := a.instruments[dep]
				priceSum, income, fxSum = priceSum.Add(r.price), income.Add(r.income), fxSum.Add(r.fx)
			}
			if !priceSum.IsZero() || !income.Equal(dec(tc.wantIncome)) || !fxSum.Equal(dec(tc.wantFX)) {
				t.Errorf("price %s income %s fx %s, want 0 %s %s", priceSum, income, fxSum, tc.wantIncome, tc.wantFX)
			}
		})
	}
}
