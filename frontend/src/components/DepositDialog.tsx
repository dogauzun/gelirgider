import { useState } from 'react'
import { useCreateDeposit, type DepositInput } from '../api/deposits'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { parseDecimalInput } from '../lib/decimal'
import {
  decimalError,
  firstError,
  fraction,
  RULES,
  textBinding,
  useDepositSubmit,
} from './depositForm'
import {
  DateInput,
  DecimalInput,
  DepositFormDialog,
  NoteField,
  RateFields,
} from './DepositFormDialog'
import { CURRENCIES } from './transactionForm'
import { Checkbox } from './ui/Checkbox'
import { Field } from './ui/Field'
import { Select } from './ui/Select'

type Form = {
  currency: string
  principal: string
  rate: string
  stopaj: string
  start: string
  maturity: string
  note: string
  fund: boolean
}

type FieldName = 'principal' | 'rate' | 'stopaj' | 'start' | 'maturity'

// The earliest start the server accepts.
const EARLIEST = '1990-01-01'

// The first field to fix. The server repeats every rule; this saves the round
// trip and puts the message under the field.
function validate(form: Form, today: string) {
  return firstError<FieldName>([
    ['principal', decimalError(form.principal, RULES.principal)],
    ['rate', decimalError(form.rate, RULES.rate)],
    ['stopaj', decimalError(form.stopaj, RULES.stopaj)],
    ['start', form.start >= EARLIEST && form.start <= today ? null : t('deposits.error.startDate')],
    ['maturity', form.maturity > form.start ? null : t('deposits.error.maturityDate')],
  ])
}

function toInput(form: Form): DepositInput {
  return {
    currency: form.currency,
    principal: parseDecimalInput(form.principal) ?? '',
    rate: fraction(form.rate),
    stopaj_rate: fraction(form.stopaj),
    start_date: form.start,
    maturity_date: form.maturity,
    ...(form.note.trim() !== '' && { note: form.note }),
    // Unticked sends nothing: a user who already recorded the cash must not
    // get it twice.
    ...(form.fund && { fund: true as const }),
  }
}

// The dialog that opens a term deposit. The stopaj is not pre-filled: it is
// the user's to state.
export function DepositDialog({
  portfolioId,
  onClose,
}: {
  portfolioId: number
  onClose: () => void
}) {
  const today = todayIstanbul()
  const [form, setForm] = useState<Form>({
    currency: 'TRY',
    principal: '',
    rate: '',
    stopaj: '',
    start: today,
    maturity: '',
    note: '',
    fund: false,
  })
  const mutation = useCreateDeposit(portfolioId)
  const { error, setError, fieldError, submit } = useDepositSubmit<FieldName, DepositInput>({
    mutation,
    validate: () => validate(form, today),
    toInput: () => toInput(form),
    doneKey: 'deposits.dialog.opened',
    onClose,
  })

  const bind = textBinding(form, setForm)

  return (
    <DepositFormDialog
      title={t('deposits.dialog.title')}
      widthClass="md:w-[36rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={t('deposits.dialog.submit')}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
      <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('transactions.currency')}>
          <Select
            value={form.currency}
            onChange={(currency) => {
              setError(null)
              setForm({ ...form, currency })
            }}
            options={CURRENCIES.map((c) => ({ value: c, label: t(`currency.${c}`) }))}
          />
        </Field>
        <Field label={t('deposits.principal')} error={fieldError('principal')}>
          <DecimalInput {...bind('principal')} />
        </Field>
        <RateFields bind={bind} fieldError={fieldError} />
        <Field label={t('deposits.start')} error={fieldError('start')}>
          <DateInput {...bind('start')} min={EARLIEST} max={today} />
        </Field>
        <Field label={t('deposits.maturity')} error={fieldError('maturity')}>
          <DateInput {...bind('maturity')} min={form.start} />
        </Field>
        <NoteField {...bind('note')} />
        <div className="sm:col-span-2">
          <Checkbox
            checked={form.fund}
            onChange={(fund) => setForm({ ...form, fund })}
            label={t('deposits.fund')}
            description={t('deposits.fundHint')}
          />
        </div>
      </div>
    </DepositFormDialog>
  )
}
