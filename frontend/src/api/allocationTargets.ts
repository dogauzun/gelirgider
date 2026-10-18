import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import type { PortfolioScope } from './summary'

export interface AllocationTarget {
  asset_class: string
  weight: string
}

const allocationTargetsQueryKey = (portfolioId: PortfolioScope) =>
  ['allocation-targets', portfolioId] as const

export function useAllocationTargets(portfolioId: PortfolioScope) {
  return useQuery({
    queryKey: allocationTargetsQueryKey(portfolioId),
    queryFn: async () =>
      (
        await apiFetch<{ targets: AllocationTarget[] }>(
          `/api/portfolios/${portfolioId}/allocation-targets`,
        )
      ).targets,
  })
}

export function useReplaceAllocationTargets(portfolioId: PortfolioScope) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (targets: AllocationTarget[]) =>
      apiFetch<{ targets: AllocationTarget[] }>(
        `/api/portfolios/${portfolioId}/allocation-targets`,
        { method: 'PUT', body: JSON.stringify({ targets }) },
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: allocationTargetsQueryKey(portfolioId) }),
  })
}
