import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { useStoredChoice } from './useStoredChoice'

const MODES = ['percent', 'money'] as const

afterEach(() => localStorage.clear())

it('falls back when the stored value is not an allowed one', () => {
  localStorage.setItem('mode', 'bogus')
  const { result } = renderHook(() => useStoredChoice('mode', MODES, 'percent'))
  expect(result.current[0]).toBe('percent')
})

it('reads an allowed stored value', () => {
  localStorage.setItem('mode', 'money')
  const { result } = renderHook(() => useStoredChoice('mode', MODES, 'percent'))
  expect(result.current[0]).toBe('money')
})

it('sets the value and writes it to localStorage', () => {
  const { result } = renderHook(() => useStoredChoice('mode', MODES, 'percent'))
  act(() => result.current[1]('money'))
  expect(result.current[0]).toBe('money')
  expect(localStorage.getItem('mode')).toBe('money')
})
