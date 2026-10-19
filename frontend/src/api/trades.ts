import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { historyQueryKey } from './history'
import type { PortfolioScope } from './summary'

// One realized sell. Amounts are 2-decimal strings in the instrument's
// currency, each portfolio applied at its average cost.
interface Trade {
  portfolio_id: number
  date: string
  instrument_id: number
  symbol: string
  name: string
  market: string
  currency: string
  quantity: string
  proceeds: string
  cost: string
  fee: string
  realized: string
  // Realized over cost; null when the cost is not positive.
  realized_pct: string | null
  // Null for a sell made while the quantity was zero or negative (a short
  // sale), and with it days.
  opened_at: string | null
  days: number | null
  // Realized at the sell date's rate; null when it has none.
  realized_reporting: string | null
}

interface TradesResponse {
  currency: string
  // Newest first.
  trades: Trade[]
  // The currencies a null realized_reporting could not be converted from.
  missing_rates: string[]
}

// Keyed under historyQueryKey, so what invalidates the history (a
// transaction) refetches the trades too.
export function useTrades(scope: PortfolioScope, instrumentId?: number) {
  return useQuery({
    queryKey: [...historyQueryKey(scope), 'trades', instrumentId ?? null] as const,
    queryFn: () =>
      apiFetch<TradesResponse>(
        `/api/portfolios/${scope}/trades${instrumentId === undefined ? '' : `?instrument_id=${instrumentId}`}`,
      ),
  })
}
