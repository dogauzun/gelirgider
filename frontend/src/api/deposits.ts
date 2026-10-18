import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { invalidatePortfolio } from './transactions'

// A term deposit of a portfolio. Decimals are exact strings; rate and
// stopaj_rate are fractions (0.45 for 45%); the money fields have 2 decimals.
// `status` is matured once a sell is booked on it, else due from the maturity
// date, else active.
export interface Deposit {
  id: number
  instrument_id: number
  currency: string
  principal: string
  rate: string
  stopaj_rate: string
  start_date: string
  maturity_date: string
  // Principal plus the net interest accrued up to today.
  value: string
  accrued_interest: string
  net_interest_at_maturity: string
  // Principal plus the net interest at maturity.
  payout: string
  status: 'active' | 'due' | 'matured'
}

// The POST body. `fund` is sent only when true: a user who already recorded
// the cash must not get it twice.
export interface DepositInput {
  currency: string
  principal: string
  rate: string
  stopaj_rate: string
  start_date: string
  maturity_date: string
  note?: string
  fund?: true
}

// Under the portfolio's prefix, so invalidatePortfolio refreshes it along
// with the summary, history and transactions a deposit changes.
const depositsQueryKey = (portfolioId: number) => ['portfolios', portfolioId, 'deposits'] as const

export function useDeposits(portfolioId: number) {
  return useQuery({
    queryKey: depositsQueryKey(portfolioId),
    queryFn: () => apiFetch<Deposit[]>(`/api/portfolios/${portfolioId}/deposits`),
  })
}

export function useCreateDeposit(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: DepositInput) =>
      apiFetch<Deposit>(`/api/portfolios/${portfolioId}/deposits`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

export function useDeleteDeposit(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<void>(`/api/deposits/${id}`, { method: 'DELETE' }),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}
