import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, useLocation, useNavigationType } from 'react-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useRange } from './useRange'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
})
afterEach(() => vi.useRealTimers())

function setup(search: string) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[`/portfolios/1${search}`]}>{children}</MemoryRouter>
  )
  return renderHook(
    () => ({
      range: useRange(),
      location: useLocation(),
      navigation: useNavigationType(),
    }),
    { wrapper },
  )
}

const DEFAULT = { key: '3m', from: '', to: '' }

it.each([
  ['', DEFAULT],
  ['?range=1y', { key: '1y', from: '', to: '' }],
  ['?range=2024', { key: '2024', from: '2024-01-01', to: '2024-12-31' }],
  ['?from=2025-03-01&to=2025-06-30', { key: 'custom', from: '2025-03-01', to: '2025-06-30' }],
  ['?range=XYZ', DEFAULT],
  ['?range=2026', DEFAULT],
  ['?from=2025-02-30&to=2025-06-30', DEFAULT],
  ['?from=2025-03-01&to=nope', DEFAULT],
  ['?from=2025-07-01&to=2025-06-30', DEFAULT],
  ['?from=2025-03-01&to=2026-12-31', DEFAULT],
])('reads %j', (search, want) => {
  expect(setup(search).result.current.range[0]).toEqual(want)
})

it('writes the range to the URL in place, and the default as no parameter', () => {
  const { result } = setup('?x=1')
  act(() => result.current.range[1]({ key: '1y', from: '2025-09-28', to: '2026-09-28' }))
  expect(result.current.location.search).toBe('?x=1&range=1y')
  expect(result.current.navigation).toBe('REPLACE')
  expect(result.current.range[0]).toEqual({ key: '1y', from: '', to: '' })

  act(() => result.current.range[1]({ key: 'custom', from: '2025-03-01', to: '2025-06-30' }))
  expect(result.current.location.search).toBe('?x=1&from=2025-03-01&to=2025-06-30')

  act(() => result.current.range[1]({ key: '3m', from: '2026-06-28', to: '2026-09-28' }))
  expect(result.current.location.search).toBe('?x=1')
})
