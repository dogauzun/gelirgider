package server

import (
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
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
	// gaps flags the scope-level amounts above left out for lack of a factor.
	gaps scopeGaps
	// held lists the instruments with an open position at the end of d.
	held []int64
	// missing lists, sorted, the currencies with no factor on d-1 or on d
	// that the day's inputs touched: what was left out of the figures above.
	missing []string
}

// scopeGaps flags which scope-level amounts a day, or a range, could not
// convert.
type scopeGaps struct{ interest, cashFX, costs bool }

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
		} else {
			out.gaps.cashFX = true
		}
	}
	for currency, net := range in.interest {
		if _, now, ok := f.of(currency); ok {
			out.interest = out.interest.Add(net.Mul(now))
		} else {
			out.gaps.interest = true
		}
	}
	for currency, cost := range in.costs {
		if _, now, ok := f.of(currency); ok {
			out.costs = out.costs.Sub(cost.Mul(now))
		} else {
			out.gaps.costs = true
		}
	}
	out.missing = holdings.SortedKeys(f.missing)
	return out
}

// instrumentSum is one instrument's return over a range, in the reporting
// currency. missing is set when a day of the range could not convert it.
type instrumentSum struct {
	price, income, fx decimal.Decimal
	missing           bool
}

// rangeReturn is the return split over a range of days: per instrument, and
// at scope level the interest, the cash fx and the costs (negative, part of
// the price return). gaps flags the scope-level amounts a day could not
// convert; missingRates is the union of the days' missing, sorted, never nil.
type rangeReturn struct {
	instruments             map[int64]*instrumentSum
	interest, cashFX, costs decimal.Decimal
	gaps                    scopeGaps
	missingRates            []string
}

// rangeAttribution sums the days i+1..j of the sweep's return split, the way
// rangeInputs cuts the same range for the returns: day i's own move is inside
// its value, and the first-price jump of an instrument that day i held
// unpriced is a flow, so it comes off that instrument's price (a term
// deposit's, income). Every instrument held on a day i..j or active on a day
// i+1..j is listed.
func (s *historySweep) rangeAttribution(i, j int) rangeReturn {
	out := rangeReturn{instruments: map[int64]*instrumentSum{}}
	of := func(id int64) *instrumentSum {
		if out.instruments[id] == nil {
			out.instruments[id] = &instrumentSum{}
		}
		return out.instruments[id]
	}
	missing := map[string]bool{}
	for d := i; d <= j; d++ {
		day := s.attribution[d]
		for _, id := range day.held {
			of(id)
		}
		if d == i {
			continue
		}
		for id, r := range day.instruments {
			t := of(id)
			t.missing = t.missing || !r.ok
			t.price, t.income, t.fx = t.price.Add(r.price), t.income.Add(r.income), t.fx.Add(r.fx)
		}
		out.interest, out.cashFX, out.costs = out.interest.Add(day.interest), out.cashFX.Add(day.cashFX), out.costs.Add(day.costs)
		out.gaps = scopeGaps{out.gaps.interest || day.gaps.interest, out.gaps.cashFX || day.gaps.cashFX, out.gaps.costs || day.gaps.costs}
		for _, c := range day.missing {
			missing[c] = true
		}
	}
	for _, id := range s.unpriced[i] {
		if jump, ok := s.firstPrice[id]; ok && jump.day <= j {
			t := of(id)
			if s.deposit[id] {
				t.income = t.income.Sub(jump.heldAt(i))
			} else {
				t.price = t.price.Sub(jump.heldAt(i))
			}
		}
	}
	out.missingRates = holdings.SortedKeys(missing)
	return out
}

// attributionBuckets are the four buckets of the scope as 2-decimal strings,
// each null when a day of the range could not convert it.
type attributionBuckets struct {
	Price  *string `json:"price"`
	Income *string `json:"income"`
	FX     *string `json:"fx"`
	CashFX *string `json:"cash_fx"`
}

// attributionPosition is one instrument's share of the range. Total is the
// sum of the unrounded buckets, rounded once.
type attributionPosition struct {
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	Market       string  `json:"market"`
	Currency     string  `json:"currency"`
	Closed       bool    `json:"closed"` // quantity zero on the last day
	Price        *string `json:"price"`
	Income       *string `json:"income"`
	FX           *string `json:"fx"`
	Total        *string `json:"total"`
}

// attributionResponse is the return split of a scope over from..to.
type attributionResponse struct {
	Currency     string                `json:"currency"`
	From         string                `json:"from"`
	To           string                `json:"to"`
	Total        attributionBuckets    `json:"total"`
	Positions    []attributionPosition `json:"positions"`
	MissingRates []string              `json:"missing_rates"`
}

// fixed2 is d as a 2-decimal string, nil when null.
func fixed2(d decimal.Decimal, null bool) *string {
	if null {
		return nil
	}
	v := d.StringFixed(2)
	return &v
}

// attributionResponse renders r, the split of the sweep's whole range, as
// the route's body.
func (s *historySweep) attributionResponse(from, to time.Time, r rangeReturn) attributionResponse {
	held := s.attribution[len(s.attribution)-1].held
	out := attributionResponse{Currency: s.currency, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Positions: []attributionPosition{}, MissingRates: r.missingRates}
	for _, id := range slices.Sorted(maps.Keys(r.instruments)) {
		t := r.instruments[id]
		out.Positions = append(out.Positions, attributionPosition{
			InstrumentID: id, Symbol: s.symbolOf[id], Market: s.marketOf[id], Currency: s.currencyOf[id], Closed: !slices.Contains(held, id),
			Price: fixed2(t.price, t.missing), Income: fixed2(t.income, t.missing), FX: fixed2(t.fx, t.missing),
			Total: fixed2(t.price.Add(t.income).Add(t.fx), t.missing),
		})
	}
	out.Total = r.totals()
	return out
}

// totals folds r into the four buckets of the scope: price is the costs plus
// every instrument's price, income the interest plus every instrument's
// income, fx the instruments' fx. A bucket is null when a scope amount it
// holds, or any instrument, could not convert.
func (r rangeReturn) totals() attributionBuckets {
	price, income, fx := r.costs, r.interest, decimal.Zero
	priceNull, incomeNull, fxNull := r.gaps.costs, r.gaps.interest, false
	for _, t := range r.instruments {
		price, income, fx = price.Add(t.price), income.Add(t.income), fx.Add(t.fx)
		priceNull, incomeNull, fxNull = priceNull || t.missing, incomeNull || t.missing, fxNull || t.missing
	}
	return attributionBuckets{Price: fixed2(price, priceNull), Income: fixed2(income, incomeNull), FX: fixed2(fx, fxNull), CashFX: fixed2(r.cashFX, r.gaps.cashFX)}
}

// attribution answers how the scope's return over a date range splits into
// price, income and currency.
//
//	@Summary	Return split over a range
//	@Description	How the scope's return from `from` to `to` splits into price, income and currency, in the reporting currency, as 2-decimal strings. `from` and `to` are resolved like the history route's, and the response carries the range actually used. total.price is the positions' price plus the scope's cash costs (standalone fee rows, fees on deposits and withdrawals); total.income is the positions' income plus interest; total.fx is the positions' currency effect and total.cash_fx the currency effect on cash. A term deposit's price change is income. The first price of an instrument held without one when the range opens is not counted: its jump from cost to market is a flow, as in the history route's net_flow. positions lists every instrument that had a position on a day of the range or any activity after `from`; closed is true when its quantity is zero on `to`; its total is the sum of the unrounded buckets, rounded once. Each bucket is rounded to 2 decimals on its own, so the four rounded totals can differ from the rounded change in total value less net flows by up to 0.02. A bucket is null when a day of the range had no rate to convert it; missing_rates lists those currencies.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id		path		string	true	"numeric id or `all`"
//	@Param		from	query		string	false	"first day, YYYY-MM-DD"
//	@Param		to		query		string	false	"last day, YYYY-MM-DD, default today"
//	@Success	200	{object}	server.attributionResponse
//	@Failure	400	{object}	object{error=string}	"to must be YYYY-MM-DD; from must be YYYY-MM-DD; from must not be after to; range exceeds 3660 days"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/attribution [get]
func (h *historyHandler) attribution(w http.ResponseWriter, r *http.Request) {
	userID, lists, ok := h.scopeLists(w, r)
	if !ok {
		return
	}
	txs := flatByDate(lists)
	from, to, ok := historyRange(w, r, txs)
	if !ok {
		return
	}
	s, err := h.sweep(r.Context(), userID, txs, from, to, sweepOpts{attribution: true})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	auth.WriteJSON(w, http.StatusOK, s.attributionResponse(from, to, s.rangeAttribution(0, len(s.entries)-1)))
}
