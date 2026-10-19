import { daysBetween } from './date'

type RiskPoint = { date: string; twr?: string | null; benchmark_twr?: string | null }

export type RiskWindow = '1y' | '3y' | 'all'

const WINDOW_DAYS = { '1y': 365, '3y': 1095 }

// windowPoints cuts a history to a window: the points dated within 365 days
// (1y) or 1095 days (3y) of the last one, or every point (all). The first
// point of the window is its base (ADR 0036).
export function windowPoints<T extends { date: string }>(points: T[], window: RiskWindow): T[] {
  if (points.length === 0 || window === 'all') return points
  const last = points[points.length - 1].date
  return points.filter((p) => daysBetween(p.date, last) <= WINDOW_DAYS[window])
}

// dailyReturns is, for each point after the first, the day's return
// (1 + twr) / (1 + previous twr) - 1, dated on the point. A day has no return
// when twr is null on it or on the point before, or when the point before is
// at -100%: a null point breaks the chain, nothing is chained over it
// (ADR 0036).
export function dailyReturns(points: RiskPoint[]): { date: string; value: number }[] {
  const returns: { date: string; value: number }[] = []
  for (let i = 1; i < points.length; i++) {
    const { twr } = points[i]
    const previous = points[i - 1].twr
    if (twr == null || previous == null) continue
    const base = 1 + Number(previous)
    if (base > 0) returns.push({ date: points[i].date, value: (1 + Number(twr)) / base - 1 })
  }
  return returns
}

export type RiskStats = {
  days: number
  volatility: number
  downside: number
  best: { date: string; value: number }
  worst: { date: string; value: number }
}

// riskStats is the annualized volatility (sample standard deviation of the
// daily returns), the downside deviation against 0 (every return in the
// divisor, no mean) and the best and worst day, over the points' daily
// returns (ADR 0036). Both spreads are annualized by √365, the history having
// a point for every calendar day. Null with fewer than two returns.
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
  }
}
