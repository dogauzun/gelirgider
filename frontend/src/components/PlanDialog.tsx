import { useState } from 'react'
import {
  useCreatePlan,
  useUpdatePlan,
  type Plan,
  type PlanInput,
  type PlanUpdate,
} from '../api/plans'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { compareDecimal, formatDecimalInput, parseDecimalInput } from '../lib/decimal'
import { decimalError, firstError, textBinding, useDepositSubmit, type Rule } from './depositForm'
import { FormDialog } from './ui/FormDialog'
import { DateInput, DecimalInput, NoteField } from './ui/Inputs'
import { InstrumentSearchInput } from './InstrumentSearchInput'
import { CURRENCIES } from './transactionForm'
import { Checkbox } from './ui/Checkbox'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Segmented } from './ui/Segmented'
import { Select } from './ui/Select'

type Kind = 'buy' | 'deposit'
const KINDS: readonly Kind[] = ['buy', 'deposit']

type Form = {
  kind: Kind
  currency: string
  amount: string
  day: string
  start: string
  end: string
  note: string
  fund: boolean
}

type FieldName = 'instrument' | 'amount' | 'day' | 'start' | 'end'

const AMOUNT: Rule = {
  label: 'plans.amount',
  fraction: 9,
  ok: (parsed) => compareDecimal(parsed, '0') > 0,
  errorKey: 'plans.error.amount',
}

// The earliest start the server accepts.
const EARLIEST = '1990-01-01'

// The latest: one year after today. Feb 29 + 1 year is a non-date that still
// compares right as a string.
const latestStart = (today: string) => `${Number(today.slice(0, 4)) + 1}${today.slice(4)}`

// The first field to fix. The server repeats every rule; this saves the round
// trip and puts the message under the field. An edit keeps the start date.
function validate(form: Form, instrumentId: number | null, startDate: string, today: string) {
  const day = Number(form.day)
  return firstError<FieldName>([
    [
      'instrument',
      form.kind === 'deposit' || instrumentId !== null ? null : t('plans.error.instrument'),
    ],
    ['amount', decimalError(form.amount, AMOUNT)],
    [
      'day',
      form.day !== '' && Number.isInteger(day) && day >= 1 && day <= 31
        ? null
        : t('error.field.day_of_month'),
    ],
    [
      'start',
      startDate >= EARLIEST && startDate <= latestStart(today) ? null : t('plans.error.startDate'),
    ],
    ['end', form.end === '' || form.end >= startDate ? null : t('plans.error.endDate')],
  ])
}

// The dialog that adds a saving plan, or with `plan` edits one. An edit
// changes the amount, day, end date, fund and note only: the server fixes the
// instrument, currency and start date.
export function PlanDialog({
  portfolioId,
  plan,
  onClose,
}: {
  portfolioId: number
  plan?: Plan
  onClose: () => void
}) {
  const today = todayIstanbul()
  const [form, setForm] = useState<Form>({
    kind: plan && plan.instrument_id === null ? 'deposit' : 'buy',
    currency: plan?.currency ?? 'TRY',
    amount: plan ? formatDecimalInput(plan.amount) : '',
    day: plan ? String(plan.day_of_month) : '',
    start: plan?.start_date ?? today,
    end: plan?.end_date ?? '',
    note: plan ? plan.note : t('plans.dialog.defaultNote'),
    fund: plan?.fund ?? false,
  })
  const [instrument, setInstrument] = useState<{
    id: number | null
    label: string
    currency: string | null
  }>({ id: null, label: '', currency: null })
  const create = useCreatePlan(portfolioId)
  const update = useUpdatePlan(portfolioId)
  const editing = plan !== undefined
  const mutation = {
    isPending: create.isPending || update.isPending,
    mutateAsync: (
      input: PlanInput | PlanUpdate,
      options: { onSuccess: () => void; onError: (error: Error) => void },
    ) =>
      plan
        ? update.mutateAsync({ id: plan.id, input: input as PlanUpdate }, options)
        : create.mutateAsync(input as PlanInput, options),
  }
  const { error, setError, fieldError, submit } = useDepositSubmit<
    FieldName,
    PlanInput | PlanUpdate
  >({
    mutation,
    validate: () => validate(form, plan?.instrument_id ?? instrument.id, form.start, today),
    toInput: () => {
      const amount = parseDecimalInput(form.amount) ?? ''
      const day_of_month = Number(form.day)
      const note = form.note.trim() === '' ? undefined : form.note
      if (plan) {
        return {
          amount,
          day_of_month,
          end_date: form.end,
          fund: form.kind === 'buy' && form.fund,
          note: note ?? '',
        }
      }
      return {
        ...(form.kind === 'buy'
          ? { instrument_id: instrument.id ?? 0 }
          : { currency: form.currency }),
        amount,
        day_of_month,
        start_date: form.start,
        ...(form.end !== '' && { end_date: form.end }),
        // Unticked sends nothing: a user who already recorded the cash must
        // not get it twice.
        ...(form.kind === 'buy' && form.fund && { fund: true as const }),
        ...(note !== undefined && { note }),
      }
    },
    doneKey: editing ? 'plans.dialog.updated' : 'plans.dialog.added',
    onClose,
  })

  const bind = textBinding(form, setForm)
  const buy = form.kind === 'buy'
  const amountLabel = t('plans.amount')
  const currency = plan?.currency ?? (buy ? instrument.currency : form.currency)

  return (
    <FormDialog
      title={editing ? t('plans.dialog.editTitle') : t('plans.dialog.addTitle')}
      widthClass="md:w-[36rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={editing ? t('plans.dialog.save') : t('plans.dialog.submit')}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
      <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
        {!editing && (
          <div className="sm:col-span-2">
            <Segmented
              options={KINDS}
              value={form.kind}
              onChange={(kind) => {
                setError(null)
                setForm({ ...form, kind })
              }}
              label={(kind) => t(`plans.dialog.kind.${kind}`)}
              aria-label={t('plans.dialog.kind')}
            />
          </div>
        )}
        {buy && !editing && (
          <div className="sm:col-span-2">
            <Field label={t('transactions.instrument')} error={fieldError('instrument')}>
              <InstrumentSearchInput
                value={instrument.label}
                selected={instrument.id !== null}
                onChange={(label) => setInstrument({ id: null, label, currency: null })}
                onSelect={(picked) =>
                  setInstrument({
                    id: picked.id,
                    label: `${picked.symbol} - ${picked.name}`,
                    currency: picked.currency,
                  })
                }
              />
            </Field>
          </div>
        )}
        {!buy && !editing && (
          <Field label={t('transactions.currency')}>
            <Select
              value={form.currency}
              onChange={(next) => {
                setError(null)
                setForm({ ...form, currency: next })
              }}
              options={CURRENCIES.map((c) => ({ value: c, label: t(`currency.${c}`) }))}
            />
          </Field>
        )}
        <Field
          label={currency ? `${amountLabel} · ${currency}` : amountLabel}
          error={fieldError('amount')}
        >
          <DecimalInput {...bind('amount')} />
        </Field>
        <Field label={t('plans.dialog.dayField')} error={fieldError('day')}>
          <input type="number" min={1} max={31} className={inputClass()} {...bind('day')} />
        </Field>
        {!editing && (
          <Field label={t('plans.start')} error={fieldError('start')}>
            <DateInput {...bind('start')} min={EARLIEST} max={latestStart(today)} />
          </Field>
        )}
        <Field
          label={`${t('plans.end')} · ${t('transactions.optional')}`}
          error={fieldError('end')}
        >
          <DateInput {...bind('end')} min={form.start} />
        </Field>
        <NoteField {...bind('note')} />
        {buy && (
          <div className="sm:col-span-2">
            <Checkbox
              checked={form.fund}
              onChange={(fund) => setForm({ ...form, fund })}
              label={t('plans.dialog.fund')}
              description={t('plans.dialog.fundHint')}
            />
          </div>
        )}
      </div>
    </FormDialog>
  )
}
