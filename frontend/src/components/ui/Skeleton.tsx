// Size comes from the call site, e.g. <Skeleton className="h-4 w-24" />.
export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse rounded bg-line ${className}`} />
}

// Placeholder rows shaped like a list card while it loads. Screen readers get
// the label instead of the pulse.
export function SkeletonRows({ label, rows = 6 }: { label: string; rows?: number }) {
  return (
    <div role="status" className="card">
      <span className="sr-only">{label}</span>
      {Array.from({ length: rows }, (_, row) => (
        <div
          key={row}
          className="flex items-center gap-4 border-b border-line px-4 py-3 last:border-0"
        >
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-4 flex-1" />
          <Skeleton className="h-4 w-24" />
        </div>
      ))}
    </div>
  )
}
