export type Drawdown = {
  series: { date: string; value: number }[]
  max: { value: number; peak: string; trough: string } | null
  current: number
}

// drawdown is, for each day, how far the return index 1 + twr is below its
// highest value so far, as a fraction (0 at a high, -0.2 at 20% below it);
// the largest of them with the date of its peak (the last day at that high)
// and of its trough (the first day at that low), null when the index never
// falls; and the last day's. It only transforms the series the history API
// returned (ADR 0011). Null when twr is null (it is null on every point or
// on none) or with fewer than two points.
// twr has 6 decimals, so a real difference in depth is far above this.
const EPSILON = 1e-9

export function drawdown(points: { date: string; twr?: string | null }[]): Drawdown | null {
  if (points.length < 2 || points.some((p) => p.twr == null)) return null
  let peak = { index: -Infinity, date: '' }
  let max: Drawdown['max'] = null
  const series = points.map((p) => {
    const index = 1 + Number(p.twr)
    if (index >= peak.index) peak = { index, date: p.date }
    // (index - peak) / peak rather than index / peak - 1: it keeps 1 / 1.25
    // at -0.2 instead of -0.19999999999999996.
    const value = (index - peak.index) / peak.index
    // Deeper by more than float noise: two equal falls from different peaks
    // can differ in the 17th digit, and the first must stay the largest.
    if (value < (max?.value ?? 0) - EPSILON) max = { value, peak: peak.date, trough: p.date }
    return { date: p.date, value }
  })
  return { series, max, current: series[series.length - 1].value }
}
