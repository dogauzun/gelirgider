import { describe, expect, it } from 'vitest'
import type { Trade } from '../api/trades'
import { tradeStats, tradeYears } from './trades'

const trade = (over: Partial<Trade>) =>
  ({ date: '2025-03-03', realized: '0', days: null, realized_reporting: null, ...over }) as Trade

describe('tradeStats', () => {
  it('counts the wins, adds the rates it has and averages the days', () => {
    const stats = tradeStats([
      trade({ realized: '78.00', realized_reporting: '78.00', days: 1 }),
      trade({ realized: '-60.00', realized_reporting: '-60.00', days: 2 }),
      trade({}),
    ])

    expect(stats).toEqual({
      count: 3,
      winRate: 1 / 3,
      // addDecimal trims trailing zeros.
      realizedReporting: '18',
      missingRates: 1,
      averageDays: 2,
    })
  })

  it('answers null for no rows and for rows with nothing to add', () => {
    expect(tradeStats([])).toEqual({
      count: 0,
      winRate: null,
      realizedReporting: null,
      missingRates: 0,
      averageDays: null,
    })
    expect(tradeStats([trade({})]).realizedReporting).toBeNull()
  })
})

describe('tradeYears', () => {
  it('lists each year once, newest first', () => {
    expect(
      tradeYears([
        trade({ date: '2025-03-03' }),
        trade({ date: '2026-01-02' }),
        trade({ date: '2025-12-31' }),
      ]),
    ).toEqual(['2026', '2025'])
  })
})
