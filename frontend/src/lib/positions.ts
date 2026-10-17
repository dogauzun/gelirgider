// The weight, return and day-change rules the positions table, the position
// page, the summary page and the Katkı card share, so they agree.
import type { PositionHistoryPoint } from '../api/positions'
import type { Position, Summary } from '../api/summary'
import { decimalUnits } from './decimal'
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

// One position's reporting value when it counts toward a share: open and
// positive, as 1e-9 units. Null for a closed one, one without a reporting
// value (no FX rate, no price) or a short.
export function shareUnits(p: Position): bigint | null {
  if (p.closed || p.market_value_reporting === null) return null
  const units = decimalUnits(p.market_value_reporting)
  return units > 0n ? units : null
}

// The total every share of the portfolio divides by, exact, in 1e-9 units: the
// open positions' positive reporting values plus cash when positive. What
// Allocation and the AI prompt count too.
export function holdingsTotal({ positions, cash }: Pick<Summary, 'positions' | 'cash'>): bigint {
  const cashUnits = decimalUnits(cash)
  return positions.reduce((sum, p) => sum + (shareUnits(p) ?? 0n), cashUnits > 0n ? cashUnits : 0n)
}

// The currency view's totals, exact, in 1e-9 units: each currency's open
// positions plus its own cash (already in the reporting currency), kept only
// when positive. Their sum is the view's own total, which differs from
// holdingsTotal when cash is negative in one currency.
export function currencyTotals(
  positions: Position[],
  cashByCurrencyReporting: Record<string, string>,
): Map<string, bigint> {
  const totals = new Map<string, bigint>()
  const add = (currency: string, units: bigint) =>
    // eslint-disable-next-line no-restricted-syntax -- sums bigint units, not strings
    totals.set(currency, (totals.get(currency) ?? 0n) + units)
  for (const p of positions) add(p.currency, shareUnits(p) ?? 0n)
  for (const [currency, amount] of Object.entries(cashByCurrencyReporting)) {
    add(currency, decimalUnits(amount))
  }
  for (const [currency, units] of totals) if (units <= 0n) totals.delete(currency)
  return totals
}

// part / whole as the number the screens show. Every share goes through it,
// so the weight column, the Dikkat card and the donut cannot disagree.
export function shareOf(part: bigint, whole: bigint): number {
  return Number(part) / Number(whole)
}

// A position's share of holdingsTotal. A position without one (no FX rate, no
// price, a short, closed) has no share.
export function weightOf(position: Position, total: bigint): number | null {
  const units = shareUnits(position)
  return units === null || total <= 0n ? null : shareOf(units, total)
}

// A day of a position's history it is held on. The position charts span the
// held days (ADR 0016): the days before the first held one and after the
// last are dropped, rather than drawn as a flat zero line, and a day inside
// the span with nothing held (a close, then a reopen) is a gap in the lines,
// not a zero.
export const isHeld = (p: PositionHistoryPoint) => Number(p.quantity) !== 0
