// Package holdings computes positions, per-currency cash, and P/L from a
// list of transactions. It is a pure function library: no HTTP, no side
// effects, and no database outside LatestCloses and MergedSeries, which only
// read the price repositories. Callers (the API layer) round for display and
// convert to a reporting currency; this package never rounds and Compute
// never converts currencies.
package holdings

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/deposit"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// MergeSeries merges fetched (from the shared prices table) and manual
// (a user's per-user overrides) into one date-ascending series
// with at most one row per date. Both inputs must already be sorted
// ascending by date (Repo.Range and ManualRepo.Range both return rows in
// that order; a single Latest row is a one-element slice). When both series
// have a row for the same date, manual wins: the user is correcting the
// fetched value for that date, not just filling a gap where no fetched
// price exists.
func MergeSeries(fetched, manual []price.Price) []price.Price {
	merged := make([]price.Price, 0, len(fetched)+len(manual))
	i, j := 0, 0
	for i < len(fetched) && j < len(manual) {
		switch {
		case fetched[i].Date < manual[j].Date:
			merged = append(merged, fetched[i])
			i++
		case fetched[i].Date > manual[j].Date:
			merged = append(merged, manual[j])
			j++
		default: // same date: manual wins
			merged = append(merged, manual[j])
			i++
			j++
		}
	}
	merged = append(merged, fetched[i:]...)
	merged = append(merged, manual[j:]...)
	return merged
}

// LatestClose picks the winning latest row for id out of fetched (the
// shared prices table) and manual (the user's overrides), the per-id
// counterpart of MergeSeries for callers that hold one Latest row per
// instrument. Manual wins when both rows carry the same date, otherwise the
// newer date wins. The second result is false when neither map has id.
func LatestClose(fetched, manual map[int64]price.Price, id int64) (price.Price, bool) {
	f, fok := fetched[id]
	m, mok := manual[id]
	switch {
	case !fok && !mok:
		return price.Price{}, false
	case !mok:
		return f, true
	case !fok:
		return m, true
	case f.Date > m.Date:
		return f, true
	default: // same date: manual wins
		return m, true
	}
}

// LatestCloses loads, for each of ids, the latest fetched row and userID's
// latest manual row dated on or before asOf and keeps the one LatestClose
// picks. Ids with neither row are omitted. A deposit of userID's has no
// stored prices: its close on asOf comes from its terms, dated asOf (not its
// maturity date, so a matured deposit never reads as stale), and is omitted
// before its start date.
func LatestCloses(ctx context.Context, src PriceSources, userID int64, ids []int64, asOf string) (map[int64]price.Price, error) {
	deposits, priced, err := splitDeposits(ctx, src, userID, ids)
	if err != nil {
		return nil, err
	}
	fetchedRows, err := src.Prices.Latest(ctx, priced, asOf)
	if err != nil {
		return nil, err
	}
	manualRows, err := src.Manual.Latest(ctx, userID, priced, asOf)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]price.Price, len(ids))
	for _, id := range priced {
		if won, ok := LatestClose(fetchedRows, manualRows, id); ok {
			out[id] = won
		}
	}
	for id, dep := range deposits {
		if c, ok := deposit.Close(dep.Terms, asOf); ok {
			out[id] = price.Price{InstrumentID: id, Date: asOf, Close: c, Source: deposit.Source}
		}
	}
	return out, nil
}

// splitDeposits looks up which of ids are userID's deposits and returns them
// with the remaining ids, in order.
func splitDeposits(ctx context.Context, src PriceSources, userID int64, ids []int64) (map[int64]deposit.Deposit, []int64, error) {
	if src.Deposits == nil {
		return nil, ids, nil
	}
	deposits, err := src.Deposits.ByInstrumentIDs(ctx, userID, ids)
	if err != nil {
		return nil, nil, err
	}
	priced := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := deposits[id]; !ok {
			priced = append(priced, id)
		}
	}
	return deposits, priced, nil
}

// PriceSources is what LatestCloses and MergedSeries read: the fetched closes
// and the user's manual prices that override them, and the user's deposits,
// whose closes come from their terms. Deposits may be nil: no deposits.
type PriceSources struct {
	Prices   *price.Repo
	Manual   *price.ManualRepo
	Deposits *deposit.Repo
}

// MergedSeries builds, for each of ids, the date-ascending series of its
// last price before from (however old: a delisted symbol or a long gap
// still has one) followed by every row in [from, to], fetched and
// userID's manual rows merged by MergeSeries. The seed is dated before
// from, so it never duplicates a Range row. Every id has an entry, empty
// when it has no price. A deposit of userID's has no stored prices: its
// series is its terms' close on each day from its start to its maturity,
// with the seed when it had started by then.
func MergedSeries(ctx context.Context, src PriceSources, userID int64, ids []int64, from, to time.Time) (map[int64][]price.Price, error) {
	deposits, ids, err := splitDeposits(ctx, src, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]price.Price, len(ids)+len(deposits))
	for id, dep := range deposits {
		out[id] = depositSeries(id, dep.Terms, from, to)
	}
	seedAsOf := from.AddDate(0, 0, -1).Format(time.DateOnly)
	fromStr, toStr := from.Format(time.DateOnly), to.Format(time.DateOnly)
	fetchedSeed, err := src.Prices.Latest(ctx, ids, seedAsOf)
	if err != nil {
		return nil, err
	}
	manualSeed, err := src.Manual.Latest(ctx, userID, ids, seedAsOf)
	if err != nil {
		return nil, err
	}
	fetchedRows, err := src.Prices.Range(ctx, ids, fromStr, toStr)
	if err != nil {
		return nil, err
	}
	manualRows, err := src.Manual.Range(ctx, userID, ids, fromStr, toStr)
	if err != nil {
		return nil, err
	}
	fetched := seeded(fetchedSeed, fetchedRows)
	manualByID := seeded(manualSeed, manualRows)
	for _, id := range ids {
		out[id] = MergeSeries(fetched[id], manualByID[id])
	}
	return out, nil
}

// depositSeries is a deposit's closes: the seed dated the day before from
// (when it had started by then, at its maturity value once matured), then
// one row per day of [from, to] from its start to its maturity.
func depositSeries(id int64, terms deposit.Terms, from, to time.Time) []price.Price {
	rows := []price.Price{}
	for day := from.AddDate(0, 0, -1); !day.After(to); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		if !day.Before(from) && date > terms.MaturityDate {
			break
		}
		if c, ok := deposit.Close(terms, date); ok {
			rows = append(rows, price.Price{InstrumentID: id, Date: date, Close: c, Source: deposit.Source})
		}
	}
	return rows
}

// seeded groups rows (sorted by instrument then date) per instrument, each
// group led by that instrument's seed row when it has one.
func seeded(seed map[int64]price.Price, rows []price.Price) map[int64][]price.Price {
	out := make(map[int64][]price.Price, len(seed))
	for id, p := range seed {
		out[id] = []price.Price{p}
	}
	for _, p := range rows {
		out[p.InstrumentID] = append(out[p.InstrumentID], p)
	}
	return out
}

// Position is one instrument's holding after processing all transactions.
// Quantity, cost, and value fields are all in Currency, the instrument's
// own currency -- Compute never converts them. Use Convert for a
// reporting-currency total. CostBasisReporting is the one exception.
type Position struct {
	InstrumentID int64
	Currency     string
	Quantity     decimal.Decimal
	AvgCost      decimal.Decimal
	CostBasis    decimal.Decimal
	LastPrice    decimal.Decimal
	MarketValue  decimal.Decimal
	UnrealizedPL decimal.Decimal
	RealizedPL   decimal.Decimal
	// RealizedCost is the cost of every share sold, the base RealizedPL is
	// a return on. It survives the position closing. It adds up
	// the same cost a sell, or a buy covering an oversold quantity,
	// subtracts from RealizedPL, so a short not yet covered can
	// leave it zero.
	RealizedCost decimal.Decimal
	// CostBasisReporting is CostBasis in the reporting currency, each buy
	// converted at its own trade date's rate. It is kept only
	// when State.FactorOn is set and is zero otherwise.
	CostBasisReporting decimal.Decimal
	// CostRateMissing reports that a buy of the open position had no rate
	// on its date, so CostBasisReporting leaves that buy out.
	CostRateMissing bool
	// HasPrice reports whether latest carried a price for this instrument.
	// When false, LastPrice is zero and MarketValue equals CostBasis.
	HasPrice bool
	// Splits are the splits that scaled Quantity, oldest first. A split on
	// no position or a closed one is skipped by Apply and not listed.
	Splits []Split
	// OpenedAt is the date of the buy that opened the current quantity,
	// empty when the position is closed or short. Merge does not combine it.
	OpenedAt string
}

// Trade is one sell row as the engine booked it. Proceeds is amount less fee;
// Cost is the average cost of the sold quantity, the amount Apply takes off
// the cost basis; Realized is Proceeds less Cost. OpenedAt is the date the
// position was opened, empty for a sell made while the quantity was zero or
// negative.
type Trade struct {
	Date                                    string
	InstrumentID                            int64
	Currency                                string
	Quantity, Proceeds, Fee, Cost, Realized decimal.Decimal
	OpenedAt                                string
}

// Split is one split applied to a position: its ex-date and new shares per
// old share.
type Split struct {
	Date  string
	Ratio decimal.Decimal
}

// Summary is the portfolio-wide result of Compute. Cash holds one entry per
// currency that ever moved (a buy, sell, dividend, deposit, withdrawal, or
// fee in that currency); a currency with no movement has no entry. All
// amounts stay in their own currency -- use Convert for a
// reporting-currency total.
type Summary struct {
	Positions []Position
	// Closed holds the positions dropped from Positions for having zero
	// quantity, in the same instrument-id order. Quantity, market value and
	// unrealized P/L are zero on them; RealizedPL is what the user earned
	// before closing, so the summary page can show where its realized total
	// came from instead of only totalling the open rows.
	Closed []Position
	Cash   map[string]decimal.Decimal
	// RealizedPL holds realized P/L per currency over every instrument the
	// transactions touched, including positions closed down to zero
	// quantity and dividends on instruments no longer held. Those are
	// dropped from Positions, but their realized P/L is still the user's,
	// so it is summed here and not over Positions.
	RealizedPL map[string]decimal.Decimal
	// Stats holds per currency what the user put in, took out, paid and
	// earned, keyed like Cash.
	Stats map[string]Stats
}

// Stats totals one currency's cash flows by kind. Every field is a positive
// amount in that currency: Fees counts every fee paid (on trades, cash
// movements, and standalone fee rows); DividendsTax is the stopaj withheld
// from dividends on an instrument; Interest is a dividend with no
// instrument, net of its fee.
type Stats struct {
	Deposits       decimal.Decimal
	Withdrawals    decimal.Decimal
	Fees           decimal.Decimal
	DividendsGross decimal.Decimal
	DividendsTax   decimal.Decimal
	Interest       decimal.Decimal
}

// Add returns the field-wise sum of a and b.
func (a Stats) Add(b Stats) Stats {
	return Stats{
		Deposits:       a.Deposits.Add(b.Deposits),
		Withdrawals:    a.Withdrawals.Add(b.Withdrawals),
		Fees:           a.Fees.Add(b.Fees),
		DividendsGross: a.DividendsGross.Add(b.DividendsGross),
		DividendsTax:   a.DividendsTax.Add(b.DividendsTax),
		Interest:       a.Interest.Add(b.Interest),
	}
}

// Scale returns a with every field multiplied by f.
func (a Stats) Scale(f decimal.Decimal) Stats {
	return Stats{
		Deposits:       a.Deposits.Mul(f),
		Withdrawals:    a.Withdrawals.Mul(f),
		Fees:           a.Fees.Mul(f),
		DividendsGross: a.DividendsGross.Mul(f),
		DividendsTax:   a.DividendsTax.Mul(f),
		Interest:       a.Interest.Mul(f),
	}
}

// IsZero reports whether every field is zero.
func (a Stats) IsZero() bool {
	return a.Deposits.IsZero() && a.Withdrawals.IsZero() && a.Fees.IsZero() &&
		a.DividendsGross.IsZero() && a.DividendsTax.IsZero() && a.Interest.IsZero()
}

// StatsOf is what one transaction adds to its currency's Stats, the single
// place that rule lives. A split adds nothing. A dividend's fee is its
// withholding tax (stopaj); one with no instrument is interest, counted net.
func StatsOf(t transaction.Transaction) Stats {
	var st Stats
	switch t.Type {
	case transaction.TypeBuy, transaction.TypeSell:
		st.Fees = t.Fee
	case transaction.TypeDeposit:
		st.Deposits, st.Fees = t.Amount, t.Fee
	case transaction.TypeWithdrawal:
		st.Withdrawals, st.Fees = t.Amount, t.Fee
	case transaction.TypeFee:
		st.Fees = t.Amount.Add(t.Fee)
	case transaction.TypeDividend:
		if t.InstrumentID != nil {
			st.DividendsGross, st.DividendsTax = t.Amount, t.Fee
		} else {
			st.Interest = t.Amount.Sub(t.Fee)
		}
	}
	return st
}

// State is the running result of applying transactions one at a time, in
// transaction.SortForApply order. Compute builds one internally and throws it away; the
// history endpoint keeps one across its day sweep, so the transactions are
// walked once for the whole series instead of once per day.
type State struct {
	// FactorOn returns the factor converting one unit of currency into the
	// reporting currency on date, and false when there is no rate. When
	// set, Apply keeps Position.CostBasisReporting; nil leaves it zero.
	FactorOn func(date, currency string) (decimal.Decimal, bool)

	// Trades holds one entry per sell, appended in apply order. Compute
	// discards it with the state.
	Trades []Trade

	positions map[int64]*Position
	cash      map[string]decimal.Decimal
	// realized is the positions' RealizedPL summed per currency as Apply
	// goes, with an entry for every currency a position is in, so
	// SummarizeAll need not add it up over every position each day.
	realized   map[string]decimal.Decimal
	stats      map[string]*Stats
	currencyOf map[int64]string
}

// NewState returns an empty State. currencyOf maps instrument id to its
// currency; an instrument absent from currencyOf is treated as TRY.
func NewState(currencyOf map[int64]string) *State {
	return &State{
		positions:  map[int64]*Position{},
		cash:       map[string]decimal.Decimal{},
		realized:   map[string]decimal.Decimal{},
		stats:      map[string]*Stats{},
		currencyOf: currencyOf,
	}
}

func (s *State) position(id int64) *Position {
	p, ok := s.positions[id]
	if !ok {
		cur := s.currencyOf[id]
		if cur == "" {
			cur = "TRY"
		}
		p = &Position{InstrumentID: id, Currency: cur}
		s.positions[id] = p
		if _, ok := s.realized[cur]; !ok {
			s.realized[cur] = decimal.Zero
		}
	}
	return p
}

func (s *State) addCash(currency string, amount decimal.Decimal) {
	s.cash[currency] = s.cash[currency].Add(amount)
}

func (s *State) stat(currency string) *Stats {
	st, ok := s.stats[currency]
	if !ok {
		st = &Stats{}
		s.stats[currency] = st
	}
	return st
}

// Apply folds one transaction into s. Transactions must arrive in
// transaction.SortForApply order: weighted-average cost depends on it.
func (s *State) Apply(t transaction.Transaction) {
	// A split books no stats, and must not create a by_currency entry.
	if t.Type != transaction.TypeSplit {
		st := s.stat(t.Currency)
		*st = st.Add(StatsOf(t))
	}
	switch t.Type {
	case transaction.TypeBuy:
		p := s.position(*t.InstrumentID)
		cost := t.Amount.Add(t.Fee)
		// A buy that covers an oversold quantity is that part of the sell
		// in reverse: the sell booked its proceeds less short, its share of
		// the negative CostBasis, so the cover books short back less what
		// covering cost, and only the rest of the buy is cost basis.
		if p.Quantity.IsNegative() {
			oversold, short, coverCost := p.Quantity.Neg(), p.CostBasis, cost
			switch {
			case t.Quantity.LessThan(oversold):
				short = p.CostBasis.Mul(t.Quantity).Div(oversold)
			case t.Quantity.GreaterThan(oversold):
				coverCost = cost.Mul(oversold).Div(t.Quantity)
			}
			realized := short.Neg().Sub(coverCost)
			p.RealizedPL = p.RealizedPL.Add(realized)
			p.RealizedCost = p.RealizedCost.Add(coverCost.Add(short))
			s.realized[p.Currency] = s.realized[p.Currency].Add(realized)
			if s.FactorOn != nil && !p.CostBasis.IsZero() {
				p.CostBasisReporting = p.CostBasisReporting.Mul(p.CostBasis.Sub(short)).Div(p.CostBasis)
			}
			p.CostBasis = p.CostBasis.Sub(short)
			cost = cost.Sub(coverCost)
		}
		if !p.Quantity.IsPositive() && p.Quantity.Add(t.Quantity).IsPositive() {
			p.OpenedAt = t.Date
		}
		p.Quantity = p.Quantity.Add(t.Quantity)
		p.CostBasis = p.CostBasis.Add(cost)
		if s.FactorOn != nil {
			if f, ok := s.FactorOn(t.Date, t.Currency); ok {
				p.CostBasisReporting = p.CostBasisReporting.Add(cost.Mul(f))
			} else {
				p.CostRateMissing = true
			}
		}
		if !p.Quantity.IsZero() {
			p.AvgCost = p.CostBasis.Div(p.Quantity)
		}
		p.resetIfClosed()
		s.addCash(t.Currency, t.Amount.Add(t.Fee).Neg())

	case transaction.TypeSell:
		p := s.position(*t.InstrumentID)
		sold := t.Quantity.Mul(p.AvgCost)
		realized := t.Amount.Sub(t.Fee).Sub(sold)
		s.Trades = append(s.Trades, Trade{Date: t.Date, InstrumentID: p.InstrumentID, Currency: p.Currency, Quantity: t.Quantity, Proceeds: t.Amount.Sub(t.Fee), Fee: t.Fee, Cost: sold, Realized: realized, OpenedAt: p.OpenedAt})
		p.RealizedPL = p.RealizedPL.Add(realized)
		p.RealizedCost = p.RealizedCost.Add(sold)
		s.realized[p.Currency] = s.realized[p.Currency].Add(realized)
		// The reporting cost shrinks by the same ratio as the native one.
		if s.FactorOn != nil && !p.CostBasis.IsZero() {
			p.CostBasisReporting = p.CostBasisReporting.Mul(p.CostBasis.Sub(sold)).Div(p.CostBasis)
		}
		p.CostBasis = p.CostBasis.Sub(sold)
		p.Quantity = p.Quantity.Sub(t.Quantity)
		// AvgCost is intentionally left unchanged while shares remain.
		p.resetIfClosed()
		s.addCash(t.Currency, t.Amount.Sub(t.Fee))

	// A dividend's fee is its withholding tax (stopaj); one with no
	// instrument is interest, counted net.
	case transaction.TypeDividend:
		net := t.Amount.Sub(t.Fee)
		if t.InstrumentID != nil {
			p := s.position(*t.InstrumentID)
			p.RealizedPL = p.RealizedPL.Add(net)
			s.realized[p.Currency] = s.realized[p.Currency].Add(net)
		}
		s.addCash(t.Currency, net)

	// A fee on a cash movement is a real cost, like it is on a buy or a
	// sell: a deposit brings in less than its amount, a withdrawal takes
	// out more.
	case transaction.TypeDeposit:
		s.addCash(t.Currency, t.Amount.Sub(t.Fee))

	case transaction.TypeWithdrawal:
		s.addCash(t.Currency, t.Amount.Add(t.Fee).Neg())

	case transaction.TypeFee:
		s.addCash(t.Currency, t.Amount.Add(t.Fee).Neg())

	// A split scales the held quantity by the ratio in t.Quantity and the
	// average cost down by it; cost basis, realized P/L and cash stay put.
	// No position, or a closed one, has nothing to scale.
	case transaction.TypeSplit:
		p, ok := s.positions[*t.InstrumentID]
		if !ok || p.Quantity.IsZero() {
			return
		}
		p.Quantity = p.Quantity.Mul(t.Quantity)
		p.AvgCost = p.AvgCost.Div(t.Quantity)
		p.Splits = append(p.Splits, Split{Date: t.Date, Ratio: t.Quantity})
	}
}

// resetIfClosed zeroes the cost of a position whose quantity reached 0, so
// what is left of it (a rounding remainder of the average cost) is not
// folded into the next buy's average cost.
func (p *Position) resetIfClosed() {
	// A sell that leaves the position short ends the opening too.
	if !p.Quantity.IsPositive() {
		p.OpenedAt = ""
	}
	if p.Quantity.IsZero() {
		p.CostBasis = decimal.Zero
		p.AvgCost = decimal.Zero
		// The zero value, as on a fresh Position, so a nil FactorOn leaves
		// the field exactly as it was.
		p.CostBasisReporting = decimal.Decimal{}
		p.CostRateMissing = false
	}
}

// Quantity is the quantity of instrumentID held after the rows applied so
// far; zero when none touched it.
func (s *State) Quantity(instrumentID int64) decimal.Decimal {
	if p, ok := s.positions[instrumentID]; ok {
		return p.Quantity
	}
	return decimal.Zero
}

// Summarize snapshots s: positions closed down to zero quantity go to
// Summary.Closed instead of Positions,
// the rest are valued with latest, which maps instrument id to its most
// recently known price in the instrument's own currency (an instrument
// absent from latest is valued at cost). What it returns is copied out of
// the running state, so a caller can keep applying transactions and
// snapshot again without an earlier Summary changing underneath it.
func (s *State) Summarize(latest map[int64]decimal.Decimal) Summary {
	ids := make([]int64, 0, len(s.positions))
	for id := range s.positions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	summary := Summary{Cash: maps.Clone(s.cash), RealizedPL: map[string]decimal.Decimal{}, Stats: make(map[string]Stats, len(s.stats))}
	for c, st := range s.stats {
		summary.Stats[c] = *st
	}
	for _, id := range ids {
		p := *s.positions[id]
		summary.RealizedPL[p.Currency] = summary.RealizedPL[p.Currency].Add(p.RealizedPL)
		if p.Quantity.IsZero() {
			summary.Closed = append(summary.Closed, p)
			continue
		}
		if price, ok := latest[id]; ok {
			p.LastPrice = price
			p.HasPrice = true
			p.MarketValue = p.Quantity.Mul(price)
		} else {
			p.MarketValue = p.CostBasis
		}
		p.UnrealizedPL = p.MarketValue.Sub(p.CostBasis)
		summary.Positions = append(summary.Positions, p)
	}

	return summary
}

// Merge sums per-portfolio summaries into one, for the all-portfolios view.
// Cash, realized P/L and stats add up per currency; positions add up
// per instrument over Positions and Closed alike, so an instrument closed
// in one portfolio and open in another is one open row carrying both
// realized P/Ls. A row goes to Closed only when every input row was closed:
// one long in a portfolio and oversold by as much in another sums to
// quantity zero but stays in Positions, with its value and cost, so the
// totals equal the per-portfolio sums. LastPrice and
// HasPrice come from any input that has a price (every input was valued
// with the same latest map), and splits are unioned by date.
//
// One summary is returned as is: summing it alone changes nothing but
// AvgCost, which would become CostBasis / Quantity instead of the average
// cost Apply keeps across sells, the one position history reports too.
// The history sweep uses SummarizeAll instead.
func Merge(summaries []Summary) Summary {
	if len(summaries) == 1 {
		return summaries[0]
	}
	out := Summary{Cash: map[string]decimal.Decimal{}, RealizedPL: map[string]decimal.Decimal{}, Stats: map[string]Stats{}}
	byID := map[int64]*Position{}
	open := map[int64]bool{}
	for _, s := range summaries {
		for _, p := range s.Positions {
			open[p.InstrumentID] = true
		}
		for c, v := range s.Stats {
			out.Stats[c] = out.Stats[c].Add(v)
		}
		for c, v := range s.Cash {
			out.Cash[c] = out.Cash[c].Add(v)
		}
		for c, v := range s.RealizedPL {
			out.RealizedPL[c] = out.RealizedPL[c].Add(v)
		}
		for _, in := range append(slices.Clone(s.Positions), s.Closed...) {
			m, ok := byID[in.InstrumentID]
			if !ok {
				m = &Position{InstrumentID: in.InstrumentID, Currency: in.Currency}
				byID[in.InstrumentID] = m
			}
			m.combine(in)
		}
	}

	ids := slices.Sorted(maps.Keys(byID))
	for _, id := range ids {
		p := *byID[id]
		sort.SliceStable(p.Splits, func(i, j int) bool { return p.Splits[i].Date < p.Splits[j].Date })
		if !open[id] {
			out.Closed = append(out.Closed, p)
			continue
		}
		if !p.Quantity.IsZero() {
			p.AvgCost = p.CostBasis.Div(p.Quantity)
		}
		out.Positions = append(out.Positions, p)
	}
	return out
}

// combine adds in, one input row of an instrument, into m for Merge. It
// leaves OpenedAt empty, as SummarizeAll does.
func (m *Position) combine(in Position) {
	m.Quantity = m.Quantity.Add(in.Quantity)
	m.CostBasis = m.CostBasis.Add(in.CostBasis)
	m.MarketValue = m.MarketValue.Add(in.MarketValue)
	m.UnrealizedPL = m.UnrealizedPL.Add(in.UnrealizedPL)
	m.RealizedPL = m.RealizedPL.Add(in.RealizedPL)
	m.RealizedCost = m.RealizedCost.Add(in.RealizedCost)
	m.CostBasisReporting = m.CostBasisReporting.Add(in.CostBasisReporting)
	m.CostRateMissing = m.CostRateMissing || in.CostRateMissing
	if in.HasPrice {
		m.LastPrice, m.HasPrice = in.LastPrice, true
	}
	for _, sp := range in.Splits {
		if !slices.ContainsFunc(m.Splits, func(x Split) bool { return x.Date == sp.Date }) {
			m.Splits = append(m.Splits, sp)
		}
	}
}

// SummarizeAll is Merge over each state's Summarize(latest), cut down to
// what the history sweep reads every day: Cash and RealizedPL per
// currency, and Positions with Quantity, CostBasis, LastPrice, HasPrice,
// MarketValue and UnrealizedPL summed per instrument. An instrument closed
// in every state is left out, as Merge moves it to Closed; one whose open
// quantities only sum to zero is kept, as in Merge. Every
// other field (Closed, Stats, AvgCost, RealizedCost, Splits,
// CostBasisReporting, CostRateMissing) is left zero: building them and copying each position
// twice was most of the `all` history's cost.
func SummarizeAll(states []*State, latest map[int64]decimal.Decimal) Summary {
	out := Summary{Cash: map[string]decimal.Decimal{}, RealizedPL: map[string]decimal.Decimal{}}
	// Positions are built in place, found through byID, and sorted once
	// at the end: a pointer per position and a growing slice of copies
	// were most of what the history sweep allocated each day.
	n := 0
	for _, s := range states {
		n += len(s.positions)
	}
	out.Positions = make([]Position, 0, n)
	byID := make(map[int64]int, n)
	for _, s := range states {
		for c, v := range s.cash {
			out.Cash[c] = out.Cash[c].Add(v)
		}
		for c, v := range s.realized {
			out.RealizedPL[c] = out.RealizedPL[c].Add(v)
		}
		for id, p := range s.positions {
			if p.Quantity.IsZero() {
				continue
			}
			price, hasPrice := latest[id]
			value := p.CostBasis
			if hasPrice {
				value = p.Quantity.Mul(price)
			}
			i, ok := byID[id]
			if !ok {
				byID[id] = len(out.Positions)
				out.Positions = append(out.Positions, Position{InstrumentID: id, Currency: p.Currency, Quantity: p.Quantity, CostBasis: p.CostBasis, LastPrice: price, HasPrice: hasPrice, MarketValue: value})
				continue
			}
			m := &out.Positions[i]
			m.Quantity = m.Quantity.Add(p.Quantity)
			m.CostBasis = m.CostBasis.Add(p.CostBasis)
			m.MarketValue = m.MarketValue.Add(value)
		}
	}
	for i := range out.Positions {
		p := &out.Positions[i]
		// The sum of each portfolio's value minus cost, taken once.
		p.UnrealizedPL = p.MarketValue.Sub(p.CostBasis)
	}
	slices.SortFunc(out.Positions, func(a, b Position) int { return cmp.Compare(a.InstrumentID, b.InstrumentID) })
	return out
}

// Compute derives positions and per-currency cash from txs using
// weighted-average cost. latest maps instrument id to its most recently
// known price, in the instrument's own currency; an instrument absent from
// latest is valued at cost. currencyOf maps instrument id to its currency;
// an instrument absent from currencyOf is treated as TRY. txs is not
// mutated.
func Compute(txs []transaction.Transaction, latest map[int64]decimal.Decimal, currencyOf map[int64]string) Summary {
	sorted := make([]transaction.Transaction, len(txs))
	copy(sorted, txs)
	transaction.SortForApply(sorted)

	state := NewState(currencyOf)
	for _, t := range sorted {
		state.Apply(t)
	}
	return state.Summarize(latest)
}

// Gap is one day a currency's cash reached a new low below zero: Amount is
// how much further below the previous low it went.
type Gap struct {
	Date     string
	Currency string
	Amount   decimal.Decimal
}

// maxDepositPlaces is the most fraction digits a deposit amount takes; a
// trade's amount, quantity × price, can carry more.
const maxDepositPlaces = 9

// CashGaps walks one portfolio's cash per currency in date order, with
// the same rules as Compute, and returns a Gap at the end of every day
// whose balance is below the lowest covered so far (starting at 0). Each
// gap is rounded up to maxDepositPlaces, so it can be recorded as a
// deposit, and the low moves down by the gap, not to the balance. The gaps
// of a currency sum to its deepest shortfall, rounded up, so depositing
// each on its day keeps that currency's cash from ever going below zero.
// Ordered by date, then currency. txs is not mutated.
func CashGaps(txs []transaction.Transaction) []Gap {
	sorted := slices.Clone(txs)
	transaction.SortForApply(sorted)

	state := NewState(nil)
	low := map[string]decimal.Decimal{}
	touched := map[string]bool{}
	var gaps []Gap
	for i, t := range sorted {
		state.Apply(t)
		touched[t.Currency] = true
		if i+1 < len(sorted) && sorted[i+1].Date == t.Date {
			continue
		}
		for _, c := range slices.Sorted(maps.Keys(touched)) {
			if bal := state.cash[c]; bal.LessThan(low[c]) {
				amount := low[c].Sub(bal).RoundCeil(maxDepositPlaces)
				gaps = append(gaps, Gap{Date: t.Date, Currency: c, Amount: amount})
				low[c] = low[c].Sub(amount)
			}
		}
		clear(touched)
	}
	return gaps
}

// Totals is a Summary converted into a single reporting currency, for
// display and aggregation.
type Totals struct {
	Cash         decimal.Decimal
	TotalValue   decimal.Decimal
	UnrealizedPL decimal.Decimal
	RealizedPL   decimal.Decimal
	// PositionsValue and CostBasis are the open positions' market value and
	// cost, summed unrounded like TotalValue so a caller rounds each once
	// instead of subtracting rounded totals.
	PositionsValue decimal.Decimal
	CostBasis      decimal.Decimal
	// Missing lists the currencies whose amounts were left out of the totals
	// above because no conversion factor could be built -- mirrors
	// Position.HasPrice. It names the currency that was *excluded*, which is
	// not always the currency whose rate is absent: the factor is
	// rates[currency] / rates[reporting], so a missing reporting rate
	// excludes every other currency. With reporting USD and no USD row in
	// rates, TRY cash is listed here even though TRY needs no rate (it is
	// implied 1) -- the absent rate is USD's. Callers that show this list to
	// a user must phrase it as "excluded for lack of a rate", never as
	// "these currencies have no rate".
	Missing []string
}

// Factor returns the multiplier that converts an amount in currency into
// reporting, rates being TRY per unit of currency with TRY implied 1:
// rates[currency] / rates[reporting]. A currency equal to reporting converts
// at 1 with no rate lookup. ok is false when either rate is absent. It is the
// rule Convert applies to every amount.
func Factor(rates map[string]decimal.Decimal, currency, reporting string) (decimal.Decimal, bool) {
	if currency == reporting {
		return decimal.NewFromInt(1), true
	}
	rateOf := func(c string) (decimal.Decimal, bool) {
		if c == "TRY" {
			return decimal.NewFromInt(1), true
		}
		r, ok := rates[c]
		return r, ok
	}
	from, ok := rateOf(currency)
	if !ok {
		return decimal.Decimal{}, false
	}
	to, ok := rateOf(reporting)
	if !ok {
		return decimal.Decimal{}, false
	}
	return from.Div(to), true
}

// Convert converts s into reporting using rates, TRY per unit of currency
// (fx_rates has no TRY row, so TRY is implied 1 here too). The conversion
// factor from currency A to reporting is rates[A] / rates[reporting]. A
// currency equal to reporting always converts at 1, with no rate lookup --
// it needs no market rate to know it is already itself. Any other currency
// whose factor cannot be built -- because its own rate or the reporting
// currency's rate is absent -- is skipped and listed in Missing. See
// Totals.Missing on why the listed currency is not always the one whose
// rate is absent.
//
// Realized P/L and cost basis convert at the same valuation-date rate as
// everything else: a simplification (ADR 0003) that does not separate FX
// gain/loss from price gain/loss. A caller that needs cost at trade-date
// rates sets State.FactorOn and reads Position.CostBasisReporting instead;
// Convert does not use it.
func Convert(s Summary, rates map[string]decimal.Decimal, reporting string) Totals {
	missing := map[string]bool{}
	// One Factor per currency: each is a 16-digit division.
	factors := map[string]decimal.Decimal{}
	factor := func(currency string) (decimal.Decimal, bool) {
		if f, ok := factors[currency]; ok {
			return f, true
		}
		f, ok := Factor(rates, currency, reporting)
		if !ok {
			missing[currency] = true
			return f, false
		}
		factors[currency] = f
		return f, true
	}

	currencies := make([]string, 0, len(s.Cash))
	for currency := range s.Cash {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)

	var totals Totals
	for _, currency := range currencies {
		f, ok := factor(currency)
		if !ok {
			continue
		}
		converted := s.Cash[currency].Mul(f)
		totals.Cash = totals.Cash.Add(converted)
		totals.TotalValue = totals.TotalValue.Add(converted)
	}

	// The positions are summed per currency first and each sum converted
	// once: decimal Add and Mul never round, so this is the same total as
	// converting every position, without a 16-digit product per position
	// that each Add then rescales to.
	type sums struct{ value, unrealized, cost decimal.Decimal }
	byCurrency := map[string]*sums{}
	for _, p := range s.Positions {
		c, ok := byCurrency[p.Currency]
		if !ok {
			c = &sums{}
			byCurrency[p.Currency] = c
		}
		c.value = c.value.Add(p.MarketValue)
		c.unrealized = c.unrealized.Add(p.UnrealizedPL)
		c.cost = c.cost.Add(p.CostBasis)
	}
	for currency, c := range byCurrency {
		f, ok := factor(currency)
		if !ok {
			continue
		}
		value := c.value.Mul(f)
		totals.TotalValue = totals.TotalValue.Add(value)
		totals.UnrealizedPL = totals.UnrealizedPL.Add(c.unrealized.Mul(f))
		totals.PositionsValue = totals.PositionsValue.Add(value)
		totals.CostBasis = totals.CostBasis.Add(c.cost.Mul(f))
	}

	// Realized P/L comes from s.RealizedPL, not from s.Positions: a closed
	// position is not listed there, but its realized P/L still counts.
	realizedCurrencies := make([]string, 0, len(s.RealizedPL))
	for currency := range s.RealizedPL {
		realizedCurrencies = append(realizedCurrencies, currency)
	}
	sort.Strings(realizedCurrencies)
	for _, currency := range realizedCurrencies {
		f, ok := factor(currency)
		if !ok {
			continue
		}
		totals.RealizedPL = totals.RealizedPL.Add(s.RealizedPL[currency].Mul(f))
	}

	for m := range missing {
		totals.Missing = append(totals.Missing, m)
	}
	sort.Strings(totals.Missing)

	return totals
}
