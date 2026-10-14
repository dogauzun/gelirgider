// Native <dialog> instead of a modal library: showModal() already gives the
// top layer, the ::backdrop, focus trapping and Escape for free.
import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { Button } from './Button'
import { t } from '../../i18n/strings'
import { Modal } from './Modal'
import { sheetPanel } from './sheetPanel'

export type ConfirmOptions = {
  title: string
  body?: string
  confirmLabel: string
  danger?: boolean
}

type ConfirmDialogProps = ConfirmOptions & {
  open: boolean
  // Extra content under the body, e.g. a type-to-confirm input that keeps
  // the confirm button disabled until it matches.
  children?: ReactNode
  confirmDisabled?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  danger = false,
  children,
  confirmDisabled = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const titleId = useId()

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return
    if (open) {
      if (!dialog.open) dialog.showModal()
      // showModal() autofocuses the first focusable child; the destructive
      // button must not be it.
      cancelRef.current?.focus()
    } else if (dialog.open) {
      dialog.close()
      // A double click closes the dialog on its first click, and its second
      // (detail 2) lands on what sat under the button, e.g. a row that opens
      // its edit dialog. Swallow that one; the listener goes with
      // the next click whatever it is.
      document.addEventListener(
        'click',
        (event) => {
          if (event.detail < 2) return
          event.stopPropagation()
          event.preventDefault()
        },
        { capture: true, once: true },
      )
    }
  }, [open])

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onCancel}>
      <div className={sheetPanel('p-5 md:w-[26rem]')}>
        {/* Titles and bodies carry user content (an email, a symbol): a long
            unbroken one wraps inside the card instead of running past it. */}
        <h2 id={titleId} className="text-ink text-[15px] font-semibold wrap-anywhere">
          {title}
        </h2>
        {body && <p className="text-ink-2 mt-2 text-[13px] wrap-anywhere">{body}</p>}
        {children}
        <div className="mt-5 flex justify-end gap-2">
          <Button ref={cancelRef} variant="secondary" onClick={onCancel}>
            {t('confirm.cancel')}
          </Button>
          <Button
            variant={danger ? 'danger' : 'primary'}
            disabled={confirmDisabled}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
