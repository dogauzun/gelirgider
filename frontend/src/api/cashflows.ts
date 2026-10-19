import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { historyQueryKey } from './history'
import type { PortfolioScope } from './summary'

// One calendar month of the cash flow endpoint, or its totals: 2-decimal
// strings in the reporting currency (ADR 0035).
export interface CashFlowBucket {
  period: string
  deposits: string
  withdrawals: string
  net: string
  dividends: string
  interest: string
  fees: string
}

export interface CashFlowsResponse {
  currency: string
  months: CashFlowBucket[]
  totals: CashFlowBucket
  // The currencies whose rows had no rate and are left out of every field.
  missing_rates: string[]
}

// Keyed under historyQueryKey, so what invalidates the history (a
// transaction) refetches the months too.
export function useCashFlows(scope: PortfolioScope) {
  return useQuery({
    queryKey: [...historyQueryKey(scope), 'cashflows'] as const,
    queryFn: () => apiFetch<CashFlowsResponse>(`/api/portfolios/${scope}/cashflows`),
  })
}
