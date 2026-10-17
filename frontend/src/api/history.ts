import { useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'
import type { PortfolioScope } from './summary'

export interface HistoryEntry {
  date: string
  total_value: string
  cash: string
  cost_basis: string
  // Set when the point holds an instrument with no price yet on its date;
  // that holding counts at cost, so the point is incomplete.
  price_unknown?: boolean
  // The benchmark's shadow portfolio value, a 2-decimal money string in the
  // reporting currency; absent when no benchmark is requested or it has no
  // value for the range (ADR 0012).
  benchmark_value?: string
  // Time-weighted return from the range's first point to this one, a
  // fraction string with 6 decimals: "0.000000" on the first point, equal to
  // returns.twr on the last. null on every point when returns.twr is null
  // (ADR 0011). The API always sends both fields; they are optional so that
  // chart points built from other series (the position page) still fit.
  twr?: string | null
  // The same for the benchmark's shadow series; null on every point without
  // a benchmark, when it has no value, or when its own twr is null (ADR 0012).
  benchmark_twr?: string | null
}

export interface HistoryResponse {
  currency: string
  points: HistoryEntry[]
  // Currencies excluded from at least one point in the range for lack of an
  // FX rate that day -- same field and same caveat as Summary.missing_rates:
  // it names the currency that was left out, not necessarily the one whose
  // rate is absent.
  missing_rates: string[]
  // Symbols held with no price yet on at least one point in the range, the
  // union behind the per-point price_unknown flags.
  missing_prices: string[]
  // Deposits minus withdrawals dated after the first point, in the reporting
  // currency: the part of last - first that is money put in rather than a
  // gain. Also holds the first-price jump of holdings the first
  // point had no price for, which is not the range's gain either.
  net_flow: string
  // The range's return by each method, as a fraction string with 6 decimals
  // ("0.250000" is 25%), null when the method is undefined (ADR 0011).
  returns: Record<ReturnMethod, string | null>
  // The requested benchmark's shadow-portfolio returns, null when none was
  // requested (ADR 0012). Its returns are all null when it has no level at
  // the range's first point. estimated_from is the first day (YYYY-MM-DD)
  // whose benchmark value is extrapolated, null when none is.
  benchmark: {
    code: Benchmark
    returns: Record<ReturnMethod, string | null>
    estimated_from: string | null
  } | null
  // The TÜFE change over the range, a fraction string with 6 decimals,
  // whatever the benchmark; null for a USD or EUR reporting currency or when
  // TÜFE has no level at the first point. inflation_estimated_from is the
  // first extrapolated day (YYYY-MM-DD) of it, null when none is. Optional
  // like twr.
  inflation?: string | null
  inflation_estimated_from?: string | null
}

export type ReturnMethod = 'simple' | 'twr' | 'mwr'
export const RETURN_METHODS: ReturnMethod[] = ['simple', 'twr', 'mwr']

// A fixed benchmark, or a position of the scope as instrument:<id>.
export type Benchmark = 'USD' | 'EUR' | 'XU100' | 'TUFE' | 'GOLD' | `instrument:${number}`

function fetchHistory(
  portfolioId: PortfolioScope,
  from: string,
  to: string,
  benchmark?: Benchmark | null,
): Promise<HistoryResponse> {
  const params = new URLSearchParams({ to })
  if (from) params.set('from', from)
  if (benchmark) params.set('benchmark', benchmark)
  return apiFetch<HistoryResponse>(`/api/portfolios/${portfolioId}/history?${params}`)
}

// historyQueryKey is the prefix shared by every date range, so invalidating it
// refetches whichever range the chart is showing.
export const historyQueryKey = (portfolioId: PortfolioScope) =>
  ['portfolios', portfolioId, 'history'] as const

export function usePortfolioHistory(
  portfolioId: PortfolioScope,
  from: string,
  to: string,
  benchmark?: Benchmark | null,
) {
  return useQuery({
    queryKey: [...historyQueryKey(portfolioId), from, to, benchmark ?? null] as const,
    queryFn: () => fetchHistory(portfolioId, from, to, benchmark),
    // A new range or benchmark keeps the last chart on screen until it
    // answers, instead of blanking it. Another portfolio starts
    // empty: its old line would be a wrong chart, not a stale one.
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === portfolioId ? previous : undefined,
  })
}

// One calendar month ("2026-09") or year ("2026") of the periods endpoint.
// Its returns are the history endpoint's for from..to (ADR 0011); partial
// is set on the first and the current bucket. benchmark is null without a
// benchmark or when it has no level on some day of the bucket (ADR 0012).
export interface PeriodBucket {
  period: string
  from: string
  to: string
  partial: boolean
  returns: Record<ReturnMethod, string | null>
  benchmark: Record<ReturnMethod, string | null> | null
}

// Every month and year from the first transaction to today, oldest first,
// with no gaps.
export interface PeriodReturns {
  currency: string
  months: PeriodBucket[]
  years: PeriodBucket[]
}

// Keyed under historyQueryKey, so whatever invalidates the history (a
// transaction, a manual price) refetches the periods too.
export function usePeriodReturns(scope: PortfolioScope, benchmark?: Benchmark | null) {
  return useQuery({
    queryKey: [...historyQueryKey(scope), 'periods', benchmark ?? null] as const,
    queryFn: () => {
      const query = benchmark ? `?${new URLSearchParams({ benchmark })}` : ''
      return apiFetch<PeriodReturns>(`/api/portfolios/${scope}/returns/periods${query}`)
    },
  })
}

// A day a portfolio's cash in currency reached a new low below zero; amount
// is the decimal deposit that keeps it at the previous low, rounded up to 9
// fraction digits.
export interface CashGap {
  portfolio_id: number
  date: string
  currency: string
  amount: string
}

// Keyed under historyQueryKey, so a transaction change refetches the gaps.
export function useCashGaps(scope: PortfolioScope) {
  return useQuery({
    queryKey: [...historyQueryKey(scope), 'cash-gaps'] as const,
    queryFn: () => apiFetch<CashGap[]>(`/api/portfolios/${scope}/cash-gaps`),
  })
}
