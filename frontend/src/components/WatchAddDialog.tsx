import { useState } from 'react'
import { useAddWatchItem, type WatchInput } from '../api/watchlist'
import { t } from '../i18n/strings'
import { textBinding, useDepositSubmit } from './depositForm'
import { FormDialog } from './ui/FormDialog'
import { NoteField } from './ui/Inputs'
import { InstrumentSearchInput } from './InstrumentSearchInput'
import { Field } from './ui/Field'

// The dialog that puts an instrument on the watchlist, with an optional note.
export function WatchAddDialog({ onClose }: { onClose: () => void }) {
  const [form, setForm] = useState({ note: '' })
  const [instrument, setInstrument] = useState<{ id: number | null; label: string }>({
    id: null,
    label: '',
  })
  const mutation = useAddWatchItem()
  const { error, setError, submit } = useDepositSubmit<never, WatchInput>({
    mutation,
    validate: () => null,
    toInput: () => ({
      instrument_id: instrument.id ?? 0,
      ...(form.note.trim() !== '' && { note: form.note }),
    }),
    doneKey: 'watchlist.added',
    onClose,
  })

  return (
    <FormDialog
      title={t('watchlist.addDialog.title')}
      widthClass="md:w-[28rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={t('watchlist.addDialog.submit')}
      submitDisabled={instrument.id === null}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
      <div className="mt-4 grid grid-cols-1 gap-4">
        <Field label={t('watchlist.instrument')}>
          <InstrumentSearchInput
            value={instrument.label}
            selected={instrument.id !== null}
            onChange={(label) => setInstrument({ id: null, label })}
            onSelect={(picked) =>
              setInstrument({ id: picked.id, label: `${picked.symbol} - ${picked.name}` })
            }
          />
        </Field>
        <NoteField {...textBinding(form, setForm)('note')} />
      </div>
    </FormDialog>
  )
}
