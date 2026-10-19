package statement

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/bist"
	"github.com/dogauzun/gelirgider/backend/internal/instrument"
	"github.com/dogauzun/gelirgider/backend/internal/transaction"
)

// marketForCurrency derives an instrument's market from the currency its
// statement row is priced in, for a row with no market hint. A row priced
// in TRY that is not a BIST share carries the hint instead: Garanti marks
// its fund rows and statement "TEFAS", a CSV row takes its market column
// (ADR 0006).
var marketForCurrency = map[string]string{"USD": instrument.MarketUS, "TRY": instrument.MarketBIST}

// otherMarkets are the markets a row with no market hint is tried in once its
// currency's market (marketForCurrency) does not know the symbol. A CSV row
// can hold a fund, a coin or gold as well as a share.
var otherMarkets = map[string][]string{"TRY": {instrument.MarketTEFAS, instrument.MarketCrypto, instrument.MarketGold}, "USD": {instrument.MarketCrypto}}

// lookupSession is one request's state for the instrument lookups a preview
// makes: createFund's TEFAS lookups and bulletinInstrument's BIST bulletin
// syncs.
type lookupSession struct {
	// ctx is the preview's single lookup deadline: the lookups
	// and syncs run on it, and none starts once it is done.
	ctx    context.Context
	client string // the requester's IP, which the lookups count against
	userID int64
	broker string
	picks  map[string]instrument.Instrument // the picks made in this preview
	// asked holds the fund codes this request has asked TEFAS about.
	asked map[string]bool
	// synced holds the dates this request has synced the BIST bulletin of,
	// true for those it fetched.
	synced map[string]bool
	// memo holds this request's instrument lookups by key, not-found ones
	// too, so a symbol on thousands of rows is looked up once.
	// It is cleared whenever the request may have added instruments.
	memo map[string]memoEntry
	// funds is the TEFAS fund list nameInstrument matches names against,
	// read on first use and dropped with memo when createFund adds a fund.
	funds []namedFund
}

// memoEntry is one lookup memo holds: an instrument, or
// instrument.ErrNotFound.
type memoEntry struct {
	in  instrument.Instrument
	err error
}

// cached returns the lookup memo holds under key, else runs look and
// remembers what it found or did not; any other error is not remembered.
func (s *lookupSession) cached(key string, look func() (instrument.Instrument, error)) (instrument.Instrument, error) {
	if e, ok := s.memo[key]; ok {
		return e.in, e.err
	}
	in, err := look()
	if err == nil || errors.Is(err, instrument.ErrNotFound) {
		if s.memo == nil {
			s.memo = map[string]memoEntry{}
		}
		s.memo[key] = memoEntry{in, err}
	}
	return in, err
}

// bySymbol is instruments.BySymbol through s's memo.
func (h *handler) bySymbol(ctx context.Context, s *lookupSession, market, symbol string) (instrument.Instrument, error) {
	return s.cached("symbol\x00"+market+"\x00"+symbol, func() (instrument.Instrument, error) {
		return h.instruments.BySymbol(ctx, market, symbol)
	})
}

// requester is who a preview is for: the client IP its TEFAS lookups and
// bulletin syncs count against, and the user and broker that key the
// stored picks.
type requester struct {
	client string
	userID int64
	broker string
}

// resolveInstrument sets the status of a row that names a symbol: ok with the
// instrument id when the symbol is found, unknown_instrument when it is not.
// A row with a market hint is looked up in that market only. One without is
// looked up in the market its currency implies, then in that currency's other
// markets, where it resolves only when exactly one of them knows the symbol.
// A row with no currency (an empty CSV currency cell) is looked up like a TRY
// row and takes the currency of the instrument it resolves to, TRY when none.
// A currency that contradicts the instrument is left for checkRow,
// which says so with the commit's own message.
// The import creates no instrument on the statement's word, so an unknown or
// ambiguous symbol is the user's to fix in the preview (ADR 0006). The one
// exception is a TEFAS row's fund code that TEFAS itself confirms, which
// createFund adds. A BIST symbol, which may be a share delisted
// since, is looked up again after its date's bulletin is synced.
// Both ask on s, the request's lookup state. A symbol none of that
// resolves takes the instrument the user picked for it in an earlier commit
// of the same broker, if any, and says so in ResolvedBy: a real
// match always wins over a stored pick. A pick made in this preview resolves
// it the same way. With no pick either, it takes the instrument of
// the learned fund names it matches (see learnedName), else a garanti fund
// name the one TEFAS fund it is cut or abbreviated from (see nameInstrument),
// and says so in ResolvedBy. It returns the instrument
// of an ok row, else the zero Instrument. Every lookup goes through s's memo.
func (h *handler) resolveInstrument(ctx context.Context, s *lookupSession, learned *nameIndex, row *previewRow) (instrument.Instrument, error) {
	given := row.Currency
	if given == "" {
		row.Currency, row.CurrencyFallback = "TRY", true
	}
	market, ok := marketForCurrency[row.Currency]
	if !ok {
		row.Status = statusUnsupportedCurrency
		return instrument.Instrument{}, nil
	}
	if row.Market != "" {
		market = row.Market
	}
	in, err := h.bySymbol(ctx, s, market, row.Symbol)
	if errors.Is(err, instrument.ErrNotFound) && row.Market == "" {
		in, err = h.soleMatch(ctx, s, otherMarkets[row.Currency], row.Symbol, row.Currency)
	}
	if errors.Is(err, instrument.ErrNotFound) && row.Market == instrument.MarketTEFAS {
		in, err = h.createFund(ctx, s, row.Symbol)
	}
	if errors.Is(err, instrument.ErrNotFound) && market == instrument.MarketBIST {
		in, err = h.bulletinInstrument(ctx, s, row.Symbol, row.Date)
	}
	if errors.Is(err, instrument.ErrNotFound) {
		in, err = s.cached("alias\x00"+row.Symbol, func() (instrument.Instrument, error) {
			return h.aliasInstrument(ctx, s.userID, s.broker, s.picks, row.Symbol)
		})
		if err == nil {
			row.ResolvedBy = resolvedByAlias
			row.InstrumentSymbol, row.InstrumentName = in.Symbol, in.Name
		}
	}
	if errors.Is(err, instrument.ErrNotFound) {
		// learned is the same for every row of the symbol (see previewRows).
		in, err = s.cached("name\x00"+row.Market+"\x00"+row.Symbol, func() (instrument.Instrument, error) {
			return h.nameInstrument(ctx, s, learned, row)
		})
		if err == nil {
			row.ResolvedBy = resolvedByName
			row.InstrumentSymbol, row.InstrumentName = in.Symbol, in.Name
		}
	}
	if errors.Is(err, instrument.ErrNotFound) {
		row.Status = statusUnknownInstrument
		return instrument.Instrument{}, nil
	}
	if err != nil {
		return instrument.Instrument{}, err
	}
	if given == "" {
		row.Currency = in.Currency
	}
	row.Status, row.InstrumentID = statusOK, &in.ID
	return in, nil
}

// aliasInstrument returns the instrument userID picked for symbol: in this
// preview (picks), which wins, else in a statement of broker, or
// instrument.ErrNotFound when they picked none.
func (h *handler) aliasInstrument(ctx context.Context, userID int64, broker string, picks map[string]instrument.Instrument, symbol string) (instrument.Instrument, error) {
	if in, ok := picks[symbol]; ok {
		return in, nil
	}
	id, err := h.imports.Alias(ctx, userID, broker, symbol)
	if errors.Is(err, ErrNotFound) {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if err != nil {
		return instrument.Instrument{}, err
	}
	return h.instruments.Get(ctx, id)
}

// checkRow runs an ok row through the commit's own validation
// (transaction.FromRequestWith), with the request the import page sends for
// it and in, the instrument the row resolved to, in place of a lookup,
// and turns a row the commit would refuse -- a zero quantity, a sell at
// price 0, a fee above a dividend -- into an error row with code "invalid"
// and the commit's "<field>: <message>" as its message. One such row would
// otherwise fail the whole commit. An unknown_instrument row gets
// the same rules short of the instrument lookup, so a price-0 sell is not
// passed off as ready once the user picks its instrument. Such a
// row keeps UnknownInstrument, so the user learns of both problems at once.
// The row's numbers go in as the strings the page sends: that
// validation is the commit's trust boundary, which the preview runs as is
// rather than repeating it on decimals.
func checkRow(row *previewRow, in instrument.Instrument) {
	if row.Status != statusOK && row.Status != statusUnknownInstrument {
		return
	}
	req := transaction.Request{
		InstrumentID: row.InstrumentID, Type: row.Type, Date: row.Date,
		Amount: text(row.Amount), Fee: text(row.Fee), Currency: &row.Currency,
	}
	if row.Quantity != nil {
		req.Quantity, req.Price = text(row.Quantity), text(row.Price)
	}
	var err error
	if row.Status == statusUnknownInstrument {
		var placeholder int64
		req.InstrumentID = &placeholder
		err = transaction.Validate(req)
	} else {
		_, _, err = transaction.FromRequestWith(req, func(id int64) (instrument.Instrument, error) {
			if id != in.ID {
				return instrument.Instrument{}, instrument.ErrNotFound
			}
			return in, nil
		})
	}
	if err != nil {
		row.UnknownInstrument = row.Status == statusUnknownInstrument
		row.Status, row.Code, row.Message = statusError, "invalid", err.Error()
	}
}

// createFund adds code as a TRY fund on TEFAS when lookupFund confirms it,
// named as TEFAS names it, and returns the instrument. Instruments are
// global, so only a fund-code-shaped symbol is asked about, never a name the
// statement printed. Each code is asked once per request (s.asked), at most
// maxFundLookups codes in all, and each lookup counts against s.client's
// fundLookupLimit, shared by every route that asks, unless it
// asked nothing (ErrFundLookupOff). A code not asked, not found
// or whose lookup failed is instrument.ErrNotFound; a
// failure is logged with the code only. A code not asked because the
// client's limit is spent is a fundLimitError, which is instrument.ErrNotFound
// as well. The lookup runs on s.ctx, and none is made once it is done;
// the store runs on ctx. Insert returns the existing row when
// another request added the code first.
func (h *handler) createFund(ctx context.Context, s *lookupSession, code string) (instrument.Instrument, error) {
	if h.lookupFund == nil || !fundCode.MatchString(code) || s.asked[code] || len(s.asked) >= maxFundLookups || s.ctx.Err() != nil {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if ok, retryAfter := h.fundLookups.Allow(s.client); !ok {
		return instrument.Instrument{}, fundLimitError(retryAfter)
	}
	s.asked[code] = true
	name, found, err := h.lookupFund(s.ctx, code)
	if errors.Is(err, ErrFundLookupOff) {
		h.fundLookups.Refund(s.client)
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if err != nil {
		slog.Default().Warn("tefas fund lookup failed", "code", code, "error", err.Error())
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if !found {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if name == "" {
		// The TEFAS fetcher renames it on its next run.
		name = code
	}
	id, err := h.instruments.Insert(ctx, instrument.MarketTEFAS, code, name, "TRY")
	if err != nil {
		return instrument.Instrument{}, err
	}
	clear(s.memo)
	s.funds = nil
	slog.Default().Info("tefas fund created from import", "code", code, "name", name)
	return h.instruments.Get(ctx, id)
}

// fundLimitError is createFund's error for a code not asked because the
// client's fundLookupLimit is spent; its value is the time until the limit
// resets. It unwraps to instrument.ErrNotFound, so the preview leaves the
// row unknown_instrument, and the fund route answers 429.
type fundLimitError time.Duration

func (fundLimitError) Error() string { return "fund lookup limit reached" }

func (fundLimitError) Unwrap() error { return instrument.ErrNotFound }

// bulletinInstrument syncs the BIST bulletin of date, which adds the symbols
// it lists as instruments, and looks symbol up again. A date
// syncBulletinOnce does not sync is instrument.ErrNotFound.
func (h *handler) bulletinInstrument(ctx context.Context, s *lookupSession, symbol, date string) (instrument.Instrument, error) {
	if !h.syncBulletinOnce(s, date) {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	return h.bySymbol(ctx, s, instrument.MarketBIST, symbol)
}

// syncBulletinOnce syncs the BIST bulletin of date and reports whether it
// ran without error. Each date is synced once per request (s.synced). Only
// a sync that fetched counts against maxBulletinSyncs, so dates whose
// bulletin is already stored do not use up the fetches a later date needs;
// a pause as a backfill makes goes before each one after the first. Each
// fetch also counts against s.client's bulletinSyncLimit; a sync
// that fetched nothing is refunded. The sync runs on s.ctx, and none starts
// once it is done. A failure is logged with the date only.
func (h *handler) syncBulletinOnce(s *lookupSession, date string) bool {
	d, err := time.Parse(time.DateOnly, date)
	if _, done := s.synced[date]; h.syncBulletin == nil || err != nil || done || s.ctx.Err() != nil {
		return false
	}
	fetches := 0
	for _, fetched := range s.synced {
		if fetched {
			fetches++
		}
	}
	if fetches >= maxBulletinSyncs {
		return false
	}
	if ok, _ := h.bulletinSyncs.Allow(s.client); !ok {
		return false
	}
	if fetches > 0 {
		time.Sleep(bist.RequestPause)
	}
	syncCtx, cancel := context.WithTimeout(s.ctx, bulletinSyncTimeout)
	s.synced[date], err = h.syncBulletin(syncCtx, d)
	cancel()
	// The sync may have added instruments the memo holds as not found.
	clear(s.memo)
	if !s.synced[date] {
		h.bulletinSyncs.Refund(s.client)
	}
	if err != nil {
		slog.Default().Warn("bist bulletin sync for import failed", "date", date, "error", err.Error())
		return false
	}
	return true
}

// soleMatch looks symbol up in each of markets and returns the instrument
// when exactly one market has it quoted in currency, else
// instrument.ErrNotFound. The lookups go through s's memo.
func (h *handler) soleMatch(ctx context.Context, s *lookupSession, markets []string, symbol, currency string) (instrument.Instrument, error) {
	var found []instrument.Instrument
	for _, market := range markets {
		in, err := h.bySymbol(ctx, s, market, symbol)
		if errors.Is(err, instrument.ErrNotFound) {
			continue
		}
		if err != nil {
			return instrument.Instrument{}, err
		}
		if in.Currency == currency {
			found = append(found, in)
		}
	}
	if len(found) != 1 {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	return found[0], nil
}
