import { cloneElement, useEffect, useId } from 'react'
import type { ReactElement } from 'react'

type ControlProps = {
  id?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}

type FieldProps = {
  label: string
  hint?: string
  error?: string
  children: ReactElement<ControlProps>
}

export function Field({ label, hint, error, children }: FieldProps) {
  const id = useId()
  const messageId = `${id}-message`
  const message = error ?? hint

  // A server error usually lands after the submit button was disabled for the
  // request, which drops focus to the page. Put it back on the invalid control,
  // unless the user has already moved on to another field (GLG-481).
  useEffect(() => {
    if (!error || document.activeElement?.matches('input, select, textarea, [role="combobox"]'))
      return
    document.getElementById(id)?.focus()
  }, [error, id])

  return (
    <div>
      <label htmlFor={id} className="block text-[13px] font-medium text-ink-2">
        {label}
      </label>
      <div className="mt-1.5">
        {cloneElement(children, {
          id,
          'aria-invalid': error ? true : undefined,
          // Keep a caller's own description when Field has none to add.
          'aria-describedby': message ? messageId : children.props['aria-describedby'],
        })}
      </div>
      {message && (
        // Keyed so the error mounts as a fresh alert instead of the hint's
        // text changing under a role screen readers may not pick up.
        <p
          key={error ? 'error' : 'hint'}
          id={messageId}
          role={error ? 'alert' : undefined}
          className={`mt-1 text-[12px] ${error ? 'text-loss' : 'text-muted'}`}
        >
          {message}
        </p>
      )}
    </div>
  )
}
