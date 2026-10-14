import { describe, expect, it } from 'vitest'
import { neighbourOf } from './useFocusLater'

describe('neighbourOf', () => {
  it('takes the next key still there, else the previous one', () => {
    const keys = ['a', 'b', 'c', 'd']
    expect(neighbourOf(keys, 'b')).toBe('c')
    expect(neighbourOf(keys, 'b', (key) => key !== 'c' && key !== 'd')).toBe('a')
    expect(neighbourOf(keys, 'd')).toBe('c')
    expect(neighbourOf(['a'], 'a')).toBeUndefined()
  })
})
