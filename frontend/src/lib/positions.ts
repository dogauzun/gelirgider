// The weight, return and day-change rules the positions table, the position
// page, the summary page and the Katkı card share, so they agree.
import type { PositionHistoryPoint } from '../api/positions'
import type { Position, Summary } from '../api/summary'
import { signedPercent } from './format'

// The one rule for P/L over cost as a ratio, for a position's
// unrealized P/L, a closed position's realized P/L and the portfolio's total
// alike: the P/L over the cost taken absolute, so a negative cost (a short,
// allowed on purpose) still reads "a gain is positive". Null without
// the P/L, without the cost (a backend that does not send it) or with a zero
// cost, which leaves nothing to divide by.
export function returnRatio(pl: string | null, cost: string | null | undefined): number | null {
  if (pl == null || cost == null || Number(cost) === 0) return null
  return Number(pl) / Math.abs(Number(cost))
}

// returnRatio as the signed percent the screens show.
export function returnPercent(pl: string | null, cost: string | null | undefined): string | null {
  const ratio = returnRatio(pl, cost)
  return ratio === null ? null : signedPercent(ratio)
}

// A position's move since the previous close over its value at that
// close: what the Günlük cell shows, and so what it sorts by. Null without a
// previous close, or with no value then. `== null` because a payload without
// the fields must not render a NaN. The base is taken absolute, as in
// returnRatio. The position page's day pill shows it too.
export function dayRatio(position: Position): number | null {
  const { day_change: change, prev_price_date: from, price_date: to } = position
  if (change == null || from == null || to == null) return null
  const base = Number(position.market_value) - Number(change)
  return base === 0 ? null : Number(change) / Math.abs(base)
}

// The unrealized P/L in the reporting currency, the column's sort key and
// footer unit, with its percent over the position's own cost. Unknown until
// the first price: the API's "0.00" would read as flat. No percent
// without the amount either (a priced position with no FX rate).
export function unrealized(position: Position): { amount: string | null; percent: string | null } {
  if (!position.has_price || position.unrealized_pl_reporting === null)
    return { amount: null, percent: null }
  return {
    amount: position.unrealized_pl_reporting,
    percent: returnPercent(position.unrealized_pl, position.cost_basis),
  }
}

// A position's share of the total, as Allocation and the AI prompt count it:
// positions with a positive reporting value plus cash when positive. A
// position without one (no FX rate, no price, a short) has no share.
export function weightOf(position: Position, total: number): number | null {
  const value = Number(position.market_value_reporting)
  return position.market_value_reporting === null || value <= 0 || total <= 0 ? null : value / total
}

// The total weightOf divides by: the open positions' positive reporting
// values plus cash when positive.
export function weightTotal(summary: Summary): number {
  return (
    summary.positions.reduce((sum, p) => {
      const value = Number(p.market_value_reporting)
      return p.closed || p.market_value_reporting === null || value <= 0 ? sum : sum + value
    }, 0) + Math.max(Number(summary.cash), 0)
  )
}

// A day of a position's history it is held on. The position charts span the
// held days (ADR 0016): the days before the first held one and after the
// last are dropped, rather than drawn as a flat zero line, and a day inside
// the span with nothing held (a close, then a reopen) is a gap in the lines,
// not a zero.
export const isHeld = (p: PositionHistoryPoint) => Number(p.quantity) !== 0
