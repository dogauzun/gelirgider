import type { ReactNode } from 'react'

// Currency and market labels: `USD`, `BIST · TRY`. A `label` becomes the
// hover title and the accessible name, for a badge whose text is a summary.
export function Badge({ children, label }: { children: ReactNode; label?: string }) {
  return (
    <span
      title={label}
      aria-label={label}
      role={label ? 'img' : undefined}
      className="inline-flex h-[18px] items-center rounded-full bg-chip px-2 text-[11px] font-medium text-muted"
    >
      {children}
    </span>
  )
}
