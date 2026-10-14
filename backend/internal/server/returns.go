package server

import (
	"math"
	"strconv"
)

// Period returns over a history range (GLG-388). values[i] is the portfolio
// total on point i in the reporting currency, flows[i] the external flow that
// entered on day i. flows[0] is always 0: a first-day flow is already inside
// values[0]. Flows are end-of-day, so a flow on day i does not earn day i.
//
// These are ratios, not money, so they are float64: the caller converts the
// exact decimals once. Chaining thousands of exact decimal factors makes the
// mantissa grow without bound.

// returnsResponse is the history response's `returns` object: the range's
// fractional return with 6 decimals ("0.080213"), null when undefined.
type returnsResponse struct {
	Simple *string `json:"simple"` // simple return, null when undefined
	TWR    *string `json:"twr"`    // time-weighted return, null when undefined
	MWR    *string `json:"mwr"`    // money-weighted return, null when undefined
}

// periodReturns is null for all three methods without a positive capital
// base: when values[0] is not positive, or when negativeCash says the cash,
// in the reporting currency, was below zero on some day of the range. The
// value is then only the P/L of positions bought with money never put in,
// and a ratio over it reads millions of percent (GLG-641, GLG-632).
func periodReturns(values, flows []float64, negativeCash bool) returnsResponse {
	if negativeCash || len(values) < 2 || values[0] <= 0 {
		return returnsResponse{}
	}
	return returnsResponse{
		Simple: formatReturn(simpleReturn(values, flows)),
		TWR:    formatReturn(timeWeightedReturn(values, flows)),
		MWR:    formatReturn(moneyWeightedReturn(values, flows)),
	}
}

// simpleReturn is the gain over everything put in: (V_last - V_0 - ΣF) /
// (V_0 + ΣF). Undefined when that base is not positive.
func simpleReturn(values, flows []float64) (float64, bool) {
	n := len(values)
	if n < 2 {
		return 0, false
	}
	var sum float64
	for _, f := range flows {
		sum += f
	}
	base := values[0] + sum
	if base <= 0 {
		return 0, false
	}
	return (values[n-1] - values[0] - sum) / base, true
}

// timeWeightedReturn chains the daily factors (V_i - F_i) / V_{i-1}. A day
// that starts at or below zero has no measurable return and is skipped. A
// negative factor has no meaning, so the whole return is undefined; a factor
// of exactly 0 is a real total loss.
func timeWeightedReturn(values, flows []float64) (float64, bool) {
	series, ok := timeWeightedSeries(values, flows)
	if !ok {
		return 0, false
	}
	return series[len(series)-1], true
}

// timeWeightedSeries is the running product behind timeWeightedReturn:
// element i is the time-weighted return from day 0 to day i, so the first is
// 0 and the last is timeWeightedReturn's result. A skipped day repeats the
// previous element. Undefined exactly when timeWeightedReturn is (GLG-952).
func timeWeightedSeries(values, flows []float64) ([]float64, bool) {
	if len(values) < 2 {
		return nil, false
	}
	series := make([]float64, len(values))
	product := 1.0
	used := false
	for i := 1; i < len(values); i++ {
		if values[i-1] > 0 {
			factor := (values[i] - flows[i]) / values[i-1]
			if factor < 0 {
				return nil, false
			}
			product *= factor
			used = true
		}
		series[i] = product - 1
	}
	if !used {
		return nil, false
	}
	return series, true
}

// twrPoints is timeWeightedSeries formatted per point, nil when the range's
// twr (rangeTWR, from periodReturns) is null, so that no point carries one
// then. The last element equals rangeTWR: the same float, the same format.
func twrPoints(values, flows []float64, rangeTWR *string) []*string {
	if rangeTWR == nil {
		return nil
	}
	series, _ := timeWeightedSeries(values, flows)
	out := make([]*string, len(series))
	for i, r := range series {
		out[i] = formatReturn(r, true)
	}
	return out
}

// moneyWeightedReturn is the period rate R (IRR) solving
//
//	V_0·(1+R) + Σ F_i·(1+R)^w_i - V_last = 0,  w_i = (n-1-i)/(n-1)
//
// where w_i is the fraction of the range left after day i, the same weight
// the modified Dietz base uses. Found by bisection on [-1, hi]. Cash flows
// that change sign can give more than one root; that case is not handled,
// bisection returns whichever root its bracket converges to.
func moneyWeightedReturn(values, flows []float64) (float64, bool) {
	n := len(values)
	if n < 2 || values[0] <= 0 {
		return 0, false
	}
	type flow struct{ w, amount float64 }
	var fs []flow
	for i, f := range flows {
		if f != 0 {
			fs = append(fs, flow{float64(n-1-i) / float64(n-1), f})
		}
	}
	last := values[n-1]
	f := func(r float64) float64 {
		v := values[0]*(1+r) - last
		for _, fl := range fs {
			v += fl.amount * math.Pow(1+r, fl.w)
		}
		return v
	}

	// lo is exactly -1: math.Pow(0, w>0) is 0 and math.Pow(0, 0) is 1, so a
	// total loss solves to -1.
	lo, hi := -1.0, 1.0
	for f(hi) <= 0 && hi < 1e6 {
		hi *= 2
	}
	if !(f(lo) <= 0 && f(hi) > 0) {
		return 0, false
	}
	for range 100 {
		mid := (lo + hi) / 2
		if f(mid) > 0 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return (lo + hi) / 2, true
}

// formatReturn renders r with 6 decimals, nil when undefined. A value that
// rounds to zero is written as "0.000000", never "-0.000000".
func formatReturn(r float64, ok bool) *string {
	if !ok {
		return nil
	}
	if math.Abs(r) < 5e-7 {
		r = 0
	}
	s := strconv.FormatFloat(r, 'f', 6, 64)
	return &s
}
