package benchmark

import (
	"math"
	"time"
)

// MonthLevel is one published monthly index: Month is "YYYY-MM", Level > 0.
type MonthLevel struct {
	Month string
	Level float64
}

// DailyCPI returns the level for every day from..to inclusive (YYYY-MM-DD),
// in order, with ok=false for days that have no level, and the first date
// whose level is extrapolated ("" when none is).
//
// Month M's level is the level at the end of M, spread geometrically over
// M's days from M−1's level. Past the last published month P the series
// continues at the ratio L_P / L_{P−1}. A missing month ends the series
// before the gap. See ADR 0012.
func DailyCPI(months []MonthLevel, from, to string) (levels []float64, have []bool, estimatedFrom string) {
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return nil, nil, ""
	}
	end, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return nil, nil, ""
	}

	// Keep the consecutive prefix of months, indexed from the first one.
	var first int
	var series []float64
	for _, m := range months {
		t, err := time.Parse("2006-01", m.Month)
		if err != nil {
			break
		}
		idx := monthIndex(t)
		if len(series) == 0 {
			first = idx
		} else if idx != first+len(series) {
			break
		}
		series = append(series, m.Level)
	}

	last := first + len(series) - 1 // index of P
	var ratio float64
	if len(series) >= 2 {
		ratio = series[len(series)-1] / series[len(series)-2]
	}
	// level returns the end-of-month level of month index i (>= first).
	level := func(i int) float64 {
		if i <= last {
			return series[i-first]
		}
		return series[len(series)-1] * math.Pow(ratio, float64(i-last))
	}

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		i := monthIndex(d)
		if len(series) < 2 || i <= first {
			levels = append(levels, 0)
			have = append(have, false)
			continue
		}
		if i > last && estimatedFrom == "" {
			estimatedFrom = d.Format(time.DateOnly)
		}
		cur := level(i)
		days := time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		v := cur
		if d.Day() != days {
			prev := level(i - 1)
			v = prev * math.Pow(cur/prev, float64(d.Day())/float64(days))
		}
		levels = append(levels, v)
		have = append(have, true)
	}
	return levels, have, estimatedFrom
}

func monthIndex(t time.Time) int {
	return t.Year()*12 + int(t.Month()) - 1
}
