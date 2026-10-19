import { useId, useState, type ComponentProps, type FormEvent } from 'react'
import { useDeleteGoal, useGoal, useSetGoal, type Goal } from '../api/goal'
import { usePortfolioSummary, type PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import {
  compareDecimal,
  formatDecimalInput,
  isAmbiguousThousands,
  parseDecimalInput,
  tooManyDigits,
} from '../lib/decimal'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { DateInput } from './ui/Inputs'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Modal } from './ui/Modal'
import { Select } from './ui/Select'
import { sheetPanel } from './ui/sheetPanel'
import { useConfirm } from './ui/useConfirm'
import { useModal } from './ui/useModal'

// The units the server accepts for a reporting currency: real only for TRY,
// usd only for a currency other than USD.
function unitsFor(currency: string): Goal['unit'][] {
  return [
    'nominal',
    ...(currency === 'TRY' ? (['real'] as const) : []),
    ...(currency !== 'USD' ? (['usd'] as const) : []),
  ]
}

// The currency a goal in `unit` is stated in.
function unitCurrency(unit: Goal['unit'], currency: string): string {
  if (unit === 'real') return 'TRY'
  return unit === 'usd' ? 'USD' : currency
}

// A decimal field with the currency laid over its right end. Field hands it
// its id and aria props, which go to the input.
function AmountInput({ suffix, ...props }: ComponentProps<'input'> & { suffix: string }) {
  return (
    <div className="relative">
      <input
        type="text"
        inputMode="decimal"
        className={inputClass({ trailingIcon: true })}
        {...props}
      />
      <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-[13px] text-muted">
        {suffix}
      </span>
    </div>
  )
}

// The set, change and delete form. It starts from the goal the scope has, so
// it mounts once that and the reporting currency are loaded.
function GoalForm({
  portfolioId,
  goal,
  currency,
  onClose,
}: {
  portfolioId: PortfolioScope
  goal: Goal | null
  currency: string
  onClose: () => void
}) {
  const units = unitsFor(currency)
  const [amount, setAmount] = useState(goal ? formatDecimalInput(goal.amount) : '')
  // A stored unit the reporting currency does not allow (a row stored before the 409 on a
  // currency change, or racing it) starts as nominal.
  const [unit, setUnit] = useState<Goal['unit']>(
    goal && units.includes(goal.unit) ? goal.unit : 'nominal',
  )
  const [date, setDate] = useState(goal?.target_date ?? '')
  const setGoal = useSetGoal(portfolioId)
  const deleteGoal = useDeleteGoal(portfolioId)
  const { confirm, dialog } = useConfirm()
  const once = useOneAtATime()

  const parsed = parseDecimalInput(amount)
  const amountValue =
    parsed !== null && compareDecimal(parsed, '0') > 0 && !tooManyDigits(parsed, 9) ? parsed : null
  const amountMessage = isAmbiguousThousands(amount)
    ? t('decimal.error.ambiguousThousands')
    : t('goal.error.amount')
  const amountError = amount.trim() === '' || amountValue !== null ? undefined : amountMessage
  // YYYY-MM-DD sorts as text, so comparing the YYYY-MM prefixes compares the months.
  const today = todayIstanbul()
  const later = date.slice(0, 7) > today.slice(0, 7)
  // The same calendar month 50 years ahead is the latest the server accepts.
  const near = date.slice(0, 7) <= `${Number(today.slice(0, 4)) + 50}${today.slice(4, 7)}`
  const dateOk = later && near
  const dateError =
    date === '' || dateOk
      ? undefined
      : t(later ? 'goal.error.targetDateFar' : 'goal.error.targetDate')
  const valid = amountValue !== null && dateOk

  function done(key: 'goal.saved' | 'goal.deleted') {
    toast(t(key))
    onClose()
  }

  function save(event: FormEvent) {
    event.preventDefault()
    if (amountValue === null || !dateOk || setGoal.isPending) return
    deleteGoal.reset()
    once(() =>
      setGoal
        .mutateAsync({ amount: amountValue, unit, target_date: date })
        .then(() => done('goal.saved')),
    )
  }

  async function remove() {
    const confirmed = await confirm({
      title: t('goal.deleteConfirm'),
      confirmLabel: t('goal.delete'),
      danger: true,
    })
    if (!confirmed) return
    setGoal.reset()
    once(() => deleteGoal.mutateAsync(undefined).then(() => done('goal.deleted')))
  }

  const serverError = setGoal.error ?? deleteGoal.error

  function edited() {
    setGoal.reset()
    deleteGoal.reset()
  }

  return (
    <>
      <form onSubmit={save} onChange={edited}>
        <div className="mt-4 flex flex-col gap-4">
          <Field label={t('goal.unit')}>
            <Select
              value={unit}
              onChange={(next) => {
                edited()
                setUnit(next)
              }}
              options={units.map((u) => ({ value: u, label: t(`goal.unit.${u}`, { currency }) }))}
            />
          </Field>
          <Field label={t('goal.amount')} error={amountError}>
            <AmountInput
              autoFocus
              suffix={unitCurrency(unit, currency)}
              value={amount}
              onChange={(event) => setAmount(event.target.value)}
            />
          </Field>
          <Field label={t('goal.date')} error={dateError}>
            <DateInput value={date} onChange={(event) => setDate(event.target.value)} />
          </Field>
        </div>
        {serverError && (
          <p role="alert" className="mt-4 text-[13px] text-loss">
            {serverError.message}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          {goal && (
            <Button
              variant="secondary"
              className="mr-auto"
              pending={deleteGoal.isPending}
              onClick={() => void remove()}
            >
              {t('goal.delete')}
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>
            {t('transactions.cancel')}
          </Button>
          <Button type="submit" disabled={!valid} pending={setGoal.isPending}>
            {t('goal.save')}
          </Button>
        </div>
      </form>
      {dialog}
    </>
  )
}

// Sets, changes or deletes the goal of a portfolio, or of the all scope.
// Mounted only while open.
export function GoalDialog({
  portfolioId,
  onClose,
}: {
  portfolioId: PortfolioScope
  onClose: () => void
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const goal = useGoal(portfolioId)
  const summary = usePortfolioSummary(portfolioId)

  let body
  if (goal.isError || summary.isError) {
    body = (
      <p role="alert" className="mt-4 text-[13px] text-loss">
        {t('goal.loadError')}
      </p>
    )
  } else if (goal.data === undefined || !summary.data) {
    body = <p className="mt-4 text-[13px] text-muted">{t('goal.loading')}</p>
  } else {
    body = (
      <GoalForm
        portfolioId={portfolioId}
        goal={goal.data}
        currency={summary.data.currency}
        onClose={onClose}
      />
    )
  }

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <div className={sheetPanel('p-5 md:w-[26rem]')}>
        <h2 id={titleId} className="text-[15px] font-semibold text-ink">
          {t('goal.title')}
        </h2>
        {body}
      </div>
    </Modal>
  )
}
