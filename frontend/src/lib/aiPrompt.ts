import type { Summary } from '../api/summary'
import { marketLabel, t } from '../i18n/strings'
import { returnRatio, weightOf, weightTotal } from './positions'

// A ratio as a plain percentage with two decimals, '-' without one.
function percent(ratio: number | null): string {
  return ratio === null ? '-' : (ratio * 100).toFixed(2)
}

// The holdings as text to paste into an AI assistant (GLG-608), in the UI
// language. Amounts are the API's own decimal strings, with no masking: the
// clipboard is not the screen (epic GLG-593).
export function buildAiPrompt(summary: Summary, portfolioName: string, today: string): string {
  const cur = summary.currency
  const open = summary.positions.filter((p) => !p.closed)
  // The weight base is the allocation card's, not total_value, which nets
  // negative cash and pushed weights past 100% (GLG-662).
  const total = weightTotal(summary)
  const row = (cells: string[]) => `| ${cells.join(' | ')} |`
  const headers = [
    'symbol',
    'market',
    'quantity',
    'avgCost',
    'lastPrice',
    'value',
    'weight',
    'pl',
  ] as const
  const lines = [
    `# ${portfolioName} · ${today} · ${cur}`,
    '',
    row(headers.map((h) => t(`summary.copyAi.${h}`, { currency: cur }))),
    row(headers.map(() => '---')),
    ...open.map((p) =>
      row([
        p.symbol,
        marketLabel(p.market),
        p.quantity,
        `${p.avg_cost} ${p.currency}`,
        p.last_price === null ? '-' : `${p.last_price} ${p.currency}`,
        p.market_value_reporting ?? '-',
        percent(weightOf(p, total)),
        percent(returnRatio(p.unrealized_pl, p.cost_basis)),
      ]),
    ),
    '',
    `${t('summary.copyAi.cash')}: ${
      Object.entries(summary.cash_by_currency)
        .filter(([, amount]) => Number(amount) !== 0)
        .map(([currency, amount]) => `${amount} ${currency}`)
        .join(', ') || '0'
    }`,
    t('summary.copyAi.totals', {
      currency: cur,
      total: summary.total_value,
      unrealized: summary.unrealized_pl ?? '-',
      realized: summary.realized_pl ?? '-',
    }),
    '',
    t('summary.copyAi.prompt'),
  ]
  return lines.join('\n')
}
