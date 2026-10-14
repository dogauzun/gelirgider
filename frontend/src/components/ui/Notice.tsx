import type { ReactNode } from 'react'
import { Icon } from './Icon'

const tones = {
  warn: 'border-warn-line bg-warn-soft text-warn',
  error: 'border-loss-line bg-loss-soft text-loss',
}

// `warn` for advice the user may act on; `error` for what failed or blocks
// the next step. An alert is an error unless told otherwise.
// `role="alert"` is for an error, after a user action or a failed
// load, so screen readers announce it; static warnings leave it off.
export function Notice({
  children,
  role,
  tone = role === 'alert' ? 'error' : 'warn',
}: {
  children: ReactNode
  role?: 'alert'
  tone?: keyof typeof tones
}) {
  return (
    <div
      role={role}
      className={`flex items-start gap-2 rounded-lg border p-3 text-[13px] ${tones[tone]}`}
    >
      <Icon name="warning" className="mt-0.5 shrink-0" />
      <div>{children}</div>
    </div>
  )
}
