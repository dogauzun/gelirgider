import { useMemo, useState } from 'react'
import { usePortfolioHistory, type Benchmark } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import { formatShortMarketDate, todayIstanbul } from '../lib/date'
import { formatNumber, MISSING, plTone, signedPercent } from '../lib/format'
import {
  riskStats,
  rollingReturns,
  windowPoints,
  type RiskStats,
  type RiskWindow,
} from '../lib/risk'
import { CashGapNotice } from './CashGapNotice'
import { LineChart } from './LazyChart'
import { HistoryEmpty } from './HistoryEmpty'
import { ChartFrame } from './ui/ChartFrame'
import { FitFigure, KPI_FIGURE, KpiCell, KpiStrip } from './ui/Kpi'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

const WINDOWS: RiskWindow[] = ['1y', '3y', 'all']

// A window with fewer daily returns says too little (ADR 0036).
const MIN_DAYS = 30

const TWO_DECIMALS = { minimumFractionDigits: 2, maximumFractionDigits: 2 }

const spread = (value: number) => formatNumber(value, SHARE_FORMAT)

// The best or the worst day: its return, and its date under it.
function DayCell({ label, day }: { label: string; day: RiskStats['best'] }) {
  return (
    <KpiCell label={label}>
      <FitFigure
        className={`${KPI_FIGURE} ${plTone(day.value)}`}
        text={signedPercent(day.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
      />
      <p className="text-[12px] text-muted tabular-nums">{formatShortMarketDate(day.date)}</p>
    </KpiCell>
  )
}

// Beta, correlation and the benchmark's volatility, or why they are not shown
// (ADR 0036, "Against the benchmark"): no benchmark picked, or one with gaps
// or fewer than MIN_DAYS paired returns in the window.
function BenchmarkRow({
  stats,
  benchmark,
  label,
}: {
  stats: RiskStats['benchmark']
  benchmark: Benchmark | null
  label: string
}) {
  if (benchmark === null) {
    return <p className="mt-3 text-[13px] text-muted">{t('risk.noBenchmark')}</p>
  }
  if (stats === null || stats.days < MIN_DAYS) {
    return (
      <p className="mt-3 text-[13px] text-muted">
        {t('risk.benchmarkNoData', { benchmark: label })}
      </p>
    )
  }
  return (
    <KpiStrip columns="grid-cols-3" className="mt-3">
      <KpiCell label={t('risk.beta')} hint={t('risk.beta.hint', { benchmark: label })}>
        <FitFigure
          className={`${KPI_FIGURE} text-ink`}
          text={formatNumber(stats.beta, TWO_DECIMALS)}
        />
      </KpiCell>
      <KpiCell label={t('risk.correlation')} hint={t('risk.correlation.hint')}>
        <FitFigure
          className={`${KPI_FIGURE} text-ink`}
          text={
            stats.correlation === null ? MISSING : formatNumber(stats.correlation, TWO_DECIMALS)
          }
        />
      </KpiCell>
      <KpiCell label={t('risk.benchmarkVolatility', { benchmark: label })}>
        <FitFigure className={`${KPI_FIGURE} text-ink`} text={spread(stats.volatility)} />
      </KpiCell>
    </KpiStrip>
  )
}

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
        total_value: String(p.value),
        cost_basis: null,
        invested: null,
        cash: '0',
        benchmark_value: p.benchmark === null ? undefined : String(p.benchmark),
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
        {figures()}
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

  // The strips of the window's figures, or why there are none.
  const figures = () => {
    if (stats === null || stats.days < MIN_DAYS) {
      return <p className="text-[13px] text-muted">{t('risk.tooShort')}</p>
    }
    return (
      <>
        <KpiStrip columns="grid-cols-2 sm:grid-cols-4">
          <KpiCell label={t('risk.volatility')} hint={t('risk.volatility.hint')}>
            <FitFigure className={`${KPI_FIGURE} text-ink`} text={spread(stats.volatility)} />
          </KpiCell>
          <KpiCell label={t('risk.downside')} hint={t('risk.downside.hint')}>
            <FitFigure className={`${KPI_FIGURE} text-ink`} text={spread(stats.downside)} />
          </KpiCell>
          <DayCell label={t('risk.bestDay')} day={stats.best} />
          <DayCell label={t('risk.worstDay')} day={stats.worst} />
        </KpiStrip>
        <p className="mt-2 text-xs text-muted">{t('risk.days', { count: stats.days })}</p>
        <BenchmarkRow
          stats={stats.benchmark}
          benchmark={benchmark}
          label={benchmarkLabel ?? benchmark ?? ''}
        />
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
