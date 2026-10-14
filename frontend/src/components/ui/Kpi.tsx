import { Popover } from '@base-ui/react/popover'
import type { ReactNode } from 'react'

// The KPI card: hairlines between the cells come from the grid gap over a
// `line` background, so the cells need no border arithmetic when the grid
// folds to two columns. On a phone the total and the last card span both
// columns, and the two half cells leave ~122px for the figure,
// which the grid's `overflow-hidden` clips mid-number: their
// figure drops to 16px there, and to 14px below 390px. The figure never
// wraps: FitFigure shrinks a figure too long for its cell below
// that size.
const KPI_SHELL = 'grid gap-px overflow-hidden rounded-card bg-line shadow-card'
export const KPI_WIDE = 'col-span-2 sm:col-span-1'
export const KPI_FIGURE =
  'mt-1 font-semibold whitespace-nowrap tabular-nums [--fig-max:14px] min-[390px]:[--fig-max:16px] sm:[--fig-max:20px]'
const KPI_LABEL = 'text-[11px] font-medium tracking-wide text-muted uppercase'

// The KPI card as a component. `columns` is the full set of grid column
// classes, phone included (`grid-cols-2 lg:grid-cols-5`); `className` places
// the card on its page.
export function KpiStrip({
  columns,
  className = '',
  children,
}: {
  columns: string
  className?: string
  children: ReactNode
}) {
  return <section className={`${className} ${KPI_SHELL} ${columns}`}>{children}</section>
}

// One cell of a KpiStrip: its label, then the figure and any sub-lines.
// `className` sets the cell's column span. `hint` explains the figure: the
// label becomes a button that shows it on hover, tap or Enter. The popup is
// portaled, so the strip's `overflow-hidden` does not clip it; Toggletip's
// bubble would be.
export function KpiCell({
  label,
  hint,
  className = '',
  children,
}: {
  label: string
  hint?: string
  className?: string
  children: ReactNode
}) {
  return (
    <div className={`bg-surface p-5 ${className}`}>
      <p className={KPI_LABEL}>
        {hint ? (
          <Popover.Root>
            <Popover.Trigger
              openOnHover
              delay={0}
              className="cursor-help rounded uppercase underline decoration-dotted underline-offset-2"
            >
              {label}
            </Popover.Trigger>
            <Popover.Portal>
              {/* Above the sticky top bar (z-20), as Menu. */}
              <Popover.Positioner sideOffset={4} align="start" className="z-30">
                {/* A dialog needs a name: the label it explains. */}
                <Popover.Popup
                  aria-label={label}
                  className="max-w-[16rem] rounded bg-surface px-2 py-1 text-[12px] text-ink shadow-pop outline-none"
                >
                  {hint}
                </Popover.Popup>
              </Popover.Positioner>
            </Popover.Portal>
          </Popover.Root>
        ) : (
          label
        )}
      </p>
      {children}
    </div>
  )
}

// A one-line figure at its --fig-max size, or smaller when the text would
// not fit the width of the paragraph: a nine- or ten-digit amount
// (₺1.234.567.890,12) at 360px, or in a fifth of the page at lg.
// The paragraph is the size container, so 100cqi is its width; a figure
// character is at most ~0.55em wide (tabular digits, "₺", sign, separators),
// so `length * 0.55em` bounds the text width. `mark`, a one-character marker
// such as the Portföyler total's missing-rates asterisk, follows the text and
// counts as one more character.
export function FitFigure({
  className,
  text,
  mark,
}: {
  className: string
  text: string
  mark?: ReactNode
}) {
  const length = text.length + (mark ? 1 : 0)
  return (
    <p className={`@container ${className}`}>
      <span
        className="block"
        style={{ fontSize: `min(var(--fig-max), ${(100 / (length * 0.55)).toFixed(2)}cqi)` }}
      >
        {text}
        {mark}
      </span>
    </p>
  )
}
