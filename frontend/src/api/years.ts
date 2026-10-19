import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { historyQueryKey, type Benchmark, type ReturnMethod } from './history'
import type { PortfolioScope } from './summary'

// One calendar year of the years endpoint (ADR 0038): money as 2-decimal
// strings in the reporting currency, returns as ratios.
export interface YearRow {
  year: string
  from: string
  to: string
  partial: boolean
  opening: string
  closing: string
  net_flow: string
  gain: string
  price: string | null
  income: string | null
  fx: string | null
  cash_fx: string | null
  returns: Record<ReturnMethod, string | null>
  benchmark: Record<ReturnMethod, string | null> | null
  inflation: string | null
}

export interface YearsResponse {
  currency: string
  years: YearRow[]
  // The currencies left out of the totals on some days.
  missing_rates: string[]
  inflation_estimated_from: string | null
}

// Keyed under historyQueryKey, so a transaction change refetches the years.
export function useYears(scope: PortfolioScope, benchmark?: Benchmark | null) {
  return useQuery({
    queryKey: [...historyQueryKey(scope), 'years', benchmark ?? null] as const,
    queryFn: () => {
      const query = benchmark ? `?${new URLSearchParams({ benchmark })}` : ''
      return apiFetch<YearsResponse>(`/api/portfolios/${scope}/years${query}`)
    },
  })
}
