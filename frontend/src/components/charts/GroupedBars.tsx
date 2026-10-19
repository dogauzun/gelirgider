import {
  Bar,
  CartesianGrid,
  BarChart as RechartsBarChart,
  ReferenceLine,
  XAxis,
  YAxis,
} from 'recharts'
import { t, type StringKey } from '../../i18n/strings'
import { formatPercentTicks, niceTicks, pressWithoutFocus } from '../../lib/chart'
import { formatCompactMoney, formatMoney, signedPercent } from '../../lib/format'
import { usePrivacy } from '../../lib/privacy'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, BAR_FRAME, GRID, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip } from './ChartTooltip'

// GroupedBars is the frame ReturnBars and CashFlowBars share: one group of up
// to two bars per category on a zero line, a legend, and a tooltip. `unit`
// picks the axis and the default tooltip values: 'percent' as ReturnBars has
// always drawn them (fractions, never masked), 'money' as BarChart does
// (compact ticks, formatMoney, masked in privacy mode). A null value draws no
// bar and the tooltip says so; series b is drawn and listed only when some
// group has a value for it. `tooltipRows` replaces the two default rows; a
// row with `series` gets that series' swatch.
export interface BarGroup {
  key: string
  label: string
  title: string
  a: number | null
  b: number | null
}

interface BarSeries {
  label: string
  color: string
  // The class of the series' bars, for tests and styling.
  className?: string
}

export interface TooltipRow {
  label: string
  value: string
  series?: 'a' | 'b'
}

export interface GroupedBarsProps {
  groups: BarGroup[]
  series: { a: BarSeries; b: BarSeries }
  unit: 'percent' | 'money'
  currency?: string
  height?: number
  // The i18n key of the chart's accessible name, given {from, to, series}.
  summaryKey: StringKey
  tooltipRows?: (group: BarGroup) => TooltipRow[]
}

function Swatch({ color }: { color: string }) {
  return (
    <span
      aria-hidden="true"
      className="size-2.5 shrink-0 rounded-xs"
      style={{ background: color }}
    />
  )
}

// A zero value is a 1px hairline, so it reads apart from a null group,
// which draws nothing.
const minPointSize = (value: number | null | undefined) => (value === 0 ? 1 : 0)

export function GroupedBars({
  groups,
  series,
  unit,
  currency = '',
  height = 200,
  summaryKey,
  tooltipRows,
}: GroupedBarsProps) {
  // Re-render when privacy flips, so money ticks and tooltip mask at once.
  usePrivacy()

  const values = groups.flatMap((g) => [g.a, g.b]).filter((v) => v !== null)
  if (values.length === 0) {
    return <p className="text-sm text-muted">{t('chart.empty')}</p>
  }
  const hasB = groups.some((g) => g.b !== null)
  const ticks = niceTicks(Math.min(0, ...values), Math.max(0, ...values))
  const labels = formatPercentTicks(ticks)
  const tickLabels = new Map(ticks.map((v, i) => [v, labels[i]]))
  const shownSeries = hasB ? [series.a, series.b] : [series.a]
  const summary = t(summaryKey, {
    from: groups[0].label,
    to: groups[groups.length - 1].label,
    series: shownSeries.map((s) => s.label).join(', '),
  })

  const format = (v: number | null) => {
    if (v === null) return t('chart.returnBars.undefined')
    return unit === 'money'
      ? formatMoney(v, currency)
      : signedPercent(v, { maximumFractionDigits: 2 })
  }
  const defaultRows = (g: BarGroup): TooltipRow[] => [
    { label: series.a.label, value: format(g.a), series: 'a' },
    ...(hasB ? [{ label: series.b.label, value: format(g.b), series: 'b' as const }] : []),
  ]

  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const i = active && activeIndex != null ? Number(activeIndex) : -1
    const group = groups[i]
    if (!group) return null
    return (
      <ChartTooltip
        title={group.title}
        rows={(tooltipRows ?? defaultRows)(group).map((row) => ({
          key: row.label,
          swatch: row.series ? <Swatch color={series[row.series].color} /> : null,
          label: row.label,
          value: row.value,
        }))}
      />
    )
  }

  const bar = (dataKey: 'a' | 'b') => (
    <Bar
      className={series[dataKey].className ?? `series-${dataKey}`}
      dataKey={dataKey}
      minPointSize={minPointSize}
      style={{ fill: series[dataKey].color }}
      animationDuration={ANIMATION_MS}
    />
  )

  return (
    // A mouse press takes no focus (pressWithoutFocus).
    <div onMouseDown={pressWithoutFocus}>
      <RechartsBarChart
        {...BAR_FRAME}
        data={groups}
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
          tickFormatter={(v: number) =>
            unit === 'money' ? formatCompactMoney(v, currency, {}) : (tickLabels.get(v) ?? '')
          }
        />
        <ReferenceLine y={0} style={{ stroke: 'var(--color-faint)' }} />
        {bar('a')}
        {hasB && bar('b')}
        {/* The card sits above or below the group's top, over its slot. */}
        <AnchoredTooltip
          content={tooltipContent}
          anchors={new Map(groups.map((g) => [g.label, Math.max(0, g.a ?? 0, g.b ?? 0)]))}
          cursor={{ style: { fill: 'var(--color-well)' } }}
          centred
        />
      </RechartsBarChart>
      <div data-chart-legend className="mt-2 flex min-h-4 gap-x-4 text-xs text-muted">
        {shownSeries.map((s) => (
          <span key={s.label} className="flex items-center gap-1">
            <Swatch color={s.color} />
            {s.label}
          </span>
        ))}
      </div>
    </div>
  )
}
