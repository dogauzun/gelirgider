package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
	"github.com/dogauzun/gelirgider/backend/internal/db"
	"github.com/dogauzun/gelirgider/backend/internal/deposit"
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
		fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn), deposits: deposit.NewRepo(conn),
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
				fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn), deposits: deposit.NewRepo(conn),
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

// attributionFixture seeds two portfolios in TRY for the range tests: P1 holds
// SB, which has no price on day 1 and gets its first one on day 4 (with a
// second buy of it before that), a fee row, a mid-range deposit and a
// dividend; P2 sells out of SC.
type attributionFixture struct {
	e                     *periodsEnv
	userID                int64
	session               string
	p1, p2                int64
	sa, sb, sc            int64
	from, to              time.Time
	fromStr, toStr, query string
}

func newAttributionFixture(t *testing.T) *attributionFixture {
	t.Helper()
	ctx := context.Background()
	f := &attributionFixture{e: newPeriodsEnv(t), fromStr: "2026-03-01", toStr: "2026-03-08"}
	f.userID, f.session = f.e.user(t, "range@example.com")
	f.p1, f.p2 = f.e.portfolio(t, f.userID), f.e.portfolio(t, f.userID)
	f.sa, f.sb, f.sc = f.e.instrument(t, "SA"), f.e.instrument(t, "SB"), f.e.instrument(t, "SC")
	f.from, _ = time.Parse(time.DateOnly, f.fromStr)
	f.to, _ = time.Parse(time.DateOnly, f.toStr)
	f.query = "?from=" + f.fromStr + "&to=" + f.toStr

	prices := price.NewRepo(f.e.conn)
	for _, p := range []struct {
		id          int64
		date, close string
	}{
		{f.sa, "2026-03-01", "100"}, {f.sa, "2026-03-05", "110"}, {f.sa, "2026-03-07", "105"},
		{f.sb, "2026-03-04", "60"}, {f.sb, "2026-03-06", "65"},
		{f.sc, "2026-03-01", "100"}, {f.sc, "2026-03-04", "120"},
	} {
		if err := prices.Upsert(ctx, p.id, p.date, p.close, "test"); err != nil {
			t.Fatal(err)
		}
	}
	trade := func(pf int64, date, typ string, id int64, qty, px string) transaction.Transaction {
		return transaction.Transaction{PortfolioID: pf, InstrumentID: &id, Type: typ, Date: date, Quantity: dec(qty), Price: dec(px), Amount: dec(qty).Mul(dec(px)), Fee: decimal.Zero, Currency: "TRY"}
	}
	cash := func(pf int64, date, typ, amount, fee string) transaction.Transaction {
		return transaction.Transaction{PortfolioID: pf, Type: typ, Date: date, Amount: dec(amount), Fee: dec(fee), Currency: "TRY"}
	}
	dividend := trade(f.p1, "2026-03-05", transaction.TypeDividend, f.sa, "0", "0")
	dividend.Amount, dividend.Fee = dec("20"), dec("2")
	txs := []transaction.Transaction{
		cash(f.p1, "2026-03-01", transaction.TypeDeposit, "10000", "0"),
		trade(f.p1, "2026-03-01", transaction.TypeBuy, f.sa, "10", "100"),
		trade(f.p1, "2026-03-01", transaction.TypeBuy, f.sb, "20", "50"),
		trade(f.p1, "2026-03-02", transaction.TypeBuy, f.sb, "10", "55"),
		cash(f.p1, "2026-03-03", transaction.TypeDeposit, "500", "0"),
		cash(f.p1, "2026-03-03", transaction.TypeFee, "7", "1"),
		dividend,
		cash(f.p2, "2026-03-01", transaction.TypeDeposit, "5000", "0"),
		trade(f.p2, "2026-03-01", transaction.TypeBuy, f.sc, "5", "100"),
		trade(f.p2, "2026-03-02", transaction.TypeBuy, f.sa, "5", "100"),
		trade(f.p2, "2026-03-05", transaction.TypeSell, f.sc, "5", "120"),
	}
	if _, err := transaction.NewRepo(f.e.conn).CreateAll(ctx, f.userID, txs); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *attributionFixture) get(t *testing.T, scope string) attributionResponse {
	t.Helper()
	var got attributionResponse
	f.e.get(t, f.session, "/api/portfolios/"+scope+"/attribution"+f.query, http.StatusOK, &got)
	return got
}

// TestRangeAttributionMatchesRangeInputs checks, on exact decimals, that the
// split over any range of days adds up to the change in total_value less the
// external flows rangeInputs counts, the first-price jump of SB included when
// the range opens before it; and that the rounded route buckets are within
// 0.02 of the same.
func TestRangeAttributionMatchesRangeInputs(t *testing.T) {
	f := newAttributionFixture(t)
	ctx := context.Background()
	conn := f.e.conn
	d := &deps{
		users: user.NewRepo(conn), portfolios: portfolio.NewRepo(conn), instruments: instrument.NewRepo(conn),
		prices: price.NewRepo(conn), manualPrices: price.NewManualRepo(conn), transactions: transaction.NewRepo(conn),
		fx: fx.NewRepo(conn), splits: split.NewRepo(conn), benchmarks: benchmark.NewRepo(conn), deposits: deposit.NewRepo(conn),
	}
	lists, err := scopeTransactions(ctx, d.portfolios, d.transactions, f.userID, fmt.Sprint(f.p1))
	if err != nil {
		t.Fatal(err)
	}
	s, err := (&historyHandler{d}).sweep(ctx, f.userID, flatByDate(lists), f.from, f.to, sweepOpts{attribution: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, jumped := s.firstPrice[f.sb]; !jumped {
		t.Fatal("fixture: SB gets no first price")
	}
	last := len(s.totals) - 1
	// wantChange is the change in total_value over days i..j less the
	// external flows of rangeInputs.
	wantChange := func(i, j int) decimal.Decimal {
		_, _, external, _ := s.rangeInputs(i, j)
		want := s.totals[j].Sub(s.totals[i])
		for _, e := range external {
			want = want.Sub(e)
		}
		return want
	}
	for _, r := range [][2]int{{0, last}, {2, last}, {0, 2}, {3, last}} {
		i, j := r[0], r[1]
		got := s.rangeAttribution(i, j)
		sum := got.interest.Add(got.cashFX).Add(got.costs)
		for _, v := range got.instruments {
			sum = sum.Add(v.price).Add(v.income).Add(v.fx)
		}
		if want := wantChange(i, j); !sum.Equal(want) {
			t.Errorf("days %d..%d: price+income+fx+cash_fx+costs = %s, want %s", i, j, sum, want)
		}
	}

	resp := f.get(t, fmt.Sprint(f.p1))
	rounded := dec(*resp.Total.Price).Add(dec(*resp.Total.Income)).Add(dec(*resp.Total.FX)).Add(dec(*resp.Total.CashFX))
	if want := wantChange(0, last); rounded.Sub(want).Abs().GreaterThan(dec("0.02")) {
		t.Errorf("rounded buckets %+v sum to %s, want %s within 0.02", resp.Total, rounded, want)
	}
	if resp.From != f.fromStr || resp.To != f.toStr || resp.Currency != "TRY" || len(resp.MissingRates) != 0 {
		t.Errorf("header = %s %s..%s missing %v", resp.Currency, resp.From, resp.To, resp.MissingRates)
	}
	for _, p := range resp.Positions {
		if p.InstrumentID == f.sb && (p.Price == nil || dec(*p.Price).IsZero()) {
			t.Errorf("SB price = %v, want its gain after the first price", p.Price)
		}
	}
}

// TestAttributionAllIsSumOfPortfolios checks that the `all` scope's totals
// and positions are the sum of the two portfolios' for the same range, and
// that closed marks the position P2 sold out of.
func TestAttributionAllIsSumOfPortfolios(t *testing.T) {
	f := newAttributionFixture(t)
	all, one, two := f.get(t, "all"), f.get(t, fmt.Sprint(f.p1)), f.get(t, fmt.Sprint(f.p2))
	add := func(a, b *string) string { return dec(*a).Add(dec(*b)).StringFixed(2) }
	for name, c := range map[string][3]*string{
		"price":   {all.Total.Price, one.Total.Price, two.Total.Price},
		"income":  {all.Total.Income, one.Total.Income, two.Total.Income},
		"fx":      {all.Total.FX, one.Total.FX, two.Total.FX},
		"cash_fx": {all.Total.CashFX, one.Total.CashFX, two.Total.CashFX},
	} {
		if *c[0] != add(c[1], c[2]) {
			t.Errorf("total %s = %s, want %s + %s", name, *c[0], *c[1], *c[2])
		}
	}
	if len(all.Positions) != 3 {
		t.Fatalf("positions = %+v, want SA, SB and SC", all.Positions)
	}
	byID := func(r attributionResponse) map[int64]attributionPosition {
		m := map[int64]attributionPosition{}
		for _, p := range r.Positions {
			m[p.InstrumentID] = p
		}
		return m
	}
	a, o, w := byID(all), byID(one), byID(two)
	zero := "0.00"
	for _, id := range []int64{f.sa, f.sb, f.sc} {
		po, pw := o[id], w[id]
		for _, p := range []*attributionPosition{&po, &pw} {
			if p.Total == nil { // not in that portfolio
				p.Price, p.Income, p.FX, p.Total = &zero, &zero, &zero, &zero
			}
		}
		if *a[id].Total != add(po.Total, pw.Total) || *a[id].Price != add(po.Price, pw.Price) || *a[id].Income != add(po.Income, pw.Income) {
			t.Errorf("instrument %d: all %+v, P1 %+v, P2 %+v", id, a[id], o[id], w[id])
		}
	}
	if !a[f.sc].Closed || a[f.sa].Closed || a[f.sb].Closed {
		t.Errorf("closed: SA %v SB %v SC %v, want false false true", a[f.sa].Closed, a[f.sb].Closed, a[f.sc].Closed)
	}
}

// TestAttributionMissingRate checks that a bucket a day could not convert is
// null for the range, and the currency is listed.
func TestAttributionMissingRate(t *testing.T) {
	e := newPeriodsEnv(t)
	userID, session := e.user(t, "missing@example.com")
	pf := e.portfolio(t, userID)
	res, err := e.conn.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES ('USDSTK', 'US', 'USDSTK', 'USD')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for _, date := range []string{"2026-03-01", "2026-03-03"} {
		if err := price.NewRepo(e.conn).Upsert(context.Background(), id, date, "10", "test"); err != nil {
			t.Fatal(err)
		}
	}
	txs := []transaction.Transaction{
		{PortfolioID: pf, Type: transaction.TypeDeposit, Date: "2026-03-01", Amount: dec("1000"), Currency: "USD"},
		{PortfolioID: pf, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2026-03-01", Quantity: dec("10"), Price: dec("10"), Amount: dec("100"), Currency: "USD"},
	}
	if _, err := transaction.NewRepo(e.conn).CreateAll(context.Background(), userID, txs); err != nil {
		t.Fatal(err)
	}
	var got attributionResponse
	e.get(t, session, fmt.Sprintf("/api/portfolios/%d/attribution?from=2026-03-01&to=2026-03-04", pf), http.StatusOK, &got)
	if got.Total != (attributionBuckets{}) || len(got.Positions) != 1 || got.Positions[0].Total != nil || got.Positions[0].Price != nil {
		t.Errorf("total %+v positions %+v, want every bucket null", got.Total, got.Positions)
	}
	if fmt.Sprint(got.MissingRates) != "[USD]" {
		t.Errorf("missing_rates = %v, want [USD]", got.MissingRates)
	}
}
