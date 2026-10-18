import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import { decimalUnits, unitsToDecimal } from '../lib/decimal'
import type { PortfolioScope } from './summary'
import { createTransaction, invalidatePortfolios, type TransactionInput } from './transactions'

// A dividend the portfolio should have received but has no transaction for
// (ADR 0009). Decimals are exact strings; gross, stopaj and net have 2
// decimals. `date` is the ex-date.
export interface ExpectedDividend {
  instrument_id: number
  symbol: string
  market: string
  date: string
  quantity: string
  per_share: string
  gross: string
  stopaj: string
  net: string
  currency: string
}

const expectedDividendsQueryKey = (portfolioId: PortfolioScope) =>
  ['portfolios', portfolioId, 'dividends', 'expected'] as const

function fetchExpectedDividends(portfolioId: number): Promise<ExpectedDividend[]> {
  return apiFetch<ExpectedDividend[]>(`/api/portfolios/${portfolioId}/dividends/expected`)
}

// Expected dividends are per portfolio: the `all` scope makes no request;
// Tüm portföyler asks each portfolio.
export function useExpectedDividends(portfolioId: PortfolioScope) {
  return useQuery({
    queryKey: expectedDividendsQueryKey(portfolioId),
    queryFn: () => fetchExpectedDividends(portfolioId as number),
    enabled: portfolioId !== 'all',
  })
}

// Each portfolio's expected dividends, in the order given.
export function useExpectedDividendsOf(portfolioIds: number[]) {
  return useQueries({
    queries: portfolioIds.map((id) => ({
      queryKey: expectedDividendsQueryKey(id),
      queryFn: () => fetchExpectedDividends(id),
    })),
  })
}

// One non-empty month ("2026-09") or year ("2026") of dividends received,
// in the reporting currency at each row's own-date rate. Amounts have 2
// decimals; `interest` is dividend rows with no instrument.
export interface DividendBucket {
  period: string
  gross: string
  tax: string
  net: string
  interest: string
}

export interface DividendInstrumentTotal {
  instrument_id: number
  symbol: string
  name: string
  count: number
  net: string
  last_date: string
}

// `missing_rates` lists currencies whose rows had no rate on their date and
// are left out of every total.
export interface DividendTimeline {
  currency: string
  months: DividendBucket[]
  years: DividendBucket[]
  // The current month and the eleven before it, summed before rounding.
  last_12_months: DividendBucket
  by_instrument: DividendInstrumentTotal[]
  missing_rates: string[]
}

const dividendTimelineQueryKey = (portfolioId: PortfolioScope) =>
  ['portfolios', portfolioId, 'dividends', 'timeline'] as const

// Per portfolio like expected dividends: the `all` scope makes no request.
export function useDividendTimeline(portfolioId: PortfolioScope) {
  return useQuery({
    queryKey: dividendTimelineQueryKey(portfolioId),
    queryFn: () => apiFetch<DividendTimeline>(`/api/portfolios/${portfolioId}/dividends/timeline`),
    enabled: portfolioId !== 'all',
  })
}

// One expected payment of the next twelve months. `date` is null when the
// month is only estimated from the last twelve months; `net_reporting` is
// null when the row's currency has no rate.
export interface DividendForecastRow {
  instrument_id: number
  symbol: string
  market: string
  period: string
  date: string | null
  planned: boolean
  quantity: string
  per_share: string
  gross: string
  stopaj: string
  net: string
  currency: string
  net_reporting: string | null
}

// `months` always has twelve entries, the reporting-currency net of each
// ("2026-10"); `missing_rates` lists currencies left out of them.
export interface DividendForecast {
  currency: string
  months: { period: string; net: string }[]
  rows: DividendForecastRow[]
  missing_rates: string[]
}

// The rows' reporting-currency net added exactly; null when none has a rate.
export function expectedNetOf(rows: DividendForecastRow[]): string | null {
  const nets = rows.flatMap((r) => (r.net_reporting === null ? [] : [r.net_reporting]))
  if (nets.length === 0) return null
  return unitsToDecimal(nets.reduce((sum, n) => sum + decimalUnits(n), 0n))
}

export function useDividendForecast(portfolioId: PortfolioScope) {
  return useQuery({
    queryKey: ['portfolios', portfolioId, 'dividends', 'forecast'] as const,
    queryFn: () => apiFetch<DividendForecast>(`/api/portfolios/${portfolioId}/dividends/forecast`),
    enabled: portfolioId !== 'all',
  })
}

function dismissExpectedDividend(
  portfolioId: number,
  row: Pick<ExpectedDividend, 'instrument_id' | 'date'>,
): Promise<void> {
  return apiFetch<void>(`/api/portfolios/${portfolioId}/dividends/expected/dismiss`, {
    method: 'POST',
    body: JSON.stringify({ instrument_id: row.instrument_id, date: row.date }),
  })
}

// The expected-dividend panel and Özet's attention card book or dismiss a
// suggestion in the portfolio it came from. Both stay pending until the
// refetch: before that the booked row is still listed, and POST transactions
// has no duplicate check, so a second click would book it twice. isPending
// lags the click by a tick, so callers also guard synchronously. Every
// portfolio query is refreshed, failed or not, since Tüm portföyler's figures
// change too.
export function useAddExpectedDividend() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ portfolioId, input }: { portfolioId: number; input: TransactionInput }) =>
      createTransaction(portfolioId, input),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}

// "Tümünü ekle": books the inputs one after another, so a failure stops the
// rest, and refreshes once at the end. Answers how many were saved and the
// error that stopped it, if any; it never rejects.
export function useAddExpectedDividends() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      portfolioId,
      inputs,
    }: {
      portfolioId: number
      inputs: TransactionInput[]
    }): Promise<{ saved: number; error: Error | null }> => {
      let saved = 0
      for (const input of inputs) {
        try {
          await createTransaction(portfolioId, input)
        } catch (error) {
          return { saved, error: error as Error }
        }
        saved++
      }
      return { saved, error: null }
    },
    onSettled: () => invalidatePortfolios(queryClient),
  })
}

export function useDismissExpectedDividend() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ portfolioId, dividend }: { portfolioId: number; dividend: ExpectedDividend }) =>
      dismissExpectedDividend(portfolioId, dividend),
    onSettled: () => invalidatePortfolios(queryClient),
  })
}
