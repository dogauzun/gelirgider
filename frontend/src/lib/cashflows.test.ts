import { describe, expect, it } from 'vitest'
import type { CashFlowBucket } from '../api/cashflows'
import { streaks, windowTotals } from './cashflows'

const month = (
  period: string,
  net: string,
  rest: Partial<CashFlowBucket> = {},
): CashFlowBucket => ({
  period,
  deposits: '0.00',
  withdrawals: '0.00',
  net,
  dividends: '0.00',
  interest: '0.00',
  fees: '0.00',
  ...rest,
})

const months = (lastNet: string) => [
  month('2026-05', '100.00'),
  month('2026-06', '0.00'),
  month('2026-07', '50.00'),
  month('2026-08', '50.00'),
  month('2026-09', '50.00'),
  month('2026-10', lastNet),
]

describe('streaks', () => {
  it('ends in the previous month while the current one is not positive', () => {
    expect(streaks(months('0.00'), '2026-10-15')).toEqual({ current: 3, longest: 3 })
  })

  it('counts the current month once it is positive', () => {
    expect(streaks(months('20.00'), '2026-10-15')).toEqual({ current: 4, longest: 4 })
  })

  it('is broken by a previous month at or below zero, and crosses a year', () => {
    const list = [month('2025-12', '5.00'), month('2026-01', '5.00'), month('2026-02', '-1.00')]
    expect(streaks(list, '2026-03-10')).toEqual({ current: 0, longest: 2 })
    expect(streaks(list, '2026-01-10')).toEqual({ current: 2, longest: 2 })
  })

  it('is zero with no months', () => {
    expect(streaks([], '2026-10-15')).toEqual({ current: 0, longest: 0 })
  })
})

describe('windowTotals', () => {
  it('adds every field exactly', () => {
    const list = [
      month('2026-09', '0.10', {
        deposits: '0.10',
        withdrawals: '0.01',
        dividends: '0.10',
        interest: '0.10',
        fees: '0.10',
      }),
      month('2026-10', '0.20', {
        deposits: '0.20',
        withdrawals: '0.02',
        dividends: '0.20',
        interest: '0.20',
        fees: '0.20',
      }),
    ]
    expect(windowTotals(list)).toEqual({
      deposits: '0.3',
      withdrawals: '0.03',
      net: '0.3',
      dividends: '0.3',
      interest: '0.3',
      fees: '0.3',
    })
  })
})
