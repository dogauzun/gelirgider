import { Area, CartesianGrid, ComposedChart, Line, ReferenceLine, XAxis, YAxis } from 'recharts'
import type { ProjectionMonth } from '../../api/projection'
import { t } from '../../i18n/strings'
import { axisTicks, formatChartValue, formatTicks, pressWithoutFocus } from '../../lib/chart'
import { formatShortMarketDate } from '../../lib/date'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, GRID, MARGIN, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip } from './ChartTooltip'

const ACCENT = 'var(--color-accent)'

// ProjectionChart draws the goal's fan (api/projection.ts): the low-high band
// in the faint colour, the median path as the accent line and the goal amount
// as a dashed line, on Recharts (ADR 0023). The axis spans the goal too, so
// the line is always in the plot. Amounts go through formatChartValue and
// formatTicks, so privacy mode masks them. `months` is not empty.
export interface ProjectionChartProps {
  months: ProjectionMonth[]
  amount: number
  height?: number
}

export function ProjectionChart({ months, amount, height = 200 }: ProjectionChartProps) {
  const rows = months.map((m) => ({ ...m, date: `${m.month}-01`, band: [m.low, m.high] }))
  const yTicks = axisTicks([amount, ...months.flatMap((m) => [m.low, m.high])])
  const tickLabels = new Map(formatTicks(yTicks).map((label, i) => [yTicks[i], label]))
  const last = rows[rows.length - 1]
  const summary = t('projection.chart.summary', {
    from: formatShortMarketDate(rows[0].date),
    to: formatShortMarketDate(last.date),
    value: formatChartValue(last.mid),
  })

  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const p = active && activeIndex != null ? rows[Number(activeIndex)] : undefined
    if (!p) return null
    return (
      <ChartTooltip
        title={formatShortMarketDate(p.date)}
        rows={(['high', 'mid', 'low'] as const).map((key) => ({
          key,
          swatch: null,
          label: t(`projection.${key}`),
          value: formatChartValue(p[key]),
        }))}
      />
    )
  }

  return (
    // A mouse press takes no focus (pressWithoutFocus).
    <div onMouseDown={pressWithoutFocus}>
      <ComposedChart
        responsive
        data={rows}
        aria-label={summary}
        margin={MARGIN}
        className="touch-pan-y tabular-nums"
        style={{ width: '100%', height }}
      >
        <CartesianGrid {...GRID} />
        <XAxis dataKey="date" tickFormatter={formatShortMarketDate} minTickGap={24} {...X_AXIS} />
        <YAxis
          {...Y_AXIS}
          ticks={yTicks}
          domain={[yTicks[0], yTicks[yTicks.length - 1]]}
          tickFormatter={(v: number) => tickLabels.get(v) ?? ''}
        />
        <ReferenceLine y={amount} strokeDasharray="4 3" style={{ stroke: 'var(--color-ink-2)' }} />
        <Area
          dataKey="band"
          type="linear"
          strokeWidth={0}
          // The style reaches the fill path, so it takes the colour through
          // currentColor: SVG attributes do not take var().
          fill="currentColor"
          fillOpacity={0.25}
          style={{ color: 'var(--color-faint)' }}
          activeDot={false}
          animationDuration={ANIMATION_MS}
        />
        <Line
          dataKey="mid"
          type="linear"
          dot={false}
          strokeWidth={1.5}
          strokeLinejoin="round"
          style={{ stroke: ACCENT }}
          activeDot={{
            r: 4,
            strokeWidth: 2,
            style: { fill: 'var(--color-surface)', stroke: ACCENT },
          }}
          animationDuration={ANIMATION_MS}
        />
        <AnchoredTooltip
          content={tooltipContent}
          cursor={{ strokeDasharray: '2 3', style: { stroke: 'var(--color-faint)' } }}
          anchors={new Map(rows.map((p) => [p.date, p.mid]))}
        />
      </ComposedChart>
    </div>
  )
}
