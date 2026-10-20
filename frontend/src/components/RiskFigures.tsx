import type { Benchmark } from '../api/history'
import { t } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import { formatShortMarketDate } from '../lib/date'
import { formatNumber, MISSING, plTone, signedPercent } from '../lib/format'
import type { RiskStats } from '../lib/risk'
import { FitFigure, KPI_FIGURE, KpiCell, KpiStrip } from './ui/Kpi'

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
// (ADR 0036, "Against the benchmark"): no benchmark picked, one with gaps or
// fewer than MIN_DAYS paired returns in the window, or one that never moves.
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
  if ('flat' in stats) {
    return (
      <p className="mt-3 text-[13px] text-muted">{t('risk.benchmarkFlat', { benchmark: label })}</p>
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

// The strips of a window's risk figures, or why there are none (ADR 0036):
// shared by the portfolio's RiskCard and the position page.
export function RiskFigures({
  stats,
  benchmark,
  benchmarkLabel,
}: {
  stats: RiskStats | null
  benchmark: Benchmark | null
  benchmarkLabel?: string
}) {
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
