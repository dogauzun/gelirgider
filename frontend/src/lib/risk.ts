import { daysBetween } from './date'

type RiskPoint = { date: string; twr?: string | null; benchmark_twr?: string | null }

export type RiskWindow = '1y' | '3y' | 'all'

const WINDOW_DAYS = { '1y': 365, '3y': 1095 }

const ROLLING_DAYS = 365

// windowPoints cuts a history to a window: the points dated within 365 days
// (1y) or 1095 days (3y) of the last one, or every point (all). The first
// point of the window is its base (ADR 0036).
export function windowPoints<T extends { date: string }>(points: T[], window: RiskWindow): T[] {
  if (points.length === 0 || window === 'all') return points
  const last = points[points.length - 1].date
  return points.filter((p) => daysBetween(p.date, last) <= WINDOW_DAYS[window])
}

// returnsOf is, for each point after the first (index i - 1), the day's return
// (1 + x) / (1 + previous x) - 1 of the running figure `field`, or null when
// the day has none: the figure is null on the point or on the one before, or
// the one before is at -100%. A null point breaks the chain, nothing is
// chained over it (ADR 0036).
function returnsOf(points: RiskPoint[], field: 'twr' | 'benchmark_twr'): (number | null)[] {
  return points.slice(1).map((point, i) => growth(point[field], points[i][field]))
}

// dailyReturns is the points' daily twr returns, dated on the later point.
export function dailyReturns(points: RiskPoint[]): { date: string; value: number }[] {
  return returnsOf(points, 'twr').flatMap((value, i) =>
    value === null ? [] : [{ date: points[i + 1].date, value }],
  )
}

// growth is the return from `from` to `to` over a running figure (1 + to) /
// (1 + from) - 1, or null when either is null or `from` is at -100% or below.
function growth(to: string | null | undefined, from: string | null | undefined): number | null {
  if (to == null || from == null) return null
  const base = 1 + Number(from)
  return base > 0 ? (1 + Number(to)) / base - 1 : null
}

// rollingReturns is, for each point, the return of the 365 days ending on it:
// (1 + twr_i) / (1 + twr_k) - 1 against k, the last point dated 365 or more
// days before it, and the benchmark's the same (null where it is missing). A
// point with no k, or with no twr or a twr at -100% on k, is left out (ADR
// 0036).
export function rollingReturns(
  points: RiskPoint[],
): { date: string; value: number; benchmark: number | null }[] {
  const out: { date: string; value: number; benchmark: number | null }[] = []
  let k = -1
  points.forEach((point, i) => {
    while (k + 1 < i && daysBetween(points[k + 1].date, point.date) >= ROLLING_DAYS) k++
    if (k < 0) return
    const value = growth(point.twr, points[k].twr)
    if (value === null) return
    out.push({
      date: point.date,
      value,
      benchmark: growth(point.benchmark_twr, points[k].benchmark_twr),
    })
  })
  return out
}

export type RiskStats = {
  days: number
  volatility: number
  downside: number
  best: { date: string; value: number }
  worst: { date: string; value: number }
  // Over the days both the portfolio and the benchmark have a return on; null
  // without a benchmark, with a gap in it, or when it never moves.
  benchmark: { days: number; volatility: number; beta: number; correlation: number | null } | null
}

const sum = (values: number[]) => values.reduce((a, v) => a + v, 0)

// benchmarkStats pairs the daily returns by day (ADR 0036, "Against the
// benchmark"): the benchmark's annualized volatility, the beta of the
// portfolio on it and their correlation. The 30-day minimum is the card's.
function benchmarkStats(points: RiskPoint[]): RiskStats['benchmark'] {
  if (points.some((p) => p.twr != null && p.benchmark_twr == null)) return null
  const portfolio = returnsOf(points, 'twr')
  const benchmark = returnsOf(points, 'benchmark_twr')
  const r: number[] = []
  const b: number[] = []
  portfolio.forEach((value, i) => {
    const other = benchmark[i]
    if (value !== null && other !== null) {
      r.push(value)
      b.push(other)
    }
  })
  const m = r.length
  if (m < 2) return null
  const rMean = sum(r) / m
  const bMean = sum(b) / m
  const bSpread = sum(b.map((v) => (v - bMean) ** 2))
  if (bSpread === 0) return null
  const rSpread = sum(r.map((v) => (v - rMean) ** 2))
  const covariance = sum(r.map((v, i) => (v - rMean) * (b[i] - bMean)))
  return {
    days: m,
    volatility: Math.sqrt(bSpread / (m - 1)) * Math.sqrt(365),
    beta: covariance / bSpread,
    correlation: rSpread === 0 ? null : covariance / Math.sqrt(rSpread * bSpread),
  }
}

// riskStats is the annualized volatility (sample standard deviation of the
// daily returns), the downside deviation against 0 (every return in the
// divisor, no mean) and the best and worst day, over the points' daily
// returns (ADR 0036). Both spreads are annualized by √365, the history having
// a point for every calendar day. Null with fewer than two returns. The
// benchmark figures are in `benchmark`.
export function riskStats(points: RiskPoint[]): RiskStats | null {
  const returns = dailyReturns(points)
  const n = returns.length
  if (n < 2) return null
  const mean = returns.reduce((sum, r) => sum + r.value, 0) / n
  const variance = returns.reduce((sum, r) => sum + (r.value - mean) ** 2, 0) / (n - 1)
  const downside = returns.reduce((sum, r) => sum + Math.min(r.value, 0) ** 2, 0) / n
  return {
    days: n,
    volatility: Math.sqrt(variance) * Math.sqrt(365),
    downside: Math.sqrt(downside) * Math.sqrt(365),
    // The first on a tie.
    best: returns.reduce((a, r) => (r.value > a.value ? r : a)),
    worst: returns.reduce((a, r) => (r.value < a.value ? r : a)),
    benchmark: benchmarkStats(points),
  }
}
