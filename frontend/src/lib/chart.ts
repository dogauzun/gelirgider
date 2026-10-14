import type { HistoryEntry } from '../api/history'
import { formatMarketDate } from './date'
import { subDecimal } from './decimal'
import { formatNumber, MASK } from './format'
import { getPrivacy } from './privacy'

// Every amount the chart prints (tooltip rows, axis ticks) goes through
// formatChartValue or formatTicks, so privacy mode masks all of them.
export function formatChartValue(value: string | number): string {
  if (getPrivacy()) return MASK
  return formatNumber(value, { maximumFractionDigits: 0 })
}

// Compact so the axis stays narrow: `450 B` in tr, `450K` in en. Compact
// notation's own rounding keeps two significant digits below 100 of a unit, so a
// narrow axis near a unit boundary (995 B, 1 Mn, 1,005 Mn) would print `1 Mn`
// twice. Fraction digits are added one at a time until every label differs, up
// to Intl's limit of 20, so even a 0.0312-0.0321 price or a 1,000,000-1,000,400
// total gets distinct ticks; not set up front, since maximumFractionDigits: 0
// would turn 1.2M/1.4M into `1M`.
export function formatTicks(ticks: number[]): string[] {
  if (getPrivacy()) return ticks.map(() => MASK)
  return distinctLabels(ticks, { notation: 'compact' })
}

// The percent mode's ticks: fractions printed as percentages, `%5`,
// with fraction digits added the same way until every label differs. Never
// masked: percentages are not amounts.
export function formatPercentTicks(ticks: number[]): string[] {
  return distinctLabels(ticks, { style: 'percent' })
}

function distinctLabels(ticks: number[], options: Intl.NumberFormatOptions): string[] {
  let labels = ticks.map((v) => formatNumber(v, options))
  for (let digits = 1; digits <= 20 && new Set(labels).size < labels.length; digits++) {
    labels = ticks.map((v) => formatNumber(v, { ...options, maximumFractionDigits: digits }))
  }
  return labels
}

// A range across calendar years carries the year, so "02 Oca" is never
// ambiguous on a multi-year chart.
export function formatDateLabel(iso: string, withYear: boolean): string {
  return formatMarketDate(iso, {
    day: '2-digit',
    month: 'short',
    ...(withYear && { year: 'numeric' }),
  })
}

// The widest x axis label at 11 px Inter, in tr and en, without and with the
// year (measured in Chrome: "01 Tem" 40 px, "May 01, 2026" 74 px), and the
// least room between two labels.
const DATE_LABEL_PX = 40
const YEAR_LABEL_PX = 74
const LABEL_GAP_PX = 8

// dateTicks picks the x axis's labelled dates: one per about 120 px of plot
// width, at least 3 and at most 7, never more than there are dates, evenly
// spaced by index and always the first and the last. With n <= dates the
// index step is at least 1, so no date repeats. Fewer, down to the two ends
// (or the first alone), when the labels would overlap: the first starts at
// its point, the last ends at its point, the rest are centred (DateAxis), and
// a label with a year is longer.
export function dateTicks(dates: string[], plotWidth: number, withYear = false): string[] {
  const label = withYear ? YEAR_LABEL_PX : DATE_LABEL_PX
  const last = dates.length - 1
  const most = Math.min(dates.length, Math.max(3, Math.min(7, Math.round(plotWidth / 120))))
  for (let n = most; n > 1; n--) {
    const at = Array.from({ length: n }, (_, i) => Math.round((i * last) / (n - 1)))
    const x = (j: number) => (at[j] * plotWidth) / last
    // Room label j-1 takes right of its point plus room label j takes left.
    const room = (j: number) => (j === 1 ? label : label / 2) + (j === n - 1 ? label : label / 2)
    if (at.every((_, j) => j === 0 || x(j) - x(j - 1) >= room(j) + LABEL_GAP_PX)) {
      return at.map((i) => dates[i])
    }
  }
  return dates.slice(0, 1)
}

// The line chart's legend reserves two text-xs lines (and the gap between
// them) below sm, where a benchmark wraps it, and one from sm up; the
// placeholder in LazyChart.tsx reserves the same, so the card keeps its
// height when the charts chunk arrives.
export const LEGEND_HEIGHT = 'min-h-9 sm:min-h-4'

// pressWithoutFocus is a chart's onMouseDown. A press would focus the
// Recharts layer under the pointer (its z-index layers have tabindex -1),
// which draws a focus frame and, as focus bubbles to Recharts' focus handler,
// pins the keyboard tooltip on the first point after the mouse leaves.
// Preventing the press's default keeps focus out, and the field
// focused elsewhere is blurred by hand, as the press would have, so a field
// that saves on blur (the portfolio rename) still saves. Tab still focuses
// the chart.
export function pressWithoutFocus(e: { preventDefault: () => void }) {
  e.preventDefault()
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
}

// niceTicks returns y-axis ticks at a 1/2/5 x 10^n step, the smallest step
// that covers [min, max], ends extended outward to whole steps, in at most six
// intervals, so a phone-height plot keeps room between its labels. Every tick
// is a multiple of the step, so 0 is a tick whenever the range spans it.
export function niceTicks(min: number, max: number): number[] {
  const magnitude = 10 ** Math.floor(Math.log10((max - min || 1) / 6))
  const step =
    [1, 2, 5, 10, 20]
      .map((m) => m * magnitude)
      .find((s) => Math.ceil(max / s) - Math.floor(min / s) <= 6) ?? 20 * magnitude
  const lo = Math.floor(min / step) * step
  const hi = Math.max(Math.ceil(max / step) * step, lo + step)
  const ticks: number[] = []
  for (let i = 0; lo + i * step <= hi + step / 2; i++) ticks.push(lo + i * step)
  return ticks
}

// axisTicks fits the y axis to the plotted values: 0 is on the axis
// only when the data reaches or crosses it, so a portfolio moving between 900k
// and 1M fills the plot instead of sitting in its top fifth. A flat series gets
// 5% of its value either side (or 1 at zero) so it still has a range to sit in.
export function axisTicks(values: number[]): number[] {
  let min = Math.min(...values)
  let max = Math.max(...values)
  if (min === max) {
    const pad = Math.abs(min) * 0.05 || 1
    min -= pad
    max += pad
  }
  return niceTicks(min, max)
}

// rangeDelta is the change in total value across the whole series, net of the
// external cash that arrived inside it (netFlow, from the history response):
// a deposit raises the total without being a gain. Null with fewer
// than two points: nothing to compare. The percentage next to it comes from
// the history response's `returns` (ADR 0011). Exact decimal subtraction,
// since floats print 0.30 - 0.10 - 0.20 as `-0.00` and lose cents above 2^53;
// the API's amounts carry 2 decimals, padded back to 2 here.
export function rangeDelta(points: HistoryEntry[], netFlow: string): string | null {
  if (points.length < 2) return null
  const [whole, fraction = ''] = subDecimal(
    subDecimal(points[points.length - 1].total_value, points[0].total_value),
    netFlow || '0',
  ).split('.')
  return `${whole}.${fraction.padEnd(2, '0')}`
}

type XY = { x: number; y: number }
type Size = { width: number; height: number }

// placeTooltip is the top-left corner of a tooltip card of size `card` for the
// point at `point`, all in chart pixels. The card goes `gap` below a point in
// the upper half of the plot and `gap` above one in the lower half, the side
// with more room, so it never covers the point or the line around it;
// it is kept inside the plot. Pushed back inside, the card
// may come closer, down to half the gap; any closer and it sits beside the
// point instead.
export function placeTooltip(point: XY, card: Size, plot: XY & Size, gap: number): XY {
  const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(v, hi))
  const below = point.y < plot.y + plot.height / 2
  const y = clamp(
    below ? point.y + gap : point.y - gap - card.height,
    plot.y,
    plot.y + plot.height - card.height,
  )
  const x = point.x < plot.x + plot.width / 2 ? point.x + gap : point.x - gap - card.width
  const clear = below ? y >= point.y + gap / 2 : y + card.height <= point.y - gap / 2
  // ponytail: beside the point the card may run past the plot's edge; a card
  // too big for the room on every side of the point has nowhere else to go.
  if (!clear) return { x, y }
  return { x: clamp(x, plot.x, plot.x + plot.width - card.width), y }
}

export type ChartMarker = { date: string; kind: 'buy' | 'sell' | 'dividend' | 'split' }

export type ChartPoint = Omit<HistoryEntry, 'total_value' | 'cost_basis'> & {
  total_value: string | null
  cost_basis: string | null
}

export type Bar = { key: string; label: string; value: string }
