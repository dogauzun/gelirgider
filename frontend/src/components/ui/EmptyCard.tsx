import type { ReactNode } from 'react'

// The card a list or page shows when there is nothing to list yet: an
// optional icon, the title, an optional sentence under it, then the actions
// as children. `className` adds the card's outer spacing (mb-6).
export function EmptyCard({
  title,
  body,
  icon,
  className,
  children,
}: {
  title: string
  body?: string
  icon?: ReactNode
  className?: string
  children?: ReactNode
}) {
  return (
    <section
      className={[className, 'flex flex-col items-center gap-3 card px-6 py-12 text-center']
        .filter(Boolean)
        .join(' ')}
    >
      {icon}
      <p className="text-[16px] font-semibold text-ink">{title}</p>
      {body && <p className="text-[13px] text-muted">{body}</p>}
      {children}
    </section>
  )
}
