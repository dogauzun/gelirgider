package server

import "math"

// shadowValues returns S_i for each day, or ok=false when the series is unusable.
// S_i is the value of a shadow portfolio that put the portfolio's own external
// flows into the benchmark on the same days: u = v0/prices[0] units on day 0,
// then u += flows[i]/prices[i] and S_i = u·prices[i]. Flows are end-of-day and
// flows[0] is ignored, as in periodReturns. u is not clamped: a withdrawal
// larger than the shadow value makes it negative. The caller carries missing
// prices forward, so a gap never reaches here.
//
// float64 on purpose: hypothetical, ratio-derived values (ADR 0012).
func shadowValues(v0 float64, prices, flows []float64) (values []float64, ok bool) {
	if len(prices) == 0 || len(prices) != len(flows) {
		return nil, false
	}
	for _, p := range prices {
		if !(p > 0) || math.IsInf(p, 0) {
			return nil, false
		}
	}
	u := v0 / prices[0]
	values = make([]float64, len(prices))
	values[0] = v0
	for i := 1; i < len(prices); i++ {
		u += flows[i] / prices[i]
		values[i] = u * prices[i]
	}
	return values, true
}
