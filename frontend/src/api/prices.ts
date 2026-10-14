import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { invalidatePortfolio } from './transactions'

export interface PriceResponse {
  instrument_id: number
  date: string
  close: string
  source: string
}

function setPrice(instrumentId: number, date: string, close: string): Promise<PriceResponse> {
  return apiFetch<PriceResponse>(`/api/instruments/${instrumentId}/prices/${date}`, {
    method: 'PUT',
    body: JSON.stringify({ close }),
  })
}

// Removes this user's own manual price for the given instrument and date.
// Fetched prices are untouched: the backend only ever deletes from
// manual_prices, so valuation falls back to the last fetched close (GLG-134).
function deletePrice(instrumentId: number, date: string): Promise<void> {
  return apiFetch<void>(`/api/instruments/${instrumentId}/prices/${date}`, { method: 'DELETE' })
}

// useSetPrice invalidates the given portfolio's queries through
// invalidatePortfolio on success so the positions table and the value chart
// pick up the new price without a manual refetch.
export function useSetPrice(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({
      instrumentId,
      date,
      close,
    }: {
      instrumentId: number
      date: string
      close: string
    }) => setPrice(instrumentId, date, close),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

// useDeletePrice invalidates the same queries as useSetPrice: removing a
// manual price changes the position's value exactly as setting one does.
export function useDeletePrice(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ instrumentId, date }: { instrumentId: number; date: string }) =>
      deletePrice(instrumentId, date),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}
