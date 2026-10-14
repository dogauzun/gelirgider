import type { Position } from '../api/summary'
import { marketLabel, t } from '../i18n/strings'
import { decimalUnits, unitsToDecimal } from './decimal'

// The three groupings of the allocation card's toggle (GLG-553): one row per
// position, per market, or per currency.
export const ALLOCATION_VIEWS = ['symbol', 'market', 'currency'] as const
export type AllocationView = (typeof ALLOCATION_VIEWS)[number]

// The allocation donut's width and height in px, shared with its placeholder
// in LazyChart.tsx.
export const DONUT_SIZE = 150

// A row's share, and a position's weight, locale-correct: "%22,7" in tr, "22.7%" in en.
// eslint-disable-next-line no-restricted-syntax -- the one declaration the rule points to
export const SHARE_FORMAT: Intl.NumberFormatOptions = {
  style: 'percent',
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
}

// One row of the donut and its legend. value and share are for drawing and
// ordering only; amount is the exact decimal sum the legend prints.
export interface AllocationRow {
  key: string
  label: string
  value: number
  amount: string
  share: number
  color: string
}

// One colour per row, never reused: past six rows the largest five keep
// cat-1..5 and the rest fold into one "Diğer" row on cat-6, the palette's
// neutral slate (GLG-780). Cash keeps its own token, the same slate a 3:1
// step lighter than cat-6 (GLG-1045), so it reads as "not invested".
const COLORS = [1, 2, 3, 4, 5, 6].map((n) => `var(--color-cat-${n})`)
const CASH_COLOR = 'var(--color-cash)'

// allocationRows groups the portfolio for the allocation card: one row per
// position (or per market, or per currency, see AllocationView) and one for
// cash, largest first, cash last. Values are the reporting-currency
// equivalents (market_value_reporting), so positions in different currencies
// compare correctly. A position with a zero or negative reporting value, or
// none at all (missing FX rate, see Summary.missing_rates), is left out of
// the rows and of the total, so the shares always sum to 1; the card notes
// the positions left out. Sums are exact (bigint units), so the printed
// amounts never go through float. other names the rows folded into "Diğer".
export function allocationRows(input: {
  positions: Position[]
  cash: string
  cashByCurrencyReporting: Record<string, string>
  view: AllocationView
}): { rows: AllocationRow[]; total: number; other: string[] } {
  const { positions, cash, cashByCurrencyReporting, view } = input

  // Positions are summed into their group before sorting. In the currency
  // view cash is not a row of its own: each currency's cash (already in the
  // reporting currency) joins that currency's positions, so USD cash counts
  // as USD exposure. A group that nets to zero or less is dropped like a
  // position is.
  const groups = new Map<string, { label: string; units: bigint }>()
  const addTo = (key: string, label: string, units: bigint) => {
    const group = groups.get(key)
    if (group) group.units += units
    else groups.set(key, { label, units })
  }
  for (const position of positions) {
    if (position.market_value_reporting === null) continue
    const units = decimalUnits(position.market_value_reporting)
    if (units <= 0n) continue
    if (view === 'symbol') addTo(`position-${position.instrument_id}`, position.symbol, units)
    else if (view === 'market') addTo(position.market, marketLabel(position.market), units)
    else addTo(position.currency, position.currency, units)
  }
  if (view === 'currency') {
    for (const [currency, amount] of Object.entries(cashByCurrencyReporting)) {
      addTo(currency, currency, decimalUnits(amount))
    }
  }
  const sorted = [...groups]
    .map(([key, group]) => ({ key, ...group }))
    .filter((row) => row.units > 0n)
    .sort((a, b) => (b.units > a.units ? 1 : b.units < a.units ? -1 : 0))

  let other: string[] = []
  if (sorted.length > COLORS.length) {
    const rest = sorted.splice(COLORS.length - 1)
    other = rest.map((row) => row.label)
    sorted.push({
      key: 'other',
      label: t('allocation.other', { count: rest.length }),
      units: rest.reduce((sum, row) => sum + row.units, 0n),
    })
  }
  const grouped = sorted.map((row, index) => ({ ...row, color: COLORS[index] }))

  // Cash goes last regardless of size: it is the one row that isn't a holding.
  const cashUnits = decimalUnits(cash)
  if (view !== 'currency' && cashUnits > 0n) {
    grouped.push({ key: 'cash', label: t('summary.cash'), units: cashUnits, color: CASH_COLOR })
  }

  const totalUnits = grouped.reduce((sum, row) => sum + row.units, 0n)
  const total = Number(unitsToDecimal(totalUnits))
  const rows = grouped.map(({ units, ...row }) => {
    const amount = unitsToDecimal(units)
    const value = Number(amount)
    return { ...row, value, amount, share: value / total }
  })
  return { rows, total, other }
}
