import type { PeriodBucket, ReturnMethod } from '../api/history'
import { excessReturn } from './returns'

// The returns at which a heatmap cell's colour steps up a strength: below 1%
// is step 1, then 1, 2, 4 and 8% and above.
const HEAT_BOUNDS = [0.01, 0.02, 0.04, 0.08]

// heatStep is a return's colour strength on the periodic returns heatmap,
// -5 to 5 with the return's sign; 0 for zero and for null.
export function heatStep(value: number | null): number {
  if (value === null || value === 0) return 0
  return Math.sign(value) * (1 + HEAT_BOUNDS.filter((b) => Math.abs(value) >= b).length)
}

// How much of the gain or loss colour each strength mixes into the card's
// surface. The cell text stays `ink`, at least 4.5:1 on every step in both
// themes (index.css.test.ts).
export const HEAT_MIX = [10, 18, 27, 36, 45]

// One heatmap row: a year, its twelve months (null where the response has no
// bucket, before the first transaction or after today), and the year bucket.
export interface PeriodRow {
  year: string
  months: (PeriodBucket | null)[]
  total: PeriodBucket
}

// periodGrid lays the periods endpoint's buckets out as the heatmap's rows,
// newest year first. A year's cell is its own bucket, never a product of its
// months: the history takes each range's first value as its base, so months
// do not chain into their year (ADR 0011).
export function periodGrid(months: PeriodBucket[], years: PeriodBucket[]): PeriodRow[] {
  const byPeriod = new Map(months.map((m) => [m.period, m]))
  return years
    .map((total) => ({
      year: total.period,
      months: Array.from(
        { length: 12 },
        (_, i) => byPeriod.get(`${total.period}-${String(i + 1).padStart(2, '0')}`) ?? null,
      ),
      total,
    }))
    .reverse()
}

// cellValue is what a cell shows: the bucket's return by the method, or with
// `excess` its return over the benchmark's (excessReturn), as the return
// card's pill. null when either return is undefined (ADR 0011).
export function cellValue(bucket: PeriodBucket, method: ReturnMethod, excess: boolean) {
  const p = bucket.returns[method]
  if (p === null) return null
  if (!excess) return Number(p)
  const b = bucket.benchmark?.[method] ?? null
  if (b === null) return null
  return excessReturn(Number(p), Number(b))
}
