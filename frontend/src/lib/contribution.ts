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
}

// What contributionRows takes: one position's amount in the reporting
// currency, from Position or from the attribution response.
export type ContributionInput = Pick<ContributionRow, 'instrumentId' | 'symbol' | 'amount'>

// contributionRows orders the inputs by amount, best first, ties by symbol.
// A row without an amount (no price, no FX rate, no day change) is left out,
// except in `range` mode, where it stays: MISSING, no bar, after the rest and
// outside the limit. With a limit, more than twice as many rows keep only the
// `limit` best and the `limit` worst: with a mostly winning portfolio the
// worst are its smallest gains. hidden counts the rows the limit left out.
export function contributionRows(
  inputs: ContributionInput[],
  mode: ContributionMode,
  limit: number | null,
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
  const max = Math.max(0, ...kept.map((row) => Math.abs(row.value)))
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
      ...kept.map((row) => ({ ...row, fraction: max > 0 ? Math.abs(row.value) / max : 0 })),
      ...missing,
    ],
    hidden: sorted.length - kept.length,
  }
}
