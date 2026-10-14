import type { ReactNode } from 'react'

export type ChartTooltipRow = {
  key: string
  swatch: ReactNode
  label: string
  value: string
  note?: string
}

// ChartTooltip is the card every chart shows on hover and on the arrow keys.
// Each chart turns Recharts' active point into these props; value is already
// formatted from the exact decimal string, so the card prints it as is.
export function ChartTooltip({ title, rows }: { title: string; rows: ChartTooltipRow[] }) {
  return (
    <div
      role="tooltip"
      aria-live="polite"
      className="pointer-events-none rounded-ctl bg-surface p-2 text-xs shadow-pop"
    >
      <div className="mb-1 font-medium text-ink">{title}</div>
      {rows.map((row) => (
        <div key={row.key} className="flex items-center gap-2 text-ink-2">
          {row.swatch}
          <span>
            {row.label}
            {row.note && <span className="text-muted"> ({row.note})</span>}
          </span>
          {row.value && <span className="ml-auto pl-2 text-ink">{row.value}</span>}
        </div>
      ))}
    </div>
  )
}
