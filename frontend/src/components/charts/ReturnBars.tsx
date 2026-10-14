import {
  Bar,
  CartesianGrid,
  BarChart as RechartsBarChart,
  ReferenceLine,
  XAxis,
  YAxis,
} from 'recharts'
import type { PeriodBucket, ReturnMethod } from '../../api/history'
import { t } from '../../i18n/strings'
import { formatPercentTicks, niceTicks, pressWithoutFocus } from '../../lib/chart'
import { formatMarketDate } from '../../lib/date'
import { signedPercent } from '../../lib/format'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, BAR_FRAME, GRID, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip } from './ChartTooltip'

const MONTHS = 24
const COLORS = { portfolio: 'var(--color-accent)', benchmark: 'var(--color-plum)' }

function Swatch({ series }: { series: keyof typeof COLORS }) {
  return (
    <span
      aria-hidden="true"
      className="size-2.5 shrink-0 rounded-xs"
      style={{ background: COLORS[series] }}
    />
  )
}

const toNumber = (v: string | null | undefined) => (v == null ? null : Number(v))

// ReturnBars draws the last 24 month buckets of the periods endpoint as
// grouped bars, the portfolio's return by the chosen method and, when a
// benchmark is chosen, the benchmark's, on a percent axis with a zero line.
// Sizing, tokens, the keyboard and the animation follow BarChart.
// A null return draws no bar and the tooltip says so; the benchmark series
// is drawn only when some month has its value. Fractions are drawn as
// numbers: they are ratios, not money (ADR 0011).
export interface ReturnBarsProps {
  buckets: PeriodBucket[]
  method: ReturnMethod
  benchmarkLabel?: string
  height?: number
}

export function ReturnBars({ buckets, method, benchmarkLabel, height = 200 }: ReturnBarsProps) {
  const shown = buckets.slice(-MONTHS)
  const rows = shown.map((b) => ({
    label: formatMarketDate(`${b.period}-01`, { month: 'short', year: '2-digit' }),
    portfolio: toNumber(b.returns[method]),
    benchmark: benchmarkLabel ? toNumber(b.benchmark?.[method]) : null,
  }))
  const values = rows.flatMap((r) => [r.portfolio, r.benchmark]).filter((v) => v !== null)
  if (values.length === 0) {
    return <p className="text-sm text-muted">{t('chart.empty')}</p>
  }
  const hasBenchmark = rows.some((r) => r.benchmark !== null)
  const ticks = niceTicks(Math.min(0, ...values), Math.max(0, ...values))
  const labels = formatPercentTicks(ticks)
  const tickLabels = new Map(ticks.map((v, i) => [v, labels[i]]))
  const series = [t('portfolio.label'), ...(hasBenchmark ? [benchmarkLabel ?? ''] : [])]
  const summary = t('chart.returnBars.summary', {
    from: rows[0].label,
    to: rows[rows.length - 1].label,
    series: series.join(', '),
  })

  const percent = (v: number | null) =>
    v === null ? t('chart.returnBars.undefined') : signedPercent(v, { maximumFractionDigits: 2 })

  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const i = active && activeIndex != null ? Number(activeIndex) : -1
    const row = rows[i]
    if (!row) return null
    return (
      <ChartTooltip
        title={formatMarketDate(`${shown[i].period}-01`, { month: 'long', year: 'numeric' })}
        rows={[
          {
            key: 'portfolio',
            swatch: <Swatch series="portfolio" />,
            label: t('portfolio.label'),
            value: percent(row.portfolio),
          },
          ...(hasBenchmark
            ? [
                {
                  key: 'benchmark',
                  swatch: <Swatch series="benchmark" />,
                  label: benchmarkLabel ?? '',
                  value: percent(row.benchmark),
                },
              ]
            : []),
        ]}
      />
    )
  }

  // A zero return is a 1px hairline, so it reads apart from a null month,
  // which draws nothing.
  const minPointSize = (value: number | null | undefined) => (value === 0 ? 1 : 0)

  return (
    // A mouse press takes no focus (pressWithoutFocus).
    <div onMouseDown={pressWithoutFocus}>
      <RechartsBarChart
        {...BAR_FRAME}
        data={rows}
        aria-label={summary}
        barGap={1}
        style={{ width: '100%', height }}
      >
        <CartesianGrid {...GRID} />
        <XAxis dataKey="label" {...X_AXIS} />
        <YAxis
          {...Y_AXIS}
          ticks={ticks}
          domain={[ticks[0], ticks[ticks.length - 1]]}
          tickFormatter={(v: number) => tickLabels.get(v) ?? ''}
        />
        <ReferenceLine y={0} style={{ stroke: 'var(--color-faint)' }} />
        <Bar
          className="series-portfolio"
          dataKey="portfolio"
          minPointSize={minPointSize}
          style={{ fill: COLORS.portfolio }}
          animationDuration={ANIMATION_MS}
        />
        {hasBenchmark && (
          <Bar
            className="series-benchmark"
            dataKey="benchmark"
            minPointSize={minPointSize}
            style={{ fill: COLORS.benchmark }}
            animationDuration={ANIMATION_MS}
          />
        )}
        {/* The card sits above or below the group's top, over its slot. */}
        <AnchoredTooltip
          content={tooltipContent}
          anchors={
            new Map(rows.map((r) => [r.label, Math.max(0, r.portfolio ?? 0, r.benchmark ?? 0)]))
          }
          cursor={{ style: { fill: 'var(--color-well)' } }}
          centred
        />
      </RechartsBarChart>
      <div data-chart-legend className="mt-2 flex min-h-4 gap-x-4 text-xs text-muted">
        {series.map((label, i) => (
          <span key={label} className="flex items-center gap-1">
            <Swatch series={i === 0 ? 'portfolio' : 'benchmark'} />
            {label}
          </span>
        ))}
      </div>
    </div>
  )
}
