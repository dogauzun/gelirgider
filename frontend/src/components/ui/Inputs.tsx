import type { ComponentProps } from 'react'
import { t } from '../../i18n/strings'
import { Field } from './Field'
import { inputClass } from './inputClass'

// A percent or money field. Field hands it its id and aria props.
export function DecimalInput(props: ComponentProps<'input'>) {
  return <input type="text" inputMode="decimal" className={inputClass()} {...props} />
}

// A date field.
export function DateInput(props: ComponentProps<'input'>) {
  return <input type="date" className={inputClass()} {...props} />
}

// The optional note field, across both columns.
export function NoteField(props: ComponentProps<'input'>) {
  return (
    <div className="sm:col-span-2">
      <Field label={`${t('transactions.note')} · ${t('transactions.optional')}`}>
        <input type="text" maxLength={500} className={inputClass()} {...props} />
      </Field>
    </div>
  )
}
