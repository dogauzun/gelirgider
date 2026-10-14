import { describe, expect, it } from 'vitest'
import { annualize, excessReturn } from './returns'

describe('annualize', () => {
  it('compounds the period return down to a yearly rate', () => {
    expect(annualize(0.21, 730)).toBeCloseTo(0.1, 10)
    expect(annualize(-1, 730)).toBe(-1)
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
