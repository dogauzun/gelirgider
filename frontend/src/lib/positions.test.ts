import { afterEach, describe, expect, it } from 'vitest'
import type { Position, Summary } from '../api/summary'
import { setLocale } from '../i18n/strings'
import { allocationRows } from './allocation'
import { attentionItems } from './attention'
import { dayRatio, holdingsTotal, positionLabel, weightOf } from './positions'

function position(id: number, symbol: string, value: string): Position {
  return {
    instrument_id: id,
    symbol,
    name: symbol,
    market: 'US',
    asset_class: null,
    sector: 'unknown',
    currency: 'USD',
    quantity: '1',
    avg_cost: value,
    cost_basis: value,
    last_price: value,
    has_price: true,
    price_source: 'yahoo',
    price_date: '2026-10-05',
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    day_change_base: null,
    market_value: value,
    unrealized_pl: '0',
    realized_pl: '0',
    market_value_reporting: value,
    unrealized_pl_reporting: '0',
    realized_pl_reporting: '0',
    total_pl_reporting: '0',
    closed: false,
  }
}

describe('the portfolio share total', () => {
  it('gives the weight column, the Dikkat card and the allocation donut the same share', () => {
    // Above 2^53 whole units: summed as Number the total rounds down to
    // 9007199254740992, and the big position weighs exactly 1.
    const big = position(1, 'BIG', '9007199254740993')
    const small = position(2, 'SMALL', '1')
    const summary: Summary = {
      currency: 'TRY',
      cash: '0',
      cash_by_currency: {},
      cash_by_currency_reporting: {},
      total_value: '0',
      positions_value: '0',
      cost_basis: '0',
      unrealized_pl: '0',
      realized_pl: '0',
      fx_pl: null,
      missing_rates: [],
      positions: [big, small],
      stats: { first_transaction_date: null, by_currency: {}, reporting: null },
    }

    const weight = weightOf(big, holdingsTotal(summary))
    const items = attentionItems({ summary, expected: [], today: '2026-10-05' })
    const concentration = (subject: string) =>
      items.find((i) => i.kind === 'concentration' && i.subject === subject)
    const rows = (view: 'symbol' | 'currency') =>
      allocationRows({
        positions: summary.positions,
        cash: summary.cash,
        cashByCurrencyReporting: summary.cash_by_currency_reporting,
        view,
      }).rows

    const attention = concentration('position')
    expect(attention && 'share' in attention && attention.share).toBe(weight)
    expect(rows('symbol')[0].share).toBe(weight)
    // The currency view's single USD row is the whole total.
    expect(rows('currency')[0].share).toBe(1)
    const usd = concentration('currency')
    expect(usd && 'share' in usd && usd.share).toBe(1)
  })
})

describe('positionLabel', () => {
  afterEach(() => setLocale('tr'))

  it('names a deposit by its currency and maturity in the locale, and every other position by its symbol', () => {
    const deposit = {
      ...position(1, 'TD-1', '1'),
      market: 'DEPOSIT',
      currency: 'TRY',
      name: 'Vadeli mevduat TRY 2026-07-01',
    }
    setLocale('tr')
    expect(positionLabel(deposit)).toBe('Vadeli mevduat TRY 2026-07-01')
    setLocale('en')
    expect(positionLabel(deposit)).toBe('Term deposit TRY 2026-07-01')
    expect(positionLabel({ ...position(2, 'THYAO', '1'), market: 'BIST' })).toBe('THYAO')
  })
})

describe('dayRatio', () => {
  // 10 held at 100 on the previous close, 105 now. A position's market value
  // says nothing about the base once trades moved the quantity, so the ratio
  // divides by the server's day_change_base.
  const moved = (marketValue: string, change: string, base: string | null): Position => ({
    ...position(1, 'A', marketValue),
    prev_price_date: '2026-10-02',
    day_change: change,
    day_change_base: base,
  })

  it('divides the day change by its base, not by the market value less the change', () => {
    // Sold 4 on the price date at 104: 6 left worth 630, 46 earned on 10 x 100.
    expect(dayRatio(moved('630.00', '46.00', '1000.00'))).toBe(0.046)
    // Sold 4 after the price date: the 4 still moved with it, 50 on 10 x 100.
    expect(dayRatio(moved('630.00', '50.00', '1000.00'))).toBe(0.05)
    // Bought 5 after the price date: the 5 did not move, 50 on 10 x 100.
    expect(dayRatio(moved('1575.00', '50.00', '1000.00'))).toBe(0.05)
  })

  it('takes the base absolute, and has no ratio without or on a zero base', () => {
    expect(dayRatio(moved('-95.00', '5.00', '-100.00'))).toBe(0.05)
    expect(dayRatio(moved('1000.00', '0.00', '0.00'))).toBeNull()
    expect(dayRatio(moved('1000.00', '50.00', null))).toBeNull()
  })
})

describe('dayRatio', () => {
  // 10 held at 100 on the previous close, 105 now. A position's market value
  // says nothing about the base once trades moved the quantity, so the ratio
  // divides by the server's day_change_base.
  const moved = (marketValue: string, change: string, base: string | null): Position => ({
    ...position(1, 'A', marketValue),
    prev_price_date: '2026-10-02',
    day_change: change,
    day_change_base: base,
  })

  it('divides the day change by its base, not by the market value less the change', () => {
    // Sold 4 on the price date at 104: 6 left worth 630, 46 earned on 10 x 100.
    expect(dayRatio(moved('630.00', '46.00', '1000.00'))).toBe(0.046)
    // Sold 4 after the price date: the 4 still moved with it, 50 on 10 x 100.
    expect(dayRatio(moved('630.00', '50.00', '1000.00'))).toBe(0.05)
    // Bought 5 after the price date: the 5 did not move, 50 on 10 x 100.
    expect(dayRatio(moved('1575.00', '50.00', '1000.00'))).toBe(0.05)
  })

  it('takes the base absolute, and has no ratio without or on a zero base', () => {
    expect(dayRatio(moved('-95.00', '5.00', '-100.00'))).toBe(0.05)
    expect(dayRatio(moved('1000.00', '0.00', '0.00'))).toBeNull()
    expect(dayRatio(moved('1000.00', '50.00', null))).toBeNull()
  })
})
