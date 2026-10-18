import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { invalidatePortfolio, invalidatePortfolios } from './transactions'

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
  // The id of the deposit this one renews, null for one opened on its own.
  renewed_from: number | null
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

// A deposit at or past its maturity with no sell yet. gross, stopaj and net
// are the interest over the whole term, 2 decimals.
export interface DueDeposit {
  id: number
  currency: string
  principal: string
  maturity_date: string
  gross: string
  stopaj: string
  net: string
}

// Under the deposits key, so every deposit edit refetches it too. The all
// scope has no route: Tüm portföyler asks each portfolio.
const dueDepositsQueryKey = (portfolioId: number) =>
  [...depositsQueryKey(portfolioId), 'due'] as const

// Each portfolio's due deposits, in the order given.
export function useDueDepositsOf(portfolioIds: number[]) {
  return useQueries({
    queries: portfolioIds.map((id) => ({
      queryKey: dueDepositsQueryKey(id),
      queryFn: () => apiFetch<DueDeposit[]>(`/api/portfolios/${id}/deposits/due`),
    })),
  })
}

// Books the principal back to cash and the interest as a dividend. It
// changes the summary, history and transactions of the deposit's portfolio
// and of the all scope, so every portfolio query is refreshed.
export function useMatureDeposit() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<void>(`/api/deposits/${id}/mature`, { method: 'POST' }),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}

// The renew POST body: the new terms, as fractions. The server takes the
// principal (old principal plus net interest), currency and start date from
// the deposit being renewed.
export interface RenewInput {
  rate: string
  stopaj_rate: string
  maturity_date: string
}

// Opens the next deposit from a due or matured one, booking the old maturity
// first when it has none. That can book a dividend, which the all scope
// shows, so every portfolio query is refreshed, like the mature hook.
export function useRenewDeposit(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: RenewInput) =>
      apiFetch<Deposit>(`/api/deposits/${id}/renew`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}
