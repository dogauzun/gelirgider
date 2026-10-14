package server

import (
	"fmt"
	"math"
	"testing"
)

func TestShadowValues(t *testing.T) {
	show := func(s *string) string {
		if s == nil {
			return "null"
		}
		return *s
	}
	tests := []struct {
		v0               float64
		prices, flows    []float64
		want             []float64 // nil: ok=false
		simple, twr, mwr string    // empty: returns not checked
	}{
		{1000, []float64{10, 8, 7.6}, []float64{0, 1000, 0}, []float64{1000, 1800, 1710}, "-0.145000", "-0.240000", "-0.190000"},
		{1000, []float64{1, 1, 1}, []float64{0, 1000, 0}, []float64{1000, 2000, 2000}, "0.000000", "0.000000", "0.000000"},
		{1000, []float64{1, 1.1, 1.21}, []float64{0, -500, 0}, []float64{1000, 600, 660}, "0.320000", "0.210000", "0.210000"},
		{0, []float64{2, 4}, []float64{0, 100}, []float64{0, 100}, "", "", ""},
		{1000, []float64{}, []float64{}, nil, "", "", ""},
		{1000, []float64{1, 0}, []float64{0, 0}, nil, "", "", ""},
		{1000, []float64{1, math.NaN()}, []float64{0, 0}, nil, "", "", ""},
		{1000, []float64{1, 2}, []float64{0}, nil, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.v0, tt.prices, tt.flows), func(t *testing.T) {
			got, ok := shadowValues(tt.v0, tt.prices, tt.flows)
			if ok != (tt.want != nil) {
				t.Fatalf("ok = %v, want %v", ok, tt.want != nil)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("values = %v, want %v", got, tt.want)
			}
			for i := range got {
				if math.Abs(got[i]-tt.want[i]) > 5e-7 {
					t.Fatalf("values = %v, want %v", got, tt.want)
				}
			}
			if tt.simple == "" {
				return
			}
			r := periodReturns(got, tt.flows, false)
			if s := show(r.Simple); s != tt.simple {
				t.Errorf("simple = %s, want %s", s, tt.simple)
			}
			if s := show(r.TWR); s != tt.twr {
				t.Errorf("twr = %s, want %s", s, tt.twr)
			}
			if s := show(r.MWR); s != tt.mwr {
				t.Errorf("mwr = %s, want %s", s, tt.mwr)
			}
		})
	}
}
