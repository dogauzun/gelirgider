import { useState } from 'react'
import { useUpdateWatchItem, type WatchItem, type WatchUpdate } from '../api/watchlist'
import { t } from '../i18n/strings'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { compareDecimal, formatDecimalInput, parseDecimalInput } from '../lib/decimal'
import { decimalError, firstError, textBinding, useDepositSubmit, type Rule } from './depositForm'
import { DecimalInput, DepositFormDialog, NoteField } from './DepositFormDialog'
import { Button } from './ui/Button'
import { Field } from './ui/Field'

// The server's limits for a target: positive, 15 integer and 8 fraction
// digits, as the manual price.
const TARGET: Rule = {
  label: 'watchlist.target',
  fraction: 8,
  ok: (parsed) => compareDecimal(parsed, '0') > 0,
  errorKey: 'error.field.target_price',
}

// The dialog that sets a watched instrument's target price and note, or
// clears the target.
export function WatchTargetDialog({ item, onClose }: { item: WatchItem; onClose: () => void }) {
  const [form, setForm] = useState({
    target: item.target_price ? formatDecimalInput(item.target_price) : '',
    note: item.note,
  })
  const noTarget = form.target.trim() === ''
  const update = useUpdateWatchItem()
  const mutation = {
    isPending: update.isPending,
    mutateAsync: (
      input: WatchUpdate,
      options: { onSuccess: () => void; onError: (error: Error) => void },
    ) => update.mutateAsync({ id: item.id, ...input }, options),
  }
  const { error, setError, fieldError, submit } = useDepositSubmit<'target', WatchUpdate>({
    mutation,
    // An empty field is no target; any other text must be a valid one.
    validate: () => firstError([['target', noTarget ? null : decimalError(form.target, TARGET)]]),
    toInput: () => ({ target_price: parseDecimalInput(form.target), note: form.note }),
    doneKey: 'watchlist.targetSaved',
    onClose,
  })
  const once = useOneAtATime()

  // Not a submit: Enter in a field must keep saving the typed target. The
  // field is not checked, as the target is going away.
  function clear() {
    if (mutation.isPending) return
    once(() =>
      mutation.mutateAsync(
        { target_price: null, note: form.note },
        {
          onSuccess: () => {
            toast(t('watchlist.targetSaved'))
            onClose()
          },
          onError: (mutationError) => setError({ field: null, message: mutationError.message }),
        },
      ),
    )
  }
  const bind = textBinding(form, setForm)

  return (
    <DepositFormDialog
      title={t('watchlist.targetDialog.title', { symbol: item.symbol })}
      widthClass="md:w-[28rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={t('watchlist.targetDialog.save')}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
      <div className="mt-4 grid grid-cols-1 gap-4">
        <Field
          label={t('watchlist.targetDialog.field', { currency: item.currency })}
          error={fieldError('target')}
        >
          <DecimalInput {...bind('target')} />
        </Field>
        <NoteField {...bind('note')} />
        {item.target_price && (
          <div>
            <Button variant="secondary" onClick={clear}>
              {t('watchlist.targetDialog.clear')}
            </Button>
          </div>
        )}
      </div>
    </DepositFormDialog>
  )
}
