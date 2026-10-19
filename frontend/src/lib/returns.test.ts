import { describe, expect, it } from 'vitest'
import { annualize, annualizedReturn, excessReturn } from './returns'

describe('annualize', () => {
  it('compounds the period return down to a yearly rate', () => {
    expect(annualize(0.21, 730)).toBeCloseTo(0.1, 10)
    expect(annualize(-1, 730)).toBe(-1)
  })
})

describe('annualizedReturn', () => {
  it.each([
    ['2025-01-01', '2026-01-01', 0.21, 0.21],
    ['2025-01-01', '2025-12-31', 0.21, 0.21],
    ['2024-01-01', '2025-01-01', 0.21, 0.21],
    ['2025-01-01', '2026-01-03', 0.21, 1.21 ** (365 / 367) - 1],
  ])('%s to %s counts 365 up to 366 days, the real count above', (from, to, ret, want) => {
    expect(annualizedReturn(ret, from, to)).toBeCloseTo(want, 10)
  })

  it('is null under 364 days, or for a missing return or date', () => {
    expect(annualizedReturn(0.21, '2025-01-01', '2025-12-30')).toBeNull()
    expect(annualizedReturn(null, '2025-01-01', '2026-01-01')).toBeNull()
    expect(annualizedReturn(0.21, undefined, '2026-01-01')).toBeNull()
    expect(annualizedReturn(0.21, '2025-01-01', null)).toBeNull()
  })
})

describe('excessReturn', () => {
  it('divides out the benchmark, null when it lost everything', () => {
    expect(excessReturn(0.32, 0.1)).toBeCloseTo(0.2, 10)
    expect(excessReturn(0.1, 0.32)).toBeCloseTo(-1 / 6, 10)
    expect(excessReturn(0.1, -1)).toBeNull()
    expect(excessReturn(0.1, -1.5)).toBeNull()
  })
})
