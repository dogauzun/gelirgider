import { Area, AreaChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from 'recharts'
import { t } from '../../i18n/strings'
import { formatDateLabel, formatPercentTicks, niceTicks, pressWithoutFocus } from '../../lib/chart'
import { signedPercent } from '../../lib/format'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, GRID, MARGIN, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip } from './ChartTooltip'

const LOSS = 'var(--color-loss)'

// DrawdownChart draws a drawdown series (lib/drawdown.ts), fractions at or
// below zero, as an area in the loss colour hanging from a zero line at the
// top, on Recharts (ADR 0023), with the shared tooltip card. A series that
// never falls still gets a 1% axis, so the flat line sits at the top.
// Percentages are not amounts, so privacy mode leaves them shown.
export interface DrawdownChartProps {
  series: { date: string; value: number }[]
  height?: number
}

export function DrawdownChart({ series, height = 140 }: DrawdownChartProps) {
  const low = Math.min(-0.01, ...series.map((p) => p.value))
  const yTicks = niceTicks(low, 0)
  const tickLabels = new Map(formatPercentTicks(yTicks).map((label, i) => [yTicks[i], label]))
  const multiYear = series[0].date.slice(0, 4) !== series[series.length - 1].date.slice(0, 4)
  const summary = t('drawdown.chart.summary', {
    from: formatDateLabel(series[0].date, multiYear),
    to: formatDateLabel(series[series.length - 1].date, multiYear),
    value: signedPercent(Math.min(...series.map((p) => p.value))),
  })

  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const p = active && activeIndex != null ? series[Number(activeIndex)] : undefined
    if (!p) return null
    return (
      <ChartTooltip
        title={formatDateLabel(p.date, multiYear)}
        rows={[{ key: 'value', swatch: null, label: signedPercent(p.value), value: '' }]}
      />
    )
  }

  return (
    // A mouse press takes no focus (pressWithoutFocus, GLG-1034).
    <div onMouseDown={pressWithoutFocus}>
      <AreaChart
        responsive
        data={series}
        aria-label={summary}
        margin={{ ...MARGIN, top: 4 }}
        className="touch-pan-y tabular-nums"
        style={{ width: '100%', height }}
      >
        <CartesianGrid {...GRID} />
        <XAxis
          dataKey="date"
          tickFormatter={(d: string) => formatDateLabel(d, multiYear)}
          minTickGap={16}
          {...X_AXIS}
        />
        <YAxis
          {...Y_AXIS}
          ticks={yTicks}
          domain={[yTicks[0], 0]}
          tickFormatter={(v: number) => tickLabels.get(v) ?? ''}
        />
        <ReferenceLine y={0} style={{ stroke: 'var(--color-line-strong)' }} />
        <Area
          className="series-drawdown"
          dataKey="value"
          type="linear"
          baseValue={0}
          strokeWidth={1.5}
          strokeLinejoin="round"
          // The style reaches the fill path too, so both take the colour
          // through currentColor: SVG attributes do not take var().
          stroke="currentColor"
          style={{ color: LOSS }}
          fill="currentColor"
          fillOpacity={0.14}
          activeDot={{
            r: 4,
            strokeWidth: 2,
            style: { fill: 'var(--color-surface)', stroke: LOSS },
          }}
          animationDuration={ANIMATION_MS}
        />
        <AnchoredTooltip
          content={tooltipContent}
          cursor={{ strokeDasharray: '2 3', style: { stroke: 'var(--color-faint)' } }}
          anchors={new Map(series.map((p) => [p.date, p.value]))}
        />
      </AreaChart>
    </div>
  )
}
