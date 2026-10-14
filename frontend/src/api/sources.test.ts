import { describe, expect, it } from 'vitest'
import { isStale } from './sources'

const now = new Date('2026-10-06T12:00:00Z')
const on = (last_success_at: string | null) => ({ enabled: true, last_success_at })

describe('isStale', () => {
  it('is false at exactly 48 hours and true one minute later', () => {
    expect(isStale(on('2026-10-04T12:00:00Z'), now)).toBe(false)
    expect(isStale(on('2026-10-04T11:59:00Z'), now)).toBe(true)
  })

  it('is true for a source that is on and never succeeded', () => {
    expect(isStale(on(null), now)).toBe(true)
  })

  // Review Focus 5: a source that is off never warns, however old.
  it('is false for a source that is off', () => {
    expect(isStale({ enabled: false, last_success_at: null }, now)).toBe(false)
    expect(isStale({ enabled: false, last_success_at: '2020-01-01T00:00:00Z' }, now)).toBe(false)
  })
})
