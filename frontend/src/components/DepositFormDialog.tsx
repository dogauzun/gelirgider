import { useId, type ComponentProps, type FormEvent, type ReactNode } from 'react'
import { t } from '../i18n/strings'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'
import { useModal } from './ui/useModal'

// A percent or money field. Field hands it its id and aria props.
export function DecimalInput(props: ComponentProps<'input'>) {
  return <input type="text" inputMode="decimal" className={inputClass()} {...props} />
}

// A date field.
export function DateInput(props: ComponentProps<'input'>) {
  return <input type="date" className={inputClass()} {...props} />
}

type Key = 'rate' | 'stopaj'

// The rate and stopaj fields, typed as percents.
export function RateFields({
  bind,
  fieldError,
}: {
  bind: (key: Key) => ComponentProps<'input'>
  fieldError: (key: Key) => string | undefined
}) {
  return (
    <>
      <Field label={t('deposits.dialog.rate')} error={fieldError('rate')}>
        <DecimalInput {...bind('rate')} />
      </Field>
      <Field label={t('deposits.dialog.stopaj')} error={fieldError('stopaj')}>
        <DecimalInput {...bind('stopaj')} />
      </Field>
    </>
  )
}

// The shell of the open and renew dialogs: title, the fields as children, the
// server error under them, then Cancel and the submit button. `error` field
// null is a server error; a field error is shown by its own Field.
export function DepositFormDialog({
  title,
  widthClass,
  error,
  pending,
  submitLabel,
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
          <Button type="submit" pending={pending}>
            {submitLabel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
