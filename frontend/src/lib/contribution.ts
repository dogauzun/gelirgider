import type { Position } from '../api/summary'
import { unrealized } from './positions'
import { sortRows } from './sortRows'

// The Katkı card's two figures: the open P/L, or today's move.
export const CONTRIBUTION_MODES = ['total', 'day'] as const
export type ContributionMode = (typeof CONTRIBUTION_MODES)[number]

// One row of the Katkı card. value is for ordering only and fraction for the
// bar's length, from 0 to 1 against the largest absolute value; amount is the
// exact decimal the row prints.
export interface ContributionRow {
  instrumentId: number
  symbol: string
  amount: string
  value: number
  fraction: number
}

// contributionRows orders the positions by their reporting-currency P/L
// (`total`) or day change (`day`), best first, ties by symbol. A position
// without that value (no price, no FX rate, no day change) is left out. With
// a limit, more than twice as many positions keep only the `limit` best and
// the `limit` worst: with a mostly winning portfolio the worst are its
// smallest gains. hidden counts the positions the limit left out.
export function contributionRows(
  positions: Position[],
  mode: ContributionMode,
  limit: number | null,
): { rows: ContributionRow[]; hidden: number } {
  const rows = positions
    .map((p) => ({
      instrumentId: p.instrument_id,
      symbol: p.symbol,
      amount: mode === 'total' ? unrealized(p).amount : p.day_change_reporting,
    }))
    .filter((row): row is typeof row & { amount: string } => row.amount !== null)
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
  return {
    rows: kept.map((row) => ({ ...row, fraction: max > 0 ? Math.abs(row.value) / max : 0 })),
    hidden: sorted.length - kept.length,
  }
}
