import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { historyQueryKey } from './history'
import type { PortfolioScope } from './summary'

// The four buckets of a range's return, 2-decimal strings in the reporting
// currency; a bucket is null when a day of the range had no rate to convert
// it (ADR 0027).
interface AttributionBuckets {
  price: string | null
  income: string | null
  fx: string | null
  cash_fx: string | null
}

export interface AttributionPosition {
  instrument_id: number
  symbol: string
  // Quantity zero on the last day of the range.
  closed: boolean
  price: string | null
  income: string | null
  fx: string | null
  total: string | null
}

interface AttributionResponse {
  currency: string
  from: string
  to: string
  total: AttributionBuckets
  positions: AttributionPosition[]
  // The currencies a null bucket could not be converted from, as in
  // HistoryResponse.missing_rates.
  missing_rates: string[]
}

// Keyed under historyQueryKey, so what invalidates the history (a
// transaction, a manual price) refetches the split too.
export function useAttribution(scope: PortfolioScope, from: string, to: string, enabled = true) {
  return useQuery({
    enabled,
    queryKey: [...historyQueryKey(scope), 'attribution', from, to] as const,
    queryFn: () => {
      const params = new URLSearchParams({ to })
      if (from) params.set('from', from)
      return apiFetch<AttributionResponse>(`/api/portfolios/${scope}/attribution?${params}`)
    },
    // A new range keeps the last split on screen until it answers. Another
    // portfolio starts empty: its old bars would be wrong, not stale.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === scope ? previous : undefined,
  })
}
