import type { PeriodBucket, ReturnMethod } from '../../api/history'
import { formatMarketDate } from '../../lib/date'
import { t } from '../../i18n/strings'
import { GroupedBars } from './GroupedBars'

const MONTHS = 24

const toNumber = (v: string | null | undefined) => (v == null ? null : Number(v))

// ReturnBars draws the last 24 month buckets of the periods endpoint as
// grouped bars, the portfolio's return by the chosen method and, when a
// benchmark is chosen, the benchmark's, on a percent axis with a zero line.
// The frame is GroupedBars. A null return draws no bar and the tooltip says
// so; the benchmark series is drawn only when some month has its value.
// Fractions are drawn as numbers: they are ratios, not money (ADR 0011).
export interface ReturnBarsProps {
  buckets: PeriodBucket[]
  method: ReturnMethod
  benchmarkLabel?: string
  height?: number
}

export function ReturnBars({ buckets, method, benchmarkLabel, height = 200 }: ReturnBarsProps) {
  const groups = buckets.slice(-MONTHS).map((b) => ({
    key: b.period,
    label: formatMarketDate(`${b.period}-01`, { month: 'short', year: '2-digit' }),
    title: formatMarketDate(`${b.period}-01`, { month: 'long', year: 'numeric' }),
    a: toNumber(b.returns[method]),
    b: benchmarkLabel ? toNumber(b.benchmark?.[method]) : null,
  }))
  return (
    <GroupedBars
      groups={groups}
      series={{
        a: {
          label: t('portfolio.label'),
          color: 'var(--color-accent)',
          className: 'series-portfolio',
        },
        b: {
          label: benchmarkLabel ?? '',
          color: 'var(--color-plum)',
          className: 'series-benchmark',
        },
      }}
      unit="percent"
      height={height}
      summaryKey="chart.returnBars.summary"
    />
  )
}
