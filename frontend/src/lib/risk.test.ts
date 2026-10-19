import { describe, expect, it } from 'vitest'
import { dailyReturns, riskStats, rollingReturns, windowPoints, type RiskStats } from './risk'

const points = (twr: (string | null)[]) =>
  twr.map((v, i) => ({ date: `2025-03-0${i + 1}`, twr: v }))

// ADR 0036's worked example: returns +2%, -1%, +3%.
const example = points([null, '0.000000', '0.020000', '0.009800', '0.040094'])

describe('dailyReturns', () => {
  it('chains the running twr into daily returns, dated on the later point', () => {
    const returns = dailyReturns(example)

    expect(returns.map((r) => r.date)).toEqual(['2025-03-03', '2025-03-04', '2025-03-05'])
    expect(returns[0].value).toBeCloseTo(0.02, 6)
    expect(returns[1].value).toBeCloseTo(-0.01, 6)
    expect(returns[2].value).toBeCloseTo(0.03, 6)
  })

  it('does not chain over a null point', () => {
    const returns = dailyReturns(points(['0.000000', '0.010000', null, '0.020000', '0.030000']))

    expect(returns.map((r) => r.date)).toEqual(['2025-03-02', '2025-03-05'])
  })

  it('has no return after a fall of 100%', () => {
    expect(dailyReturns(points(['0.100000', '-1.000000', '0.500000']))).toHaveLength(1)
  })
})

describe('riskStats', () => {
  it('annualizes the volatility and the downside deviation and finds the best and worst day', () => {
    const stats = riskStats(example) as RiskStats

    expect(stats.days).toBe(3)
    expect(stats.volatility).toBeCloseTo(0.3977, 3)
    expect(stats.downside).toBeCloseTo(0.1103, 3)
    expect(stats.best.date).toBe('2025-03-05')
    expect(stats.best.value).toBeCloseTo(0.03, 6)
    expect(stats.worst.date).toBe('2025-03-04')
    expect(stats.worst.value).toBeCloseTo(-0.01, 6)
  })

  describe('against the benchmark', () => {
    const withBenchmark = (benchmark: (string | null)[]) =>
      example.map((p, i) => ({ ...p, benchmark_twr: benchmark[i] }))
    const half = [null, '0.000000', '0.010000', '0.004950', '0.020024']

    it('pairs the days: beta, correlation and the benchmark volatility', () => {
      const { benchmark } = riskStats(withBenchmark(half)) as RiskStats

      expect(benchmark?.days).toBe(3)
      expect(benchmark?.beta).toBeCloseTo(2, 2)
      expect(benchmark?.correlation).toBeCloseTo(1, 2)
      expect(benchmark?.volatility).toBeCloseTo(0.1989, 3)
    })

    it('is null with a gap in the benchmark, the portfolio figures unchanged', () => {
      const stats = riskStats(withBenchmark([null, '0.000000', '0.010000', null, '0.020024']))

      expect(stats?.benchmark).toBeNull()
      expect(stats?.volatility).toBeCloseTo(0.3977, 3)
    })

    it('is null without a benchmark and with one that never moves', () => {
      expect(riskStats(example)?.benchmark).toBeNull()
      const flat = [null, '0.000000', '0.000000', '0.000000', '0.000000']
      expect(riskStats(withBenchmark(flat))?.benchmark).toBeNull()
    })
  })

  it('is null with fewer than two returns and with twr null everywhere', () => {
    expect(riskStats(points(['0.000000', '0.010000']))).toBeNull()
    expect(riskStats(points([null, null, null]))).toBeNull()
  })
})

describe('windowPoints', () => {
  const day = (i: number) => ({
    date: new Date(Date.UTC(2024, 0, 1 + i)).toISOString().slice(0, 10),
  })
  const history = Array.from({ length: 400 }, (_, i) => day(i))

  it('keeps the points within 365 or 1095 days of the last one, or all', () => {
    expect(windowPoints(history, '1y')).toHaveLength(366)
    expect(windowPoints(history, '3y')).toHaveLength(400)
    expect(windowPoints(history, 'all')).toHaveLength(400)
  })

  it('is empty without points', () => {
    expect(windowPoints([], '1y')).toEqual([])
  })
})

describe('rollingReturns', () => {
  const addDays = (days: number) =>
    new Date(Date.parse('2024-01-01') + days * 86_400_000).toISOString().slice(0, 10)
  // Point i is dated start + i days; twr is i * 0.001, the benchmark's i * 0.0005.
  const series = (count: number, benchmark = false) =>
    Array.from(
      { length: count },
      (_, i): { date: string; twr: string | null; benchmark_twr: string | null } => ({
        date: addDays(i),
        twr: (i * 0.001).toFixed(6),
        benchmark_twr: benchmark ? (i * 0.0005).toFixed(6) : null,
      }),
    )

  it('returns the 365 days ending on each point, from the 366th day', () => {
    const rolling = rollingReturns(series(400))

    expect(rolling).toHaveLength(35)
    expect(rolling[0].date).toBe(addDays(365))
    expect(rolling[0].value).toBeCloseTo(0.365, 6)
    expect(rolling[34].value).toBeCloseTo(1.399 / 1.034 - 1, 6)
    expect(rolling[0].benchmark).toBeNull()
  })

  it('does the same over the benchmark', () => {
    expect(rollingReturns(series(400, true))[0].benchmark).toBeCloseTo(0.1825, 6)
  })

  it('has none under a year of history', () => {
    expect(rollingReturns(series(300))).toEqual([])
  })

  it('leaves out a point whose base has no twr', () => {
    const data = series(400)
    data[1] = { ...data[1], twr: null }

    // Point 366 has k = 1, point 367 has k = 2.
    expect(rollingReturns(data).map((r) => r.date)).not.toContain(addDays(366))
    expect(rollingReturns(data)).toHaveLength(34)
  })
})
