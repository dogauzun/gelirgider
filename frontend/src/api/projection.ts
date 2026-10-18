import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import type { Goal } from './goal'
import type { PortfolioScope } from './summary'

// One month of the fan: `month` is YYYY-MM; low, mid and high are the 25th,
// 50th and 75th percentile paths in the goal's unit. Floats, an estimate.
export interface ProjectionMonth {
  month: string
  low: number
  mid: number
  high: number
}

// The goal's projection. `months` is empty when there is too little history.
// `reached_at` (YYYY-MM) is set when the median path reaches the amount;
// otherwise `monthly_gap` is the extra monthly flow, in `currency` (the
// reporting currency), that would.
export interface Projection {
  unit: Goal['unit']
  currency: string
  amount: string
  target_date: string
  months: ProjectionMonth[]
  reached_at: string | null
  monthly_gap: number | null
  progress: number
  history_months: number
}

// Enabled once the scope has a goal: without one the route answers 404.
export function useProjection(portfolioId: PortfolioScope, enabled: boolean) {
  return useQuery({
    queryKey: ['projection', portfolioId],
    queryFn: () => apiFetch<Projection>(`/api/portfolios/${portfolioId}/projection`),
    enabled,
  })
}

export const progressPercent = (progress: number) => `${Math.round(progress * 100)}%`
