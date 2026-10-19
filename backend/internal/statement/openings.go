package statement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/holdings"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
	"github.com/shopspring/decimal"
)

// Opening positions: the error code of an opening buy no price
// was found for, and how many instruments one preview may fetch prices for.
const (
	codeOpeningPrice  = "opening_price"
	codeClosingPrice  = "closing_price"
	maxOpeningFetches = 20
)

// proposeOpenings puts the opening rows in front of preview: what the account
// held before period_start, which the first statement of an account lists
// only as month-end balances. An instrument gets an opening buy
// when the statement holds more of it at period_end (0 when it does not list
// it) than the portfolio would with this preview's ok rows, and the
// portfolio has no transaction of it dated before period_start; one with
// earlier transactions is an ordinary mismatch. A split after that day
// scales the buy once committed, so its quantity is the shortfall divided
// by the splits' ratio and a price the statement prints is multiplied by it.
// The buy is dated the day
// before period_start and priced with the average cost the statement prints
// for the holding when the preview has no buy of it (CostPrice),
// else the stored price on or before
// that date, for a BIST instrument after syncing the bulletin of the last
// weekday on or before it (once per preview, as for an unknown symbol),
// else the one an on-demand fetch stores (for at most
// maxOpeningFetches instruments), else the statement's own period-end price
// (ApproximatePrice); with none it is an error row coded opening_price for
// the user to price. One deposit per currency, on the same day, pays for the
// priced buys so they do not push cash negative; it is rounded up to the
// cent, as an amount takes at most 9 fraction digits. A statement without holdings or a cash balance, such as a
// CSV, is not compared at all (see reconcile), and a symbol that resolves to
// no instrument gets nothing. A preview that still has a buy or sell of an
// unknown instrument gets nothing at all: that row may belong to an
// instrument whose holding does resolve (a Garanti fund row named by its
// name, its holding by its code), so no shortfall is known until the user
// resolves it. Rows of an unknown symbol whose units net to zero over the
// preview, such as a money-market fund bought and sold within the month,
// cannot change a period-end holding and do not block.
// An instrument the portfolio holds more of at period_end than the statement
// lists, and held before period_start, gets a closing sell of the
// difference instead, divided by the splits' ratio as an opening buy's
// shortfall is, at most what it held the day before period_start,
// dated and priced like an opening buy but never at the average cost; with
// no price it is an error row coded closing_price. It needs a statement that
// lists holdings, is keyed close-<instrument>, follows the opening rows and
// starts unchecked (Closing). One withdrawal per currency, also
// Closing and keyed close-cash-<currency>, follows them with the priced
// sells' proceeds rounded down to the cent; the page books it in step with
// the sells it checks.
// An opening row is keyed by its instrument or currency, not its place, so
// a preview requested again with a pick that adds one keeps the keys of the
// others, which the page keeps the user's edits by.
func (h *handler) proposeOpenings(ctx context.Context, req requester, txs []transaction.Transaction, st Statement, holdIns []instrument.Instrument, preview []previewRow) ([]previewRow, error) {
	if len(st.Holdings) == 0 && st.CashBalance == nil || unresolvedTrades(preview) {
		return preview, nil
	}
	start, err := time.Parse(time.DateOnly, st.PeriodStart)
	if err != nil {
		// The period warning already says the commit refuses it.
		return preview, nil
	}
	r := &openingRun{h: h, ctx: ctx, userID: req.userID, txs: txs, st: st, preview: preview, date: start.AddDate(0, 0, -1).Format(time.DateOnly)}
	summary, instruments, err := h.periodEnd(ctx, txs, st, preview)
	if err != nil {
		return nil, err
	}
	r.held = positionQuantities(summary)
	// What the portfolio holds on the day before the period, which a
	// closing sell on that day cannot exceed.
	dayBefore, _, err := h.periodEnd(ctx, txs, Statement{PeriodEnd: r.date}, nil)
	if err != nil {
		return nil, err
	}
	r.before = positionQuantities(dayBefore)
	r.earlier = earlierInstruments(txs, st.PeriodStart)
	cands := candidates(st, holdIns, preview, summary, instruments)
	if r.ratio, err = r.splitRatios(cands); err != nil {
		return nil, err
	}
	defer r.startPricing(req.client, start)()
	closings, openings, err := r.rows(cands)
	if err != nil {
		return nil, err
	}
	// One withdrawal per currency, after the closing sells, takes their
	// proceeds out again, rounded down to the cent so cash never goes
	// negative: the sales happened in months not imported, and so did
	// whatever the money went to.
	withdrawals, err := r.cashRows(transaction.TypeWithdrawal, r.proceeds, decimal.Decimal.RoundFloor)
	if err != nil {
		return nil, err
	}
	closings = append(closings, withdrawals...)
	if len(openings) == 0 {
		return append(closings, preview...), nil
	}
	cash, err := r.cashRows(transaction.TypeDeposit, r.deposits, decimal.Decimal.RoundCeil)
	if err != nil {
		return nil, err
	}
	return append(append(append(cash, openings...), closings...), preview...), nil
}

// unresolvedTrades reports whether preview still has a buy or sell of an
// unknown instrument whose symbol's units do not net to zero, or one whose
// quantity does not read, which proposeOpenings waits on.
func unresolvedTrades(preview []previewRow) bool {
	unknown := map[string]decimal.Decimal{}
	for _, row := range preview {
		trade := row.Type == transaction.TypeBuy || row.Type == transaction.TypeSell
		if !trade || (row.Status != statusUnknownInstrument && !row.UnknownInstrument) {
			continue
		}
		if row.Quantity == nil {
			return true
		}
		q := *row.Quantity
		if row.Type == transaction.TypeSell {
			q = q.Neg()
		}
		unknown[row.Symbol] = unknown[row.Symbol].Add(q)
	}
	for _, net := range unknown {
		if !net.IsZero() {
			return true
		}
	}
	return false
}

// positionQuantities maps each of summary's positions to its quantity.
func positionQuantities(summary holdings.Summary) map[int64]decimal.Decimal {
	q := map[int64]decimal.Decimal{}
	for _, p := range summary.Positions {
		q[p.InstrumentID] = p.Quantity
	}
	return q
}

// earlierInstruments holds the instruments of txs with a transaction dated
// before periodStart.
func earlierInstruments(txs []transaction.Transaction, periodStart string) map[int64]bool {
	earlier := map[int64]bool{}
	for _, t := range txs {
		if t.InstrumentID != nil && t.Date < periodStart {
			earlier[*t.InstrumentID] = true
		}
	}
	return earlier
}

// candidate is an instrument proposeOpenings may open or close: what the
// statement holds of it, its period-end price and its average cost, the
// last two zero when the statement prints none fit to price it with.
type candidate struct {
	in                  instrument.Instrument
	stated, price, cost decimal.Decimal
}

// candidates are the instruments the statement holds, in its order, then
// the positions of its market it is silent about, the same two lists
// reconcile compares.
func candidates(st Statement, holdIns []instrument.Instrument, preview []previewRow, summary holdings.Summary, instruments map[int64]instrument.Instrument) []candidate {
	// The average cost a statement prints at period end includes its own
	// buys of the period, so it is no price for the opening alone.
	bought := map[int64]bool{}
	for _, row := range preview {
		if row.Type == transaction.TypeBuy && row.InstrumentID != nil {
			bought[*row.InstrumentID] = true
		}
	}
	var cands []candidate
	seen := map[int64]bool{}
	for i, hold := range st.Holdings {
		in := holdIns[i]
		if in.ID == 0 || seen[in.ID] {
			continue
		}
		seen[in.ID] = true
		c := candidate{in: in, stated: hold.Quantity}
		// A unit price in another currency, such as a Garanti
		// foreign-currency fund's USD price of a TRY fund, is no
		// price for the instrument.
		if hold.Currency == in.Currency {
			c.price = hold.Price
			if !bought[in.ID] {
				c.cost = hold.AvgCost
			}
		}
		cands = append(cands, c)
	}
	market := st.Market
	if market == "" {
		market = marketForCurrency[st.Currency]
	}
	for _, p := range summary.Positions {
		if in := instruments[p.InstrumentID]; in.Market == market && !seen[in.ID] {
			cands = append(cands, candidate{in: in})
		}
	}
	return cands
}

// currencyTotals sums amounts per currency, keeping the currencies in the
// order they first came.
type currencyTotals struct {
	order []string
	sum   map[string]decimal.Decimal
}

func (t *currencyTotals) add(currency string, amount decimal.Decimal) {
	if _, ok := t.sum[currency]; !ok {
		if t.sum == nil {
			t.sum = map[string]decimal.Decimal{}
		}
		t.order = append(t.order, currency)
	}
	t.sum[currency] = t.sum[currency].Add(amount)
}

// openingRun is the state of one proposeOpenings call its row builders
// share.
type openingRun struct {
	h       *handler
	ctx     context.Context
	userID  int64
	txs     []transaction.Transaction
	st      Statement
	preview []previewRow
	// date is the day before period_start, every proposed row's date.
	date string
	// held is what the portfolio holds at period_end with the preview's ok
	// rows, before what it holds on date; ratio is the splits' ratio after
	// date per instrument; earlier holds the instruments with a transaction
	// before period_start.
	held, before, ratio map[int64]decimal.Decimal
	earlier             map[int64]bool
	// proceeds and deposits total the priced closing sells and opening
	// buys per currency.
	proceeds, deposits currencyTotals
	// The pricing state startPricing and fetchReady set up for priceOf.
	client       string
	slotWait     time.Duration
	busyLogged   bool
	fetchCtx     context.Context
	release      func()
	session      *lookupSession
	bulletinDate string
	fetches      int
}

// splitRatios is what one unit bought on date holds at period_end per
// candidate: the ratio of the splits after date that the committed opening
// is scaled by, as the statement's holding already is.
func (r *openingRun) splitRatios(cands []candidate) (map[int64]decimal.Decimal, error) {
	probes := slices.Clone(r.preview)
	one, zero := decimal.NewFromInt(1), decimal.Zero
	for _, c := range cands {
		id := c.in.ID
		probes = append(probes, previewRow{
			Status: statusOK, Type: transaction.TypeBuy, Date: r.date, InstrumentID: &id,
			Quantity: &one, Price: &one, Amount: &one, Fee: &zero, Currency: c.in.Currency,
		})
	}
	probed, _, err := r.h.periodEnd(r.ctx, r.txs, r.st, probes)
	if err != nil {
		return nil, err
	}
	ratio := map[int64]decimal.Decimal{}
	for _, p := range probed.Positions {
		ratio[p.InstrumentID] = p.Quantity.Sub(r.held[p.InstrumentID])
	}
	return ratio, nil
}

// startPricing sets up priceOf's fetches and bulletin sync, and returns the
// func that ends them. One deadline for all the fetches, not one each:
// maxOpeningFetches hung fetches would outlast a proxy's timeout.
// An instrument left unfetched is priced as if its fetch found nothing. The
// bulletin sync shares that deadline. A weekend has no bulletin.
func (r *openingRun) startPricing(client string, start time.Time) func() {
	r.client, r.slotWait = client, transaction.FetchSlotWait
	day := start.AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	r.bulletinDate = day.Format(time.DateOnly)
	return func() {
		if r.release != nil {
			r.release()
		}
	}
}

// fetchReady reports whether priceOf may fetch or sync. The first call that
// gets the fetch slot of transaction.AcquireFetchSlot starts the shared
// deadline, and the slot goes at its end, at the latest: a parse whose
// prices are all stored never queues, and one queued behind others keeps its
// whole deadline for its own fetches. A wait that runs out skips that
// candidate's fetch; a later one waits again, in what is left of
// transaction.FetchSlotWait for the whole parse.
func (r *openingRun) fetchReady() bool {
	if r.fetchCtx == nil {
		if r.slotWait <= 0 {
			return false
		}
		waitStart := time.Now()
		ctx, release, err := transaction.AcquireFetchSlot(r.ctx, r.slotWait)
		r.slotWait -= time.Since(waitStart)
		if err != nil {
			// Once per parse, not per candidate, and not for a request that went away.
			if !r.busyLogged && !errors.Is(err, context.Canceled) {
				r.busyLogged = true
				slog.Default().Error("on-demand price fetch failed", "error", err)
			}
			return false
		}
		r.fetchCtx, r.release = ctx, release
		r.session = &lookupSession{ctx: ctx, client: r.client, synced: map[string]bool{}}
	}
	return r.fetchCtx.Err() == nil
}

// priceOf prices row, c's opening or closing row, by the rule of
// proposeOpenings; an error row coded opening_price or closing_price when
// nothing does.
func (r *openingRun) priceOf(c candidate, row *previewRow) (decimal.Decimal, bool, error) {
	id := c.in.ID
	var p decimal.Decimal
	var found bool
	var err error
	if c.cost.IsPositive() {
		p, found, row.CostPrice = c.cost, true, true
	} else {
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	// The on-demand fetch only queues a backfill for an old BIST date,
	// so the bulletin of the last weekday on or before it is synced
	// here, once for all the BIST openings.
	if err == nil && !found && c.in.Market == instrument.MarketBIST && r.h.syncBulletin != nil && r.fetchReady() && r.h.syncBulletinOnce(r.session, r.bulletinDate) {
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	if err == nil && !found && r.h.onDemandFetch != nil && r.fetches < maxOpeningFetches && r.fetchReady() {
		r.fetches++
		transaction.EnsurePrice(r.fetchCtx, r.h.onDemandFetch, c.in, transaction.Transaction{InstrumentID: &id, Date: r.date})
		p, found, err = r.h.storedPrice(r.ctx, r.userID, id, r.date)
	}
	if err != nil {
		return p, false, err
	}
	if !found && c.price.IsPositive() {
		p, found, row.ApproximatePrice = c.price, true, true
	}
	if !found {
		row.Status, row.Code, row.Message = statusError, codeOpeningPrice, "no price for the opening position"
		if row.Closing {
			row.Code, row.Message = codeClosingPrice, "no price for the closing position"
		}
	}
	return p, found, nil
}

// tradeRow is the opening buy or, of kind sell, the closing sell of qty
// units of c, keyed open-<instrument> or close-<instrument>, priced by
// priceOf; the amount of a row still ok after checkRow is added to totals.
// The amount is quantity x price exactly, which the commit takes whatever
// its digits.
func (r *openingRun) tradeRow(c candidate, kind string, qty decimal.Decimal, totals *currencyTotals) (previewRow, error) {
	id, fee := c.in.ID, decimal.Zero
	row := previewRow{
		Key: fmt.Sprintf("open-%d", id), Status: statusOK, Type: kind, Date: r.date,
		Symbol: c.in.Symbol, Market: c.in.Market, InstrumentID: &id, Quantity: &qty,
		Fee: &fee, Currency: c.in.Currency, Opening: true,
	}
	if kind == transaction.TypeSell {
		row.Key, row.Opening, row.Closing = fmt.Sprintf("close-%d", id), false, true
	}
	p, found, err := r.priceOf(c, &row)
	if err != nil || !found {
		return row, err
	}
	amount := qty.Mul(p)
	row.Price, row.Amount = &p, &amount
	checkRow(&row, c.in)
	if row.Status == statusOK {
		totals.add(row.Currency, amount)
	}
	return row, nil
}

// rows is the closing sells and the opening buys of cands, in their order.
func (r *openingRun) rows(cands []candidate) (closings, openings []previewRow, err error) {
	// Never nil: a preview with no rows at all is still a JSON array.
	closings, openings = []previewRow{}, []previewRow{}
	for _, c := range cands {
		row, ok, err := r.closingRow(c)
		if err == nil && !ok {
			row, ok, err = r.openingRow(c)
			if ok {
				openings = append(openings, row)
			}
		} else if ok {
			closings = append(closings, row)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return closings, openings, nil
}

// closingRow is c's closing sell, and true, when the portfolio holds more
// of it than the statement, held since before the period. The
// difference and the statement's price are post-split, the sell before the
// splits, as for an opening.
func (r *openingRun) closingRow(c candidate) (previewRow, bool, error) {
	id := c.in.ID
	excess, closePrice := r.held[id].Sub(c.stated), c.price
	if ratio := r.ratio[id]; ratio.IsPositive() && !ratio.Equal(decimal.NewFromInt(1)) {
		excess, closePrice = excess.DivRound(ratio, 9), closePrice.Mul(ratio)
	}
	if excess = decimal.Min(excess, r.before[id]); !excess.IsPositive() || !r.earlier[id] || len(r.st.Holdings) == 0 {
		return previewRow{}, false, nil
	}
	c.cost, c.price = decimal.Zero, closePrice
	row, err := r.tradeRow(c, transaction.TypeSell, excess, &r.proceeds)
	return row, true, err
}

// openingRow is c's opening buy, and true, when the statement holds more of
// it than the portfolio, which has no transaction of it before the period.
func (r *openingRun) openingRow(c candidate) (previewRow, bool, error) {
	id := c.in.ID
	shortfall := c.stated.Sub(r.held[id])
	if !shortfall.IsPositive() || r.earlier[id] {
		return previewRow{}, false, nil
	}
	// The shortfall and the statement's prices are post-split, the
	// opening before the splits; 9 fraction digits is what a quantity
	// takes.
	if ratio := r.ratio[id]; ratio.IsPositive() && !ratio.Equal(decimal.NewFromInt(1)) {
		shortfall = shortfall.DivRound(ratio, 9)
		c.price, c.cost = c.price.Mul(ratio), c.cost.Mul(ratio)
	}
	row, err := r.tradeRow(c, transaction.TypeBuy, shortfall, &r.deposits)
	return row, true, err
}

// cashRows is one cash row of kind per currency of totals, in their order,
// each with its total rounded to the cent by round: the open-cash-*
// deposits that pay for the priced opening buys, or the close-cash-*
// withdrawals that take the priced closing sells' proceeds out again.
func (r *openingRun) cashRows(kind string, totals currencyTotals, round func(decimal.Decimal, int32) decimal.Decimal) ([]previewRow, error) {
	rows := make([]previewRow, 0, len(totals.order))
	for _, cur := range totals.order {
		amount, fee := round(totals.sum[cur], 2), decimal.Zero
		row := previewRow{
			Key: "open-cash-" + cur, Status: statusOK, Type: kind, Date: r.date,
			Amount: &amount, Fee: &fee, Currency: cur, Opening: true,
		}
		if kind == transaction.TypeWithdrawal {
			row.Key, row.Opening, row.Closing = "close-cash-"+cur, false, true
		}
		checkRow(&row, instrument.Instrument{})
		rows = append(rows, row)
	}
	return rows, nil
}

// storedPrice is the price of instrumentID on or before date: userID's
// manual price or the fetched one, the manual one winning on the same date,
// else the newer, as the summary merges them.
func (h *handler) storedPrice(ctx context.Context, userID, instrumentID int64, date string) (decimal.Decimal, bool, error) {
	winners, err := holdings.LatestCloses(ctx, h.priceSources, userID, []int64{instrumentID}, date)
	if err != nil {
		return decimal.Decimal{}, false, err
	}
	won, ok := winners[instrumentID]
	return won.Close, ok, nil
}

// orZero is a preview row's number, zero when the row has none.
func orZero(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}

// text is a preview row's number as the request the import page sends for
// the row carries it, "" when the row has none.
func text(d *decimal.Decimal) *string {
	s := ""
	if d != nil {
		s = d.String()
	}
	return &s
}
