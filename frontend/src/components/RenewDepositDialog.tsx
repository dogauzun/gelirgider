import { useState } from 'react'
import { useRenewDeposit, type Deposit, type RenewInput } from '../api/deposits'
import { t } from '../i18n/strings'
import { daysBetween } from '../lib/date'
import { formatDecimalInput, mulDecimal } from '../lib/decimal'
import { formatMoney } from '../lib/format'
import {
  decimalError,
  firstError,
  fraction,
  RULES,
  textBinding,
  useDepositSubmit,
} from './depositForm'
import { RateFields } from './DepositFormDialog'
import { FormDialog } from './ui/FormDialog'
import { DateInput, DecimalInput } from './ui/Inputs'
import { Field } from './ui/Field'

type Form = { rate: string; stopaj: string; maturity: string }

// The first field to fix. The server repeats every rule; this saves the round
// trip and puts the message under the field.
const validate = (form: Form, oldMaturity: string) =>
  firstError<keyof Form>([
    ['rate', decimalError(form.rate, RULES.rate)],
    ['stopaj', decimalError(form.stopaj, RULES.stopaj)],
    ['maturity', form.maturity > oldMaturity ? null : t('deposits.error.maturityDate')],
  ])

// The rate, the stopaj and the term of the old deposit, as the form shows
// them: percents, and a maturity one old term after the old maturity.
function oldTerms(deposit: Deposit): Form {
  const term = daysBetween(deposit.start_date, deposit.maturity_date)
  const maturity = new Date(`${deposit.maturity_date}T00:00:00Z`)
  maturity.setUTCDate(maturity.getUTCDate() + term)
  return {
    rate: formatDecimalInput(mulDecimal(deposit.rate, '100')),
    stopaj: formatDecimalInput(mulDecimal(deposit.stopaj_rate, '100')),
    maturity: maturity.toISOString().slice(0, 10),
  }
}

// The dialog that renews a due or matured deposit. The new principal is the
// old one plus its net interest, which is the list item's payout; the server
// computes it again, so it is shown and not sent.
export function RenewDepositDialog({
  deposit,
  onClose,
}: {
  deposit: Deposit
  onClose: () => void
}) {
  const [form, setForm] = useState<Form>(() => oldTerms(deposit))
  const mutation = useRenewDeposit(deposit.id)
  const { error, setError, fieldError, submit } = useDepositSubmit<keyof Form, RenewInput>({
    mutation,
    validate: () => validate(form, deposit.maturity_date),
    toInput: () => ({
      rate: fraction(form.rate),
      stopaj_rate: fraction(form.stopaj),
      maturity_date: form.maturity,
    }),
    doneKey: 'deposits.renewDialog.renewed',
    onClose,
  })
  const bind = textBinding(form, setForm)

  return (
    <FormDialog
      title={t('deposits.renewDialog.title')}
      widthClass="md:w-[28rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={t('deposits.renewDialog.submit')}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
      <div className="mt-4 grid grid-cols-1 gap-4">
        <Field label={t('deposits.renewDialog.principal')}>
          <DecimalInput readOnly value={formatMoney(deposit.payout, deposit.currency)} />
        </Field>
        <RateFields bind={bind} fieldError={fieldError} />
        <Field label={t('deposits.maturity')} error={fieldError('maturity')}>
          <DateInput {...bind('maturity')} min={deposit.maturity_date} />
        </Field>
      </div>
    </FormDialog>
  )
}
