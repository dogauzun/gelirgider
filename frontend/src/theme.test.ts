import { afterEach, describe, expect, it } from 'vitest'
import { getTheme, setTheme } from './theme'

describe('theme', () => {
  // The attribute is global to the document, so leave the default behind.
  afterEach(() => setTheme('system'))

  it('sets data-theme for an explicit choice and removes it for system', () => {
    setTheme('dark')

    expect(getTheme()).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('theme')).toBe('dark')

    setTheme('system')

    expect(getTheme()).toBe('system')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })
})
