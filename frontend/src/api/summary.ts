import { useQueries, useQuery } from '@tanstack/react-query'
import { apiFetch } from '../api'

export interface Position {
  instrument_id: number
  symbol: string
  name: string
  // BIST, US, TEFAS, CRYPTO or GOLD.
  market: string
  // The instrument's stored class (equity, fund, commodity, crypto, cash or
  // deposit); null when it has none.
  asset_class: string | null
  // The instrument's sector key (financials, energy, ... or a fund_* key for
  // a TEFAS fund); 'unknown' when it has none, never null.
  sector: string
  currency: string
  quantity: string
  avg_cost: string
  cost_basis: string
  last_price: string | null
  has_price: boolean
  // Where last_price came from ('manual' for this user's own entry, else the
  // fetching provider) and the date it carries. Both null when there is no
  // price. A manual price is tagged and removable by this date.
  price_source: string | null
  price_date: string | null
  // The latest close before price_date and its date, and the move between
  // the two: the move of the position as held at the end of price_date, with
  // shares bought or sold on price_date moving from their own trade price and
  // later trades ignored, in the position's currency, then in the reporting
  // currency (null there too without a rate). All null
  // when either price is missing or the position is closed.
  prev_close: string | null
  prev_price_date: string | null
  day_change: string | null
  day_change_reporting: string | null
  // The value of the shares day_change counts, in the position's currency:
  // what day_change is a percentage of. Null when day_change is null.
  day_change_base: string | null
  market_value: string
  unrealized_pl: string
  realized_pl: string
  // The cost of every share sold, in the position's currency: the base of a
  // closed position's realized percent. Zero or below after a short. Optional
  // until the backend sends it, so test it with != null.
  realized_cost?: string
  // Null when this position's currency has no FX rate for the valuation
  // date (see missing_rates on Summary) -- excluded from reporting totals.
  market_value_reporting: string | null
  unrealized_pl_reporting: string | null
  realized_pl_reporting: string | null
  // unrealized_pl_reporting plus realized_pl_reporting, summed unrounded and
  // rounded once on the server, so it can be a cent off the two rounded
  // fields' sum. Null without a rate.
  total_pl_reporting: string | null
  // The currency part of this position's gain in the reporting currency:
  // cost basis at today's rate less its trade-date cost (ADR 0003, Phase 16).
  // Null for a position in the reporting currency or with no usable rate.
  // Optional until the backend sends it, so test it with != null.
  fx_pl_reporting?: string | null
  // A closed position, listed only for its realized P/L. Quantity 0 alone is
  // not closed: in the all-portfolios view a long and an equal short in two
  // portfolios sum to 0 and stay open.
  closed: boolean
  // Splits that changed this position, oldest first; ratio is new shares per
  // old share as an exact decimal string. Optional until the backend sends it,
  // so read it as `splits ?? []`.
  splits?: { date: string; ratio: string }[]
}

export interface Summary {
  // The user's reporting currency; every *_reporting field and the totals
  // below are expressed in it.
  currency: string
  cash: string
  cash_by_currency: Record<string, string>
  // Each cash_by_currency entry converted into the reporting currency, keyed
  // by its own currency. A currency with no rate is left out (it is named in
  // missing_rates), the same way `cash` leaves it out.
  cash_by_currency_reporting: Record<string, string>
  total_value: string
  // The open positions' market value and cost, leaving out the same
  // missing_rates currencies as total_value. Each is rounded once on the
  // server; total_value less cash less unrealized_pl can be a cent off.
  positions_value: string
  cost_basis: string
  // Null when every currency contributing to the total was excluded from it
  // for lack of a rate: the sum is then 0.00 only because nothing was added
  // to it, so render MISSING rather than a wrong zero. The page could work this
  // out itself for cash and total value but not for realized P/L,
  // which also counts closed positions.
  unrealized_pl: string | null
  realized_pl: string | null
  // The currency move on foreign cash and on the cost of foreign positions,
  // which neither P/L figure above holds. Null when the portfolio
  // has no currency besides the reporting one: the page then hides it.
  fx_pl: string | null
  // The open positions' day_change_reporting plus the price-date move of
  // closed positions sold out on or after it, summed unrounded and rounded
  // once on the server, so it can be a cent off the rows' sum. A
  // position with no previous close or no rate is left out; null when none
  // is left, so render MISSING, not a zero. Optional until the backend sends
  // it, so test it with != null.
  day_change?: string | null
  // Currencies excluded from the totals above because no conversion factor
  // could be built for the valuation date. It names what was left out, not
  // what is absent from the rate table: conversion divides by the reporting
  // currency's rate, so a missing USD rate lists TRY even though TRY needs
  // no rate. Never label these as "currencies with no rate".
  missing_rates: string[]
  // Open and closed positions both. A closed one (sold down to zero, or a
  // dividend on an instrument no longer held) has quantity and market value
  // zero and only its realized P/L set, so the page can account for the
  // realized_pl above instead of totalling the open rows alone.
  positions: Position[]
  // What the user put in, took out, paid and earned.
  stats: SummaryStats
}

// One currency's totals as 2-decimal strings, each a positive amount. fees
// counts every fee paid; dividends_tax is the stopaj withheld from dividends
// on an instrument; interest is instrument-less dividends, net of their fee.
interface StatsFields {
  deposits: string
  withdrawals: string
  // deposits less withdrawals, and dividends_gross less dividends_tax, each
  // worked out unrounded and rounded once on the server, so either can be a
  // cent off the rounded fields' difference.
  net_contribution: string
  fees: string
  dividends_gross: string
  dividends_tax: string
  dividends_net: string
  interest: string
}

export interface SummaryStats {
  // The earliest transaction date in scope; null with no transactions.
  first_transaction_date: string | null
  by_currency: Record<string, StatsFields>
  // by_currency summed into the reporting currency at today's rate. Null
  // when a currency with a nonzero figure has no rate (see missing_rates).
  reporting: StatsFields | null
}

// A portfolio id, or 'all' for every portfolio of the user together (ADR 0014).
export type PortfolioScope = number | 'all'

export const summaryQueryKey = (portfolioId: PortfolioScope) =>
  ['portfolios', portfolioId, 'summary'] as const

function fetchSummary(portfolioId: PortfolioScope): Promise<Summary> {
  return apiFetch<Summary>(`/api/portfolios/${portfolioId}/summary`)
}

export function usePortfolioSummary(portfolioId: PortfolioScope, enabled = true) {
  return useQuery({
    queryKey: summaryQueryKey(portfolioId),
    queryFn: () => fetchSummary(portfolioId),
    enabled,
  })
}

// One summary per portfolio, in the order given.
export function usePortfolioSummaries(portfolioIds: number[]) {
  return useQueries({
    queries: portfolioIds.map((id) => ({
      queryKey: summaryQueryKey(id),
      queryFn: () => fetchSummary(id),
    })),
  })
}
