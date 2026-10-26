package statement

import (
	"context"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/shopspring/decimal"
)

// An opening run reads the stored closes of all its candidates in one call,
// and re-reads one instrument only after the on-demand fetch for it.
func TestOpeningRunLoadsClosesOnce(t *testing.T) {
	stored := map[int64]price.Price{}
	for id := int64(1); id <= 3; id++ {
		stored[id] = price.Price{InstrumentID: id, Close: decimal.NewFromInt(10 * id)}
	}
	var calls [][]int64
	fetched := []int64{}
	h := &handler{onDemandFetch: func(_ context.Context, in instrument.Instrument, _ string) {
		fetched = append(fetched, in.ID)
		stored[in.ID] = price.Price{InstrumentID: in.ID, Close: decimal.NewFromInt(7)}
	}}
	r := &openingRun{h: h, ctx: context.Background(), date: "2026-01-01"}
	r.latest = func(ids []int64) (map[int64]price.Price, error) {
		calls = append(calls, ids)
		out := map[int64]price.Price{}
		for _, id := range ids {
			if p, ok := stored[id]; ok {
				out[id] = p
			}
		}
		return out, nil
	}
	defer r.startPricing("test", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))()

	var cands []candidate
	for id := int64(1); id <= 5; id++ {
		r.ids = append(r.ids, id)
		cands = append(cands, candidate{
			in:     instrument.Instrument{ID: id, Symbol: "F", Market: instrument.MarketTEFAS, Currency: "TRY"},
			stated: decimal.NewFromInt(2),
		})
	}
	if err := r.refresh(r.ids); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	_, openings, err := r.rows(cands)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}

	// One batched call for the five candidates, then one single-id call
	// after each of the two fetches; the three stored closes cost no more.
	if len(calls) != 3 || len(calls[0]) != 5 || len(calls[1]) != 1 || len(calls[2]) != 1 {
		t.Errorf("latest calls = %v, want one of 5 ids then [4] and [5]", calls)
	}
	if len(fetched) != 2 || len(openings) != 5 {
		t.Fatalf("fetched %v, %d opening rows; want 2 fetches, 5 rows", fetched, len(openings))
	}
	for i, want := range []int64{10, 20, 30, 7, 7} {
		if row := openings[i]; row.Status != statusOK || row.Price == nil || !row.Price.Equal(decimal.NewFromInt(want)) {
			t.Errorf("row %d = %+v, want price %d", i, row, want)
		}
	}
}
