import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { compareDecimal } from '../lib/decimal'
import { invalidatePortfolio, invalidatePortfolios } from './transactions'

// A monthly saving plan of a portfolio: a buy of the instrument, or with no
// instrument a cash deposit. Decimals are exact strings; dates are market
// dates. `status` is ended once next_date is past end_date, due from next_date,
// else active.
export interface Plan {
  id: number
  portfolio_id: number
  instrument_id: number | null
  symbol: string | null
  market: string | null
  currency: string
  amount: string
  day_of_month: number
  start_date: string
  end_date: string | null
  next_date: string
  // The amount is also booked as a cash deposit before each buy.
  fund: boolean
  note: string
  status: 'active' | 'due' | 'ended'
}

// The POST body. A buy plan sends instrument_id and no currency, a deposit
// plan the currency and no instrument. `fund` is sent only when true.
export interface PlanInput {
  instrument_id?: number
  currency?: string
  amount: string
  day_of_month: number
  start_date: string
  end_date?: string
  fund?: true
  note?: string
}

// The PUT body. The instrument, currency and start date are fixed; an empty
// end_date clears the stored one.
export interface PlanUpdate {
  amount: string
  day_of_month: number
  end_date: string
  fund: boolean
  note: string
}

// Under the portfolio's prefix, so invalidatePortfolio refreshes it along
// with the summary, history and transactions a plan changes.
const plansQueryKey = (portfolioId: number) => ['portfolios', portfolioId, 'plans'] as const

export function usePlans(portfolioId: number) {
  return useQuery({
    queryKey: plansQueryKey(portfolioId),
    queryFn: () => apiFetch<Plan[]>(`/api/portfolios/${portfolioId}/plans`),
  })
}

export function useCreatePlan(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: PlanInput) =>
      apiFetch<Plan>(`/api/portfolios/${portfolioId}/plans`, {
        method: 'POST',
        body: JSON.stringify(input),
      }),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

export function useUpdatePlan(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: PlanUpdate }) =>
      apiFetch<Plan>(`/api/plans/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

export function useDeletePlan(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<void>(`/api/plans/${id}`, { method: 'DELETE' }),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

// The occurrence a plan has come to and what booking it would write. For a
// buy plan with no close on or before `date` the price, price_date, quantity
// and booked_amount are null; a close above the amount gives quantity "0". A
// deposit plan has no instrument, price or quantity and books its amount.
export interface DuePlan {
  plan_id: number
  instrument_id: number | null
  symbol: string | null
  market: string | null
  currency: string
  amount: string
  date: string
  // The occurrences from `date` through today.
  missed: number
  price: string | null
  price_date: string | null
  quantity: string | null
  booked_amount: string | null
}

// A deposit plan always books; a buy needs a close and an amount that buys
// at least one unit, else the book request answers 409 (ADR 0030).
export const canBookPlan = (due: DuePlan) =>
  due.instrument_id === null || (due.quantity !== null && compareDecimal(due.quantity, '0') > 0)

// Under the plans key, so every plan edit refetches it too. The all scope has
// no route: Tüm portföyler asks each portfolio.
const duePlansQueryKey = (portfolioId: number) => [...plansQueryKey(portfolioId), 'due'] as const

// Each portfolio's due plans, in the order given.
export function useDuePlansOf(portfolioIds: number[]) {
  return useQueries({
    queries: portfolioIds.map((id) => ({
      queryKey: duePlansQueryKey(id),
      queryFn: () => apiFetch<DuePlan[]>(`/api/portfolios/${id}/plans/due`),
    })),
  })
}

// Books the occurrence and moves the plan on. It changes the summary, history
// and transactions of the plan's portfolio and of the all scope, so every
// portfolio query is refreshed.
export function useBookPlan() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<Plan>(`/api/plans/${id}/book`, { method: 'POST' }),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}

// Moves the plan to its next occurrence without booking anything.
export function useSkipPlan() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch<Plan>(`/api/plans/${id}/skip`, { method: 'POST' }),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}
