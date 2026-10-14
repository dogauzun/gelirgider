package holdings_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

func TestCompute(t *testing.T) {
	var instrumentID int64 = 1

	tests := []struct {
		name             string
		txs              []transaction.Transaction
		latest           map[int64]decimal.Decimal
		wantQuantity     string
		wantAvgCost      string
		wantRealizedPL   string
		wantUnrealizedPL string
		wantCash         string
	}{
		{
			// buy 100 @ 10 (fee 5), buy 100 @ 12 (fee 0), sell 50 @ 15 (fee 2),
			// dividend 30 (fee 0), latest price 14, plus a deposit of 5000.
			//
			// cost basis after both buys: 1005 + 1200 = 2205, avg cost = 2205/200 = 11.025
			// sell realized: 750 - 2 - 50*11.025 = 750 - 2 - 551.25 = 196.75
			// cost basis after sell: 2205 - 551.25 = 1653.75, quantity: 150
			// dividend realized: 30
			// total realized: 196.75 + 30 = 226.75
			// market value: 150 * 14 = 2100, unrealized: 2100 - 1653.75 = 446.25
			// cash: 5000 - 1005 - 1200 + 748 + 30 = 3573
			name: "weighted average cost across buys, sell, and dividend",
			txs: []transaction.Transaction{
				{ID: 5, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2024-01-05", Amount: d("5000"), Currency: "TRY"},
				{ID: 1, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("100"), Price: d("10"), Amount: d("1000"), Fee: d("5"), Currency: "TRY"},
				{ID: 4, InstrumentID: &instrumentID, Type: transaction.TypeDividend, Date: "2024-01-04", Amount: d("30"), Currency: "TRY"},
				{ID: 3, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-01-03", Quantity: d("50"), Price: d("15"), Amount: d("750"), Fee: d("2"), Currency: "TRY"},
				{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("100"), Price: d("12"), Amount: d("1200"), Currency: "TRY"},
			},
			latest:           map[int64]decimal.Decimal{1: d("14")},
			wantQuantity:     "150",
			wantAvgCost:      "11.025",
			wantRealizedPL:   "226.75",
			wantUnrealizedPL: "446.25",
			wantCash:         "3573",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			currencyOf := map[int64]string{instrumentID: "TRY"}
			summary := holdings.Compute(tc.txs, tc.latest, currencyOf)

			if !summary.Cash["TRY"].Equal(d(tc.wantCash)) {
				t.Errorf("Cash[TRY] = %s, want %s", summary.Cash["TRY"], tc.wantCash)
			}
			if len(summary.Positions) != 1 {
				t.Fatalf("len(Positions) = %d, want 1", len(summary.Positions))
			}
			pos := summary.Positions[0]
			if pos.Currency != "TRY" {
				t.Errorf("Currency = %s, want TRY", pos.Currency)
			}
			if !pos.Quantity.Equal(d(tc.wantQuantity)) {
				t.Errorf("Quantity = %s, want %s", pos.Quantity, tc.wantQuantity)
			}
			if !pos.AvgCost.Equal(d(tc.wantAvgCost)) {
				t.Errorf("AvgCost = %s, want %s", pos.AvgCost, tc.wantAvgCost)
			}
			if !pos.RealizedPL.Equal(d(tc.wantRealizedPL)) {
				t.Errorf("RealizedPL = %s, want %s", pos.RealizedPL, tc.wantRealizedPL)
			}
			if !pos.UnrealizedPL.Equal(d(tc.wantUnrealizedPL)) {
				t.Errorf("UnrealizedPL = %s, want %s", pos.UnrealizedPL, tc.wantUnrealizedPL)
			}
			if !pos.HasPrice {
				t.Errorf("HasPrice = false, want true")
			}
			if !pos.LastPrice.Equal(d("14")) {
				t.Errorf("LastPrice = %s, want 14", pos.LastPrice)
			}
		})
	}
}

// TestComputeNegativeQuantity covers selling more than is held: the
// resulting negative quantity is returned as-is, with no error or warning.
func TestComputeNegativeQuantity(t *testing.T) {
	var instrumentID int64 = 7

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-02-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-02-02", Quantity: d("15"), Price: d("12"), Amount: d("180"), Currency: "TRY"},
	}

	// avg cost after the buy: 100/10 = 10
	// realized: 180 - 0 - 15*10 = 30
	// cost basis after sell: 100 - 15*10 = -50, quantity: 10 - 15 = -5
	summary := holdings.Compute(txs, nil, nil)

	if len(summary.Positions) != 1 {
		t.Fatalf("len(Positions) = %d, want 1", len(summary.Positions))
	}
	pos := summary.Positions[0]
	if pos.Currency != "TRY" {
		t.Errorf("Currency = %s, want TRY (absent from currencyOf)", pos.Currency)
	}
	if !pos.Quantity.Equal(d("-5")) {
		t.Errorf("Quantity = %s, want -5", pos.Quantity)
	}
	if !pos.CostBasis.Equal(d("-50")) {
		t.Errorf("CostBasis = %s, want -50", pos.CostBasis)
	}
	if !pos.RealizedPL.Equal(d("30")) {
		t.Errorf("RealizedPL = %s, want 30", pos.RealizedPL)
	}
	if pos.HasPrice {
		t.Errorf("HasPrice = true, want false (no latest price given)")
	}
	if !pos.MarketValue.Equal(pos.CostBasis) {
		t.Errorf("MarketValue = %s, want equal to CostBasis %s when no price known", pos.MarketValue, pos.CostBasis)
	}
}

// TestComputeZeroQuantityDropped covers that a fully-closed position is
// dropped from Positions but its realized P/L still counts toward its
// currency's totals via Convert.
func TestComputeZeroQuantityDropped(t *testing.T) {
	var instrumentID int64 = 9

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2024-03-01", Quantity: d("10"), Price: d("10"), Amount: d("100"), Currency: "TRY"},
		{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2024-03-02", Quantity: d("10"), Price: d("12"), Amount: d("120"), Currency: "TRY"},
		{ID: 3, InstrumentID: &instrumentID, Type: transaction.TypeDividend, Date: "2024-03-03", Amount: d("5"), Currency: "TRY"},
	}

	summary := holdings.Compute(txs, nil, nil)

	if len(summary.Positions) != 0 {
		t.Fatalf("len(Positions) = %d, want 0 (fully closed position dropped)", len(summary.Positions))
	}
	// cash: -100 + 120 + 5 = 25
	if !summary.Cash["TRY"].Equal(d("25")) {
		t.Errorf("Cash[TRY] = %s, want 25", summary.Cash["TRY"])
	}
	// realized: (120 - 100) from the sell + 5 dividend, kept although the
	// position itself is gone.
	if !summary.RealizedPL["TRY"].Equal(d("25")) {
		t.Errorf("RealizedPL[TRY] = %s, want 25", summary.RealizedPL["TRY"])
	}
	// The closed position is still reported, so the summary page can show
	// where that realized 25 came from.
	if len(summary.Closed) != 1 {
		t.Fatalf("len(Closed) = %d, want 1", len(summary.Closed))
	}
	closed := summary.Closed[0]
	if closed.InstrumentID != instrumentID || !closed.RealizedPL.Equal(d("25")) {
		t.Errorf("Closed[0] = {id %d, realized %s}, want {id 9, realized 25}", closed.InstrumentID, closed.RealizedPL)
	}
	if !closed.Quantity.IsZero() || !closed.MarketValue.IsZero() || !closed.UnrealizedPL.IsZero() {
		t.Errorf("Closed[0] quantity/value/unrealized = %s/%s/%s, want all zero", closed.Quantity, closed.MarketValue, closed.UnrealizedPL)
	}
	totals := holdings.Convert(summary, nil, "TRY")
	if !totals.RealizedPL.Equal(d("25")) {
		t.Errorf("Convert RealizedPL = %s, want 25", totals.RealizedPL)
	}
}

// TestComputeRealizedCost covers the base of a closed position's realized
// percentage: the cost of the shares sold, kept after the position
// closes and summed across portfolios. A sell's fee lowers the proceeds, not
// the cost of what was sold.
func TestComputeRealizedCost(t *testing.T) {
	var instrumentID int64 = 7

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-01", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
		{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2026-09-02", Quantity: d("4"), Price: d("120"), Amount: d("480"), Fee: d("2"), Currency: "TRY"},
		{ID: 3, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2026-09-03", Quantity: d("6"), Price: d("90"), Amount: d("540"), Currency: "TRY"},
	}

	summary := holdings.Compute(txs, nil, nil)

	if len(summary.Closed) != 1 {
		t.Fatalf("len(Closed) = %d, want 1", len(summary.Closed))
	}
	p := summary.Closed[0]
	if !p.RealizedCost.Equal(d("1000")) || !p.RealizedPL.Equal(d("18")) {
		t.Errorf("realized cost/P/L = %s/%s, want 1000/18", p.RealizedCost, p.RealizedPL)
	}
	merged := holdings.Merge([]holdings.Summary{summary, summary})
	if got := merged.Closed[0].RealizedCost; !got.Equal(d("2000")) {
		t.Errorf("merged RealizedCost = %s, want 2000", got)
	}
}

// TestComputeSameDaySellFirst covers a day trade entered sell-first (a
// newest-first broker history): the buy still applies before the sell on
// that day, so the sale is not booked at an average cost of 0 and the next
// buy starts from a clean cost basis.
func TestComputeSameDaySellFirst(t *testing.T) {
	var instrumentID int64 = 576

	txs := []transaction.Transaction{
		{ID: 1, Type: transaction.TypeDeposit, Date: "2026-09-01", Amount: d("10000"), Currency: "TRY"},
		{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2026-09-02", Quantity: d("10"), Price: d("120"), Amount: d("1200"), Currency: "TRY"},
		{ID: 3, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-02", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
		{ID: 4, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-10", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
	}

	summary := holdings.Compute(txs, map[int64]decimal.Decimal{instrumentID: d("100")}, nil)

	if len(summary.Positions) != 1 {
		t.Fatalf("len(Positions) = %d, want 1", len(summary.Positions))
	}
	p := summary.Positions[0]
	if !summary.RealizedPL["TRY"].Equal(d("200")) {
		t.Errorf("RealizedPL[TRY] = %s, want 200", summary.RealizedPL["TRY"])
	}
	if !p.AvgCost.Equal(d("100")) || !p.CostBasis.Equal(d("1000")) || !p.UnrealizedPL.IsZero() {
		t.Errorf("avg/cost basis/unrealized = %s/%s/%s, want 100/1000/0", p.AvgCost, p.CostBasis, p.UnrealizedPL)
	}
}

// TestApplyCoverOversold covers a buy that covers an oversold quantity: it
// books realized P/L for the covered part, the short's
// proceeds less the cover's cost and fee share, and only the rest of the
// buy becomes cost basis. Each row is on its own date and FactorOn is 2.
func TestApplyCoverOversold(t *testing.T) {
	var id int64 = 9
	tx := func(typ, date, qty, amount, fee string) transaction.Transaction {
		return transaction.Transaction{InstrumentID: &id, Type: typ, Date: date, Quantity: d(qty), Amount: d(amount), Fee: d(fee), Currency: "TRY"}
	}
	tests := []struct {
		name                                 string
		txs                                  []transaction.Transaction
		wantRealized, wantRealizedCost       string
		wantQty, wantCost, wantReportingCost string
	}{
		{
			name: "sell from nothing, then cover",
			txs: []transaction.Transaction{
				tx(transaction.TypeSell, "2026-09-01", "10", "1200", "0"),
				tx(transaction.TypeBuy, "2026-09-02", "10", "1000", "0"),
			},
			wantRealized: "200", wantRealizedCost: "1000", wantQty: "0", wantCost: "0", wantReportingCost: "0",
		},
		{
			name: "partial oversell, covered higher",
			txs: []transaction.Transaction{
				tx(transaction.TypeBuy, "2026-09-01", "5", "500", "0"),
				tx(transaction.TypeSell, "2026-09-02", "10", "1200", "0"),
				tx(transaction.TypeBuy, "2026-09-03", "5", "550", "0"),
			},
			wantRealized: "150", wantRealizedCost: "1050", wantQty: "0", wantCost: "0", wantReportingCost: "0",
		},
		{
			name: "partial oversell, cover goes long with a fee",
			txs: []transaction.Transaction{
				tx(transaction.TypeBuy, "2026-09-01", "5", "500", "0"),
				tx(transaction.TypeSell, "2026-09-02", "10", "1200", "0"),
				tx(transaction.TypeBuy, "2026-09-03", "10", "1100", "10"),
			},
			wantRealized: "145", wantRealizedCost: "1055", wantQty: "5", wantCost: "555", wantReportingCost: "1110",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := holdings.NewState(nil)
			state.FactorOn = func(string, string) (decimal.Decimal, bool) { return d("2"), true }
			for _, x := range tt.txs {
				state.Apply(x)
			}
			s := state.Summarize(nil)
			p := append(s.Positions, s.Closed...)[0]
			if !p.RealizedPL.Equal(d(tt.wantRealized)) || !s.RealizedPL["TRY"].Equal(d(tt.wantRealized)) || !p.RealizedCost.Equal(d(tt.wantRealizedCost)) {
				t.Errorf("realized P/L/summary/cost = %s/%s/%s, want %s/%s/%s", p.RealizedPL, s.RealizedPL["TRY"], p.RealizedCost, tt.wantRealized, tt.wantRealized, tt.wantRealizedCost)
			}
			if !p.Quantity.Equal(d(tt.wantQty)) || !p.CostBasis.Equal(d(tt.wantCost)) || !p.CostBasisReporting.Equal(d(tt.wantReportingCost)) {
				t.Errorf("quantity/cost/reporting cost = %s/%s/%s, want %s/%s/%s", p.Quantity, p.CostBasis, p.CostBasisReporting, tt.wantQty, tt.wantCost, tt.wantReportingCost)
			}
		})
	}
}

// TestComputeSameDaySellThenRebuy covers a sell and a same-day rebuy entered
// in trade order: the sell realizes against the old lot and the rebuy starts
// a new one, since the sell has the shares and so keeps its id order.
func TestComputeSameDaySellThenRebuy(t *testing.T) {
	var instrumentID int64 = 576

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-01", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
		{ID: 2, InstrumentID: &instrumentID, Type: transaction.TypeSell, Date: "2026-09-02", Quantity: d("10"), Price: d("150"), Amount: d("1500"), Currency: "TRY"},
		{ID: 3, InstrumentID: &instrumentID, Type: transaction.TypeBuy, Date: "2026-09-02", Quantity: d("10"), Price: d("200"), Amount: d("2000"), Currency: "TRY"},
	}

	summary := holdings.Compute(txs, nil, nil)

	if len(summary.Positions) != 1 {
		t.Fatalf("len(Positions) = %d, want 1", len(summary.Positions))
	}
	p := summary.Positions[0]
	if !summary.RealizedPL["TRY"].Equal(d("500")) {
		t.Errorf("RealizedPL[TRY] = %s, want 500", summary.RealizedPL["TRY"])
	}
	if !p.Quantity.Equal(d("10")) || !p.AvgCost.Equal(d("200")) || !p.CostBasis.Equal(d("2000")) {
		t.Errorf("qty/avg/cost basis = %s/%s/%s, want 10/200/2000", p.Quantity, p.AvgCost, p.CostBasis)
	}
}

// TestComputeMultiCurrency covers cash tracked per currency and positions
// valued in their own currency: a TRY buy, a USD buy, a USD deposit, and a
// TRY dividend.
func TestComputeMultiCurrency(t *testing.T) {
	var tryInstrument int64 = 1
	var usdInstrument int64 = 2

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: &tryInstrument, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Price: d("100"), Amount: d("1000"), Currency: "TRY"},
		{ID: 2, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: d("500"), Currency: "USD"},
		{ID: 3, InstrumentID: &usdInstrument, Type: transaction.TypeBuy, Date: "2024-01-03", Quantity: d("5"), Price: d("50"), Amount: d("250"), Currency: "USD"},
		{ID: 4, InstrumentID: &tryInstrument, Type: transaction.TypeDividend, Date: "2024-01-04", Amount: d("20"), Currency: "TRY"},
	}
	currencyOf := map[int64]string{tryInstrument: "TRY", usdInstrument: "USD"}
	latest := map[int64]decimal.Decimal{tryInstrument: d("110"), usdInstrument: d("55")}

	summary := holdings.Compute(txs, latest, currencyOf)

	// TRY cash: -1000 (buy) + 20 (dividend) = -980
	if !summary.Cash["TRY"].Equal(d("-980")) {
		t.Errorf("Cash[TRY] = %s, want -980", summary.Cash["TRY"])
	}
	// USD cash: 500 (deposit) - 250 (buy) = 250
	if !summary.Cash["USD"].Equal(d("250")) {
		t.Errorf("Cash[USD] = %s, want 250", summary.Cash["USD"])
	}
	if len(summary.Cash) != 2 {
		t.Errorf("len(Cash) = %d, want 2 (only currencies that moved)", len(summary.Cash))
	}

	if len(summary.Positions) != 2 {
		t.Fatalf("len(Positions) = %d, want 2", len(summary.Positions))
	}
	byID := map[int64]holdings.Position{}
	for _, p := range summary.Positions {
		byID[p.InstrumentID] = p
	}
	if byID[tryInstrument].Currency != "TRY" {
		t.Errorf("TRY position currency = %s, want TRY", byID[tryInstrument].Currency)
	}
	if byID[usdInstrument].Currency != "USD" {
		t.Errorf("USD position currency = %s, want USD", byID[usdInstrument].Currency)
	}
	// USD position: market value 5*55 = 275, cost basis 250, unrealized 25.
	if !byID[usdInstrument].MarketValue.Equal(d("275")) {
		t.Errorf("USD MarketValue = %s, want 275", byID[usdInstrument].MarketValue)
	}
	if !byID[usdInstrument].UnrealizedPL.Equal(d("25")) {
		t.Errorf("USD UnrealizedPL = %s, want 25", byID[usdInstrument].UnrealizedPL)
	}

	// Convert into TRY reporting currency, USD = 40 TRY.
	rates := map[string]decimal.Decimal{"USD": d("40")}
	totalsTRY := holdings.Convert(summary, rates, "TRY")
	// cash: -980 (TRY) + 250*40 (USD) = -980 + 10000 = 9020
	if !totalsTRY.Cash.Equal(d("9020")) {
		t.Errorf("Convert(TRY).Cash = %s, want 9020", totalsTRY.Cash)
	}
	// TRY position market value: 10*110 = 1100. USD position: 275*40 = 11000.
	// total value: cash 9020 + 1100 + 11000 = 21120
	if !totalsTRY.TotalValue.Equal(d("21120")) {
		t.Errorf("Convert(TRY).TotalValue = %s, want 21120", totalsTRY.TotalValue)
	}
	// unrealized: TRY position (1100-1000=100) + USD position (25*40=1000) = 1100
	if !totalsTRY.UnrealizedPL.Equal(d("1100")) {
		t.Errorf("Convert(TRY).UnrealizedPL = %s, want 1100", totalsTRY.UnrealizedPL)
	}
	// realized: TRY dividend 20, no USD realized.
	if !totalsTRY.RealizedPL.Equal(d("20")) {
		t.Errorf("Convert(TRY).RealizedPL = %s, want 20", totalsTRY.RealizedPL)
	}
	if len(totalsTRY.Missing) != 0 {
		t.Errorf("Convert(TRY).Missing = %v, want empty", totalsTRY.Missing)
	}

	// Convert into USD reporting currency: factor for TRY amounts is
	// rates[TRY](=1 implied) / rates[USD](=40).
	totalsUSD := holdings.Convert(summary, rates, "USD")
	// cash: -980/40 + 250 = -24.5 + 250 = 225.5
	if !totalsUSD.Cash.Equal(d("225.5")) {
		t.Errorf("Convert(USD).Cash = %s, want 225.5", totalsUSD.Cash)
	}
}

// TestConvertMissingRate covers a currency with cash or a position but no
// entry in rates: its amounts are excluded from the totals and it is
// listed in Missing.
func TestConvertMissingRate(t *testing.T) {
	var eurInstrument int64 = 3

	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Currency: "TRY"},
		{ID: 2, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2024-01-02", Amount: d("100"), Currency: "EUR"},
		{ID: 3, InstrumentID: &eurInstrument, Type: transaction.TypeBuy, Date: "2024-01-03", Quantity: d("2"), Price: d("30"), Amount: d("60"), Currency: "EUR"},
	}
	currencyOf := map[int64]string{eurInstrument: "EUR"}
	summary := holdings.Compute(txs, nil, currencyOf)

	// No EUR rate supplied.
	totals := holdings.Convert(summary, map[string]decimal.Decimal{}, "TRY")

	// Only TRY cash counts: 1000.
	if !totals.Cash.Equal(d("1000")) {
		t.Errorf("Cash = %s, want 1000 (EUR excluded)", totals.Cash)
	}
	if !totals.TotalValue.Equal(d("1000")) {
		t.Errorf("TotalValue = %s, want 1000 (EUR position excluded)", totals.TotalValue)
	}
	if len(totals.Missing) != 1 || totals.Missing[0] != "EUR" {
		t.Errorf("Missing = %v, want [EUR]", totals.Missing)
	}
}

// TestConvertMissingReportingRate pins the contract documented on
// Totals.Missing: when the reporting currency itself has no rate,
// the factor rates[currency]/rates[reporting] cannot be built for anything
// else, so every other currency is excluded and listed -- including TRY,
// which needs no rate of its own. Missing names what was left out of the
// totals, not what is absent from rates, so the UI must not present a
// listed currency as the one lacking a rate.
func TestConvertMissingReportingRate(t *testing.T) {
	txs := []transaction.Transaction{
		{ID: 1, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2026-09-01", Amount: d("500"), Currency: "TRY"},
		{ID: 2, InstrumentID: nil, Type: transaction.TypeDeposit, Date: "2026-09-02", Amount: d("100"), Currency: "USD"},
	}
	summary := holdings.Compute(txs, nil, nil)

	// Reporting in USD with an empty rate table: no USD row to divide by.
	totals := holdings.Convert(summary, map[string]decimal.Decimal{}, "USD")

	// USD cash still counts at factor 1 -- it is already the reporting
	// currency and needs no rate. TRY cash cannot be converted.
	if !totals.Cash.Equal(d("100")) {
		t.Errorf("Cash = %s, want 100 (TRY excluded, USD at 1)", totals.Cash)
	}
	if len(totals.Missing) != 1 || totals.Missing[0] != "TRY" {
		t.Errorf("Missing = %v, want [TRY] (the excluded currency, not the absent rate)", totals.Missing)
	}
}

// TestMergeSeries checks the merge rule: manual wins on a date both
// series have, dates found in only one series pass through untouched, and
// the result stays date-ascending regardless of which series is longer.
func TestMergeSeries(t *testing.T) {
	fetched := []price.Price{
		{Date: "2024-01-01", Close: d("10")},
		{Date: "2024-01-02", Close: d("11")},
		{Date: "2024-01-04", Close: d("13")},
	}
	manual := []price.Price{
		{Date: "2024-01-02", Close: d("99")}, // overrides fetched same-date row
		{Date: "2024-01-03", Close: d("12")}, // gap fetched has no row for
	}

	got := holdings.MergeSeries(fetched, manual)

	want := []struct{ date, close string }{
		{"2024-01-01", "10"},
		{"2024-01-02", "99"},
		{"2024-01-03", "12"},
		{"2024-01-04", "13"},
	}
	if len(got) != len(want) {
		t.Fatalf("len(MergeSeries) = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Date != w.date || got[i].Close.String() != w.close {
			t.Errorf("MergeSeries[%d] = {%s %s}, want {%s %s}", i, got[i].Date, got[i].Close, w.date, w.close)
		}
	}

	// Either side empty just passes the other through.
	if got := holdings.MergeSeries(nil, manual); len(got) != len(manual) {
		t.Errorf("MergeSeries(nil, manual) = %+v, want manual passed through", got)
	}
	if got := holdings.MergeSeries(fetched, nil); len(got) != len(fetched) {
		t.Errorf("MergeSeries(fetched, nil) = %+v, want fetched passed through", got)
	}
}

// TestLatestClose covers the per-id pick the summary, portfolio list and
// statement import share: manual wins on an equal date, the newer date wins
// otherwise, a lone row passes through, and no row at all reports false.
func TestLatestClose(t *testing.T) {
	tests := []struct {
		name            string
		fetched, manual map[int64]price.Price
		wantClose       string
		wantOK          bool
	}{
		{"equal date, manual wins", map[int64]price.Price{1: {Date: "2024-01-02", Close: d("10")}}, map[int64]price.Price{1: {Date: "2024-01-02", Close: d("99")}}, "99", true},
		{"fetched newer", map[int64]price.Price{1: {Date: "2024-01-03", Close: d("11")}}, map[int64]price.Price{1: {Date: "2024-01-02", Close: d("99")}}, "11", true},
		{"manual only", nil, map[int64]price.Price{1: {Date: "2024-01-02", Close: d("99")}}, "99", true},
		{"neither", map[int64]price.Price{2: {Date: "2024-01-02", Close: d("5")}}, nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := holdings.LatestClose(tt.fetched, tt.manual, 1)
			if ok != tt.wantOK || (ok && got.Close.String() != tt.wantClose) {
				t.Errorf("LatestClose = {%s %s}, %v; want close %q, ok %v", got.Date, got.Close, ok, tt.wantClose, tt.wantOK)
			}
		})
	}
}

// TestStateMatchesCompute pins the incremental path the history sweep now
// uses to the batch one it replaced: applying transactions one at
// a time and snapshotting must equal Compute over the same prefix, for
// every prefix. A history point is a pure function of that snapshot, so
// equal snapshots mean an unchanged series. The generated set spans a
// decade, three instruments in three currencies, and all six transaction
// types, with several transactions sharing a date.
func TestStateMatchesCompute(t *testing.T) {
	currencyOf := map[int64]string{1: "TRY", 2: "USD", 3: "EUR"}
	latest := map[int64]decimal.Decimal{1: d("18.5"), 2: d("212.34"), 3: d("7.125")}
	types := []string{
		transaction.TypeBuy, transaction.TypeBuy, transaction.TypeSell,
		transaction.TypeDividend, transaction.TypeDeposit,
		transaction.TypeWithdrawal, transaction.TypeFee,
	}

	const count = 700
	txs := make([]transaction.Transaction, 0, count)
	day := time.Date(2016, 1, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		day = day.AddDate(0, 0, i%5)
		id := int64(i%3 + 1)
		quantity := d(fmt.Sprintf("%d.5", i%7+1))
		unitPrice := d(fmt.Sprintf("%d.25", i%13+3))
		tx := transaction.Transaction{
			ID:           int64(i + 1),
			InstrumentID: &id,
			Type:         types[i%len(types)],
			Date:         day.Format(time.DateOnly),
			Quantity:     quantity,
			Price:        unitPrice,
			Amount:       quantity.Mul(unitPrice),
			Fee:          d("1.75"),
			Currency:     currencyOf[id],
		}
		switch tx.Type {
		case transaction.TypeDeposit, transaction.TypeWithdrawal, transaction.TypeFee:
			tx.InstrumentID = nil
			tx.Quantity, tx.Price = decimal.Zero, decimal.Zero
			tx.Amount = d(fmt.Sprintf("%d00", i%9+1))
		}
		txs = append(txs, tx)
	}

	state := holdings.NewState(currencyOf)
	for i := 0; i <= len(txs); i++ {
		if i > 0 {
			state.Apply(txs[i-1])
		}
		got := fmt.Sprint(state.Summarize(latest))
		want := fmt.Sprint(holdings.Compute(txs[:i], latest, currencyOf))
		if got != want {
			t.Fatalf("after %d transactions:\n got %s\nwant %s", i, got, want)
		}
	}
}

// TestSummarizeAllMatchesMerge pins the `all` history sweep's snapshot
// to the Merge of per-portfolio Summarize calls it replaced:
// after every transaction, every field the sweep reads and every converted
// total must be equal. Four portfolios trade five instruments in three
// currencies, one with no price; instrument 5 is sold short in portfolio 0
// and bought in portfolio 1, so its merged quantity is zero while neither
// portfolio's is.
func TestSummarizeAllMatchesMerge(t *testing.T) {
	currencyOf := map[int64]string{1: "TRY", 2: "USD", 3: "EUR", 4: "TRY", 5: "TRY"}
	latest := map[int64]decimal.Decimal{1: d("18.5"), 2: d("212.34"), 3: d("7.125"), 5: d("20")}
	rates := map[string]decimal.Decimal{"USD": d("32.4531")}
	types := []string{
		transaction.TypeBuy, transaction.TypeBuy, transaction.TypeSell,
		transaction.TypeDividend, transaction.TypeDeposit,
		transaction.TypeWithdrawal, transaction.TypeFee, transaction.TypeBuy,
	}

	var txs []transaction.Transaction
	day := time.Date(2016, 1, 4, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 400; i++ {
		day = day.AddDate(0, 0, i%3)
		id := int64(i%4 + 1)
		quantity := d(fmt.Sprintf("%d.%d", i%7+1, i%3))
		unitPrice := d(fmt.Sprintf("%d.%02d", i%13+3, i%17))
		tx := transaction.Transaction{
			ID:           int64(i + 1),
			PortfolioID:  int64(i % 4),
			InstrumentID: &id,
			Type:         types[i%len(types)],
			Date:         day.Format(time.DateOnly),
			Quantity:     quantity,
			Amount:       quantity.Mul(unitPrice),
			Fee:          d("1.75"),
			Currency:     currencyOf[id],
		}
		switch tx.Type {
		case transaction.TypeDeposit, transaction.TypeWithdrawal, transaction.TypeFee:
			tx.InstrumentID = nil
			tx.Quantity = decimal.Zero
			tx.Amount = d(fmt.Sprintf("%d00", i%9+1))
		}
		txs = append(txs, tx)
	}
	five := int64(5)
	txs = append(txs,
		transaction.Transaction{PortfolioID: 0, InstrumentID: &five, Type: transaction.TypeSell, Quantity: d("10"), Amount: d("200"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: 1, InstrumentID: &five, Type: transaction.TypeBuy, Quantity: d("10"), Amount: d("150"), Currency: "TRY"},
		transaction.Transaction{PortfolioID: 0, InstrumentID: &five, Type: transaction.TypeSplit, Quantity: d("2")},
		transaction.Transaction{PortfolioID: 1, InstrumentID: &five, Type: transaction.TypeSplit, Quantity: d("2")},
	)

	equal := func(a, b decimal.Decimal) bool { return a.Equal(b) }
	states := make([]*holdings.State, 4)
	for i := range states {
		states[i] = holdings.NewState(currencyOf)
	}
	for i, tx := range txs {
		states[tx.PortfolioID].Apply(tx)
		summaries := make([]holdings.Summary, len(states))
		for j, s := range states {
			summaries[j] = s.Summarize(latest)
		}
		want := holdings.Merge(summaries)
		got := holdings.SummarizeAll(states, latest)

		if !maps.EqualFunc(got.Cash, want.Cash, equal) || !maps.EqualFunc(got.RealizedPL, want.RealizedPL, equal) {
			t.Fatalf("after %d: cash %v realized %v, want %v %v", i+1, got.Cash, got.RealizedPL, want.Cash, want.RealizedPL)
		}
		if !slices.EqualFunc(got.Positions, want.Positions, func(g, w holdings.Position) bool {
			return g.InstrumentID == w.InstrumentID && g.Currency == w.Currency && g.HasPrice == w.HasPrice &&
				g.Quantity.Equal(w.Quantity) && g.CostBasis.Equal(w.CostBasis) && g.LastPrice.Equal(w.LastPrice) &&
				g.MarketValue.Equal(w.MarketValue) && g.UnrealizedPL.Equal(w.UnrealizedPL)
		}) {
			t.Fatalf("after %d: positions\n got %v\nwant %v", i+1, got.Positions, want.Positions)
		}
		for _, reporting := range []string{"TRY", "USD", "EUR"} {
			g, w := holdings.Convert(got, rates, reporting), holdings.Convert(want, rates, reporting)
			if !g.TotalValue.Equal(w.TotalValue) || !g.Cash.Equal(w.Cash) || !g.UnrealizedPL.Equal(w.UnrealizedPL) || !g.RealizedPL.Equal(w.RealizedPL) || !slices.Equal(g.Missing, w.Missing) {
				t.Fatalf("after %d in %s: totals %+v, want %+v", i+1, reporting, g, w)
			}
		}
	}
	if got := holdings.SummarizeAll(states, latest); !slices.ContainsFunc(got.Positions, func(p holdings.Position) bool { return p.InstrumentID == 5 }) {
		t.Errorf("positions = %v, want instrument 5 kept at merged quantity 0 (GLG-827)", got.Positions)
	}
}

// TestComputeCashFee checks that the fee on a cash movement moves cash
// too. Deposit 1000 fee 5 = +995, withdrawal 200 fee 3 = -203, fee 50 fee 2
// = -52, so cash is 740.
func TestComputeCashFee(t *testing.T) {
	txs := []transaction.Transaction{
		{ID: 1, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Fee: d("5"), Currency: "TRY"},
		{ID: 2, Type: transaction.TypeWithdrawal, Date: "2024-01-02", Amount: d("200"), Fee: d("3"), Currency: "TRY"},
		{ID: 3, Type: transaction.TypeFee, Date: "2024-01-03", Amount: d("50"), Fee: d("2"), Currency: "TRY"},
	}

	got := holdings.Compute(txs, nil, nil).Cash["TRY"]
	if got.String() != "740" {
		t.Errorf("cash = %s, want 740", got)
	}
}

func TestComputeSplit(t *testing.T) {
	var a, b int64 = 1, 2

	// buy 10 @ 100; on 2024-01-10 a 2-for-1 split and a sell of 5 @ 60. The
	// sell has the lower id, but the split must still apply first: 20 held,
	// avg 50, sell realizes 300 - 5*50 = 50, 15 left. A split on an
	// instrument never held creates no position.
	s := holdings.Compute([]transaction.Transaction{
		{ID: 1, InstrumentID: &a, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Amount: d("1000"), Currency: "TRY"},
		{ID: 2, InstrumentID: &a, Type: transaction.TypeSell, Date: "2024-01-10", Quantity: d("5"), Amount: d("300"), Currency: "TRY"},
		{ID: 3, InstrumentID: &a, Type: transaction.TypeSplit, Date: "2024-01-10", Quantity: d("2")},
		{InstrumentID: &b, Type: transaction.TypeSplit, Date: "2024-01-05", Quantity: d("2")},
	}, nil, nil)
	if len(s.Positions) != 1 || len(s.Closed) != 0 {
		t.Fatalf("positions %d closed %d, want 1 and 0", len(s.Positions), len(s.Closed))
	}
	p := s.Positions[0]
	if !p.Quantity.Equal(d("15")) || !p.AvgCost.Equal(d("50")) || !p.RealizedPL.Equal(d("50")) || !p.CostBasis.Equal(d("750")) {
		t.Errorf("got qty %s avg %s realized %s basis %s, want 15 50 50 750", p.Quantity, p.AvgCost, p.RealizedPL, p.CostBasis)
	}
	if !s.Cash["TRY"].Equal(d("-700")) {
		t.Errorf("cash = %s, want -700", s.Cash["TRY"])
	}

	// 50% bonus issue: 10 -> 15, avg 100 -> 100/1.5, cost basis unchanged.
	s = holdings.Compute([]transaction.Transaction{
		{ID: 1, InstrumentID: &a, Type: transaction.TypeBuy, Date: "2024-01-01", Quantity: d("10"), Amount: d("1000"), Currency: "TRY"},
		{InstrumentID: &a, Type: transaction.TypeSplit, Date: "2024-02-01", Quantity: d("1.5")},
	}, nil, nil)
	p = s.Positions[0]
	want := d("100").Div(d("1.5"))
	if !p.Quantity.Equal(d("15")) || !p.AvgCost.Equal(want) || !p.CostBasis.Equal(d("1000")) {
		t.Errorf("got qty %s avg %s basis %s, want 15 %s 1000", p.Quantity, p.AvgCost, p.CostBasis, want)
	}
}

// TestComputeReportingCost checks that with FactorOn set, each buy adds
// its cost at its own date's rate, a sell shrinks it by the same ratio as
// the native cost, a split leaves it alone, and a close resets it.
func TestComputeReportingCost(t *testing.T) {
	var id int64 = 1
	rates := map[string]decimal.Decimal{
		"2024-01-02": d("40"), "2024-02-01": d("44"), "2024-06-03": d("48"),
	}
	state := holdings.NewState(map[int64]string{id: "USD"})
	state.FactorOn = func(date, currency string) (decimal.Decimal, bool) {
		if currency != "USD" {
			t.Fatalf("FactorOn currency = %s, want USD", currency)
		}
		r, ok := rates[date]
		return r, ok
	}

	steps := []struct {
		name      string
		tx        transaction.Transaction
		basis     string
		reporting string
		missing   bool
	}{
		{"buy 10 @ 50 at 40", transaction.Transaction{Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("10"), Amount: d("500")}, "500", "20000", false},
		{"buy 10 @ 60 at 44", transaction.Transaction{Type: transaction.TypeBuy, Date: "2024-02-01", Quantity: d("10"), Amount: d("600")}, "1100", "46400", false},
		{"sell 5", transaction.Transaction{Type: transaction.TypeSell, Date: "2024-03-01", Quantity: d("5"), Amount: d("350")}, "825", "34800", false},
		{"2:1 split", transaction.Transaction{Type: transaction.TypeSplit, Date: "2024-04-01", Quantity: d("2")}, "825", "34800", false},
		{"sell all", transaction.Transaction{Type: transaction.TypeSell, Date: "2024-05-02", Quantity: d("30"), Amount: d("2400")}, "0", "0", false},
		{"rebuy 10 @ 70 at 48", transaction.Transaction{Type: transaction.TypeBuy, Date: "2024-06-03", Quantity: d("10"), Amount: d("700")}, "700", "33600", false},
		{"buy with no rate", transaction.Transaction{Type: transaction.TypeBuy, Date: "2024-07-01", Quantity: d("5"), Amount: d("400")}, "1100", "33600", true},
		{"sell all again", transaction.Transaction{Type: transaction.TypeSell, Date: "2024-08-01", Quantity: d("15"), Amount: d("1500")}, "0", "0", false},
	}
	for _, st := range steps {
		st.tx.InstrumentID = &id
		st.tx.Currency = "USD"
		state.Apply(st.tx)
		s := state.Summarize(nil)
		var p holdings.Position
		if len(s.Positions) == 1 {
			p = s.Positions[0]
		} else {
			p = s.Closed[0]
		}
		if !p.CostBasis.Equal(d(st.basis)) || !p.CostBasisReporting.Equal(d(st.reporting)) || p.CostRateMissing != st.missing {
			t.Errorf("%s: basis %s reporting %s missing %v, want %s %s %v",
				st.name, p.CostBasis, p.CostBasisReporting, p.CostRateMissing, st.basis, st.reporting, st.missing)
		}
	}
}

// TestMergeOne checks that one summary comes back as is, with the
// average cost Apply kept across the sell rather than CostBasis / Quantity
// recomputed.
func TestMergeOne(t *testing.T) {
	var id int64 = 1
	s := holdings.Compute([]transaction.Transaction{
		{ID: 1, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("3"), Amount: d("10"), Fee: d("0"), Currency: "TRY"},
		{ID: 2, InstrumentID: &id, Type: transaction.TypeSell, Date: "2024-01-03", Quantity: d("2"), Amount: d("8"), Fee: d("0"), Currency: "TRY"},
	}, map[int64]decimal.Decimal{id: d("4")}, map[int64]string{id: "TRY"})

	if got := holdings.Merge([]holdings.Summary{s}); !reflect.DeepEqual(got, s) {
		t.Errorf("Merge = %+v, want %+v", got, s)
	}
}

// TestMerge checks that per-portfolio summaries sum per instrument and
// per currency into one all-portfolios summary.
func TestMerge(t *testing.T) {
	split := holdings.Split{Date: "2024-06-01", Ratio: d("2")}
	tests := []struct {
		name         string
		in           []holdings.Summary
		wantCash     map[string]string
		wantRealized map[string]string
		want         holdings.Position
	}{
		{
			// Instrument 1 closed in A (realized 50) and open in B: one
			// open row carrying both realized P/Ls and one split.
			name: "closed in one portfolio, open in another",
			in: []holdings.Summary{
				{
					Closed:     []holdings.Position{{InstrumentID: 1, Currency: "TRY", RealizedPL: d("50"), Splits: []holdings.Split{split}}},
					Cash:       map[string]decimal.Decimal{"TRY": d("100"), "USD": d("10")},
					RealizedPL: map[string]decimal.Decimal{"TRY": d("50")},
				},
				{
					Positions: []holdings.Position{{InstrumentID: 1, Currency: "TRY", Quantity: d("30"), AvgCost: d("10"), CostBasis: d("300"),
						LastPrice: d("12"), HasPrice: true, MarketValue: d("360"), UnrealizedPL: d("60"), RealizedPL: d("20"), Splits: []holdings.Split{split}}},
					Cash:       map[string]decimal.Decimal{"TRY": d("-300"), "USD": d("5")},
					RealizedPL: map[string]decimal.Decimal{"TRY": d("20")},
				},
			},
			wantCash:     map[string]string{"TRY": "-200", "USD": "15"},
			wantRealized: map[string]string{"TRY": "70"},
			want: holdings.Position{InstrumentID: 1, Quantity: d("30"), AvgCost: d("10"), CostBasis: d("300"), LastPrice: d("12"), HasPrice: true,
				MarketValue: d("360"), UnrealizedPL: d("60"), RealizedPL: d("70"), Splits: []holdings.Split{split}},
		},
		{
			// Long in A, oversold by as much in B. The quantities
			// cancel, but the row is open, so its value and cost stay in
			// the totals.
			name: "long in one portfolio, oversold in another",
			in: []holdings.Summary{
				{
					Positions:  []holdings.Position{{InstrumentID: 1, Currency: "TRY", Quantity: d("-10"), RealizedPL: d("1500")}},
					Cash:       map[string]decimal.Decimal{"TRY": d("1500")},
					RealizedPL: map[string]decimal.Decimal{"TRY": d("1500")},
				},
				{
					Positions: []holdings.Position{{InstrumentID: 1, Currency: "TRY", Quantity: d("10"), AvgCost: d("200"), CostBasis: d("2000"),
						MarketValue: d("2000")}},
					Cash:       map[string]decimal.Decimal{"TRY": d("-2000")},
					RealizedPL: map[string]decimal.Decimal{"TRY": d("0")},
				},
			},
			wantCash:     map[string]string{"TRY": "-500"},
			wantRealized: map[string]string{"TRY": "1500"},
			want:         holdings.Position{InstrumentID: 1, CostBasis: d("2000"), MarketValue: d("2000"), RealizedPL: d("1500")},
		},
		{
			// Open in both: cost and reporting cost sum, avg cost is
			// 380 / 40, and one missing rate marks the merged row.
			name: "open in both, reporting cost",
			in: []holdings.Summary{
				{
					Positions: []holdings.Position{{InstrumentID: 1, Currency: "USD", Quantity: d("10"), AvgCost: d("8"), CostBasis: d("80"),
						MarketValue: d("80"), CostBasisReporting: d("2400"), CostRateMissing: true}},
					Cash:       map[string]decimal.Decimal{},
					RealizedPL: map[string]decimal.Decimal{"USD": d("0")},
				},
				{
					Positions: []holdings.Position{{InstrumentID: 1, Currency: "USD", Quantity: d("30"), AvgCost: d("10"), CostBasis: d("300"),
						MarketValue: d("300"), CostBasisReporting: d("9600")}},
					Cash:       map[string]decimal.Decimal{},
					RealizedPL: map[string]decimal.Decimal{"USD": d("0")},
				},
			},
			wantCash:     map[string]string{},
			wantRealized: map[string]string{"USD": "0"},
			want: holdings.Position{InstrumentID: 1, Quantity: d("40"), AvgCost: d("9.5"), CostBasis: d("380"), MarketValue: d("380"),
				CostBasisReporting: d("12000"), CostRateMissing: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := holdings.Merge(tc.in)
			if len(got.Positions) != 1 || len(got.Closed) != 0 {
				t.Fatalf("got %d positions, %d closed, want 1, 0", len(got.Positions), len(got.Closed))
			}
			p, w := got.Positions[0], tc.want
			for name, pair := range map[string][2]decimal.Decimal{
				"Quantity": {p.Quantity, w.Quantity}, "AvgCost": {p.AvgCost, w.AvgCost}, "CostBasis": {p.CostBasis, w.CostBasis},
				"LastPrice": {p.LastPrice, w.LastPrice}, "MarketValue": {p.MarketValue, w.MarketValue}, "UnrealizedPL": {p.UnrealizedPL, w.UnrealizedPL},
				"RealizedPL": {p.RealizedPL, w.RealizedPL}, "CostBasisReporting": {p.CostBasisReporting, w.CostBasisReporting},
			} {
				if !pair[0].Equal(pair[1]) {
					t.Errorf("%s = %s, want %s", name, pair[0], pair[1])
				}
			}
			if p.HasPrice != w.HasPrice || p.CostRateMissing != w.CostRateMissing {
				t.Errorf("HasPrice, CostRateMissing = %v, %v, want %v, %v", p.HasPrice, p.CostRateMissing, w.HasPrice, w.CostRateMissing)
			}
			if fmt.Sprint(p.Splits) != fmt.Sprint(w.Splits) {
				t.Errorf("Splits = %v, want %v", p.Splits, w.Splits)
			}
			for name, pair := range map[string][2]map[string]string{"Cash": {toStrings(got.Cash), tc.wantCash}, "RealizedPL": {toStrings(got.RealizedPL), tc.wantRealized}} {
				if fmt.Sprint(pair[0]) != fmt.Sprint(pair[1]) {
					t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
				}
			}
		})
	}
}

func toStrings(m map[string]decimal.Decimal) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}

// TestComputeStats checks that every row type adds to its currency's
// stats, and Merge sums them per currency.
func TestComputeStats(t *testing.T) {
	var id int64 = 1
	tests := []struct {
		name string
		txs  []transaction.Transaction
		want holdings.Stats
	}{
		{
			name: "every row type",
			txs: []transaction.Transaction{
				{ID: 1, Type: transaction.TypeDeposit, Date: "2024-01-01", Amount: d("1000"), Fee: d("1"), Currency: "TRY"},
				{ID: 2, InstrumentID: &id, Type: transaction.TypeBuy, Date: "2024-01-02", Quantity: d("10"), Price: d("10"), Amount: d("100"), Fee: d("5"), Currency: "TRY"},
				{ID: 3, InstrumentID: &id, Type: transaction.TypeSell, Date: "2024-01-03", Quantity: d("5"), Price: d("12"), Amount: d("60"), Fee: d("5"), Currency: "TRY"},
				{ID: 4, Type: transaction.TypeWithdrawal, Date: "2024-01-04", Amount: d("200"), Fee: d("0"), Currency: "TRY"},
				{ID: 5, Type: transaction.TypeFee, Date: "2024-01-05", Amount: d("10"), Fee: d("0"), Currency: "TRY"},
				{ID: 6, InstrumentID: &id, Type: transaction.TypeDividend, Date: "2024-01-06", Amount: d("100"), Fee: d("15"), Currency: "TRY"},
				{ID: 7, Type: transaction.TypeDividend, Date: "2024-01-07", Amount: d("50"), Fee: d("0"), Currency: "TRY"},
				{ID: 8, InstrumentID: &id, Type: transaction.TypeSplit, Date: "2024-01-08", Quantity: d("2"), Currency: "TRY"},
			},
			want: holdings.Stats{Deposits: d("1000"), Withdrawals: d("200"), Fees: d("21"), DividendsGross: d("100"), DividendsTax: d("15"), Interest: d("50")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := holdings.Compute(tc.txs, nil, map[int64]string{id: "TRY"})
			if len(s.Stats) != 1 {
				t.Fatalf("Stats = %v, want only TRY", s.Stats)
			}
			if got := fmt.Sprint(s.Stats["TRY"]); got != fmt.Sprint(tc.want) {
				t.Errorf("Stats[TRY] = %s, want %s", got, fmt.Sprint(tc.want))
			}

			usd := holdings.Summary{Stats: map[string]holdings.Stats{"USD": {Deposits: d("7")}}}
			merged := holdings.Merge([]holdings.Summary{s, s, usd})
			double := holdings.Stats{Deposits: d("2000"), Withdrawals: d("400"), Fees: d("42"), DividendsGross: d("200"), DividendsTax: d("30"), Interest: d("100")}
			if got := fmt.Sprint(merged.Stats); got != fmt.Sprint(map[string]holdings.Stats{"TRY": double, "USD": usd.Stats["USD"]}) {
				t.Errorf("merged Stats = %s", got)
			}
		})
	}
}
