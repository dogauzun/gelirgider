import { usePortfolioHistory } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { CashGapNotice } from './CashGapNotice'
import { t } from '../i18n/strings'
import { formatShortMarketDate, todayIstanbul } from '../lib/date'
import { drawdown } from '../lib/drawdown'
import { plTone, signedPercent } from '../lib/format'
import { DrawdownChart } from './LazyChart'
import { ChartFrame } from './ui/ChartFrame'

// The "Zirveden düşüş" card on the Performans tab: how far the
// return index 1 + twr of the all-time history is below its highest value so
// far (lib/drawdown.ts), with the largest drawdown and its peak and trough
// dates and the current one in the header, and the chart under them. The
// all-time history is Özet's "Tümü" range without a benchmark, so the two
// share a cached query. With twr null (ADR 0011) the card gives the same
// explanation as the periodic returns card above it, the cash gaps notice.
export function DrawdownCard({ portfolioId }: { portfolioId: PortfolioScope }) {
  const query = usePortfolioHistory(portfolioId, '', todayIstanbul())
  const points = query.data?.points ?? []
  const dd = drawdown(points)
  const figure = (label: string, value: number, dates?: string) => (
    <span className="flex items-baseline gap-1.5">
      <span className="text-muted">{label}</span>
      <b className={`font-semibold tabular-nums ${plTone(value)}`}>{signedPercent(value)}</b>
      {dates && <span className="text-muted tabular-nums">{dates}</span>}
    </span>
  )

  // What the chart's place holds once the history is in.
  const body = () => {
    // One day, with twr null: a scope with no transactions (today, all
    // zero) or the day of its first one, not an undefined return. The
    // empty text is the periodic returns card's.
    if (points.length < 2) {
      const noTransactions = points.every((p) =>
        [p.total_value, p.cash, p.cost_basis].every((v) => Number(v) === 0),
      )
      return (
        <p className="text-[13px] text-muted">
          {t(noTransactions ? 'transactions.empty' : 'chart.empty')}
        </p>
      )
    }
    if (dd) return <DrawdownChart series={dd.series} />
    return <CashGapNotice scope={portfolioId} />
  }

  return (
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 pt-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('drawdown.title')}</h2>
          <span className="text-xs text-muted">{t('drawdown.subtitle')}</span>
        </div>
        {dd && (
          <p data-testid="drawdown-figures" className="flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
            {figure(
              t('drawdown.max'),
              dd.max?.value ?? 0,
              dd.max
                ? `${formatShortMarketDate(dd.max.peak)} – ${formatShortMarketDate(dd.max.trough)}`
                : undefined,
            )}
            {figure(t('drawdown.current'), dd.current)}
          </p>
        )}
      </div>
      <div className="p-4 pt-3">
        <ChartFrame query={query} height={140} errorTitle={t('drawdown.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
