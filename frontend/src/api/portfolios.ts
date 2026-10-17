import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { t } from '../i18n/strings'
import { decimalUnits, unitsToDecimal } from '../lib/decimal'
import { toast } from '../lib/toast'

export interface Portfolio {
  id: number
  name: string
  created_at: string
  total_value: string
  // Cash in `currency`, and unrealized P/L, both leaving out the same
  // missing_rates currencies as total_value. unrealized_pl is null when every
  // currency contributing to it was excluded for lack of a rate -- the same
  // rule as Summary.unrealized_pl, and the caller renders MISSING rather than a
  // zero that reads as a real figure.
  cash: string
  unrealized_pl: string | null
  currency: string
  // Currencies excluded from total_value because no conversion factor could
  // be built -- same field and same caveat as Summary.missing_rates: it names
  // what was left out, not what is absent from the rate table, so never label
  // these as "currencies with no rate".
  missing_rates: string[]
}

// POST /api/portfolios answers with only these three fields (portfolioResponse
// in backend/internal/portfolio/handlers.go); the valuation fields on Portfolio
// are computed for the list only.
export type CreatedPortfolio = Pick<Portfolio, 'id' | 'name' | 'created_at'>

export const portfoliosQueryKey = ['portfolios'] as const

// The exact sum of one money field over the portfolios, as a decimal string:
// a Number sum rounds the cents once they pass 2^53 units.
export function sumPortfolios(
  portfolios: Portfolio[] | undefined,
  pick: (p: Portfolio) => string,
): string {
  return unitsToDecimal((portfolios ?? []).reduce((acc, p) => acc + decimalUnits(pick(p)), 0n))
}

function fetchPortfolios(): Promise<Portfolio[]> {
  return apiFetch<Portfolio[]>('/api/portfolios')
}

function createPortfolio(name: string): Promise<CreatedPortfolio> {
  return apiFetch<CreatedPortfolio>('/api/portfolios', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
}

function renamePortfolio(id: number, name: string): Promise<void> {
  return apiFetch<void>(`/api/portfolios/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ name }),
  })
}

// force=true deletes the portfolio's transactions along with it.
// The server refuses with 409 without it, so the flag is only ever sent after
// the user confirms the cascade by name.
function deletePortfolio(id: number, force = false): Promise<void> {
  return apiFetch<void>(`/api/portfolios/${id}${force ? '?force=true' : ''}`, { method: 'DELETE' })
}

export function usePortfolios() {
  return useQuery({ queryKey: portfoliosQueryKey, queryFn: fetchPortfolios })
}

export function useCreatePortfolio() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => createPortfolio(name),
    // eslint-disable-next-line no-restricted-syntax -- invalidates the list key only
    onSuccess: () => queryClient.invalidateQueries({ queryKey: portfoliosQueryKey }),
  })
}

export function useRenamePortfolio() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => renamePortfolio(id, name),
    // eslint-disable-next-line no-restricted-syntax -- invalidates the list key only
    onSuccess: () => queryClient.invalidateQueries({ queryKey: portfoliosQueryKey }),
  })
}

export function useDeletePortfolio() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, force }: { id: number; force?: boolean }) => deletePortfolio(id, force),
    // Both delete paths (a home card, the portfolio page) only move focus
    // afterwards, so the toast's status region says what happened.
    // The deleted portfolio's own queries are left out: its page is still on
    // screen until the list refetch redirects it, and refetching them can only
    // answer 404. Not removed either, or that page would build and
    // fetch them again on its next render; they go once unused.
    onSuccess: (_, { id }) => {
      toast(t('portfolios.deleted'))
      // eslint-disable-next-line no-restricted-syntax -- invalidates the list key only
      return queryClient.invalidateQueries({
        queryKey: portfoliosQueryKey,
        predicate: (query) => query.queryKey[1] !== id,
      })
    },
  })
}
