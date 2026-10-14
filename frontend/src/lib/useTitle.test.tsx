import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useTitle } from './useTitle'

describe('useTitle', () => {
  it('joins the known parts and the app name', () => {
    const { rerender } = renderHook((name?: string) => useTitle('Özet', name))
    expect(document.title).toBe('Özet · gelirgider')
    rerender('My portfolio')
    expect(document.title).toBe('Özet · My portfolio · gelirgider')
  })
})
