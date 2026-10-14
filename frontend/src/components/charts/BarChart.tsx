import { Bar, CartesianGrid, BarChart as RechartsBarChart, XAxis, YAxis } from 'recharts'
import { t } from '../../i18n/strings'
import { pressWithoutFocus, type Bar as ChartBar } from '../../lib/chart'
import { formatCompactMoney, formatMoney } from '../../lib/format'
import { usePrivacy } from '../../lib/privacy'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, BAR_FRAME, GRID, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip } from './ChartTooltip'

// BarChart draws one accent bar per entry against a zero baseline, with two
// y ticks (0 and the max), on Recharts (ADR 0023). Recharts draws, thins the
// x labels so they never overlap, and handles the pointer, the keyboard (the
// chart is one focus stop; Left and Right move bar by bar) and the animation.
// Values are decimal strings: Number() is for drawing only, and the tooltip
// formats the exact string. A negative value is drawn as zero; a zero bar is
// a 1px hairline so the slot still reads as "nothing" rather than missing,
// and a non-zero bar is at least 3px, so a ₺812 month next to ₺1,28 Mn does
// not look like an empty one (GLG-939).
export interface BarChartProps {
  bars: ChartBar[]
  currency: string
  height?: number
}

export function BarChart({ bars, currency, height = 200 }: BarChartProps) {
  // Re-render when privacy flips, so ticks and tooltip mask at once.
  usePrivacy()

  const rows = bars.map((b) => ({ label: b.label, value: Math.max(0, Number(b.value)) }))
  const max = Math.max(0, ...rows.map((r) => r.value))
  if (max === 0) {
    return <p className="text-sm text-muted">{t('chart.empty')}</p>
  }

  const top = bars[rows.findIndex((r) => r.value === max)]
  const summary = t('chart.bars.summary', {
    count: bars.length,
    label: top.label,
    value: formatMoney(top.value, currency),
  })

  // The tooltip reads the active bar's exact string, never Recharts' payload.
  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const bar = active && activeIndex != null ? bars[Number(activeIndex)] : undefined
    if (!bar) return null
    return (
      <ChartTooltip
        title={bar.label}
        // One series needs no swatch or name: the amount sits in the label
        // slot so it lines up under the title.
        rows={[{ key: 'value', swatch: null, label: formatMoney(bar.value, currency), value: '' }]}
      />
    )
  }

  return (
    // A mouse press takes no focus (pressWithoutFocus, GLG-1034).
    <div onMouseDown={pressWithoutFocus}>
      <RechartsBarChart
        {...BAR_FRAME}
        data={rows}
        aria-label={summary}
        style={{ width: '100%', height }}
      >
        <CartesianGrid {...GRID} />
        <XAxis dataKey="label" {...X_AXIS} />
        <YAxis
          {...Y_AXIS}
          ticks={[0, max]}
          domain={[0, max]}
          // Compact currency ticks (`₺450 B`) with the sign where formatMoney
          // puts it (GLG-893); `{}` keeps Intl's default compact rounding.
          tickFormatter={(v: number) => formatCompactMoney(v, currency, {})}
        />
        <Bar
          className="series-bars"
          dataKey="value"
          radius={[3, 3, 0, 0]}
          minPointSize={(value) => (value ? 3 : 1)}
          style={{ fill: 'var(--color-accent)' }}
          animationDuration={ANIMATION_MS}
        />
        {/* The card sits above or below the active bar's top, over its slot. */}
        <AnchoredTooltip
          content={tooltipContent}
          anchors={new Map(rows.map((r) => [r.label, r.value]))}
          cursor={{ style: { fill: 'var(--color-well)' } }}
          centred
        />
      </RechartsBarChart>
    </div>
  )
}
