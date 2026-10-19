import { describe, expect, it } from 'vitest'
import { drawdown } from './drawdown'

const series = (...twr: (string | null)[]) =>
  twr.map((v, i) => ({ date: `2025-01-${String(i + 1).padStart(2, '0')}`, twr: v }))

const values = (r: ReturnType<typeof drawdown>) => r!.series.map((p) => p.value)

describe('drawdown', () => {
  it('gives zeros and no maximum for a rising series', () => {
    const r = drawdown(series('0.000000', '0.010000', '0.010000', '0.050000'))
    expect(values(r)).toEqual([0, 0, 0, 0])
    expect(r!.max).toBeNull()
    expect(r!.current).toBe(0)
  })

  it('gives the depth with its peak and trough dates after a fall and a full recovery', () => {
    // Index 1, 1.25, 1, 1.125, 1.25, 1.3: a 20% fall from the 2nd to the 3rd.
    const r = drawdown(series('0', '0.25', '0', '0.125', '0.25', '0.3'))
    expect(values(r)).toEqual([0, 0, -0.2, -0.1, 0, 0])
    expect(r!.max).toEqual({
      value: -0.2,
      peak: '2025-01-02',
      trough: '2025-01-03',
      recovered: '2025-01-05',
    })
    expect(r!.current).toBe(0)
  })

  it('takes the last day of a flat peak as the peak', () => {
    const r = drawdown(series('0', '0.25', '0.25', '0'))
    expect(r!.max).toEqual({
      value: -0.2,
      peak: '2025-01-03',
      trough: '2025-01-04',
      recovered: null,
    })
  })

  it('ends on a new low on the last day', () => {
    const r = drawdown(series('0', '-0.1', '-0.05', '-0.2'))
    expect(r!.max!.value).toBeCloseTo(-0.2)
    expect(r!.max!.peak).toBe('2025-01-01')
    expect(r!.max!.trough).toBe('2025-01-04')
    expect(r!.current).toBeCloseTo(-0.2)
    expect(r!.max!.recovered).toBeNull()
  })

  it('does not carry an earlier fall recovery over to a deeper later fall', () => {
    // Index 1, 0.8, 1, 0.7, 0.9: the first fall recovers on the 3rd, the
    // deeper second one (peak the 3rd) has not.
    const r = drawdown(series('0', '-0.2', '0', '-0.3', '-0.1'))
    expect(r!.max).toMatchObject({ peak: '2025-01-03', trough: '2025-01-04', recovered: null })
  })

  it('keeps the first of two equal lows', () => {
    // Index 1, 0.8, 1, 0.8: both lows are -20% from the same peak.
    const r = drawdown(series('0', '-0.2', '0', '-0.2'))
    expect(r!.max).toMatchObject({ peak: '2025-01-01', trough: '2025-01-02' })
    expect(r!.max!.value).toBeCloseTo(-0.2)
    expect(r!.current).toBe(r!.max!.value)
  })

  // Index 1, 0.8, 1.5, 1.2: both falls are 20%, but in floats the second
  // comes out a hair deeper (-0.20000000000000004 vs -0.19999999999999996).
  it('keeps the first of two equal lows from different peaks', () => {
    const r = drawdown(series('0', '-0.2', '0.5', '0.2'))
    expect(r!.max).toMatchObject({ peak: '2025-01-01', trough: '2025-01-02' })
  })

  it('is null when the return is undefined or there is nothing to compare', () => {
    expect(drawdown(series(null, null, null))).toBeNull()
    expect(drawdown(series('0', null))).toBeNull()
    expect(drawdown(series('0'))).toBeNull()
    expect(drawdown([])).toBeNull()
  })
})
