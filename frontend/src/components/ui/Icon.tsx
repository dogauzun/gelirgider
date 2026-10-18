import type { ReactNode } from 'react'

// Inline stroke paths on a 24x24 grid, rendered at 16px. One component instead
// of ten files: the paths are the only thing that differs.
const paths = {
  plus: <path d="M12 5v14M5 12h14" />,
  dots: (
    <g fill="currentColor" stroke="none">
      <circle cx="12" cy="5" r="1.5" />
      <circle cx="12" cy="12" r="1.5" />
      <circle cx="12" cy="19" r="1.5" />
    </g>
  ),
  pencil: <path d="M4 20h4L19 9a2.1 2.1 0 0 0-3-3L5 17v3z" />,
  copy: (
    <>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1" />
    </>
  ),
  trash: (
    <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M10 11v6M14 11v6" />
  ),
  target: (
    <>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="4.5" />
    </>
  ),
  eye: (
    <>
      <path d="M2 12s3.6-6 10-6 10 6 10 6-3.6 6-10 6-10-6-10-6z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  eyeOff: (
    <path d="M3 3l18 18M10.6 6.2A9.9 9.9 0 0 1 12 6c6.4 0 10 6 10 6a17 17 0 0 1-3.3 3.9M6.3 8.1C3.7 9.7 2 12 2 12s3.6 6 10 6a10 10 0 0 0 3.5-.6M9.9 9.9a3 3 0 0 0 4.2 4.2" />
  ),
  warning: (
    <path d="M12 3.5 2.8 19a1 1 0 0 0 .9 1.5h16.6a1 1 0 0 0 .9-1.5L12 3.5zM12 9v5M12 17.4h.01" />
  ),
  x: <path d="M6 6l12 12M18 6L6 18" />,
  chevronDown: <path d="M6 9.5l6 6 6-6" />,
  chevronLeft: <path d="M14.5 6l-6 6 6 6" />,
  chevronRight: <path d="M9.5 6l6 6-6 6" />,
  check: <path d="M5 12.5l4.5 4.5L19 7.5" />,
  minus: <path d="M6 12h12" />,
  // The sidebar's glyphs: the small-screen menu button, all
  // portfolios, administration.
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  layers: <path d="M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5" />,
  shield: <path d="M12 3 5 6v5c0 4.5 3 8.3 7 10 4-1.7 7-5.5 7-10V6l-7-3z" />,
} satisfies Record<string, ReactNode>

type IconName = keyof typeof paths

type IconProps = {
  name: IconName
  // Given: the icon carries the meaning and is announced. Omitted: decorative.
  label?: string
  className?: string
}

export function Icon({ name, label, className }: IconProps) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {paths[name]}
    </svg>
  )
}
