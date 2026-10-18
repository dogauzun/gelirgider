import { createElement, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n/strings'
import { invalidatePortfolio } from './transactions'
import { progressPercent, useProjection } from './projection'

// A transaction or price change moves the projection, so the goal card and
// the header pill must refetch with the rest of the portfolio.
it('refetches the projection when the portfolio is invalidated', async () => {
  const fetchSpy = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(async () => Response.json({ months: [], progress: 0 }))
  const queryClient = new QueryClient()
  const { result } = renderHook(() => useProjection(1, true), {
    wrapper: ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: queryClient }, children),
  })
  await waitFor(() => expect(result.current.data).toBeDefined())
  expect(fetchSpy).toHaveBeenCalledTimes(1)
  await invalidatePortfolio(queryClient, 1)
  await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
})

afterEach(() => setLocale('tr'))

// The goal pill and card read the percent like every other one: "%50" in tr,
// "50%" in en.
it('formats goal progress in the active locale', () => {
  expect(progressPercent(0.5)).toBe('%50')
  setLocale('en')
  expect(progressPercent(0.5)).toBe('50%')
})
