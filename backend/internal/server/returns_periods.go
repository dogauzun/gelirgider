package server

import (
	"errors"
	"net/http"

	"github.com/dogauzun/gelirgider/backend/internal/auth"
	"github.com/dogauzun/gelirgider/backend/internal/portfolio"
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
	userID := auth.MustUserID(r.Context())
	lists, err := scopeTransactions(r.Context(), h.portfolios, h.transactions, userID, r.PathValue("id"))
	if errors.Is(err, portfolio.ErrNotFound) {
		auth.WriteError(w, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	txs := flatByDate(lists)
	benchmarkCode := r.URL.Query().Get("benchmark")
	benchmarkInstrument, known := parseBenchmark(benchmarkCode, distinctInstrumentIDs(txs))
	if !known {
		auth.WriteError(w, http.StatusBadRequest, "unknown benchmark")
		return
	}

	today := istanbulToday()
	resp := periodsResponse{Months: []periodBucket{}, Years: []periodBucket{}}
	if len(txs) > 0 {
		from, err := defaultHistoryFrom(txs, today)
		if err != nil {
			auth.WriteInternalError(w, r, err)
			return
		}
		if !from.After(today) {
			s, err := h.sweep(r.Context(), userID, txs, from, today, benchmarkRef{benchmarkCode, benchmarkInstrument})
			if err != nil {
				auth.WriteInternalError(w, r, err)
				return
			}
			resp.Currency = s.currency
			resp.Months = s.buckets(len("2006-01"))
			resp.Years = s.buckets(len("2006"))
			auth.WriteJSON(w, http.StatusOK, resp)
			return
		}
	}
	// Nothing to sweep: no transaction, or only future-dated ones.
	usr, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		auth.WriteInternalError(w, r, err)
		return
	}
	resp.Currency = usr.ReportingCurrency
	auth.WriteJSON(w, http.StatusOK, resp)
}

// buckets cuts the sweep into calendar periods by the first n characters
// of each date (7 for months, 4 for years). Each bucket's returns come from
// the same rangeInputs and periodReturns as a history range over the same
// days, so a year's bucket equals the chart header for that year's preset.
func (s *historySweep) buckets(n int) []periodBucket {
	out := []periodBucket{}
	for i := 0; i < len(s.entries); {
		period := s.entries[i].Date[:n]
		j := i
		for j+1 < len(s.entries) && s.entries[j+1].Date[:n] == period {
			j++
		}
		values, flows, _, negativeCash := s.rangeInputs(i, j)
		b := periodBucket{Period: period, From: s.entries[i].Date, To: s.entries[j].Date, Returns: periodReturns(values, flows, negativeCash)}
		if _, returns, ok := s.rangeBenchmark(i, j, flows, negativeCash); ok {
			b.Benchmark = &returns
		}
		out = append(out, b)
		i = j + 1
	}
	if len(out) > 0 {
		out[0].Partial = true
		out[len(out)-1].Partial = true
	}
	return out
}
