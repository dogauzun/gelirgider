import type { ReactNode } from 'react'

// The header of a page that is not a portfolio page: a 20 px title, an
// optional muted line under it, optional actions on the right. Below lg the
// top bar shows the title, so the h1 is for screen readers only there, as on
// the portfolio pages (GLG-940).
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-[20px] font-semibold text-ink max-lg:sr-only">{title}</h1>
        {description && <p className="mt-1 text-[13px] text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap gap-2">{actions}</div>}
    </div>
  )
}
