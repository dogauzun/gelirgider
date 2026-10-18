import type { AllocationTarget } from '../api/allocationTargets'
import type { Position } from '../api/summary'
import { marketLabel, sectorLabel, t } from '../i18n/strings'
import { compareUnits, decimalUnits, unitsToDecimal } from './decimal'
import { currencyTotals, holdingsTotal, shareOf, shareUnits } from './positions'

// The groupings of the allocation card's toggle: one row per position, per
// market, per asset class, per sector, or per currency.
export const ALLOCATION_VIEWS = ['symbol', 'market', 'assetClass', 'sector', 'currency'] as const
export type AllocationView = (typeof ALLOCATION_VIEWS)[number]

export type AssetClass = 'equity' | 'fund' | 'commodity' | 'crypto' | 'cash' | 'deposit' | 'unknown'

// A position's asset class when its instrument stores none, read from its
// market. shortcut: US ETFs count as equity because their instruments carry
// no asset_class yet, upgrade when they do. A market missing here falls into
// the 'unknown' row.
const ASSET_CLASS_BY_MARKET: Record<string, AssetClass> = {
  US: 'equity',
  BIST: 'equity',
  TEFAS: 'fund',
  CRYPTO: 'crypto',
  GOLD: 'commodity',
  DEPOSIT: 'deposit',
}

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

export const DRIFT_FORMAT: Intl.NumberFormatOptions = {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
  signDisplay: 'exceptZero',
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

// The group key and legend label of a position in the non-currency views.
function groupOf(position: Position, view: AllocationView): [string, string] {
  if (view === 'symbol') return [`position-${position.instrument_id}`, position.symbol]
  if (view === 'assetClass') {
    const assetClass =
      (position.asset_class as AssetClass | null) ??
      ASSET_CLASS_BY_MARKET[position.market] ??
      'unknown'
    return [`class-${assetClass}`, t(`allocation.assetClass.${assetClass}`)]
  }
  if (view === 'sector') return [`sector-${position.sector}`, sectorLabel(position.sector)]
  return [position.market, marketLabel(position.market)]
}

// One colour per row, never reused: past six rows the largest five keep
// cat-1..5 and the rest fold into one "Diğer" row on cat-6, the palette's
// neutral slate. Cash keeps its own token, the same slate a 3:1
// step lighter than cat-6, so it reads as "not invested".
const COLORS = [1, 2, 3, 4, 5, 6].map((n) => `var(--color-cat-${n})`)
const CASH_COLOR = 'var(--color-cash)'

// allocationRows groups the portfolio for the allocation card: one row per
// position (or per market, asset class, sector, or currency, see AllocationView) and one for
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
  if (view === 'currency') {
    for (const [currency, units] of currencyTotals(positions, cashByCurrencyReporting)) {
      groups.set(currency, { label: currency, units })
    }
  } else {
    for (const position of positions) {
      const units = shareUnits(position)
      if (units === null) continue
      const [key, label] = groupOf(position, view)
      const group = groups.get(key)
      if (group) group.units += units
      else groups.set(key, { label, units })
    }
  }
  const sorted = [...groups]
    .map(([key, group]) => ({ key, ...group }))
    .sort((a, b) => compareUnits(b.units, a.units))

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

  const totalUnits =
    view === 'currency'
      ? grouped.reduce((sum, row) => sum + row.units, 0n)
      : holdingsTotal({ positions, cash })
  const total = Number(unitsToDecimal(totalUnits))
  const rows = grouped.map(({ units, ...row }) => {
    const amount = unitsToDecimal(units)
    return { ...row, value: Number(amount), amount, share: shareOf(units, totalUnits) }
  })
  return { rows, total, other }
}

const classOfRow = (key: string) => (key.startsWith('class-') ? key.slice('class-'.length) : key)

export function driftRows(
  rows: AllocationRow[],
  targets: AllocationTarget[],
): (AllocationRow & { target: number | null; drift: number | null })[] {
  const weights = new Map(targets.map((x) => [x.asset_class, Number(x.weight) / 100]))
  const out = rows.map((row) => {
    const target = weights.get(classOfRow(row.key)) ?? null
    return { ...row, target, drift: target === null ? null : (row.share - target) * 100 }
  })
  for (const [assetClass, target] of weights) {
    if (rows.some((row) => classOfRow(row.key) === assetClass)) continue
    out.push({
      key: assetClass === 'cash' ? 'cash' : `class-${assetClass}`,
      label:
        assetClass === 'cash'
          ? t('summary.cash')
          : t(`allocation.assetClass.${assetClass as AssetClass}`),
      value: 0,
      amount: '0',
      share: 0,
      color: CASH_COLOR,
      target,
      drift: -target * 100,
    })
  }
  return out
}

// A row's target as an integer count of 1e-12, so the planner stays in bigint.
const TARGET_SCALE = 10n ** 12n

// planContribution splits a buy of `amount` (units) across the classes under
// their target: with T = holdings + amount, a class's gap is
// max(0, target * T - current). Gaps that fit in the amount are filled and the
// remainder goes by target weight; otherwise the amount is split in
// proportion to the gaps. Rounding leftovers go to the largest gap, so the
// parts sum to the amount. Rows without a target are left out.
export function planContribution(
  rows: ReturnType<typeof driftRows>,
  amount: bigint,
): { assetClass: string; amount: bigint }[] {
  const classes = rows.flatMap((row) =>
    row.target === null
      ? []
      : [
          {
            assetClass: classOfRow(row.key),
            weight: BigInt(Math.round(row.target * Number(TARGET_SCALE))),
            current: decimalUnits(row.amount),
          },
        ],
  )
  if (classes.length === 0 || amount <= 0n) return []

  const total = rows.reduce((sum, row) => sum + decimalUnits(row.amount), 0n) + amount
  // Gaps in units * TARGET_SCALE, to divide only once.
  const gaps = classes.map(({ weight, current }) => {
    const gap = weight * total - current * TARGET_SCALE
    return gap > 0n ? gap : 0n
  })
  const gapSum = gaps.reduce((sum, gap) => sum + gap, 0n)

  let parts: bigint[]
  if (gapSum <= amount * TARGET_SCALE) {
    const filled = gaps.map((gap) => gap / TARGET_SCALE)
    const rest = amount - filled.reduce((sum, part) => sum + part, 0n)
    const weightSum = classes.reduce((sum, { weight }) => sum + weight, 0n)
    parts = classes.map(({ weight }, i) => filled[i] + (rest * weight) / weightSum)
  } else {
    parts = gaps.map((gap) => (amount * gap) / gapSum)
  }

  const largest = gaps.reduce((best, gap, i) => (gap > gaps[best] ? i : best), 0)
  parts[largest] += amount - parts.reduce((sum, part) => sum + part, 0n)

  return classes.flatMap(({ assetClass }, i) =>
    parts[i] > 0n ? [{ assetClass, amount: parts[i] }] : [],
  )
}
