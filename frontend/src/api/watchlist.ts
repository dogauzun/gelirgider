import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'

// A watched instrument with its latest close. Prices are exact strings in the
// instrument's currency; every price field is null without a close, and
// day_change (2 decimals) is null without a previous one. target_side is fixed
// by the server when the target is set.
export interface WatchItem {
  id: number
  instrument_id: number
  symbol: string
  name: string
  market: string
  currency: string
  last_price: string | null
  price_date: string | null
  price_source: string | null
  prev_close: string | null
  prev_price_date: string | null
  day_change: string | null
  target_price: string | null
  target_side: 'above' | 'below' | null
  target_reached: boolean
  note: string
  created_at: string
}

export interface WatchInput {
  instrument_id: number
  note?: string
}

// null clears the target; an omitted note keeps the stored one.
export interface WatchUpdate {
  target_price: string | null
  note?: string
}

const watchlistQueryKey = ['watchlist'] as const

export function useWatchlist(enabled = true) {
  return useQuery({
    queryKey: watchlistQueryKey,
    queryFn: () => apiFetch<WatchItem[]>('/api/watchlist'),
    enabled,
  })
}

export function useAddWatchItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: WatchInput) =>
      apiFetch<WatchItem>('/api/watchlist', { method: 'POST', body: JSON.stringify(input) }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: watchlistQueryKey }),
  })
}

export function useUpdateWatchItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...input }: WatchUpdate & { id: number }) =>
      apiFetch<WatchItem>(`/api/watchlist/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: watchlistQueryKey }),
  })
}

export function useRemoveWatchItem() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<void>(`/api/watchlist/${id}`, { method: 'DELETE' }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: watchlistQueryKey }),
  })
}
