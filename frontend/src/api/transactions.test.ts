import { createElement, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import {
  invalidatePortfolio,
  invalidatePortfolios,
  transactionsCsvUrl,
  useDeleteTransaction,
  useTransactions,
  type Transaction,
} from './transactions'

describe('invalidatePortfolio', () => {
  // The sidebar never remounts, so its portfolio values only move when
  // something invalidates the list.
  it('a transaction change refreshes the portfolio list', async () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    await invalidatePortfolio(queryClient, 1)
    expect(spy).toHaveBeenCalledWith({ queryKey: ['portfolios'], exact: true })
  })

  it('invalidatePortfolios marks every portfolios query stale', async () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    await invalidatePortfolios(queryClient)
    expect(spy).toHaveBeenCalledWith({ queryKey: ['portfolios'] })
  })
})

describe('useDeleteTransaction', () => {
  // The delete can land after its page is gone: a cached list
  // nothing shows is dropped, so coming back does not paint the row again.
  it('drops the inactive transaction lists of the portfolio', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))
    const queryClient = new QueryClient()
    queryClient.setQueryData(['portfolios', 1, 'transactions', {}], [{ id: 5 }])
    queryClient.setQueryData(['portfolios', 2, 'transactions', {}], [{ id: 6 }])
    const { result } = renderHook(() => useDeleteTransaction(1), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(QueryClientProvider, { client: queryClient }, children),
    })
    await result.current.mutateAsync(5)
    expect(queryClient.getQueryData(['portfolios', 1, 'transactions', {}])).toBeUndefined()
    expect(queryClient.getQueryData(['portfolios', 2, 'transactions', {}])).toEqual([{ id: 6 }])
  })
})

describe('useTransactions', () => {
  // The hook hands back the loaded rows, not the pages, while
  // paging still follows the X-Next-Cursor of the last page.
  it('returns the loaded pages as one list of rows', async () => {
    const tx = (id: number) => ({ id }) as Transaction
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input).includes('before=c1')
        ? Response.json([tx(3)])
        : Response.json([tx(1), tx(2)], { headers: { 'X-Next-Cursor': 'c1' } }),
    )
    const queryClient = new QueryClient()
    const { result } = renderHook(() => useTransactions(1), {
      wrapper: ({ children }: { children: ReactNode }) =>
        createElement(QueryClientProvider, { client: queryClient }, children),
    })
    await waitFor(() => expect(result.current.data).toEqual([tx(1), tx(2)]))
    expect(result.current.hasNextPage).toBe(true)
    await act(() => result.current.fetchNextPage())
    await waitFor(() => expect(result.current.data).toEqual([tx(1), tx(2), tx(3)]))
    expect(result.current.hasNextPage).toBe(false)
  })
})

describe('transactionsCsvUrl', () => {
  // The export carries the list's filters, the import one included.
  it('carries the import filter', () => {
    expect(transactionsCsvUrl(1)).toBe('/api/portfolios/1/transactions.csv')
    expect(transactionsCsvUrl(1, { importId: 7 })).toBe(
      '/api/portfolios/1/transactions.csv?import_id=7',
    )
    expect(
      transactionsCsvUrl(1, {
        types: ['buy'],
        instrumentId: 3,
        from: '2026-01-01',
        importId: 'none',
      }),
    ).toBe(
      '/api/portfolios/1/transactions.csv?type=buy&instrument_id=3&import_id=none&from=2026-01-01',
    )
  })
})
