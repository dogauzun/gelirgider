import { addDecimal, decimalUnits, subDecimal } from './decimal'
import { sortRows } from './sortRows'

// The Katkı card's three figures: the open P/L, today's move, or the page
// range's total, closed positions included.
export const CONTRIBUTION_MODES = ['total', 'day', 'range'] as const
export type ContributionMode = (typeof CONTRIBUTION_MODES)[number]

// One row of the Katkı card. value is for ordering only and fraction for the
// bar's length, from 0 to 1 against the largest absolute value; amount is the
// exact decimal the row prints, null for a range row without a total.
export interface ContributionRow {
  instrumentId: number
  symbol: string
  amount: string | null
  value: number
  fraction: number
  // The day mode's remainder row: no position behind it, so no link.
  other?: boolean
}

// What contributionRows takes: one position's amount in the reporting
// currency, from Position or from the attribution response.
export type ContributionInput = Pick<
  ContributionRow,
  'instrumentId' | 'symbol' | 'amount' | 'other'
>

// A cent, in the API's 1e-9 units: the summary's day_change is rounded once,
// the rows one by one, so they may differ by that much.
const CENT = 10_000_000n

// dayRemainder is the day change the open rows do not hold: the price-date move
// of positions sold out on or after it. Null when the rows add up to the total
// within a cent, or when there is no total. Sums every open row, shown or not.
export function dayRemainder(
  dayChange: string | null,
  inputs: ContributionInput[],
  symbol: string,
): ContributionInput | null {
  if (dayChange === null) return null
  const sum = inputs.reduce(
    (acc, row) => (row.amount === null ? acc : addDecimal(acc, row.amount)),
    '0',
  )
  const amount = subDecimal(dayChange, sum)
  const units = decimalUnits(amount)
  return units > CENT || units < -CENT ? { instrumentId: 0, symbol, amount, other: true } : null
}

// contributionRows orders the inputs by amount, best first, ties by symbol.
// A row without an amount (no price, no FX rate, no day change) is left out,
// except in `range` mode, where it stays: MISSING, no bar, after the rest and
// outside the limit. With a limit, more than twice as many rows keep only the
// `limit` best and the `limit` worst: with a mostly winning portfolio the
// worst are its smallest gains. hidden counts the rows the limit left out.
// other, the day mode's remainder row, goes last, outside the limit, and its
// bar counts toward the scale.
export function contributionRows(
  inputs: ContributionInput[],
  mode: ContributionMode,
  limit: number | null,
  other: ContributionInput | null = null,
): { rows: ContributionRow[]; hidden: number } {
  const rows = inputs
    .filter((row): row is ContributionInput & { amount: string } => row.amount !== null)
    .map((row) => ({ ...row, value: Number(row.amount) }))
  // By symbol first, then best first: the sort is stable, so ties keep symbol order.
  const bySymbol = sortRows(rows, (row) => row.symbol, { key: 'symbol', dir: 'asc' })
  const sorted = sortRows(bySymbol, (row) => row.value, { key: 'value', dir: 'desc' })

  const kept =
    limit !== null && sorted.length > 2 * limit
      ? [...sorted.slice(0, limit), ...sorted.slice(-limit)]
      : sorted
  // The largest absolute value is the best or the worst row, so it is kept.
  const tail = other ? [{ ...other, value: Number(other.amount) }] : []
  const max = Math.max(0, ...[...kept, ...tail].map((row) => Math.abs(row.value)))
  const missing =
    mode === 'range'
      ? sortRows(
          inputs.filter((row) => row.amount === null),
          (row) => row.symbol,
          { key: 'symbol', dir: 'asc' },
        ).map((row) => ({ ...row, value: 0, fraction: 0 }))
      : []
  return {
    rows: [
      ...[...kept, ...tail].map((row) => ({
        ...row,
        fraction: max > 0 ? Math.abs(row.value) / max : 0,
      })),
      ...missing,
    ],
    hidden: sorted.length - kept.length,
  }
}
