import { t } from '../../i18n/strings'
import { Button } from './Button'
import { Icon } from './Icon'

// A query that failed to load: what failed, the translated server message,
// and a Retry that refetches it. It is an alert so screen readers
// announce the failure; a second view of the same failure passes
// announce={false} so it is heard once.
export function LoadError({
  title,
  error,
  onRetry,
  retrying = false,
  announce = true,
  className = '',
}: {
  title: string
  error: unknown
  onRetry: () => void
  retrying?: boolean
  announce?: boolean
  className?: string
}) {
  const detail = error instanceof Error ? error.message : ''
  return (
    <div
      role={announce ? 'alert' : undefined}
      className={`flex flex-wrap items-center gap-3 rounded-lg border border-line bg-loss-soft p-3 text-[13px] ${className}`}
    >
      <Icon name="warning" className="shrink-0 text-loss" />
      <div className="min-w-0 flex-1">
        <p className="font-medium text-loss">{title}</p>
        {detail && detail !== title && <p className="text-ink-2">{detail}</p>}
      </div>
      <Button variant="secondary" onClick={onRetry} pending={retrying}>
        {t('loadError.retry')}
      </Button>
    </div>
  )
}
