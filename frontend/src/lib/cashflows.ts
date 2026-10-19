import type { CashFlowBucket } from '../api/cashflows'
import { addDecimal } from './decimal'

const monthIndex = (period: string) => Number(period.slice(0, 4)) * 12 + Number(period.slice(5, 7))

// The run of months with a positive net that ends in the month of `today`
// ("YYYY-MM-DD"), or in the month before it when this one is not positive
// yet (it is still open), and the longest such run anywhere in the list.
export function streaks(
  months: CashFlowBucket[],
  today: string,
): { current: number; longest: number } {
  const positive = months.filter((m) => Number(m.net) > 0).map((m) => monthIndex(m.period))
  const set = new Set(positive)
  let longest = 0
  let run = 0
  positive.forEach((month, i) => {
    run = i > 0 && positive[i - 1] === month - 1 ? run + 1 : 1
    longest = Math.max(longest, run)
  })
  let month = monthIndex(today.slice(0, 7))
  if (!set.has(month)) month--
  let current = 0
  while (set.has(month - current)) current++
  return { current, longest }
}

// The months' amounts added exactly, field by field.
export function windowTotals(months: CashFlowBucket[]) {
  const sum = (field: keyof Omit<CashFlowBucket, 'period'>) =>
    months.reduce((total, m) => addDecimal(total, m[field]), '0')
  return {
    deposits: sum('deposits'),
    withdrawals: sum('withdrawals'),
    net: sum('net'),
    dividends: sum('dividends'),
    interest: sum('interest'),
    fees: sum('fees'),
  }
}
