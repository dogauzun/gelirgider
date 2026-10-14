type MarkProps = {
  size?: number
  className?: string
}

export function Mark({ size = 22, className }: MarkProps) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 22 22"
      className={className}
      aria-hidden="true"
    >
      <rect width="22" height="22" rx="6" fill="var(--color-ink)" />
      <polyline
        points="5,15 9,10 12,12.5 17,6"
        fill="none"
        stroke="var(--color-on-ink)"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="17" cy="6" r="1.6" fill="var(--color-on-ink)" />
    </svg>
  )
}
