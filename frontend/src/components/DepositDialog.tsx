import { useId, useState, type FormEvent } from 'react'
import { useCreateDeposit, type DepositInput } from '../api/deposits'
import { t, type StringKey } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import {
  compareDecimal,
  isAmbiguousThousands,
  mulDecimal,
  parseDecimalInput,
  tooManyDigits,
} from '../lib/decimal'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { CURRENCIES } from './transactionForm'
import { Button } from './ui/Button'
import { Checkbox } from './ui/Checkbox'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Modal } from './ui/Modal'
import { Select } from './ui/Select'
import { sheetPanel } from './ui/sheetPanel'
import { useModal } from './ui/useModal'

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
// `field` null is a server error, shown under the form.
type FormError = { field: FieldName | null; message: string }

// The earliest start the server accepts.
const EARLIEST = '1990-01-01'

// How a decimal field is checked. `fraction` is the digits after the point it
// may have: percent fields keep 7, so the fraction sent has the server's 9.
type Rule = {
  label: StringKey
  fraction: number
  ok: (parsed: string) => boolean
  errorKey: StringKey
}

const positive = (parsed: string) => compareDecimal(parsed, '0') > 0

const RULES: Record<'principal' | 'rate' | 'stopaj', Rule> = {
  principal: {
    label: 'deposits.principal',
    fraction: 9,
    ok: positive,
    errorKey: 'deposits.error.principal',
  },
  rate: {
    label: 'deposits.rate',
    fraction: 7,
    // The server takes a fraction of at most 10, which is 1000 %.
    ok: (parsed) => positive(parsed) && compareDecimal(parsed, '1000') <= 0,
    errorKey: 'deposits.error.rate',
  },
  stopaj: {
    label: 'deposits.stopaj',
    fraction: 7,
    ok: (parsed) => compareDecimal(parsed, '100') < 0,
    errorKey: 'deposits.error.stopaj',
  },
}

// The message for a typed decimal that is not an allowed value, else null.
function decimalError(text: string, rule: Rule): string | null {
  if (isAmbiguousThousands(text)) return t('decimal.error.ambiguousThousands')
  const parsed = parseDecimalInput(text)
  if (parsed === null || !rule.ok(parsed)) return t(rule.errorKey)
  if (tooManyDigits(parsed, rule.fraction)) {
    return t('decimal.error.tooManyDigits', { field: t(rule.label), fraction: rule.fraction })
  }
  return null
}

// The first field to fix. The server repeats every rule; this saves the round
// trip and puts the message under the field.
function validate(form: Form, today: string): FormError | null {
  const checks: [FieldName, string | null][] = [
    ['principal', decimalError(form.principal, RULES.principal)],
    ['rate', decimalError(form.rate, RULES.rate)],
    ['stopaj', decimalError(form.stopaj, RULES.stopaj)],
    ['start', form.start >= EARLIEST && form.start <= today ? null : t('deposits.error.startDate')],
    ['maturity', form.maturity > form.start ? null : t('deposits.error.maturityDate')],
  ]
  const failed = checks.find(([, message]) => message !== null)
  return failed ? { field: failed[0], message: failed[1] as string } : null
}

// Rate and stopaj are typed as percents and sent as fractions: "45" -> "0.45",
// a decimal shift on the string, never through a float.
const fraction = (percent: string) => mulDecimal(parseDecimalInput(percent) ?? '0', '0.01')

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
  const [error, setError] = useState<FormError | null>(null)
  const mutation = useCreateDeposit(portfolioId)
  const once = useOneAtATime()
  const dialogRef = useModal()
  const titleId = useId()
  const fieldError = (field: FieldName) => (error?.field === field ? error.message : undefined)

  function submit(event: FormEvent) {
    event.preventDefault()
    // Enter in a field comes straight here: a second press while the save
    // runs would open the deposit twice.
    if (mutation.isPending) return
    const invalid = validate(form, today)
    setError(invalid)
    if (invalid) return
    once(() =>
      mutation.mutateAsync(toInput(form), {
        onSuccess: () => {
          toast(t('deposits.dialog.opened'))
          onClose()
        },
        onError: (mutationError) => setError({ field: null, message: mutationError.message }),
      }),
    )
  }

  const decimal = (key: 'principal' | 'rate' | 'stopaj') => (
    <input
      type="text"
      inputMode="decimal"
      value={form[key]}
      onChange={(event) => setForm({ ...form, [key]: event.target.value })}
      className={inputClass()}
    />
  )
  const date = (key: 'start' | 'maturity', min: string, max?: string) => (
    <input
      type="date"
      value={form[key]}
      min={min}
      max={max}
      onChange={(event) => setForm({ ...form, [key]: event.target.value })}
      className={inputClass()}
    />
  )

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <form
        onSubmit={submit}
        // An edit drops the error, which belonged to the form as submitted.
        onChange={() => setError(null)}
        className={sheetPanel('max-h-[92vh] overflow-y-auto p-5 md:w-[36rem]')}
      >
        <h2 id={titleId} className="text-ink text-[15px] font-semibold">
          {t('deposits.dialog.title')}
        </h2>
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
            {decimal('principal')}
          </Field>
          <Field label={t('deposits.dialog.rate')} error={fieldError('rate')}>
            {decimal('rate')}
          </Field>
          <Field label={t('deposits.dialog.stopaj')} error={fieldError('stopaj')}>
            {decimal('stopaj')}
          </Field>
          <Field label={t('deposits.start')} error={fieldError('start')}>
            {date('start', EARLIEST, today)}
          </Field>
          <Field label={t('deposits.maturity')} error={fieldError('maturity')}>
            {date('maturity', form.start)}
          </Field>
          <div className="sm:col-span-2">
            <Field label={`${t('transactions.note')} · ${t('transactions.optional')}`}>
              <input
                type="text"
                value={form.note}
                maxLength={500}
                onChange={(event) => setForm({ ...form, note: event.target.value })}
                className={inputClass()}
              />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Checkbox
              checked={form.fund}
              onChange={(fund) => setForm({ ...form, fund })}
              label={t('deposits.fund')}
              description={t('deposits.fundHint')}
            />
          </div>
        </div>
        {error && error.field === null && (
          <p role="alert" className="text-loss mt-4 text-[13px]">
            {error.message}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('transactions.cancel')}
          </Button>
          <Button type="submit" pending={mutation.isPending}>
            {t('deposits.dialog.submit')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
