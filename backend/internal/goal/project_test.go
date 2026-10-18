package goal

import (
	"math"
	"testing"
)

func flat(n int, r float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = r
	}
	return out
}

func TestProject(t *testing.T) {
	base := ProjectionInput{
		Value:          1000,
		MonthlyReturns: flat(12, 0),
		MonthlyFlows:   flat(12, 100),
		Amount:         1500,
		Unit:           "nominal",
		FirstMonth:     "2026-11",
		Months:         6,
	}

	t.Run("reaches month 5", func(t *testing.T) {
		p := Project(base)
		if p.ReachedAt == nil || *p.ReachedAt != "2027-03" {
			t.Fatalf("ReachedAt = %v, want 2027-03", p.ReachedAt)
		}
		if p.MonthlyGap != nil {
			t.Errorf("MonthlyGap = %v, want nil", *p.MonthlyGap)
		}
		if len(p.Months) != 6 || p.Months[0].Month != "2026-11" || p.Months[0].Mid != 1100 {
			t.Errorf("Months = %v", p.Months)
		}
		if p.Progress != 1000.0/1500 || p.HistoryMonths != 12 {
			t.Errorf("Progress %v HistoryMonths %v", p.Progress, p.HistoryMonths)
		}
	})

	t.Run("unreachable gives the rounded up gap", func(t *testing.T) {
		in := base
		in.Months = 3
		p := Project(in)
		if p.ReachedAt != nil || p.MonthlyGap == nil || *p.MonthlyGap != 67 {
			t.Fatalf("ReachedAt %v MonthlyGap %v, want nil and 67", p.ReachedAt, p.MonthlyGap)
		}
	})

	t.Run("an exact gap is not rounded past itself", func(t *testing.T) {
		in := base
		in.Months, in.Amount = 3, 1480
		if p := Project(in); p.MonthlyGap == nil || *p.MonthlyGap != 60 {
			t.Fatalf("MonthlyGap = %v, want 60", p.MonthlyGap)
		}
	})

	t.Run("five returns give no projection", func(t *testing.T) {
		in := base
		in.MonthlyReturns = flat(5, 0)
		p := Project(in)
		if len(p.Months) != 0 || p.HistoryMonths != 5 || p.ReachedAt != nil || p.MonthlyGap != nil {
			t.Fatalf("got %+v", p)
		}
	})

	t.Run("real is below nominal", func(t *testing.T) {
		in := base
		in.Unit, in.MonthlyCPIRate = "real", 0.02
		real, nominal := Project(in), Project(base)
		if real.Months[5].Mid >= nominal.Months[5].Mid {
			t.Errorf("real %v not below nominal %v", real.Months[5].Mid, nominal.Months[5].Mid)
		}
		want := 1600 / math.Pow(1.02, 6)
		if math.Abs(real.Months[5].Mid-want) > 1e-9 {
			t.Errorf("real mid = %v, want %v", real.Months[5].Mid, want)
		}
	})

	t.Run("usd divides by the rate", func(t *testing.T) {
		in := base
		in.Unit, in.USDRate, in.Amount = "usd", 40, 50
		p := Project(in)
		if p.Months[0].Mid != 1100.0/40 || p.Progress != 25.0/50 {
			t.Errorf("mid %v progress %v", p.Months[0].Mid, p.Progress)
		}
	})

	t.Run("percentiles are nearest rank", func(t *testing.T) {
		in := base
		in.MonthlyReturns = []float64{0.04, 0.01, 0.03, 0.02, 0.06, 0.05}
		in.MonthlyFlows = flat(1, 0)
		m := Project(in).Months[0]
		if m.Low != 1000*1.02 || m.Mid != 1000*1.03 || m.High != 1000*1.05 {
			t.Errorf("month 1 = %+v", m)
		}
	})
}
