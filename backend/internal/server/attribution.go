package server

import (
	"slices"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/shopspring/decimal"
)

// dayInput is what attributeDay splits one day's return from: the day d and
// the day before it, d-1. Amounts are native, in their own currency; rates
// are TRY per unit of currency (holdings.Factor).
type dayInput struct {
	// prevValue and value are the native market value per instrument id at
	// the end of d-1 and d (holdings.Position.MarketValue: the cost basis of
	// a position with no price yet, as total_value counts it). A position
	// closed on d is absent from value.
	prevValue, value map[int64]decimal.Decimal
	currencyOf       map[int64]string // instrument id to currency, TRY when absent
	// tradeCash is the day's cash spent on each instrument: a buy's amount
	// plus fee, a sell's minus amount plus fee. dividend is the day's
	// dividend net of stopaj per instrument.
	tradeCash, dividend map[int64]decimal.Decimal
	// interest is the day's interest net, costs the day's other cash costs (a
	// standalone fee row's amount plus fee, the fee on a deposit or a
	// withdrawal), both per currency. prevCash is the native cash per currency
	// at the end of d-1.
	interest, costs, prevCash map[string]decimal.Decimal
	prevRates, rates          map[string]decimal.Decimal
	reporting                 string
}

// instrumentReturn is one instrument's share of a day's return, in the
// reporting currency. ok is false when its currency had no factor on d-1 or
// on d, and the three amounts are then zero.
type instrumentReturn struct {
	price, income, fx decimal.Decimal
	ok                bool
}

// dayReturn is one day's return in the reporting currency, split into price,
// income and currency (the identity is at attributeDay). instruments holds
// only the instruments with a non-zero amount or no factor, so a quiet day
// stays small. Interest is income at scope level; costs is negative and, like
// the instruments' price, part of the price return.
type dayReturn struct {
	instruments map[int64]instrumentReturn
	interest    decimal.Decimal
	cashFX      decimal.Decimal
	costs       decimal.Decimal
	// missing lists, sorted, the currencies with no factor on d-1 or on d
	// that the day's inputs touched: what was left out of the figures above.
	missing []string
}

// dayFactors caches each currency's factor on d-1 and on d.
type dayFactors struct {
	in      *dayInput
	cache   map[string][2]decimal.Decimal
	missing map[string]bool
}

// of returns the factors of currency on d-1 and d; ok is false, and the
// currency is noted as missing, when either has none.
func (f *dayFactors) of(currency string) (prev, now decimal.Decimal, ok bool) {
	if pair, found := f.cache[currency]; found {
		return pair[0], pair[1], true
	}
	if f.missing[currency] {
		return prev, now, false
	}
	prev, okPrev := holdings.Factor(f.in.prevRates, currency, f.in.reporting)
	now, okNow := holdings.Factor(f.in.rates, currency, f.in.reporting)
	if !okPrev || !okNow {
		f.missing[currency] = true
		return prev, now, false
	}
	f.cache[currency] = [2]decimal.Decimal{prev, now}
	return prev, now, true
}

// attributeDay splits the return of day d against d-1 into price, income and
// currency, in the reporting currency. With f the factor into reporting:
//
//	fx      = MV_prev * (f_d - f_prev)
//	price   = (MV_d - MV_prev - tradeCash) * f_d
//	income  = net * f_d, at the pay-date rate
//	cash_fx = sum cash_prev * (f_d - f_prev)
//	costs   = -sum cost * f_d
//
// so, with every rate present, price + income + interest + fx + cashFX +
// costs equals total_d - total_prev - flow_d exactly, where flow_d is the
// day's deposits less withdrawals at f_d. Costs have no position; scope level
// they count as price. A split day has price 0 for the instrument: the
// quantity and the close move opposite ways and no trade cash is booked.
func attributeDay(in dayInput) dayReturn {
	f := &dayFactors{in: &in, cache: map[string][2]decimal.Decimal{}, missing: map[string]bool{}}
	out := dayReturn{instruments: map[int64]instrumentReturn{}}

	// A position closed today is in prevValue only, and a dividend can come
	// on an instrument no longer held.
	ids := map[int64]bool{}
	for _, m := range []map[int64]decimal.Decimal{in.prevValue, in.value, in.tradeCash, in.dividend} {
		for id := range m {
			ids[id] = true
		}
	}
	for id := range ids {
		currency := in.currencyOf[id]
		if currency == "" {
			currency = "TRY"
		}
		prev, now, ok := f.of(currency)
		if !ok {
			out.instruments[id] = instrumentReturn{}
			continue
		}
		r := instrumentReturn{
			ok:     true,
			fx:     in.prevValue[id].Mul(now.Sub(prev)),
			price:  in.value[id].Sub(in.prevValue[id]).Sub(in.tradeCash[id]).Mul(now),
			income: in.dividend[id].Mul(now),
		}
		if !r.price.IsZero() || !r.income.IsZero() || !r.fx.IsZero() {
			out.instruments[id] = r
		}
	}

	for currency, cash := range in.prevCash {
		if prev, now, ok := f.of(currency); ok {
			out.cashFX = out.cashFX.Add(cash.Mul(now.Sub(prev)))
		}
	}
	for currency, net := range in.interest {
		if _, now, ok := f.of(currency); ok {
			out.interest = out.interest.Add(net.Mul(now))
		}
	}
	for currency, cost := range in.costs {
		if _, now, ok := f.of(currency); ok {
			out.costs = out.costs.Sub(cost.Mul(now))
		}
	}
	for currency := range f.missing {
		out.missing = append(out.missing, currency)
	}
	slices.Sort(out.missing)
	return out
}
