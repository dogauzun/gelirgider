import { useEffect, useState } from 'react'
import { t } from '../../i18n/strings'
import { flushToasts, pauseToasts, removeToast, useToasts, type Toast } from '../../lib/toast'
import { Icon } from './Icon'

export function Toaster() {
  const items = useToasts()
  // By id, so a toast that leaves under the pointer or with focus (undo
  // pressed, pushed out) cannot keep the rest paused.
  const [hoverId, setHoverId] = useState<number | null>(null)
  const [focusId, setFocusId] = useState<number | null>(null)
  const hold = items.some((item) => item.id === hoverId || item.id === focusId)

  useEffect(() => {
    pauseToasts(hold)
    return () => pauseToasts(false)
  }, [hold])

  useEffect(() => {
    window.addEventListener('pagehide', flushToasts)
    return () => window.removeEventListener('pagehide', flushToasts)
  }, [])

  const renderToast = (item: Toast) => (
    <div
      key={item.id}
      onPointerEnter={() => setHoverId(item.id)}
      onPointerLeave={() => setHoverId(null)}
      onFocus={() => setFocusId(item.id)}
      onBlur={() => setFocusId(null)}
      className={`pointer-events-auto flex max-w-full items-center gap-3 rounded-lg px-4 py-2.5 text-[13px] shadow-pop ${item.error ? 'border border-loss-line bg-loss-soft text-loss' : 'bg-surface text-ink'}`}
    >
      <span>{item.message}</span>
      {item.undo && (
        <button
          type="button"
          autoFocus={item.focus}
          onClick={() => {
            item.undo?.()
            removeToast(item.id)
          }}
          className="shrink-0 font-medium underline underline-offset-2"
        >
          {t('toast.undo')}
        </button>
      )}
      {item.error && (
        <button
          type="button"
          aria-label={t('toast.close')}
          onClick={() => removeToast(item.id)}
          className="shrink-0"
        >
          <Icon name="x" />
        </button>
      )}
    </div>
  )

  // The live regions are always in the page: one that arrives together with
  // its text is often not announced (GLG-795). Errors go to an assertive one
  // (GLG-848). From lg they centre on the content column, right of the
  // sidebar (232 px, AppShell's grid); below md they sit above the floating
  // add button (bottom-4, size-14) rather than over it (GLG-940).
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-50 flex flex-col items-center gap-2 px-4 max-md:bottom-24 lg:left-[232px]">
      <div role="alert" aria-atomic="false" className="flex flex-col items-center gap-2">
        {items.filter((item) => item.error).map(renderToast)}
      </div>
      <div role="status" aria-atomic="false" className="flex flex-col items-center gap-2">
        {items.filter((item) => !item.error).map(renderToast)}
      </div>
    </div>
  )
}
