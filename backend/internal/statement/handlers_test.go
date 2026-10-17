package statement

import (
	"context"
	"fmt"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

// countingInstruments counts the instrument queries the import routes make,
// and the All ones among them.
type countingInstruments struct {
	*instrument.Repo
	queries, alls int
}

func (c *countingInstruments) Get(ctx context.Context, id int64) (instrument.Instrument, error) {
	c.queries++
	return c.Repo.Get(ctx, id)
}

func (c *countingInstruments) ByIDs(ctx context.Context, ids []int64) (map[int64]instrument.Instrument, error) {
	c.queries++
	return c.Repo.ByIDs(ctx, ids)
}

func (c *countingInstruments) BySymbol(ctx context.Context, market, symbol string) (instrument.Instrument, error) {
	c.queries++
	return c.Repo.BySymbol(ctx, market, symbol)
}

func (c *countingInstruments) All(ctx context.Context, market string) ([]instrument.Instrument, error) {
	c.queries++
	c.alls++
	return c.Repo.All(ctx, market)
}

// A batch's names and picks load their instruments in one query, not one
// per entry, and an unknown one still answers with its own index.
func TestAliasInstrumentsLoadInOneQuery(t *testing.T) {
	repo := openTestDB(t)
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'TEFAS', 'A', 'TRY'), (2, 'BBB', 'TEFAS', 'B', 'TRY'), (3, 'CCC', 'TEFAS', 'C', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	store := &countingInstruments{Repo: instrument.NewRepo(repo.db)}
	h := &handler{imports: repo, instruments: store}

	// 47 names over three instruments, one symbol named twice and one name
	// of instrument 0 to forget: 50 entries.
	var names []aliasRequest
	for i := range 47 {
		names = append(names, aliasRequest{Symbol: fmt.Sprintf("FUND %d", i), InstrumentID: int64(i%3 + 1)})
	}
	names = append(names, aliasRequest{Symbol: "SAME", InstrumentID: 1}, aliasRequest{Symbol: "SAME", InstrumentID: 2}, aliasRequest{Symbol: "GONE"})
	out, bad, err := h.aliasInstruments(context.Background(), "names", names)
	if err != nil || bad != "" {
		t.Fatalf("aliasInstruments: %q, %v", bad, err)
	}
	if store.queries != 1 {
		t.Errorf("instrument queries = %d, want 1", store.queries)
	}
	if len(out) != 48 || out["FUND 4"].Symbol != "BBB" || out["SAME"].ID != 2 {
		t.Errorf("got %d instruments, FUND 4 %q, SAME %d; want 48, BBB, 2", len(out), out["FUND 4"].Symbol, out["SAME"].ID)
	}
	if _, ok := out["GONE"]; ok {
		t.Error("the name of instrument 0 was kept")
	}

	picks := []aliasRequest{{Symbol: "A", InstrumentID: 1}, {Symbol: "B", InstrumentID: 98}, {Symbol: "C", InstrumentID: 2}, {Symbol: "D", InstrumentID: 99}}
	if _, bad, err := h.aliasInstruments(context.Background(), "picks", picks); err != nil || bad != "picks[1].instrument_id: unknown instrument" {
		t.Errorf("unknown pick: %q, %v; want picks[1].instrument_id: unknown instrument", bad, err)
	}
}
