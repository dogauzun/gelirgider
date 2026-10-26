package server

import (
	"context"
	"net/http"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// periodBucket is one calendar month or year of the periods response: its
// returns are the history endpoint's for the range from..to (ADR 0011).
type periodBucket struct {
	Period  string          `json:"period"`  // "2026-09" or "2026"
	From    string          `json:"from"`    // first day, the period's first or the first transaction
	To      string          `json:"to"`      // last day, the period's last or today
	Partial bool            `json:"partial"` // the first and the current bucket
	Returns returnsResponse `json:"returns"`
	// Benchmark is the benchmark's returns over the same range, null
	// without ?benchmark= or when the benchmark has no level on some day of
	// the range (ADR 0012).
	Benchmark *returnsResponse `json:"benchmark"`
	// flow is the bucket's net external flow for the goal projection: the
	// sum of rangeInputs' flows plus its first day's, which rangeInputs drops.
	flow float64
}

// periodsResponse is every calendar month and year from the first
// transaction to today, oldest first, with no gaps.
type periodsResponse struct {
	Currency string         `json:"currency"`
	Months   []periodBucket `json:"months"`
	Years    []periodBucket `json:"years"`
}

// periods answers the returns of every calendar month and year of the
// scope, from one sweep of the history cut into periods.
//
//	@Summary	Portfolio returns per calendar month and year
//	@Description	One bucket per calendar month and per calendar year, from the first transaction (at most 3660 days back) to today, oldest first. A bucket's returns are what the history endpoint returns for the same from..to: a year's from..to is the calendar-year preset's range (YYYY-01-01..YYYY-12-31) clamped to the first transaction and to today, a month's likewise. The first and the current bucket are partial. A bucket's benchmark is null without the parameter, or when the benchmark has no level on some day of the bucket.
//	@Tags		portfolios
//	@Produce	json
//	@Param		id			path		string	true	"numeric id or `all`"
//	@Param		benchmark	query		string	false	"USD, EUR, XU100, TUFE, GOLD, or instrument:<id> for an instrument with a transaction in the scope"
//	@Success	200	{object}	server.periodsResponse
//	@Failure	400	{object}	object{error=string}	"unknown benchmark"
//	@Failure	404	{object}	object{error=string}	"portfolio not found"
//	@Failure	500	{object}	object{error=string}	"internal error"
//	@Router		/portfolios/{id}/returns/periods [get]
func (h *historyHandler) periods(w http.ResponseWriter, r *http.Request) {
	userID, txs, bm, ok := h.scopeWithBenchmark(w, r)
	if !ok {
		return
	}

	s, _, currency, err := h.sweepToToday(r.Context(), userID, txs, sweepOpts{benchmark: bm})
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	resp := periodsResponse{Currency: currency, Months: []periodBucket{}, Years: []periodBucket{}}
	if s != nil {
		resp.Months = s.buckets(len("2006-01"))
		resp.Years = s.buckets(len("2006"))
	}
	auth.WriteJSON(w, http.StatusOK, resp)
}

// sweepToToday sweeps the scope's rows from the default history start to
// today (Istanbul). With nothing to sweep, no transaction or only
// future-dated ones, s is nil and currency is the user's reporting currency;
// otherwise currency is the sweep's. from is the sweep's first day.
func (h *historyHandler) sweepToToday(ctx context.Context, userID int64, txs []transaction.Transaction, opts sweepOpts) (s *historySweep, from time.Time, currency string, err error) {
	today := istanbulToday()
	if len(txs) > 0 {
		from, err = defaultHistoryFrom(txs, today)
		if err != nil {
			return nil, from, "", err
		}
		if !from.After(today) {
			s, err = h.sweep(ctx, userID, txs, from, today, opts)
			if err != nil {
				return nil, from, "", err
			}
			return s, from, s.currency, nil
		}
	}
	currency, err = h.users.ReportingCurrency(ctx, userID)
	return nil, from, currency, err
}

func sum(xs []float64) float64 {
	total := 0.0
	for _, x := range xs {
		total += x
	}
	return total
}

// buckets cuts the sweep into calendar periods by the first n characters
// of each date (7 for months, 4 for years). Each bucket's returns come from
// the same rangeInputs and periodReturns as a history range over the same
// days, so a year's bucket equals the chart header for that year's preset.
func (s *historySweep) buckets(n int) []periodBucket {
	out := []periodBucket{}
	for _, span := range s.periodSpans(n) {
		i, j := span[0], span[1]
		values, flows, _, negativeCash := s.rangeInputs(i, j)
		b := periodBucket{Period: s.entries[i].Date[:n], From: s.entries[i].Date, To: s.entries[j].Date, Returns: periodReturns(values, flows, negativeCash), flow: sum(flows) + s.flows[i].InexactFloat64()}
		if _, returns, ok := s.rangeBenchmark(i, j, flows, negativeCash); ok {
			b.Benchmark = &returns
		}
		out = append(out, b)
	}
	if len(out) > 0 {
		out[0].Partial = true
		out[len(out)-1].Partial = true
	}
	return out
}

// periodSpans is the [i, j] entry indexes of each run of consecutive days
// sharing the first n characters of their date (7 for months, 4 for years).
func (s *historySweep) periodSpans(n int) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s.entries); {
		j := i
		for j+1 < len(s.entries) && s.entries[j+1].Date[:n] == s.entries[i].Date[:n] {
			j++
		}
		spans = append(spans, [2]int{i, j})
		i = j + 1
	}
	return spans
}
