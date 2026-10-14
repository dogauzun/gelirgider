package benchmark_test

import (
	"math"
	"slices"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/benchmark"
)

func TestDailyCPI(t *testing.T) {
	janFeb := []benchmark.MonthLevel{{"2025-01", 100}, {"2025-02", 110}}
	gap := []benchmark.MonthLevel{{"2025-01", 100}, {"2025-02", 110}, {"2025-04", 200}}
	leap := []benchmark.MonthLevel{{"2024-01", 100}, {"2024-02", 105}}

	cases := []struct {
		name      string
		months    []benchmark.MonthLevel
		from, to  string
		want      []float64 // 0 where have is false
		wantHave  []bool
		estimated string
		exact     bool // compare levels with ==
	}{
		{"mid month", janFeb, "2025-02-14", "2025-02-14", []float64{100 * math.Pow(1.1, 14.0/28)}, []bool{true}, "", false},
		{"last day of month", janFeb, "2025-02-28", "2025-02-28", []float64{110}, []bool{true}, "", true},
		{"extrapolated month end", janFeb, "2025-03-31", "2025-03-31", []float64{121}, []bool{true}, "2025-03-31", false},
		{"extrapolated mid month", janFeb, "2025-03-15", "2025-03-15", []float64{110 * math.Pow(1.1, 15.0/31)}, []bool{true}, "2025-03-15", false},
		{"estimate starts in range", janFeb, "2025-02-27", "2025-03-02",
			[]float64{100 * math.Pow(1.1, 27.0/28), 110, 110 * math.Pow(1.1, 1.0/31), 110 * math.Pow(1.1, 2.0/31)},
			[]bool{true, true, true, true}, "2025-03-01", false},
		{"first stored month", janFeb, "2025-01-10", "2025-01-12", []float64{0, 0, 0}, []bool{false, false, false}, "", false},
		{"one month only", janFeb[:1], "2025-02-01", "2025-02-02", []float64{0, 0}, []bool{false, false}, "", false},
		{"leap day", leap, "2024-02-29", "2024-02-29", []float64{105}, []bool{true}, "", true},
		{"gap ends series", gap, "2025-02-28", "2025-04-30",
			nil, nil, "2025-03-01", false},
		{"gap extrapolates", gap, "2025-04-30", "2025-04-30", []float64{133.1}, []bool{true}, "2025-04-30", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			levels, have, est := benchmark.DailyCPI(c.months, c.from, c.to)
			if est != c.estimated {
				t.Errorf("estimatedFrom = %q, want %q", est, c.estimated)
			}
			if c.want == nil {
				return
			}
			if !slices.Equal(have, c.wantHave) {
				t.Fatalf("have = %v, want %v", have, c.wantHave)
			}
			for i, w := range c.want {
				if c.exact && levels[i] != w {
					t.Errorf("level[%d] = %v, want exactly %v", i, levels[i], w)
				}
				if math.Abs(levels[i]-w) > 5e-7 {
					t.Errorf("level[%d] = %.6f, want %.6f", i, levels[i], w)
				}
			}
		})
	}

	// The ticket's worked value, to 6 decimals.
	levels, _, _ := benchmark.DailyCPI(janFeb, "2025-02-14", "2025-02-14")
	if got := math.Round(levels[0]*1e6) / 1e6; got != 104.880885 {
		t.Errorf("2025-02-14 = %.6f, want 104.880885", got)
	}
}
