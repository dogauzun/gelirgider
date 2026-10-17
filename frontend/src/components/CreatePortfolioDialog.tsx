import { useId, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { useCreatePortfolio } from '../api/portfolios'
import { t } from '../i18n/strings'
import type { NavState } from '../lib/navState'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'

// Tüm portföyler, the portfolio header and the sidebar open it; it opens
// the new portfolio, onCreated is for a caller's own step.
// Mounted only while
// open, so the name and the server error reset themselves on every reopen
// instead of needing an effect to clear them.
export function CreatePortfolioDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated?: (id: number) => void
}) {
  const navigate = useNavigate()
  const dialogRef = useModal()
  const titleId = useId()
  const [name, setName] = useState('')
  // A blank name is answered here, with the text the server's 400 maps to,
  // instead of a silent no-op.
  const [blank, setBlank] = useState(false)
  const createMutation = useCreatePortfolio()
  const once = useOneAtATime()

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmed = name.trim()
    setBlank(!trimmed)
    if (!trimmed) return
    // Not mutateAsync's per-call onSuccess: the hook's list refetch runs
    // first and can unmount this dialog (Tüm portföyler's empty state goes
    // with it), and TanStack drops per-call callbacks of an unmounted
    // observer. The promise still resolves.
    once(async () => {
      const created = await createMutation.mutateAsync(trimmed)
      toast(t('portfolios.created'))
      onCreated?.(created.id)
      // The list's opener may be gone by now: the new page takes focus
      // unless it moved on, as from the header.
      void navigate(`/portfolios/${created.id}`, {
        state: { focusAfterCreate: true } satisfies NavState,
      })
      onClose()
    })
  }

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <form onSubmit={handleSubmit} className={sheetPanel('p-5 md:w-[26rem]')}>
        <h2 id={titleId} className="text-[15px] font-semibold text-ink">
          {t('portfolios.new')}
        </h2>
        {/* No maxLength: it counts UTF-16 units, so it cut emoji names at
            half the server's 100 characters; the server checks the length
            and its error shows below the field. */}
        <div className="mt-4">
          <Field
            label={t('portfolios.name')}
            hint={t('portfolios.nameHint')}
            error={blank ? t('error.portfolioName') : createMutation.error?.message}
          >
            <input
              type="text"
              autoFocus
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                // A server error is about the name sent: any edit drops it,
                // as on the signed-out forms. Only an error: a
                // reset while pending would drop the success that closes.
                if (createMutation.isError) createMutation.reset()
              }}
              className={inputClass()}
            />
          </Field>
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('confirm.cancel')}
          </Button>
          <Button type="submit" pending={createMutation.isPending}>
            {t('portfolios.create')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
