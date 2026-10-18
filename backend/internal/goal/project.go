package goal

import (
	"math"
	"slices"
	"time"
)

const minReturns = 6

type ProjectionInput struct {
	Value          float64
	MonthlyReturns []float64
	MonthlyFlows   []float64
	Amount         float64
	Unit           string
	MonthlyCPIRate float64
	USDRate        float64
	FirstMonth     string
	Months         int
}

type ProjectionMonth struct {
	Month string
	Low   float64
	Mid   float64
	High  float64
}

type Projection struct {
	Months        []ProjectionMonth
	ReachedAt     *string
	MonthlyGap    *float64
	Progress      float64
	HistoryMonths int
}

func Project(in ProjectionInput) Projection {
	p := Projection{Months: []ProjectionMonth{}, HistoryMonths: len(in.MonthlyReturns)}
	p.Progress = in.Value * in.scale(0) / in.Amount
	first, err := time.Parse("2006-01", in.FirstMonth)
	if len(in.MonthlyReturns) < minReturns || in.Months < 1 || err != nil {
		return p
	}
	rates := [3]float64{
		percentile(in.MonthlyReturns, 0.25),
		percentile(in.MonthlyReturns, 0.5),
		percentile(in.MonthlyReturns, 0.75),
	}
	flow := percentile(in.MonthlyFlows, 0.5)
	v := [3]float64{in.Value, in.Value, in.Value}
	for k := 1; k <= in.Months; k++ {
		m := ProjectionMonth{Month: first.AddDate(0, k-1, 0).Format("2006-01")}
		for i := range v {
			v[i] = v[i]*(1+rates[i]) + flow
		}
		s := in.scale(k)
		m.Low, m.Mid, m.High = v[0]*s, v[1]*s, v[2]*s
		if p.ReachedAt == nil && m.Mid >= in.Amount {
			p.ReachedAt = &m.Month
		}
		p.Months = append(p.Months, m)
	}
	if p.ReachedAt == nil {
		gap := in.monthlyGap(rates[1], flow)
		p.MonthlyGap = &gap
	}
	return p
}

func (in ProjectionInput) scale(k int) float64 {
	switch in.Unit {
	case "real":
		return math.Pow(1+in.MonthlyCPIRate, -float64(k))
	case "usd":
		return 1 / in.USDRate
	}
	return 1
}

func (in ProjectionInput) monthlyGap(rate, flow float64) float64 {
	s := in.scale(in.Months)
	final := func(g float64) float64 {
		v := in.Value
		for range in.Months {
			v = v*(1+rate) + flow + g
		}
		return v * s
	}
	lo, hi := 0.0, in.Amount/s
	for range 40 {
		mid := (lo + hi) / 2
		if final(mid) >= in.Amount {
			hi = mid
		} else {
			lo = mid
		}
	}
	return math.Ceil(hi - 1e-6)
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(xs))
	return sorted[int(math.Ceil(p*float64(len(sorted))))-1]
}
