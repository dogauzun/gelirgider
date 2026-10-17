import {
  useEffect,
  useId,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { flushSync } from 'react-dom'
import { ApiError, translateError } from '../api'
import { usePortfolioSummary } from '../api/summary'
import {
  useCreateTransaction,
  useUpdateTransaction,
  type Transaction,
  type TransactionInput,
} from '../api/transactions'
import { t } from '../i18n/strings'
import { bottomBar } from '../lib/bottomBar'
import { todayIstanbul } from '../lib/date'
import { formatMoney } from '../lib/format'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { InstrumentSearchInput } from './InstrumentSearchInput'
import {
  cashEffect,
  CURRENCIES,
  emptyForm,
  feeLabelKey,
  fromTransaction,
  needsCurrencySelect,
  rowCurrency,
  sellExceedsHeld,
  toInput,
  TYPES,
  TYPES_WITH_AMOUNT,
  TYPES_WITH_INSTRUMENT,
  TYPES_WITH_QUANTITY_PRICE,
  typeShortLabelKey,
  validate,
  type FormError,
  type FormField,
  type FormState,
  type FormType,
} from './transactionForm'
import { Button } from './ui/Button'
import { useConfirm } from './ui/useConfirm'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Notice } from './ui/Notice'
import { Segmented } from './ui/Segmented'
import { Select } from './ui/Select'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'

// apiFetch already translated the server error, but with the fee labelled
// Komisyon; translate it again with the label for this transaction's type.
function serverErrorMessage(error: Error, type: FormType): string {
  if (!(error instanceof ApiError) || typeof error.body.error !== 'string') return error.message
  return translateError(error.body.error, feeLabelKey(type))
}

// The inputs under the type control: the ones the chosen type uses.
function TransactionFields({
  form,
  setForm,
  fieldError,
  clearError,
  round,
  withNote,
}: {
  form: FormState
  setForm: (form: FormState) => void
  fieldError: (field: FormField) => string | undefined
  clearError: () => void
  // Keys the instrument search, see "add and next" in TransactionDialog.
  round: number
  withNote: boolean
}) {
  const showInstrument = TYPES_WITH_INSTRUMENT.has(form.type)
  const showQuantityPrice = TYPES_WITH_QUANTITY_PRICE.has(form.type)
  const showAmount = TYPES_WITH_AMOUNT.has(form.type)

  return (
    <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
      {showInstrument && (
        <div className="sm:col-span-2">
          <Field label={t('transactions.instrument')} error={fieldError('instrument')}>
            <InstrumentSearchInput
              key={round}
              value={form.instrumentLabel}
              selected={form.instrumentId !== null}
              onChange={(text) =>
                setForm({
                  ...form,
                  instrumentId: null,
                  instrumentLabel: text,
                  instrumentSymbol: null,
                  instrumentCurrency: null,
                })
              }
              onSelect={(instrument) =>
                setForm({
                  ...form,
                  instrumentId: instrument.id,
                  instrumentLabel: `${instrument.symbol} - ${instrument.name}`,
                  instrumentSymbol: instrument.symbol,
                  instrumentCurrency: instrument.currency,
                })
              }
            />
          </Field>
        </div>
      )}

      <Field label={t('transactions.date')}>
        <input
          type="date"
          value={form.date}
          min="1990-01-01"
          max={todayIstanbul()}
          onChange={(event) => setForm({ ...form, date: event.target.value })}
          className={inputClass()}
        />
      </Field>

      {needsCurrencySelect(form) ? (
        <Field label={t('transactions.currency')}>
          <Select
            value={form.currency}
            onChange={(currency) => {
              setForm({ ...form, currency })
              clearError()
            }}
            options={CURRENCIES.map((c) => ({ value: c, label: t(`currency.${c}`) }))}
          />
        </Field>
      ) : (
        form.instrumentCurrency && (
          <Field label={t('transactions.currency')}>
            <input
              type="text"
              readOnly
              value={`${form.instrumentCurrency} · ${t('transactions.fromInstrument')}`}
              className={inputClass({ readOnly: true })}
            />
          </Field>
        )
      )}

      {showQuantityPrice && (
        <>
          <Field label={t('transactions.quantity')} error={fieldError('quantity')}>
            <input
              type="text"
              inputMode="decimal"
              value={form.quantity}
              onChange={(event) => setForm({ ...form, quantity: event.target.value })}
              className={inputClass()}
            />
          </Field>
          <Field label={t('transactions.price')} error={fieldError('price')}>
            <input
              type="text"
              inputMode="decimal"
              value={form.price}
              onChange={(event) => setForm({ ...form, price: event.target.value })}
              className={inputClass()}
            />
          </Field>
        </>
      )}

      {showAmount && (
        <Field label={t('transactions.amount')} error={fieldError('amount')}>
          <input
            type="text"
            inputMode="decimal"
            value={form.amount}
            onChange={(event) => setForm({ ...form, amount: event.target.value })}
            className={inputClass()}
          />
        </Field>
      )}

      {form.type !== 'fee' && (
        <Field label={t(feeLabelKey(form.type))} error={fieldError('fee')}>
          <input
            type="text"
            inputMode="decimal"
            value={form.fee}
            onChange={(event) => setForm({ ...form, fee: event.target.value })}
            className={inputClass()}
          />
        </Field>
      )}

      {/* The import commit sets every row's note to the broker and the
          period (ADR 0006), so the preview (onSubmit) offers none. */}
      {withNote && (
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
      )}
    </div>
  )
}

// Cmd on a Mac, Ctrl everywhere else -- the hint has to name the key the user
// actually has.
const submitShortcutLabel = navigator.userAgent.includes('Mac') ? '⌘↵' : 'Ctrl+↵'

export function TransactionDialog({
  portfolioId,
  editing,
  initial,
  onClose,
  onSubmit,
  focusFallback,
  context,
}: {
  portfolioId: number
  editing: Transaction | null
  // Add mode only: the form to start from instead of an empty one (an
  // expected dividend).
  initial?: FormState
  onClose: () => void
  // When given, the dialog hands the validated input to the caller instead of
  // saving it, so a caller that keeps unsaved rows in local state (the
  // statement import preview) reuses the same form. That input always carries
  // the row's currency, the instrument's for an instrument type, and the
  // instrument's symbol for the caller's row to show.
  onSubmit?: (input: TransactionInput, symbol: string | undefined) => void
  // Selector focused on close when the element that opened the dialog is
  // gone (useModal).
  focusFallback?: string
  // Shown under the title: the import preview puts the statement line being
  // fixed here, which the dialog would otherwise cover.
  context?: ReactNode
}) {
  // Keyed by `editing?.id` in the parent, so switching between add mode and
  // editing a row remounts this component with a fresh initial state instead
  // of needing an effect to re-sync it.
  const [baseline, setBaseline] = useState<FormState>(() =>
    editing ? fromTransaction(editing) : (initial ?? emptyForm()),
  )
  const [form, setForm] = useState<FormState>(baseline)
  // Bumped by "add and next" to remount the instrument search, whose debounced
  // query would otherwise list the last instrument's matches under the
  // cleared field for a moment.
  const [round, setRound] = useState(0)
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [error, setError] = useState<FormError | null>(null)
  const createMutation = useCreateTransaction(portfolioId)
  const updateMutation = useUpdateTransaction(portfolioId)
  const once = useOneAtATime()
  // The summary only knows saved rows: the import preview (onSubmit) holds
  // unsaved buys it cannot see, so the oversell hint is off there.
  const summary = usePortfolioSummary(portfolioId, !onSubmit && form.type === 'sell')
  // Mounted only while open (the parent conditions on it), so showModal on
  // mount is the whole lifecycle; closing unmounts the element.
  const dialogRef = useModal(focusFallback)
  // showModal() focuses the first focusable element in tree order: the first
  // type radio, even when another type is checked and that one has tabIndex
  // -1. Focus the checked radio instead, the group's Tab stop.
  // Declared after useModal, so it runs after showModal().
  useEffect(() => {
    dialogRef.current?.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')?.focus()
  }, [dialogRef])
  const titleId = useId()
  // Each submit sets a fresh error object, so a repeated submit with the same
  // mistake moves focus back to the field too. Field focuses its own control
  // only when focus is off every input; a Ctrl+Enter from another input needs
  // this. Runs after the Field effects, so aria-invalid is already set.
  useEffect(() => {
    if (!error?.field) return
    dialogRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, [error, dialogRef])
  const fieldError = (field: FormField) => (error?.field === field ? error.message : undefined)
  const isPending = createMutation.isPending || updateMutation.isPending

  function submit(keepOpen: boolean) {
    // The pending buttons ignore clicks, but ⌘↵ comes straight here: a second
    // press while the save runs would book the row twice.
    if (isPending) return
    const validationError = validate(form)
    if (validationError) {
      setError(validationError)
      return
    }
    setError(null)
    const input = toInput(form, editing)

    if (onSubmit) {
      // The server fills in an instrument row's currency on save, but the
      // caller keeps the row itself, so it gets the instrument's here: the
      // import commit refuses any other.
      onSubmit(
        { ...input, currency: rowCurrency(form) },
        input.instrument_id !== undefined ? (form.instrumentSymbol ?? undefined) : undefined,
      )
      // Same "add and next" behaviour as a saved row, minus the request.
      if (keepOpen && !editing) startNext()
      else onClose()
      return
    }

    const onError = (mutationError: Error) =>
      setError({ field: null, message: serverErrorMessage(mutationError, form.type) })
    if (editing) {
      once(() =>
        updateMutation.mutateAsync(
          { id: editing.id, input },
          {
            onSuccess: () => {
              toast(t('transactions.saved'))
              onClose()
            },
            onError,
          },
        ),
      )
      return
    }
    once(() =>
      createMutation.mutateAsync(input, {
        // Keeping the type and the date is what makes "add and next" worth
        // it: a day of transactions is usually the same type.
        onSuccess: () => {
          toast(t('transactions.added'))
          if (keepOpen) startNext()
          else onClose()
        },
        onError,
      }),
    )
  }

  // "Add and next" starts a fresh form, which is the new baseline: closing it
  // untouched loses nothing. Focus goes to the first field (the instrument for
  // a trade, else the date), so the next row is typed without the mouse;
  // flushSync renders the fresh form before that. It keeps the
  // starting form's cash currency: the import preview starts on the
  // statement's, which the next row is in too.
  function startNext() {
    const next = { ...emptyForm(form.type, form.date), currency: (initial ?? emptyForm()).currency }
    flushSync(() => {
      setBaseline(next)
      setForm(next)
      setRound(round + 1)
    })
    dialogRef.current?.querySelector<HTMLElement>('input')?.focus()
  }

  // Backdrop, Escape and Vazgeç all come here: a changed form asks before it
  // is thrown away, an untouched one just closes.
  async function requestClose() {
    const dirty = (Object.keys(form) as (keyof FormState)[]).some(
      (key) => form[key] !== baseline[key],
    )
    if (
      dirty &&
      !(await confirm({
        title: t('transactions.discardConfirm'),
        body: t('transactions.discardConfirmBody'),
        confirmLabel: t('transactions.discardButton'),
        danger: true,
      }))
    ) {
      return
    }
    onClose()
  }

  function handleKeyDown(event: KeyboardEvent<HTMLFormElement>) {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      submit(true)
    }
  }

  const effect = cashEffect(form)
  const heldWhenOversold = onSubmit ? null : sellExceedsHeld(form, summary.data?.positions, editing)
  // The ⌘↵ hint names a key a touch screen has none of. jsdom has
  // no matchMedia, hence the optional call.
  const finePointer = window.matchMedia?.('(pointer: fine)').matches === true

  return (
    <>
      <Modal ref={dialogRef} aria-labelledby={titleId} onClose={() => void requestClose()}>
        <form
          onSubmit={(event: FormEvent<HTMLFormElement>) => {
            event.preventDefault()
            submit(false)
          }}
          onKeyDown={handleKeyDown}
          // A validation error belongs to the form as it was when submitted, so
          // any edit drops it instead of leaving it under a fixed field.
          // Delegated onChange covers every input below; the type
          // buttons and the currency Select, which fires no change event, clear
          // it themselves.
          onChange={() => setError(null)}
          className={sheetPanel('max-h-[92vh] overflow-y-auto p-5 md:w-[36rem]')}
        >
          <h2 id={titleId} className="text-ink text-[15px] font-semibold">
            {editing ? t('transactions.editTitle') : t('transactions.addTitle')}
          </h2>
          {context && <div className="mt-2">{context}</div>}

          <div className="mt-4">
            {/* The type only decides which fields are shown; it keeps the rest of
              the form intact so a Buy -> Deposit -> Buy round trip does not
              lose the instrument or the amount. toInput drops
              whatever the chosen type does not use. */}
            <span className="text-ink-2 block text-[13px] font-medium">
              {t('transactions.type')}
            </span>
            <Segmented
              options={TYPES}
              value={form.type}
              onChange={(type) => {
                setError(null)
                setForm({ ...form, type })
              }}
              label={(type) => t(typeShortLabelKey[type])}
              aria-label={t('transactions.type')}
              // Seven labels do not fit one row at phone width, so below sm: they
              // sit in two rows of four.
              trackClassName="rounded-ctl bg-well grid grid-cols-4 gap-0.5 p-0.5 sm:flex"
              className="mt-1.5"
              itemClassName="h-8 flex-1"
            />
          </div>

          <TransactionFields
            form={form}
            setForm={setForm}
            fieldError={fieldError}
            clearError={() => setError(null)}
            round={round}
            withNote={!onSubmit}
          />

          {effect && (
            <div className="bg-paper rounded-ctl mt-4 flex items-baseline justify-between px-3 py-2 text-[13px]">
              <span className="text-muted">
                {t(effect.out ? 'transactions.cashOut' : 'transactions.cashIn')}
              </span>
              <span className="text-ink font-medium">
                {effect.out ? '−' : '+'}
                {formatMoney(effect.amount, rowCurrency(form))}
              </span>
            </div>
          )}

          {heldWhenOversold !== null && (
            <div className="mt-4">
              <Notice>
                {t('transactions.warning.sellExceedsHeld', { held: heldWhenOversold })}
              </Notice>
            </div>
          )}

          {error && error.field === null && (
            <p role="alert" className="text-loss mt-4 text-[13px]">
              {error.message}
            </p>
          )}

          {/* Stuck to the bottom of the scrolling sheet, so Kaydet stays in
              view on a phone whatever the form's height. Sticky
              offsets count from inside the sheet's p-5, hence -bottom-5. Keyboard
              focus never scrolls a field under it. */}
          <div
            ref={bottomBar}
            className="border-line bg-surface sticky -bottom-5 -mx-5 mt-5 -mb-5 flex flex-wrap items-center justify-end gap-3 border-t px-5 pt-3 pb-[max(1.25rem,env(safe-area-inset-bottom))]"
          >
            {!editing && finePointer && (
              <p className="text-muted mr-auto text-[12px]">
                {t('transactions.addAndNext', { key: submitShortcutLabel })}
              </p>
            )}
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => void requestClose()}>
                {t('transactions.cancel')}
              </Button>
              {!editing && (
                <Button variant="secondary" pending={isPending} onClick={() => submit(true)}>
                  {t('transactions.addAndNew')}
                </Button>
              )}
              <Button type="submit" pending={isPending}>
                {editing ? t('transactions.save') : t('transactions.add')}
              </Button>
            </div>
          </div>
        </form>
      </Modal>
      {/* A sibling, not a child: React bubbles the confirm's cancel and click
        events up its tree, and inside this dialog they would close it. */}
      {confirmDialog}
    </>
  )
}
