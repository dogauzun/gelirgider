// Button's classes live apart from the component so a link styled as a
// button can share them (react-refresh wants component files to export only
// components).

export const variants = {
  primary: 'bg-accent text-on-ink shadow-ctl hover:bg-accent-ink',
  // `raised`, not `surface`: in dark a secondary on a card needs a fill
  // lighter than the card, and a hover that does not sink into it.
  secondary: 'bg-raised text-ink shadow-ctl hover:bg-raised-hover',
  ghost: 'text-ink-2 hover:bg-paper',
  danger: 'bg-loss text-on-ink shadow-ctl',
  // A destructive action that sits on every row of a list, so it should not
  // be the loudest thing on the page; its confirm keeps \`danger\`.
  'danger-quiet': 'bg-raised text-loss shadow-ctl hover:bg-loss-soft',
}

export const sizes = {
  md: 'h-9 text-[14px]',
  lg: 'h-11 text-[15px]',
  // A row action in a dense list, or a lookup under a field.
  sm: 'h-6 text-[12px]',
}

// Kept apart from the height so `iconOnly` can drop it; `p-0` alone would lose
// to `px-*` in the generated stylesheet.
const paddings = {
  md: 'px-3.5',
  lg: 'px-4.5',
  sm: 'px-2',
}

// The class string of a Button, for a link that looks like one, so
// the two cannot drift apart.
export function buttonClass({
  variant = 'primary',
  size = 'md',
  iconOnly = false,
  className = '',
}: {
  variant?: keyof typeof variants
  size?: keyof typeof sizes
  iconOnly?: boolean
  className?: string
} = {}) {
  return `inline-flex items-center justify-center gap-2 rounded-ctl font-medium whitespace-nowrap disabled:opacity-50 aria-disabled:opacity-50 ${variants[variant]} ${sizes[size]} ${iconOnly ? 'w-9 p-0' : paddings[size]} ${className}`
}
