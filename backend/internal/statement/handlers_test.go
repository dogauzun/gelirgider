package statement

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/ratelimit"
	"github.com/shopspring/decimal"
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

// A statement that repeats a few symbols on many rows looks each symbol up
// once, an unknown one included, not once per row.
func TestPreviewLooksUpEachSymbolOnce(t *testing.T) {
	repo := openTestDB(t)
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES (1, 'AAA', 'BIST', 'A', 'TRY'), (2, 'BBB', 'US', 'B', 'USD')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	store := &countingInstruments{Repo: instrument.NewRepo(repo.db)}
	h := &handler{imports: repo, instruments: store}

	one := decimal.NewFromInt(1)
	var st Statement
	for range 50 {
		for _, s := range []struct{ symbol, currency string }{{"AAA", "TRY"}, {"BBB", "USD"}, {"ZZZ", "TRY"}} {
			st.Trades = append(st.Trades, Trade{Date: "2026-01-10", Symbol: s.symbol, Side: "buy", Quantity: one, Price: one, Amount: one, Currency: s.currency})
		}
		st.Dividends = append(st.Dividends, Dividend{Date: "2026-01-12", Symbol: "AAA", Gross: one, Currency: "TRY"})
	}

	rows, err := h.previewRows(context.Background(), requester{userID: 1, broker: "csv"}, nil, nil, st)
	if err != nil {
		t.Fatalf("previewRows: %v", err)
	}
	want := map[string]string{"AAA": statusOK, "BBB": statusOK, "ZZZ": statusUnknownInstrument}
	for _, row := range rows {
		if row.Status != want[row.Symbol] {
			t.Fatalf("row %s %s: status %s, want %s", row.Key, row.Symbol, row.Status, want[row.Symbol])
		}
	}
	// AAA in BIST, BBB in US, ZZZ in BIST and then in each of TEFAS, CRYPTO
	// and GOLD (soleMatch): 6 queries for 200 rows.
	if store.queries != 6 {
		t.Errorf("instrument queries = %d, want 6", store.queries)
	}
}

// A garanti statement that repeats a few cut fund names on many rows reads
// the TEFAS fund list once, and once more after createFund adds a fund the
// later names must match.
func TestPreviewLoadsTefasFundsOnce(t *testing.T) {
	repo := openTestDB(t)
	if _, err := repo.db.Exec(`INSERT INTO instruments (id, symbol, market, name, currency) VALUES
		(1, 'AAA', 'TEFAS', 'AK PORTFÖY KISA VADELİ BORÇLANMA ARAÇLARI FONU', 'TRY'),
		(2, 'BBB', 'TEFAS', 'GARANTİ PORTFÖY BİRİNCİ HİSSE SENEDİ FONU', 'TRY'),
		(3, 'CCC', 'TEFAS', 'İŞ PORTFÖY PARA PİYASASI FONU', 'TRY')`); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	store := &countingInstruments{Repo: instrument.NewRepo(repo.db)}
	lookupFund := func(context.Context, string) (string, bool, error) { return "DENİZ PORTFÖY ALTIN FONU", true, nil }
	h := &handler{imports: repo, instruments: store, lookupFund: lookupFund, fundLookups: ratelimit.New(fundLookupLimit, time.Minute)}

	// Four names, then the code createFund adds, then a name of its fund,
	// each on 20 rows.
	want := map[string]string{
		"AK PORTFÖY KISA VAD": "ok name AAA", "GPY 1. HİSSE SEN": "ok name BBB", "İŞ PORTFÖY PARA PİY": "ok name CCC",
		"YOK BÖYLE FON": "unknown_instrument  ", "DDD": "ok  ", "DENİZ PORTFÖY ALT": "ok name DDD",
	}
	one := decimal.NewFromInt(1)
	var st Statement
	for _, symbols := range [][]string{{"AK PORTFÖY KISA VAD", "GPY 1. HİSSE SEN", "İŞ PORTFÖY PARA PİY", "YOK BÖYLE FON"}, {"DDD"}, {"DENİZ PORTFÖY ALT"}} {
		for range 20 {
			for _, symbol := range symbols {
				st.Trades = append(st.Trades, Trade{Date: "2026-01-10", Symbol: symbol, Side: "buy", Quantity: one, Price: one, Amount: one, Currency: "TRY", Market: "TEFAS"})
			}
		}
	}

	rows, err := h.previewRows(context.Background(), requester{userID: 1, broker: "garanti"}, nil, nil, st)
	if err != nil {
		t.Fatalf("previewRows: %v", err)
	}
	for _, row := range rows {
		if got := fmt.Sprintf("%s %s %s", row.Status, row.ResolvedBy, row.InstrumentSymbol); got != want[row.Symbol] {
			t.Fatalf("row %s %s = %q, want %q", row.Key, row.Symbol, got, want[row.Symbol])
		}
	}
	if store.alls != 2 {
		t.Errorf("TEFAS list reads = %d, want 2", store.alls)
	}
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
