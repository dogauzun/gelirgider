import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { historyQueryKey } from './history'
import { useDeletePortfolio } from './portfolios'
import { summaryQueryKey } from './summary'

afterEach(() => vi.restoreAllMocks())

describe('useDeletePortfolio', () => {
  // GLG-1005. The deleted portfolio's page is still on screen when the delete
  // succeeds; refetching its queries can only answer 404. The list and Tüm
  // portföyler change with the delete, so those do refetch.
  it('refetches the list and Tüm portföyler, never the deleted portfolio', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => new Response('[]', { status: 200 }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const keys = [
      ['portfolios'],
      summaryQueryKey(7),
      [...historyQueryKey(7), '2026-01-01', '2026-10-05', null],
      summaryQueryKey('all'),
    ]
    // Each query fetches the path its key spells, so the spy sees which ran.
    const observers = keys.map(
      (queryKey) =>
        new QueryObserver(queryClient, {
          queryKey,
          queryFn: async () => (await fetch(`/api/${queryKey.join('/')}`)).json(),
        }),
    )
    const unsubscribes = observers.map((observer) => observer.subscribe(() => {}))
    await vi.waitFor(() =>
      expect(observers.every((observer) => observer.getCurrentResult().isSuccess)).toBe(true),
    )
    fetchSpy.mockClear()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useDeletePortfolio(), { wrapper })

    await act(() => result.current.mutateAsync({ id: 7 }))

    expect(fetchSpy.mock.calls.map(([path]) => path)).toEqual([
      '/api/portfolios/7',
      '/api/portfolios',
      '/api/portfolios/all/summary',
    ])
    unsubscribes.forEach((unsubscribe) => unsubscribe())
  })
})
