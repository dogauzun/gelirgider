package statement

import (
	"context"
	"maps"
	"slices"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/price"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// reconcileEntry compares one symbol's period-end quantity: what the
// statement says the account holds against what the portfolio would compute
// once this preview's ok rows are committed. StatementQuantity is null for a
// position the statement does not mention -- most often a month that was
// never imported. Average cost is not compared: Midas rounds it and
// its cost-basis rules are not necessarily ours (ADR 0006). InstrumentID is
// the instrument the line compares, left out for a holding that resolves to
// none: the page moves the line with the rows the user checks and edits.
type reconcileEntry struct {
	Symbol            string  `json:"symbol"`
	InstrumentID      int64   `json:"instrument_id,omitempty"`
	StatementQuantity *string `json:"statement_quantity"`
	PortfolioQuantity string  `json:"portfolio_quantity"`
	Match             bool    `json:"match"`
}

// cashReconcileEntry compares the account's period-end cash in the statement
// currency: the balance the statement prints against the cash the portfolio
// would hold at period_end once this preview's ok rows are committed. Other
// currencies are not compared: the statement says nothing about them. Match
// allows a difference of up to 0.01.
type cashReconcileEntry struct {
	Currency        string `json:"currency"`
	StatementAmount string `json:"statement_amount"`
	PortfolioAmount string `json:"portfolio_amount"`
	Match           bool   `json:"match"`
}

// cashTolerance is the largest difference between statement and portfolio
// cash that still counts as a match.
var cashTolerance = decimal.RequireFromString("0.01")

// periodEnd folds txs dated up to period_end and the preview's ok rows that
// are not flagged as duplicates through the holdings engine, and returns the
// summary with the instruments it names: what the portfolio would hold at
// period_end once a plain commit books this preview,
// splits included.
func (h *handler) periodEnd(ctx context.Context, txs []transaction.Transaction, st Statement, preview []previewRow) (holdings.Summary, map[int64]instrument.Instrument, error) {
	// Existing rows dated after the period are not in the broker's snapshot,
	// and neither are proposed ones: the comparison is as of period_end.
	applied := make([]transaction.Transaction, 0, len(txs)+len(preview))
	for _, t := range txs {
		// List's split rows are added again below, for the preview's rows
		// too.
		if t.Date <= st.PeriodEnd && t.Type != transaction.TypeSplit {
			applied = append(applied, t)
		}
	}
	for _, row := range preview {
		// An ok row either names an instrument or is a cash movement, which
		// the cash comparison needs. A row flagged as a duplicate is
		// left out, as the page leaves it unchecked: the comparison is of the
		// rows a plain commit books. So is a closing sell.
		if row.Status != statusOK || row.DuplicateOf != "" || row.Closing || row.Date > st.PeriodEnd {
			continue
		}
		applied = append(applied, transaction.Transaction{
			InstrumentID: row.InstrumentID,
			Type:         row.Type,
			Date:         row.Date,
			Quantity:     orZero(row.Quantity),
			Price:        orZero(row.Price),
			Amount:       orZero(row.Amount),
			Fee:          orZero(row.Fee),
			Currency:     row.Currency,
		})
	}
	// The split rows List returns once the preview is committed: each
	// instrument's splits dated after its first row, up to period_end and
	// today (ADR 0008). A preview's first row of an instrument can be its
	// first row at all, such as an opening buy before an in-period split.
	first := map[int64]string{}
	for _, t := range applied {
		if t.InstrumentID == nil {
			continue
		}
		if d, ok := first[*t.InstrumentID]; !ok || t.Date < d {
			first[*t.InstrumentID] = t.Date
		}
	}
	splits, err := h.splits.ListMany(ctx, slices.Collect(maps.Keys(first)))
	if err != nil {
		return holdings.Summary{}, nil, err
	}
	today := price.Today()
	for id, rows := range splits {
		for _, s := range rows {
			if s.Date > first[id] && s.Date <= st.PeriodEnd && s.Date <= today {
				applied = append(applied, transaction.Transaction{InstrumentID: &id, Type: transaction.TypeSplit, Date: s.Date, Quantity: s.Ratio})
			}
		}
	}
	// A proposed bonus row (a buy at price 0) books a split, as a stored
	// one does.
	applied, err = transaction.DropBookedSplits(applied)
	if err != nil {
		return holdings.Summary{}, nil, err
	}

	seen := map[int64]bool{}
	var ids []int64
	for _, t := range applied {
		if t.InstrumentID != nil && !seen[*t.InstrumentID] {
			seen[*t.InstrumentID] = true
			ids = append(ids, *t.InstrumentID)
		}
	}
	instruments, err := h.instruments.ByIDs(ctx, ids)
	if err != nil {
		return holdings.Summary{}, nil, err
	}
	currencyOf := make(map[int64]string, len(instruments))
	for id, in := range instruments {
		currencyOf[id] = in.Currency
	}

	// No prices are looked up: quantities and cash are all this compares,
	// and the proposed rows carry no id, so they sort first among their
	// date's rows -- which changes average cost but never the quantity a
	// position or the cash ends on.
	return holdings.Compute(applied, nil, currencyOf), instruments, nil
}

// reconcile compares the statement's period-end snapshot and cash balance
// with the positions and cash the portfolio would hold at period_end once
// the rows this preview starts checked are committed. Nothing is written:
// the transactions are folded through the holdings engine in memory, the
// same engine the summary endpoint runs, so position and cash maths live in
// exactly one place.
func (h *handler) reconcile(ctx context.Context, txs []transaction.Transaction, st Statement, holdIns []instrument.Instrument, preview []previewRow) ([]reconcileEntry, *cashReconcileEntry, error) {
	// A statement with no snapshot and no balance, such as a CSV, has
	// nothing to compare against: listing every position it is "silent
	// about" would flag the whole portfolio. A Midas statement
	// only lacks a balance when it lacks its currency too, and then lists no
	// silent positions either, so this changes nothing for it; one that
	// prints a balance but no positions is still compared.
	if len(st.Holdings) == 0 && st.CashBalance == nil {
		return []reconcileEntry{}, nil, nil
	}
	summary, instruments, err := h.periodEnd(ctx, txs, st, preview)
	if err != nil {
		return nil, nil, err
	}
	computed := map[int64]decimal.Decimal{}
	for _, p := range summary.Positions {
		computed[p.InstrumentID] = p.Quantity
	}

	// holdIns[i] is holding i's instrument (holdingInstruments), so a holding
	// named by a remembered pick compares with that instrument's position.
	// One that resolves to none is compared with nothing held.
	entries := []reconcileEntry{}
	mentioned := map[int64]bool{}
	for i, hold := range st.Holdings {
		var quantity decimal.Decimal
		if id := holdIns[i].ID; id != 0 {
			mentioned[id] = true
			quantity = computed[id]
		}
		stated := hold.Quantity.String()
		entries = append(entries, reconcileEntry{
			Symbol:            hold.Symbol,
			InstrumentID:      holdIns[i].ID,
			StatementQuantity: &stated,
			PortfolioQuantity: quantity.String(),
			Match:             hold.Quantity.Equal(quantity),
		})
	}

	// Positions the statement is silent about. Only the market the statement
	// covers is listed: it says nothing about the others, so a BIST position
	// is not a mismatch of a USD statement, nor of a TRY fund statement
	// whose Market is TEFAS.
	market := st.Market
	if market == "" {
		market = marketForCurrency[st.Currency]
	}
	for _, p := range summary.Positions {
		in := instruments[p.InstrumentID]
		if in.Market != market || mentioned[in.ID] {
			continue
		}
		entries = append(entries, reconcileEntry{
			Symbol:            in.Symbol,
			InstrumentID:      in.ID,
			PortfolioQuantity: p.Quantity.String(),
		})
	}

	var cash *cashReconcileEntry
	if st.CashBalance != nil && st.Currency != "" {
		held := summary.Cash[st.Currency]
		cash = &cashReconcileEntry{
			Currency:        st.Currency,
			StatementAmount: st.CashBalance.String(),
			PortfolioAmount: held.String(),
			Match:           st.CashBalance.Sub(held).Abs().LessThanOrEqual(cashTolerance),
		}
	}
	return entries, cash, nil
}
