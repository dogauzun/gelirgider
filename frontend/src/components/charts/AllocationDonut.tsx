import { Pie, PieChart, Tooltip } from 'recharts'
import { t } from '../../i18n/strings'
import { DONUT_SIZE, SHARE_FORMAT, type AllocationRow } from '../../lib/allocation'
import { pressWithoutFocus } from '../../lib/chart'
import { formatMoney, formatNumber } from '../../lib/format'
import { usePrivacy } from '../../lib/privacy'
import type { TooltipProps } from './AnchoredTooltip'
import { ChartTooltip } from './ChartTooltip'

const RING = 16
// About a 1 px gap between slices at the ring's inner edge; Recharts drops
// the padding when there is one slice, so a single row is a full ring.
const PADDING_ANGLE = 1
// The other slices while one is highlighted.
const DIMMED = 0.3

export interface AllocationDonutProps {
  rows: AllocationRow[]
  currency: string
  // The total in the middle, already formatted (and masked in privacy mode).
  totalLabel: string
  // The highlighted row's key, shared with the legend.
  active: string | null
  onActive: (key: string | null) => void
}

// AllocationDonut draws the allocation rows as a ring on Recharts, slices in
// the legend's order from twelve o'clock, with "Toplam" and the total in the
// middle as our own HTML. It is decorative: the card's legend list carries
// every number, so the donut is aria-hidden, has no focus stop and no
// keyboard layer (GLG-947). Hovering a slice highlights it and its legend row
// through onActive, and shows the shared tooltip card.
export function AllocationDonut({
  rows,
  currency,
  totalLabel,
  active,
  onActive,
}: AllocationDonutProps) {
  // Re-render when privacy flips, so the tooltip masks at once.
  usePrivacy()

  // No `key` field: Recharts spreads each entry onto its sector.
  const data = rows.map((row) => ({
    name: row.label,
    value: row.value,
    fill: row.color,
    fillOpacity: active !== null && active !== row.key ? DIMMED : 1,
  }))

  function tooltipContent({ active: shown, activeIndex }: TooltipProps) {
    const row = shown && activeIndex != null ? rows[Number(activeIndex)] : undefined
    if (!row) return null
    return (
      <ChartTooltip
        title={row.label}
        rows={[
          {
            key: 'value',
            swatch: (
              <span
                className="size-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: row.color }}
              />
            ),
            label: formatMoney(row.amount, currency),
            value: formatNumber(row.share, SHARE_FORMAT),
          },
        ]}
      />
    )
  }

  return (
    // A press or tap takes no focus (pressWithoutFocus, GLG-1034, GLG-1044).
    <div
      aria-hidden="true"
      className="relative shrink-0"
      data-allocation-donut
      onMouseDown={pressWithoutFocus}
    >
      <PieChart
        width={DONUT_SIZE}
        height={DONUT_SIZE}
        margin={{ top: 0, right: 0, bottom: 0, left: 0 }}
        accessibilityLayer={false}
      >
        <Pie
          data={data}
          dataKey="value"
          outerRadius={DONUT_SIZE / 2}
          innerRadius={DONUT_SIZE / 2 - RING}
          startAngle={90}
          endAngle={-270}
          paddingAngle={PADDING_ANGLE}
          stroke="none"
          rootTabIndex={-1}
          onMouseEnter={(_, index) => onActive(rows[index]?.key ?? null)}
          onMouseLeave={() => onActive(null)}
        />
        {/* Recharts keeps the card inside the donut's box, flipping it to the
            slice's other side, so it never runs past a phone's edge (GLG-1028). */}
        <Tooltip content={tooltipContent} isAnimationActive={false} wrapperStyle={{ zIndex: 10 }} />
      </PieChart>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-[11px] text-muted">{t('allocation.total')}</span>
        <span className="text-[14px] font-semibold text-ink tabular-nums">{totalLabel}</span>
      </div>
    </div>
  )
}
