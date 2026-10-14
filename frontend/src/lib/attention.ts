import type { ExpectedDividend } from '../api/dividends'
import type { Summary } from '../api/summary'
import { daysBetween } from './date'
import { decimalUnits } from './decimal'
import { dayRatio } from './positions'

// The thresholds of the "Dikkat" card (spec: UI v2 core pages). A share is
// flagged only above its threshold, never at it.
export const ATTENTION = {
  staleManualDays: 7,
  positionShare: 0.25,
  cashShare: 0.4,
  currencyShare: 0.5,
  movers: 2,
} as const

// `portfolio` names the portfolio on Tüm portföyler, where items of several
// portfolios share one card. Amounts are exact decimal strings; a share or a
// ratio is a number, for display only.
export type AttentionItem = { portfolio?: string } & (
  | { kind: 'missing-rate'; currency: string; positions: number }
  | { kind: 'missing-price'; instrument_id: number; symbol: string }
  // The count and the oldest of them, which "Güncelle" opens.
  | { kind: 'stale-manual-prices'; count: number; instrument_id: number; symbol: string }
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
}): AttentionItem[] {
  const { summary, expected, estimatedFrom, today, portfolioName } = input
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
        p.price_source === 'manual' &&
        p.price_date !== null &&
        daysBetween(p.price_date, today) > ATTENTION.staleManualDays,
    )
    .sort((a, b) => (a.price_date! < b.price_date! ? -1 : 1))
  if (staleManual.length > 0) {
    items.push({
      kind: 'stale-manual-prices',
      count: staleManual.length,
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
    .slice(0, ATTENTION.movers)
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
    .filter((p) => p.market_value_reporting !== null)
    .map((p) => ({ p, units: decimalUnits(p.market_value_reporting!) }))
    .filter(({ units }) => units > 0n)
  const cash = decimalUnits(summary.cash)
  const total = valued.reduce((sum, { units }) => sum + units, 0n) + (cash > 0n ? cash : 0n)
  if (total > 0n) {
    const share = (units: bigint) => Number(units) / Number(total)
    for (const { p, units } of [...valued].sort((a, b) => (b.units > a.units ? 1 : -1))) {
      if (!p.has_price || !above(units, total, ATTENTION.positionShare)) continue
      items.push({
        kind: 'concentration',
        subject: 'position',
        label: p.symbol,
        instrument_id: p.instrument_id,
        share: share(units),
      })
    }
    if (above(cash, total, ATTENTION.cashShare)) {
      items.push({
        kind: 'concentration',
        subject: 'cash',
        label: summary.currency,
        share: share(cash),
      })
    }
  }

  // The currency view has its own total, as Allocation's: one row per
  // currency, its positions plus its own cash, and only the positive rows
  // count. Against the net cash above, a negative TRY balance would push USD
  // past 100%.
  const byCurrency = new Map<string, bigint>()
  for (const { p, units } of valued) {
    byCurrency.set(p.currency, (byCurrency.get(p.currency) ?? 0n) + units)
  }
  for (const [currency, amount] of Object.entries(summary.cash_by_currency_reporting)) {
    byCurrency.set(currency, (byCurrency.get(currency) ?? 0n) + decimalUnits(amount))
  }
  const currencyTotal = [...byCurrency.values()].reduce(
    (sum, units) => (units > 0n ? sum + units : sum),
    0n,
  )
  for (const [currency, units] of byCurrency) {
    if (currency === summary.currency || units <= 0n) continue
    if (!above(units, currencyTotal, ATTENTION.currencyShare)) continue
    items.push({
      kind: 'concentration',
      subject: 'currency',
      label: currency,
      share: Number(units) / Number(currencyTotal),
    })
  }

  return portfolioName ? items.map((item) => ({ ...item, portfolio: portfolioName })) : items
}

// The items of several portfolios on one card (Tüm portföyler): each list
// comes from attentionItems with its portfolio's name. Bugün keeps the
// largest movers over all of them, as many as one portfolio shows; the
// benchmark is the combined chart's, so its estimate is one unnamed line.
export function mergeAttention(
  lists: AttentionItem[][],
  estimatedFrom?: string | null,
): AttentionItem[] {
  const items = lists.flat()
  const movers = items
    .filter((item): item is Extract<AttentionItem, { kind: 'mover' }> => item.kind === 'mover')
    .sort((a, b) => Math.abs(Number(b.amount)) - Math.abs(Number(a.amount)))
    .slice(0, ATTENTION.movers)
  return [
    ...items.filter((item) => item.kind !== 'mover'),
    ...movers,
    ...(estimatedFrom ? [{ kind: 'estimated-benchmark' as const, from: estimatedFrom }] : []),
  ]
}
