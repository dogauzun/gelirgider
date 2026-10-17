import type { ExpectedDividend } from '../api/dividends'
import type { Summary } from '../api/summary'
import { daysBetween } from './date'
import { compareDecimal, decimalUnits } from './decimal'
import { currencyTotals, dayRatio, holdingsTotal, shareOf } from './positions'

// The thresholds of the "Dikkat" card (spec: UI v2 core pages). A share is
// flagged only above its threshold, never at it.
export const ATTENTION = {
  staleManualDays: 7,
  positionShare: 0.25,
  cashShare: 0.4,
  tryCashShare: 0.4,
  currencyShare: 0.5,
  movers: 2,
} as const

// The JSON shape of GET /api/me/attention-rules: per rule an optional on/off
// flag and an optional threshold, each falling back to ATTENTION.
type Rule = { enabled?: boolean; value?: number }
export type AttentionRules = Partial<
  Record<
    | 'stale_manual_days'
    | 'position_share'
    | 'cash_share'
    | 'try_cash_share'
    | 'no_hedge'
    | 'currency_share'
    | 'movers',
    Rule
  >
>

// The threshold of a rule, or null when the owner switched it off.
function threshold(rules: AttentionRules, key: keyof AttentionRules, fallback: number) {
  const rule = rules[key]
  return rule?.enabled === false ? null : (rule?.value ?? fallback)
}

// How many movers the Bugün group shows, 0 when the owner switched it off.
export function moverCount(rules: AttentionRules = {}): number {
  return threshold(rules, 'movers', ATTENTION.movers) ?? 0
}

// `portfolio` names the portfolio on Tüm portföyler, where items of several
// portfolios share one card. Amounts are exact decimal strings; a share or a
// ratio is a number, for display only.
export type AttentionItem = { portfolio?: string } & (
  | { kind: 'missing-rate'; currency: string; positions: number }
  | { kind: 'missing-price'; instrument_id: number; symbol: string }
  // The count, the day limit it was counted against and the oldest of them, which "Güncelle" opens.
  | {
      kind: 'stale-manual-prices'
      count: number
      days: number
      instrument_id: number
      symbol: string
    }
  | { kind: 'estimated-benchmark'; from: string }
  | { kind: 'dividend'; dividend: ExpectedDividend }
  // amount is day_change_reporting in `currency`; ratio is dayRatio, the
  // percent the Günlük cell shows, null without a previous close.
  | {
      kind: 'mover'
      instrument_id: number
      symbol: string
      amount: string
      currency: string
      ratio: number | null
    }
  // label is the symbol, the reporting currency for cash, or the foreign
  // currency.
  | {
      kind: 'concentration'
      subject: 'position' | 'cash' | 'currency'
      label: string
      instrument_id?: number
      share: number
    }
  // The two TRY-reporting rules (Yoğunlaşma): TRY cash over the total, and no
  // USD, EUR, gold or US stock at all.
  | { kind: 'try-cash-share'; share: number }
  | { kind: 'no-hedge' }
)

// Whether part / whole is above share, exactly: the amounts go in as 1e-9
// units and the share as basis points, so exactly 25% is never above 25%.
function above(part: bigint, whole: bigint, share: number): boolean {
  return part * 10_000n > whole * BigInt(Math.round(share * 10_000))
}

// attentionItems lists what needs the owner's attention, in display order:
// Veri, Temettü onayı, Bugün, Yoğunlaşma. Closed positions take no part. A
// position without a price is a Veri item and is never a mover or a share,
// but it counts in the totals at the value the summary gives it (its cost),
// as in Allocation. Shares are of the total in the reporting currency
// including cash; like Allocation, a position without a positive reporting
// value is left out of the total.
export function attentionItems(input: {
  summary: Summary
  expected: ExpectedDividend[]
  estimatedFrom?: string | null
  today: string
  portfolioName?: string
  rules?: AttentionRules
}): AttentionItem[] {
  const { summary, expected, estimatedFrom, today, portfolioName, rules = {} } = input
  const staleDays = threshold(rules, 'stale_manual_days', ATTENTION.staleManualDays)
  const positionShare = threshold(rules, 'position_share', ATTENTION.positionShare)
  const cashShare = threshold(rules, 'cash_share', ATTENTION.cashShare)
  const tryCashShare =
    summary.currency === 'TRY' ? threshold(rules, 'try_cash_share', ATTENTION.tryCashShare) : null
  const noHedge = summary.currency === 'TRY' && rules.no_hedge?.enabled !== false
  const currencyShare = threshold(rules, 'currency_share', ATTENTION.currencyShare)
  const open = summary.positions.filter((p) => !p.closed)
  const priced = open.filter((p) => p.has_price)
  const items: AttentionItem[] = []

  // Veri.
  for (const currency of summary.missing_rates) {
    const positions = open.filter(
      (p) => p.currency === currency && p.market_value_reporting === null,
    ).length
    items.push({ kind: 'missing-rate', currency, positions })
  }
  for (const p of open.filter((p) => !p.has_price)) {
    items.push({ kind: 'missing-price', instrument_id: p.instrument_id, symbol: p.symbol })
  }
  const staleManual = priced
    .filter(
      (p) =>
        staleDays !== null &&
        p.price_source === 'manual' &&
        p.price_date !== null &&
        daysBetween(p.price_date, today) > staleDays,
    )
    .sort((a, b) => (a.price_date! < b.price_date! ? -1 : 1))
  if (staleManual.length > 0) {
    items.push({
      kind: 'stale-manual-prices',
      count: staleManual.length,
      days: staleDays!,
      instrument_id: staleManual[0].instrument_id,
      symbol: staleManual[0].symbol,
    })
  }
  if (estimatedFrom) items.push({ kind: 'estimated-benchmark', from: estimatedFrom })

  // Temettü onayı.
  for (const dividend of expected) items.push({ kind: 'dividend', dividend })

  // Bugün.
  const movers = priced
    .filter((p) => p.day_change_reporting !== null && Number(p.day_change_reporting) !== 0)
    .sort(
      (a, b) => Math.abs(Number(b.day_change_reporting)) - Math.abs(Number(a.day_change_reporting)),
    )
    .slice(0, moverCount(rules))
  for (const p of movers) {
    items.push({
      kind: 'mover',
      instrument_id: p.instrument_id,
      symbol: p.symbol,
      amount: p.day_change_reporting!,
      currency: summary.currency,
      ratio: dayRatio(p),
    })
  }

  // Yoğunlaşma.
  const valued = open
    .filter((p) => p.market_value_reporting !== null && decimalUnits(p.market_value_reporting) > 0n)
    .sort((a, b) => compareDecimal(b.market_value_reporting!, a.market_value_reporting!))
  const cash = decimalUnits(summary.cash)
  const total = holdingsTotal(summary)
  if (total > 0n) {
    for (const p of valued) {
      const units = decimalUnits(p.market_value_reporting!)
      if (positionShare === null || !p.has_price || !above(units, total, positionShare)) continue
      items.push({
        kind: 'concentration',
        subject: 'position',
        label: p.symbol,
        instrument_id: p.instrument_id,
        share: shareOf(units, total),
      })
    }
    // The more specific TRY rule replaces the generic one when it fires.
    const tryCash = decimalUnits(summary.cash_by_currency_reporting.TRY ?? '0')
    if (tryCashShare !== null && above(tryCash, total, tryCashShare)) {
      items.push({ kind: 'try-cash-share', share: shareOf(tryCash, total) })
    } else if (cashShare !== null && above(cash, total, cashShare)) {
      items.push({
        kind: 'concentration',
        subject: 'cash',
        label: summary.currency,
        share: shareOf(cash, total),
      })
    }
  }

  // The currency view has its own total, as Allocation's: one row per
  // currency, its positions plus its own cash, and only the positive rows
  // count. Against the net cash above, a negative TRY balance would push USD
  // past 100%.
  const byCurrency = currencyTotals(open, summary.cash_by_currency_reporting)
  const currencyTotal = [...byCurrency.values()].reduce((sum, units) => sum + units, 0n)
  for (const [currency, units] of byCurrency) {
    if (currency === summary.currency) continue
    if (currencyShare === null || !above(units, currencyTotal, currencyShare)) continue
    items.push({
      kind: 'concentration',
      subject: 'currency',
      label: currency,
      share: shareOf(units, currencyTotal),
    })
  }

  // Any open USD, EUR, gold or US position, or positive USD or EUR cash, is a
  // hedge. A term deposit in USD or EUR is such a position. An empty
  // portfolio has nothing to hedge.
  const hedged =
    open.some(
      (p) =>
        p.currency === 'USD' || p.currency === 'EUR' || p.market === 'GOLD' || p.market === 'US',
    ) ||
    Object.entries(summary.cash_by_currency).some(
      ([currency, amount]) =>
        (currency === 'USD' || currency === 'EUR') && decimalUnits(amount) > 0n,
    )
  if (noHedge && total > 0n && !hedged) items.push({ kind: 'no-hedge' })

  return portfolioName ? items.map((item) => ({ ...item, portfolio: portfolioName })) : items
}

// The items of several portfolios on one card (Tüm portföyler): each list
// comes from attentionItems with its portfolio's name. Bugün keeps the
// largest movers over all of them, as many as one portfolio shows; the
// benchmark is the combined chart's, so its estimate is one unnamed line.
export function mergeAttention(
  lists: AttentionItem[][],
  estimatedFrom?: string | null,
  movers: number = ATTENTION.movers,
): AttentionItem[] {
  const items = lists.flat()
  const topMovers = items
    .filter((item): item is Extract<AttentionItem, { kind: 'mover' }> => item.kind === 'mover')
    .sort((a, b) => Math.abs(Number(b.amount)) - Math.abs(Number(a.amount)))
    .slice(0, movers)
  return [
    ...items.filter((item) => item.kind !== 'mover'),
    ...topMovers,
    ...(estimatedFrom ? [{ kind: 'estimated-benchmark' as const, from: estimatedFrom }] : []),
  ]
}
