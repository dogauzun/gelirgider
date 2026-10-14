package server

import (
	"fmt"
	"math"
	"testing"
)

func TestPeriodReturns(t *testing.T) {
	show := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	tests := []struct {
		values, flows    []float64
		simple, twr, mwr string
	}{
		{[]float64{1000, 1250}, []float64{0, 0}, "0.250000", "0.250000", "0.250000"},
		{[]float64{1000, 1800, 1710}, []float64{0, 1000, 0}, "-0.145000", "-0.240000", "-0.190000"},
		{[]float64{10000, 1011000}, []float64{0, 1000000}, "0.000990", "0.100000", "0.100000"},
		{[]float64{1000, 700, 577.5}, []float64{0, -500, 0}, "0.155000", "-0.010000", "0.102500"},
		{[]float64{0, 1200}, []float64{0, 1200}, "null", "null", "null"},
		{[]float64{0, 1000, 1100}, []float64{0, 1000, 0}, "null", "null", "null"},
		{[]float64{-100, 1000, 1100}, []float64{0, 1000, 0}, "null", "null", "null"},
		{[]float64{1000, 0}, []float64{0, 0}, "-1.000000", "-1.000000", "-1.000000"},
		{[]float64{1000, -100}, []float64{0, 0}, "-1.100000", "null", "null"},
		{[]float64{1000}, []float64{0}, "null", "null", "null"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.values, tt.flows), func(t *testing.T) {
			got := periodReturns(tt.values, tt.flows, false)
			if s := show(got.Simple); s != tt.simple {
				t.Errorf("simple = %s, want %s", s, tt.simple)
			}
			if s := show(got.TWR); s != tt.twr {
				t.Errorf("twr = %s, want %s", s, tt.twr)
			}
			if s := show(got.MWR); s != tt.mwr {
				t.Errorf("mwr = %s, want %s", s, tt.mwr)
			}
		})
	}
	// Row 1 again, with cash below zero on some day of the range: all null.
	if got := periodReturns([]float64{1000, 1250}, []float64{0, 0}, true); got != (returnsResponse{}) {
		t.Errorf("negative cash: returns = %+v, want all null", got)
	}
}

// TestTimeWeightedSeries checks the running product behind the daily twr on
// history points (GLG-952): it starts at 0 and ends where timeWeightedReturn
// ends, for every worked example of ADR 0011, and is undefined exactly when
// that return is.
func TestTimeWeightedSeries(t *testing.T) {
	examples := []struct{ values, flows []float64 }{
		{[]float64{1000, 1250}, []float64{0, 0}},
		{[]float64{1000, 1800, 1710}, []float64{0, 1000, 0}},
		{[]float64{10000, 1011000}, []float64{0, 1000000}},
		{[]float64{1000, 700, 577.5}, []float64{0, -500, 0}},
		{[]float64{0, 1200}, []float64{0, 1200}},
		{[]float64{0, 1000, 1100}, []float64{0, 1000, 0}},
		{[]float64{-100, 1000, 1100}, []float64{0, 1000, 0}},
		{[]float64{1000, 0}, []float64{0, 0}},
		{[]float64{1000, -100}, []float64{0, 0}},
		{[]float64{1000}, []float64{0}},
	}
	for _, ex := range examples {
		t.Run(fmt.Sprint(ex.values, ex.flows), func(t *testing.T) {
			series, ok := timeWeightedSeries(ex.values, ex.flows)
			twr, twrOK := timeWeightedReturn(ex.values, ex.flows)
			if ok != twrOK {
				t.Fatalf("ok = %v, want %v as timeWeightedReturn", ok, twrOK)
			}
			if !ok {
				return
			}
			if len(series) != len(ex.values) {
				t.Fatalf("len = %d, want %d", len(series), len(ex.values))
			}
			if series[0] != 0 {
				t.Errorf("first = %v, want 0", series[0])
			}
			if last := series[len(series)-1]; last != twr {
				t.Errorf("last = %v, want timeWeightedReturn %v", last, twr)
			}
		})
	}

	tests := []struct {
		name          string
		values, flows []float64
		want          []float64 // nil: undefined
	}{
		// ADR 0011 row 2: the 1000 deposit on day 1 lifts the value from 1000
		// to 1800, but the day's factor is (1800 - 1000) / 1000, so the series
		// reads -20%, not +80%.
		{"deposit", []float64{1000, 1800, 1710}, []float64{0, 1000, 0}, []float64{0, -0.2, -0.24}},
		{"deposit, flat", []float64{1000, 2000, 2000}, []float64{0, 1000, 0}, []float64{0, 0, 0}},
		// Day 3 starts at 0, so it is skipped and repeats day 2's value.
		{"skipped day", []float64{1000, 1100, 0, 500, 550}, []float64{0, 0, -1100, 500, 0}, []float64{0, 0.1, 0.1, 0.1, 0.21}},
		// Day 2's factor is negative: the whole series is undefined, not
		// just the days after it.
		{"negative factor", []float64{1000, 1100, -100, 50}, []float64{0, 0, 0, 0}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := timeWeightedSeries(tt.values, tt.flows)
			if tt.want == nil {
				if ok || got != nil {
					t.Errorf("got %v, %v; want nil, false", got, ok)
				}
				return
			}
			if !ok || len(got) != len(tt.want) {
				t.Fatalf("got %v, %v; want %v, true", got, ok, tt.want)
			}
			for i := range got {
				if math.Abs(got[i]-tt.want[i]) > 1e-12 {
					t.Errorf("got %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}
