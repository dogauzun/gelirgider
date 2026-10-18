import { describe, expect, it } from 'vitest'
import type { Position, Summary } from '../api/summary'
import { allocationRows } from './allocation'
import { attentionItems } from './attention'
import { holdingsTotal, weightOf } from './positions'

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
