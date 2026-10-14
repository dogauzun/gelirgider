import { describe, expect, it } from 'vitest'
import { patchSearchParams } from './searchParams'

describe('patchSearchParams', () => {
  it('sets, deletes on undefined and empty, keeps the rest, leaves prev alone', () => {
    const prev = new URLSearchParams('a=1&b=2&c=3&d=4')
    const next = patchSearchParams(prev, { a: 'x', b: undefined, c: '', e: 'new' })
    expect(next.toString()).toBe('a=x&d=4&e=new')
    expect(prev.toString()).toBe('a=1&b=2&c=3&d=4')
  })
})
