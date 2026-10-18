import { describe, expect, it } from 'vitest'
import type { AllocationTarget } from '../api/allocationTargets'
import type { ExpectedDividend } from '../api/dividends'
import type { Position, Summary } from '../api/summary'
import { attentionItems, type AttentionRules } from './attention'

const TODAY = '2026-10-05'

function position(id: number, symbol: string, value: string, extra: Partial<Position> = {}) {
  const p: Position = {
    instrument_id: id,
    symbol,
    name: symbol,
    market: 'BIST',
    asset_class: null,
    sector: 'unknown',
    currency: 'TRY',
    quantity: '1',
    avg_cost: value,
    cost_basis: value,
    last_price: value,
    has_price: true,
    price_source: 'yahoo',
    price_date: TODAY,
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
  return { ...p, ...extra }
}

function summary(positions: Position[], cash = '0', extra: Partial<Summary> = {}): Summary {
  return {
    currency: 'TRY',
    cash,
    cash_by_currency: { TRY: cash },
    cash_by_currency_reporting: { TRY: cash },
    total_value: '0',
    positions_value: '0',
    cost_basis: '0',
    unrealized_pl: '0',
    realized_pl: '0',
    fx_pl: null,
    missing_rates: [],
    positions,
    stats: { first_transaction_date: null, by_currency: {}, reporting: null },
    ...extra,
  }
}

const dividend: ExpectedDividend = {
  instrument_id: 9,
  symbol: 'TUPRS',
  market: 'BIST',
  date: '2026-10-12',
  quantity: '100',
  per_share: '48',
  gross: '4800.00',
  stopaj: '720.00',
  net: '4080.00',
  currency: 'TRY',
}

// Spread over enough small positions that no concentration rule fires.
const spread = (n: number, from = 100) =>
  Array.from({ length: n }, (_, i) => position(from + i, `P${i}`, '100'))

// The two TRY rules are off here: most fixtures are all-TRY BIST, and their
// tests are about the other rules. The TRY rules have their own describe.
const tryRulesOff: AttentionRules = {
  try_cash_share: { enabled: false },
  no_hedge: { enabled: false },
}
const run = (s: Summary, rest: Partial<Parameters<typeof attentionItems>[0]> = {}) =>
  attentionItems({
    summary: s,
    expected: [],
    today: TODAY,
    ...rest,
    rules: { ...tryRulesOff, ...rest.rules },
  })

describe('attentionItems', () => {
  it('returns nothing for an empty portfolio', () => {
    expect(run(summary([]))).toEqual([])
  })

  it('names a currency left out for lack of a rate with its position count', () => {
    const usd = (id: number) =>
      position(id, `U${id}`, '10', { currency: 'USD', market: 'US', market_value_reporting: null })
    const items = run(summary([...spread(8), usd(1), usd(2)], '0', { missing_rates: ['USD'] }))
    expect(items).toEqual([{ kind: 'missing-rate', currency: 'USD', positions: 2 }])
  })

  it('lists a position without a price under Veri and never as a mover or a share', () => {
    const unpriced = position(1, 'XYZ', '0', {
      has_price: false,
      last_price: null,
      price_source: null,
      price_date: null,
      market_value: '0',
      market_value_reporting: '0',
      // Even a stray value must not count.
      day_change_reporting: '999',
    })
    const items = run(summary([unpriced, ...spread(8)]))
    expect(items).toEqual([{ kind: 'missing-price', instrument_id: 1, symbol: 'XYZ' }])
  })

  it('counts a position without a price at its cost in the total, like Allocation', () => {
    // Real summaries value an unpriced position at cost. Total 10000: EREGL
    // 20% and cash 35% are under their thresholds; GARAN is never a share.
    const garan = position(2, 'GARAN', '4500', {
      has_price: false,
      last_price: null,
      price_source: null,
      price_date: null,
    })
    const items = run(summary([position(1, 'EREGL', '2000'), garan], '3500'))
    expect(items).toEqual([{ kind: 'missing-price', instrument_id: 2, symbol: 'GARAN' }])
  })

  it('counts a position without a price in its currency share', () => {
    // USD 4000 against TRY 3000 priced + 2000 unpriced at cost: USD is
    // 4000 / 9000, under 50%.
    const usd = (id: number) => position(id, `U${id}`, '1000', { currency: 'USD', market: 'US' })
    const unpriced = position(1, 'XYZ', '2000', {
      has_price: false,
      last_price: null,
      price_source: null,
      price_date: null,
    })
    const try_ = (id: number) => position(id, `T${id}`, '1000')
    const items = run(
      summary([usd(10), usd(11), usd(12), usd(13), try_(20), try_(21), try_(22), unpriced]),
    )
    expect(items).toEqual([{ kind: 'missing-price', instrument_id: 1, symbol: 'XYZ' }])
  })

  it('counts manual prices older than 7 days as one item; exactly 7 days is not stale', () => {
    const manual = (id: number, date: string) =>
      position(id, `M${id}`, '100', { price_source: 'manual', price_date: date })
    const items = run(
      summary([
        manual(1, '2026-09-20'),
        manual(2, '2026-09-10'),
        manual(3, '2026-09-27'),
        manual(4, '2026-09-28'),
        ...spread(8),
      ]),
    )
    expect(items).toEqual([
      { kind: 'stale-manual-prices', count: 3, days: 7, instrument_id: 2, symbol: 'M2' },
    ])
  })

  it('takes the stale day limit and the on/off flags from the rules', () => {
    const manual = position(1, 'M1', '100', { price_source: 'manual', price_date: '2026-10-01' })
    const stale = (rules: AttentionRules) =>
      run(summary([manual, ...spread(8)]), { rules }).filter(
        (i) => i.kind === 'stale-manual-prices',
      )
    // 4 days old: stale only once the limit is below that.
    expect(stale({})).toEqual([])
    expect(stale({ stale_manual_days: { value: 3 } })).toEqual([
      { kind: 'stale-manual-prices', count: 1, days: 3, instrument_id: 1, symbol: 'M1' },
    ])
    expect(stale({ stale_manual_days: { enabled: false, value: 3 } })).toEqual([])
  })

  it('emits no concentration item for a disabled position share rule', () => {
    // A is 30% of 100; the other seven are 10% each.
    const rest = Array.from({ length: 7 }, (_, i) => position(2 + i, `S${i}`, '10'))
    const big = summary([position(1, 'A', '30'), ...rest])
    const rules: AttentionRules = { position_share: { enabled: false } }
    expect(run(big).some((i) => i.kind === 'concentration')).toBe(true)
    expect(run(big, { rules })).toEqual([])
  })

  it('says from when the benchmark values are estimates', () => {
    expect(run(summary(spread(8)), { estimatedFrom: '2026-09-01' })).toEqual([
      { kind: 'estimated-benchmark', from: '2026-09-01' },
    ])
  })

  it('lists each expected dividend', () => {
    const second = { ...dividend, instrument_id: 10, symbol: 'THYAO' }
    expect(run(summary(spread(8)), { expected: [dividend, second] })).toEqual([
      { kind: 'dividend', dividend },
      { kind: 'dividend', dividend: second },
    ])
  })

  it('picks the two largest absolute day changes as movers', () => {
    const moved = (id: number, change: string | null) =>
      position(id, `D${id}`, '100', {
        last_price: '100',
        prev_close: '50',
        prev_price_date: '2026-10-02',
        day_change: '50',
        day_change_reporting: change,
      })
    const items = run(
      summary([moved(1, '5'), moved(2, '-30'), moved(3, '20'), moved(4, null), ...spread(6)]),
    )
    expect(items).toEqual([
      { kind: 'mover', instrument_id: 2, symbol: 'D2', amount: '-30', currency: 'TRY', ratio: 1 },
      { kind: 'mover', instrument_id: 3, symbol: 'D3', amount: '20', currency: 'TRY', ratio: 1 },
    ])
  })

  it("a short mover's ratio has the sign of its day change", () => {
    // Short one share: the price fell from 100 to 95, a gain of 5,
    // which the Günlük cell shows as +5%.
    const short = position(1, 'S', '-95', {
      quantity: '-1',
      last_price: '95',
      prev_close: '100',
      prev_price_date: '2026-10-02',
      day_change: '5',
      day_change_reporting: '5',
    })
    expect(run(summary([short, ...spread(6)]))).toEqual([
      { kind: 'mover', instrument_id: 1, symbol: 'S', amount: '5', currency: 'TRY', ratio: 0.05 },
    ])
  })

  it('has no movers when every day change is null or zero', () => {
    const items = run(
      summary([
        position(1, 'A', '100', { day_change_reporting: '0.00' }),
        position(2, 'B', '100', { day_change_reporting: null }),
        ...spread(6),
      ]),
    )
    expect(items).toEqual([])
  })

  it('flags a position above 25% of the total, not one at exactly 25%', () => {
    // B and C are exactly a quarter of 100; A is 25.01%.
    const items = run(
      summary([
        position(1, 'A', '25.01'),
        position(2, 'B', '25'),
        position(3, 'C', '25'),
        position(4, 'D', '24.99'),
      ]),
    )
    expect(items).toEqual([
      {
        kind: 'concentration',
        subject: 'position',
        label: 'A',
        instrument_id: 1,
        share: expect.closeTo(0.2501, 6),
      },
    ])
  })

  it('flags cash above 40%, not at exactly 40%', () => {
    expect(run(summary(spread(6), '400'))).toEqual([])
    expect(run(summary(spread(6), '400.01'))[0]).toMatchObject({ subject: 'cash' })
  })

  it('flags one foreign currency above 50%, counting its cash, not at exactly 50%', () => {
    const usd = (id: number, value: string) =>
      position(id, `U${id}`, value, { currency: 'USD', market: 'US' })
    const withUsd = (cashUsd: string) =>
      summary([usd(1, '200'), usd(2, '200'), usd(3, '200'), ...spread(4)], cashUsd, {
        cash_by_currency: { USD: '1' },
        cash_by_currency_reporting: { USD: cashUsd },
      })
    // 600 of 1000.
    expect(run(withUsd('0'))).toEqual([
      { kind: 'concentration', subject: 'currency', label: 'USD', share: 0.6 },
    ])
    const exact = summary([usd(1, '200'), usd(2, '200'), usd(3, '100'), ...spread(5)])
    expect(run(exact)).toEqual([])
  })

  it('sums the positive currency rows for a currency share, as Allocation does', () => {
    // TRY nets to 100 - 200 = -100 and drops out, so USD's 300 is the whole
    // currency view: 100%, never 300 over the net total of 200 (150%).
    const items = run(
      summary([position(1, 'A', '100')], '100', {
        cash_by_currency: { TRY: '-200', USD: '10' },
        cash_by_currency_reporting: { TRY: '-200', USD: '300' },
      }),
    )
    expect(items).toContainEqual({
      kind: 'concentration',
      subject: 'currency',
      label: 'USD',
      share: 1,
    })
  })

  it('gives a cash-only portfolio only its cash share', () => {
    expect(run(summary([], '1000'))).toEqual([
      { kind: 'concentration', subject: 'cash', label: 'TRY', share: 1 },
    ])
  })

  it('ignores closed positions', () => {
    const closed = position(1, 'C', '0', {
      closed: true,
      quantity: '0',
      has_price: false,
      price_source: 'manual',
      price_date: '2020-01-01',
      day_change_reporting: '10',
    })
    expect(run(summary([closed]))).toEqual([])
  })

  it('leaves a position without a reporting value out of the shares', () => {
    // Without B's 900, A's 100 is the whole of a total of 400 with cash.
    const items = run(
      summary(
        [
          position(1, 'A', '100'),
          position(2, 'B', '900', { currency: 'USD', market_value_reporting: null }),
        ],
        '300',
        { missing_rates: ['USD'] },
      ),
    )
    expect(items).toEqual([
      { kind: 'missing-rate', currency: 'USD', positions: 1 },
      { kind: 'concentration', subject: 'cash', label: 'TRY', share: 0.75 },
    ])
  })

  it('orders the groups and names the portfolio when given', () => {
    const items = attentionItems({
      summary: summary(
        [
          position(1, 'A', '100', {
            day_change_reporting: '3',
            prev_close: '97',
            last_price: '100',
          }),
        ],
        '0',
      ),
      expected: [dividend],
      estimatedFrom: '2026-09-01',
      today: TODAY,
      portfolioName: 'Ana',
    })
    expect(items.map((item) => [item.kind, item.portfolio])).toEqual([
      ['estimated-benchmark', 'Ana'],
      ['dividend', 'Ana'],
      ['mover', 'Ana'],
      ['concentration', 'Ana'],
      ['no-hedge', 'Ana'],
    ])
  })
})

describe('TRY cash share and no-hedge rules', () => {
  const all = (s: Summary, rules: AttentionRules = {}) =>
    attentionItems({ summary: s, expected: [], today: TODAY, rules })
  const kinds = (items: ReturnType<typeof all>) =>
    items.map((item) => (item.kind === 'concentration' ? item.subject : item.kind))

  it('flags an all-TRY portfolio without a hedge; a GRAM position clears it', () => {
    const tryOnly = summary(spread(8), '100')
    expect(kinds(all(tryOnly))).toEqual(['no-hedge'])
    const gram = position(1, 'GRAM', '100', { market: 'GOLD' })
    expect(kinds(all(summary([gram, ...spread(8)], '100')))).toEqual([])
    expect(kinds(all(tryOnly, { no_hedge: { enabled: false } }))).toEqual([])
  })

  it('counts a USD position or USD cash as a hedge, and ignores an empty portfolio', () => {
    const usd = position(1, 'U', '100', { currency: 'USD', market: 'BIST' })
    expect(kinds(all(summary([usd, ...spread(8)])))).toEqual([])
    const usdCash = summary(spread(8), '0', { cash_by_currency: { USD: '10' } })
    expect(kinds(all(usdCash))).toEqual([])
    expect(all(summary([]))).toEqual([])
  })

  it('gives 50% TRY cash the TRY item and no generic cash item, until the rule is off', () => {
    const gram = position(1, 'GRAM', '100', { market: 'GOLD' })
    const half = summary([gram, ...spread(4)], '500')
    expect(all(half)).toEqual([{ kind: 'try-cash-share', share: 0.5 }])
    expect(all(half, { try_cash_share: { enabled: false } })).toEqual([
      { kind: 'concentration', subject: 'cash', label: 'TRY', share: 0.5 },
    ])
    // Exactly at the threshold is not above it.
    expect(all(summary([gram, ...spread(5)], '400'))).toEqual([])
  })

  it('does not count a USD cash balance as TRY cash', () => {
    const gram = position(1, 'GRAM', '100', { market: 'GOLD' })
    const s = summary([gram, ...spread(4)], '500', { cash_by_currency_reporting: { USD: '500' } })
    expect(all(s)).toEqual([{ kind: 'concentration', subject: 'cash', label: 'TRY', share: 0.5 }])
  })

  it('applies neither TRY rule to a USD reporting currency, and the generic cash rule works', () => {
    const s = summary(spread(2), '500', {
      currency: 'USD',
      cash_by_currency: { USD: '500' },
      cash_by_currency_reporting: { USD: '500' },
    })
    expect(all(s)).toEqual([
      { kind: 'concentration', subject: 'cash', label: 'USD', share: 500 / 700 },
    ])
  })
})

describe('allocation drift rule', () => {
  // 50% equity, 50% cash.
  const half = summary([position(1, 'A', '500')], '500')
  const drift = (targets: AllocationTarget[], rules: AttentionRules = {}) =>
    attentionItems({ summary: half, expected: [], today: TODAY, rules, targets }).filter(
      (item) => item.kind === 'allocation-drift',
    )
  const split = (equity: string, cash: string) => [
    { asset_class: 'equity', weight: equity },
    { asset_class: 'cash', weight: cash },
  ]

  it('flags each class drifting above the threshold, signed, and not one at it or below', () => {
    expect(drift(split('44', '56'))).toEqual([
      { kind: 'allocation-drift', assetClass: 'Hisse senedi', drift: expect.closeTo(6) },
      { kind: 'allocation-drift', assetClass: 'Nakit', drift: expect.closeTo(-6) },
    ])
    expect(drift(split('45', '55'))).toEqual([])
    expect(drift(split('46', '54'))).toEqual([])
  })

  it('uses the rule value, and nothing when the rule is off or there are no targets', () => {
    expect(drift(split('46', '54'), { allocation_drift: { value: 3 } })).toHaveLength(2)
    expect(drift(split('44', '56'), { allocation_drift: { enabled: false } })).toEqual([])
    expect(drift([])).toEqual([])
  })
})
