import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from '@tanstack/react-query'
import { apiFetch, apiFetchWithHeaders } from '../api'
import { portfoliosQueryKey } from './portfolios'
import type { PortfolioScope } from './summary'

export type TransactionType = 'buy' | 'sell' | 'dividend' | 'deposit' | 'withdrawal' | 'fee'

export interface Transaction {
  id: number
  portfolio_id: number
  instrument_id: number | null
  instrument_symbol: string | null
  instrument_name: string | null
  type: TransactionType
  date: string
  quantity: string | null
  price: string | null
  amount: string
  fee: string
  currency: string
  note: string | null
  created_at: string
  // The import this transaction came from, null when it was entered by hand.
  // Read-only: it is set by the commit endpoint, never sent back.
  import_id: number | null
}

// TransactionInput is the writable shape for create/update. Fields that
// don't apply to a given type (see backend/internal/transaction/handlers.go
// fromRequest) are simply omitted rather than sent as null/empty.
export interface TransactionInput {
  instrument_id?: number
  type: TransactionType
  date: string
  quantity?: string
  price?: string
  amount?: string
  fee?: string
  currency?: string
  note?: string
}

const transactionsQueryKey = (portfolioId: number) =>
  ['portfolios', portfolioId, 'transactions'] as const

// Rows per request. The server defaults to 100 and caps at 500 (see
// backend/internal/transaction/handlers.go); 100 keeps the response and the
// rendered table small on a portfolio with thousands of transactions.
const PAGE_SIZE = 100

export interface TransactionPage {
  transactions: Transaction[]
  // Cursor for the next page, from the X-Next-Cursor response header. null on
  // the last page: the server only sends it when a further page has rows.
  nextCursor: string | null
}

// ImportFilter narrows the list to one import's rows (an id from
// useImports) or, as 'none', to the rows entered by hand.
export type ImportFilter = number | 'none'

// TransactionFilter narrows the list. types to those transaction types,
// instrumentId to one instrument's rows, from and to to an
// inclusive YYYY-MM-DD date range, and importId to one import's
// rows or the hand-entered ones. Every field is optional: an empty
// filter is the whole list.
export interface TransactionFilter {
  types?: TransactionType[]
  instrumentId?: number
  from?: string
  to?: string
  importId?: ImportFilter
}

// filterQuery is the list's filters as query parameters, the same for a page
// of the list and for the CSV export, so the export holds the rows the list
// shows.
function filterQuery({
  types,
  instrumentId,
  from,
  to,
  importId,
}: TransactionFilter): URLSearchParams {
  const query = new URLSearchParams()
  // The filter has to be a query parameter: paging is server-side, so
  // filtering on the client would only filter the loaded pages.
  if (types && types.length > 0) query.set('type', types.join(','))
  if (instrumentId) query.set('instrument_id', String(instrumentId))
  if (importId) query.set('import_id', String(importId))
  // Inclusive YYYY-MM-DD bounds.
  if (from) query.set('from', from)
  if (to) query.set('to', to)
  return query
}

async function fetchTransactions(
  portfolioId: number,
  before?: string,
  filter: TransactionFilter = {},
): Promise<TransactionPage> {
  const query = new URLSearchParams({ limit: String(PAGE_SIZE) })
  if (before) query.set('before', before)
  for (const [key, value] of filterQuery(filter)) {
    query.set(key, value)
  }
  const { data, headers } = await apiFetchWithHeaders<Transaction[]>(
    `/api/portfolios/${portfolioId}/transactions?${query}`,
  )
  return { transactions: data, nextCursor: headers.get('X-Next-Cursor') }
}

// transactionsCsvUrl is the CSV export link for the list with these filters.
// A plain link, not a fetch: the browser sends the session cookie and saves
// the file under the server's name.
export function transactionsCsvUrl(portfolioId: number, filter: TransactionFilter = {}): string {
  const search = filterQuery(filter).toString()
  return `/api/portfolios/${portfolioId}/transactions.csv${search ? `?${search}` : ''}`
}

export function createTransaction(
  portfolioId: number,
  input: TransactionInput,
): Promise<Transaction> {
  return apiFetch<Transaction>(`/api/portfolios/${portfolioId}/transactions`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

function updateTransaction(id: number, input: TransactionInput): Promise<Transaction> {
  return apiFetch<Transaction>(`/api/transactions/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

// keepalive: the transactions page defers a delete behind its undo toast and
// sends it when the page is left, which would otherwise cancel the request.
function deleteTransaction(id: number): Promise<void> {
  return apiFetch<void>(`/api/transactions/${id}`, { method: 'DELETE', keepalive: true })
}

// flattenPages is useTransactions' select. Module level, not inline: the
// observer reuses the selected rows only while select is the same function.
const flattenPages = (data: InfiniteData<TransactionPage>) =>
  data.pages.flatMap((page) => page.transactions)

// Pages arrive newest first. Each mutation still invalidates the query, which
// refetches the pages already loaded -- bounded now at PAGE_SIZE rows each
// instead of the whole history in one response.
//
// The filter's fields join the query key after transactionsQueryKey's
// prefix, so changing the filter starts a fresh infinite query at the first
// page while a mutation can still invalidate every filter at once through
// the prefix. data is the loaded pages' rows in that order.
export function useTransactions(portfolioId: number, filter: TransactionFilter = {}) {
  return useInfiniteQuery({
    queryKey: [
      ...transactionsQueryKey(portfolioId),
      (filter.types ?? []).join(','),
      filter.instrumentId ?? null,
      filter.from ?? null,
      filter.to ?? null,
      filter.importId ?? null,
    ],
    queryFn: ({ pageParam }) => fetchTransactions(portfolioId, pageParam, filter),
    // Switching the filter is a new query key, so it restarts at the first
    // page. Holding the previous rows until it answers keeps the filter
    // control itself on screen instead of unmounting it mid-click.
    placeholderData: keepPreviousData,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    select: flattenPages,
  })
}

export function useCreateTransaction(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: TransactionInput) => createTransaction(portfolioId, input),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

export function useUpdateTransaction(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: TransactionInput }) =>
      updateTransaction(id, input),
    onSuccess: () => invalidatePortfolio(queryClient, portfolioId),
  })
}

// The delete may land after its page is gone (it waits behind an undo toast):
// once the open lists have refetched, cached lists that are not
// shown are dropped, so coming back fetches them rather than painting the
// row again.
export function useDeleteTransaction(portfolioId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteTransaction(id),
    onSuccess: async () => {
      await invalidatePortfolio(queryClient, portfolioId)
      queryClient.removeQueries({ queryKey: transactionsQueryKey(portfolioId), type: 'inactive' })
    },
  })
}

// useFillCashGaps records the scope's cash gaps (useCashGaps) as deposits
// carrying note; the server recomputes them, so no amounts are sent. It
// answers the new transaction ids. In the all scope the deposits land in
// several portfolios, so every portfolio query is refreshed.
export function useFillCashGaps(scope: PortfolioScope) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (note: string) =>
      apiFetch<number[]>(`/api/portfolios/${scope}/cash-gaps`, {
        method: 'POST',
        body: JSON.stringify({ note }),
      }),
    onSuccess: () => invalidatePortfolios(queryClient),
  })
}

// invalidatePortfolio refreshes everything cached for one portfolio: a
// transaction changes the transactions, summary, history, position history,
// expected dividends and dividend timeline, and stale data would paint wrong
// totals until something else refetches. They all sit under
// ['portfolios', portfolioId], so one prefix match covers them and any key
// added there later. The portfolio list carries each portfolio's value, which
// the always-mounted sidebar shows; it is matched exactly because the prefix
// above is not its own. The all scope's keys are not touched.
export function invalidatePortfolio(
  queryClient: ReturnType<typeof useQueryClient>,
  portfolioId: number,
) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: portfoliosQueryKey, exact: true }),
    queryClient.invalidateQueries({ queryKey: [...portfoliosQueryKey, portfolioId] }),
  ])
}

// Prefix match: also hits ['portfolios', 'all', ...] and every per-portfolio key.
export const invalidatePortfolios = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: portfoliosQueryKey })

export function useInvalidatePortfolios() {
  const queryClient = useQueryClient()
  return () => invalidatePortfolios(queryClient)
}
