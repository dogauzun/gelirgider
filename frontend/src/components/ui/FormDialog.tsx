import { useId, type FormEvent, type ReactNode } from 'react'
import { t } from '../../i18n/strings'
import { Button } from './Button'
import { Modal } from './Modal'
import { sheetPanel } from './sheetPanel'
import { useModal } from './useModal'

// The shell of the form dialogs: title, the fields as children, the
// server error under them, then Cancel and the submit button. `error` field
// null is a server error; a field error is shown by its own Field.
export function FormDialog({
  title,
  widthClass,
  error,
  pending,
  submitLabel,
  submitDisabled = false,
  onSubmit,
  onEdit,
  onClose,
  children,
}: {
  title: string
  widthClass: string
  error: { field: string | null; message: string } | null
  pending: boolean
  submitLabel: string
  // Set until the form can be sent at all, such as no instrument picked.
  submitDisabled?: boolean
  onSubmit: (event: FormEvent) => void
  // An edit drops the error, which belonged to the form as submitted.
  onEdit: () => void
  onClose: () => void
  children: ReactNode
}) {
  const dialogRef = useModal()
  const titleId = useId()
  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <form
        onSubmit={onSubmit}
        onChange={onEdit}
        className={sheetPanel(`max-h-[92vh] overflow-y-auto p-5 ${widthClass}`)}
      >
        <h2 id={titleId} className="text-ink text-[15px] font-semibold">
          {title}
        </h2>
        {children}
        {error && error.field === null && (
          <p role="alert" className="text-loss mt-4 text-[13px]">
            {error.message}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('transactions.cancel')}
          </Button>
          <Button type="submit" pending={pending} disabled={submitDisabled}>
            {submitLabel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
