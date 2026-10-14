import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { historyQueryKey } from './history'

// One day of one holding (ADR 0016), in the instrument's own currency.
// close is null before the first price; the rest are zero on a day the
// position is not held.
export interface PositionHistoryPoint {
  date: string
  close: string | null
  quantity: string
  avg_cost: string
  cost_basis: string
  market_value: string
}

export interface PositionHistoryResponse {
  currency: string
  points: PositionHistoryPoint[]
}

function fetchPositionHistory(
  portfolioId: number,
  instrumentId: number,
  from: string,
  to: string,
): Promise<PositionHistoryResponse> {
  const params = new URLSearchParams({ to })
  if (from) params.set('from', from)
  return apiFetch<PositionHistoryResponse>(
    `/api/portfolios/${portfolioId}/instruments/${instrumentId}/history?${params}`,
  )
}

// Under the portfolio's history key, so invalidatePortfolio refreshes it
// after a transaction edit.
export function usePositionHistory(
  portfolioId: number,
  instrumentId: number,
  from: string,
  to: string,
) {
  return useQuery({
    queryKey: [...historyQueryKey(portfolioId), 'position', instrumentId, from, to] as const,
    queryFn: () => fetchPositionHistory(portfolioId, instrumentId, from, to),
    // As usePortfolioHistory: the same position's last range stays on screen
    // while a new one loads, another position starts empty.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === portfolioId && previousQuery.queryKey[4] === instrumentId
        ? previous
        : undefined,
  })
}
