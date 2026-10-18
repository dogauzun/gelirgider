import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, apiFetch } from '../api'
import { projectionQueryKey } from './projection'
import type { PortfolioScope } from './summary'

// The savings goal of a portfolio, or of the all scope: reach `amount` in
// `unit` by `target_date` (YYYY-MM-DD). `nominal` is the reporting currency,
// `real` is TRY in today's prices (TRY reporting currency only), `usd` needs a
// non-USD reporting currency.
export interface Goal {
  amount: string
  unit: 'nominal' | 'real' | 'usd'
  target_date: string
}

const goalQueryKey = (portfolioId: PortfolioScope) => ['portfolios', portfolioId, 'goal'] as const

const path = (portfolioId: PortfolioScope) => `/api/portfolios/${portfolioId}/goal`

// A scope with no goal answers 404 "goal not found": that is null, not an
// error. A 404 for a missing portfolio stays an error.
export function useGoal(portfolioId: PortfolioScope, enabled = true) {
  return useQuery({
    queryKey: goalQueryKey(portfolioId),
    enabled,
    queryFn: async () => {
      try {
        return await apiFetch<Goal>(path(portfolioId))
      } catch (error) {
        if (
          error instanceof ApiError &&
          error.status === 404 &&
          error.body.error === 'goal not found'
        ) {
          return null
        }
        throw error
      }
    },
  })
}

// The goal changes what the projection shows, so both queries refetch; the
// projection key is the one the goal card uses.
function useGoalMutation<T>(portfolioId: PortfolioScope, request: (input: T) => Promise<unknown>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: request,
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: goalQueryKey(portfolioId) }),
        queryClient.invalidateQueries({ queryKey: projectionQueryKey(portfolioId) }),
      ]),
  })
}

export function useSetGoal(portfolioId: PortfolioScope) {
  return useGoalMutation(portfolioId, (goal: Goal) =>
    apiFetch<Goal>(path(portfolioId), { method: 'PUT', body: JSON.stringify(goal) }),
  )
}

export function useDeleteGoal(portfolioId: PortfolioScope) {
  return useGoalMutation(portfolioId, () => apiFetch<void>(path(portfolioId), { method: 'DELETE' }))
}
