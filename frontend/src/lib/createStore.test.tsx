import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { createStore, readStored, writeStored } from './createStore'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

it('gets, sets and re-renders subscribers, running onSet first', () => {
  const seen: number[] = []
  const store = createStore(
    () => 1,
    (next) => seen.push(next),
  )
  const { result } = renderHook(() => store.use())
  expect(result.current).toBe(1)
  act(() => store.set(2))
  expect(store.get()).toBe(2)
  expect(result.current).toBe(2)
  expect(seen).toEqual([2])
})

it('keeps the same reference until set', () => {
  const store = createStore<ReadonlySet<number>>(() => new Set())
  expect(store.get()).toBe(store.get())
})

it('reads and writes localStorage', () => {
  writeStored('k', 'v')
  expect(readStored('k')).toBe('v')
  expect(readStored('missing')).toBeNull()
})

it('swallows a throwing storage', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('disabled')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('disabled')
  })
  expect(readStored('k')).toBeNull()
  expect(() => writeStored('k', 'v')).not.toThrow()
})
