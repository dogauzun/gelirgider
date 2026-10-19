import { useMemo, useState } from 'react'
import { usePortfolioHistory, type Benchmark } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { riskStats, rollingReturns, windowPoints, type RiskWindow } from '../lib/risk'
import { CashGapNotice } from './CashGapNotice'
import { LineChart } from './LazyChart'
import { HistoryEmpty } from './HistoryEmpty'
import { RiskFigures } from './RiskFigures'
import { ChartFrame } from './ui/ChartFrame'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

const WINDOWS: RiskWindow[] = ['1y', '3y', 'all']

// The "Risk" card on the Performans tab (ADR 0036): the annualized volatility
// and downside deviation and the best and worst day of the all-time history's
// daily returns (lib/risk.ts), cut to "1Y | 3Y | Tümü". It opens on 1Y and
// the choice is not stored. The history is asked with the page's benchmark
// for the benchmark figures; the first four read only twr.
export function RiskCard({
  portfolioId,
  benchmark,
  benchmarkLabel,
  excessLabel,
}: {
  portfolioId: PortfolioScope
  benchmark: Benchmark | null
  benchmarkLabel?: string
  excessLabel?: string
}) {
  const [span, setSpan] = useState<RiskWindow>('1y')
  const query = usePortfolioHistory(portfolioId, '', todayIstanbul(), benchmark)
  const points = query.data?.points ?? []
  const stats = riskStats(windowPoints(points, span))
  // Over all the history, not the window. Keyed on query.data: LineChart
  // derives its rows from the array's identity.
  const rolling = useMemo(
    () =>
      rollingReturns(query.data?.points ?? []).map((p) => ({
        date: p.date,
        total_value: p.value.toFixed(9),
        cost_basis: null,
        invested: null,
        cash: '0',
        benchmark_value: p.benchmark === null ? undefined : p.benchmark.toFixed(9),
      })),
    [query.data],
  )

  // What the figures' place holds once the history is in.
  const body = () => {
    if (points.length < 2) return <HistoryEmpty points={points} />
    // twr is null on every point or on none (ADR 0011).
    if (points.every((p) => p.twr == null)) return <CashGapNotice scope={portfolioId} />
    return (
      <>
        <RiskFigures stats={stats} benchmark={benchmark} benchmarkLabel={benchmarkLabel} />
        <div className="mt-4">
          <h3 className="text-[13px] font-semibold text-ink">{t('risk.rolling.title')}</h3>
          <p className="mb-2 text-xs text-muted">{t('risk.rolling.hint')}</p>
          {rolling.length === 0 ? (
            <p className="text-[13px] text-muted">{t('risk.rolling.empty')}</p>
          ) : (
            <LineChart
              unit="percent"
              points={rolling}
              height={180}
              labels={{ value: t('risk.rolling.line'), excess: excessLabel }}
              benchmarkLabel={benchmarkLabel}
            />
          )}
        </div>
      </>
    )
  }

  return (
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 pt-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('risk.title')}</h2>
          <span className="text-xs text-muted">{t('risk.subtitle')}</span>
        </div>
        <Segmented
          options={WINDOWS}
          value={span}
          onChange={setSpan}
          label={(option) => t(`risk.window.${option}`)}
          aria-label={t('risk.window')}
          itemClassName={TAP_Y}
        />
      </div>
      <div className="p-4 pt-3">
        <ChartFrame query={query} height={120} errorTitle={t('risk.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
