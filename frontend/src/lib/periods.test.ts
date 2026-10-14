import { describe, expect, it } from 'vitest'
import type { PeriodBucket } from '../api/history'
import { cellValue, heatStep, periodGrid } from './periods'

const bucket = (
  period: string,
  twr: string | null = '0.010000',
  benchmark: string | null = null,
): PeriodBucket => ({
  period,
  from: `${period}-01`.slice(0, 10),
  to: `${period}-28`.slice(0, 10),
  partial: false,
  returns: { simple: twr, twr, mwr: twr },
  benchmark: benchmark === null ? null : { simple: benchmark, twr: benchmark, mwr: benchmark },
})

describe('heatStep', () => {
  it('steps at 1, 2, 4 and 8 percent, keeping the sign', () => {
    expect(heatStep(0.0099)).toBe(1)
    expect(heatStep(0.01)).toBe(2)
    expect(heatStep(0.0199)).toBe(2)
    expect(heatStep(0.02)).toBe(3)
    expect(heatStep(0.0399)).toBe(3)
    expect(heatStep(0.04)).toBe(4)
    expect(heatStep(0.0799)).toBe(4)
    expect(heatStep(0.08)).toBe(5)
    expect(heatStep(2.5)).toBe(5)
    expect(heatStep(-0.0001)).toBe(-1)
    expect(heatStep(-0.01)).toBe(-2)
    expect(heatStep(-0.02)).toBe(-3)
    expect(heatStep(-0.04)).toBe(-4)
    expect(heatStep(-0.08)).toBe(-5)
  })

  it('is 0 for zero and for null', () => {
    expect(heatStep(0)).toBe(0)
    expect(heatStep(null)).toBe(0)
  })
})

describe('periodGrid', () => {
  it('puts each month in its column, the year last, newest year first', () => {
    // The first year starts in December, the current one ends in October.
    const months = [
      bucket('2024-12'),
      ...Array.from({ length: 12 }, (_, i) => bucket(`2025-${String(i + 1).padStart(2, '0')}`)),
      ...Array.from({ length: 10 }, (_, i) => bucket(`2026-${String(i + 1).padStart(2, '0')}`)),
    ]
    const years = [bucket('2024'), bucket('2025'), bucket('2026')]
    const rows = periodGrid(months, years)

    expect(rows.map((r) => r.year)).toEqual(['2026', '2025', '2024'])
    expect(rows.map((r) => r.total)).toEqual([years[2], years[1], years[0]])
    expect(rows.every((r) => r.months.length === 12)).toBe(true)
    expect(rows[2].months.map((m) => m?.period ?? null)).toEqual([
      ...Array(11).fill(null),
      '2024-12',
    ])
    expect(rows[1].months.every((m) => m !== null)).toBe(true)
    expect(rows[0].months.map((m) => m?.period ?? null)).toEqual([
      '2026-01',
      '2026-02',
      '2026-03',
      '2026-04',
      '2026-05',
      '2026-06',
      '2026-07',
      '2026-08',
      '2026-09',
      '2026-10',
      null,
      null,
    ])
  })

  it('leaves a month the response lacks empty', () => {
    const rows = periodGrid([bucket('2026-01'), bucket('2026-03')], [bucket('2026')])
    expect(rows[0].months.slice(0, 3).map((m) => m?.period ?? null)).toEqual([
      '2026-01',
      null,
      '2026-03',
    ])
  })
})

describe('cellValue', () => {
  it("reads the method's return, or its excess over the benchmark", () => {
    const b = bucket('2026-01', '0.100000', '0.050000')
    expect(cellValue(b, 'twr', false)).toBe(0.1)
    expect(cellValue(b, 'twr', true)).toBeCloseTo(1.1 / 1.05 - 1, 12)
  })

  it('is null when either return is null', () => {
    expect(cellValue(bucket('2026-01', null), 'mwr', false)).toBeNull()
    expect(cellValue(bucket('2026-01', '0.100000'), 'mwr', true)).toBeNull()
    expect(cellValue(bucket('2026-01', null, '0.050000'), 'mwr', true)).toBeNull()
  })
})
