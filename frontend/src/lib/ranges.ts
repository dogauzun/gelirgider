import type { StringKey } from '../i18n/strings'

// Range buttons for the value-over-time chart. "all" sends no `from`, so the
// backend defaults it to the portfolio's earliest transaction date. "ytd" is
// appended last: the default range is picked by index.
export type ChartRange = {
  key: string
  labelKey: StringKey
  deltaKey: StringKey
  months: number | null
}

export const RANGES: ChartRange[] = [
  { key: '1m', labelKey: 'chart.range.1m', deltaKey: 'chart.rangeDelta.1m', months: 1 },
  { key: '3m', labelKey: 'chart.range.3m', deltaKey: 'chart.rangeDelta.3m', months: 3 },
  { key: '1y', labelKey: 'chart.range.1y', deltaKey: 'chart.rangeDelta.1y', months: 12 },
  { key: 'all', labelKey: 'chart.range.all', deltaKey: 'chart.rangeDelta.all', months: null },
  { key: 'ytd', labelKey: 'chart.range.ytd', deltaKey: 'chart.rangeDelta.ytd', months: null },
]

// The chart range picked in RangePicker: a RANGES key, a calendar year
// ("2025"), or "custom" with the typed dates in `from` / `to`.
export type RangeValue = { key: string; from: string; to: string }

// The API `from` / `to` of a range: "all" sends no `from`, so the backend
// defaults it to the portfolio's earliest transaction date.
export function resolveRange(value: RangeValue, today: string): { from: string; to: string } {
  if (value.key === 'custom') return { from: value.from, to: value.to || today }
  if (/^\d{4}$/.test(value.key)) return { from: `${value.key}-01-01`, to: `${value.key}-12-31` }
  // Jan 1 of today's Europe/Istanbul year.
  if (value.key === 'ytd') return { from: `${today.slice(0, 4)}-01-01`, to: today }
  const months = RANGES.find((r) => r.key === value.key)?.months ?? null
  if (months === null) return { from: '', to: today }
  const date = new Date(`${today}T00:00:00Z`)
  date.setUTCMonth(date.getUTCMonth() - months)
  return { from: date.toISOString().slice(0, 10), to: today }
}
