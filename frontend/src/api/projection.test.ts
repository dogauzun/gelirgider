import { createElement, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { invalidatePortfolio } from './transactions'
import { useProjection } from './projection'

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
