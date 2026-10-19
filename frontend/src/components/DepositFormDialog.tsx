import type { ComponentProps } from 'react'
import { t } from '../i18n/strings'
import { Field } from './ui/Field'
import { DecimalInput } from './ui/Inputs'

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
