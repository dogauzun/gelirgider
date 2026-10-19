import { useId, useState, type FormEvent } from 'react'
import { useReplaceAllocationTargets, type AllocationTarget } from '../api/allocationTargets'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import {
  addDecimal,
  compareDecimal,
  decimalUnits,
  formatDecimalInput,
  parseDecimalInput,
  unitsToDecimal,
} from '../lib/decimal'
import { formatNumber } from '../lib/format'
import { DecimalInput } from './ui/Inputs'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'
import { useModal } from './ui/useModal'

const CLASSES = ['equity', 'fund', 'commodity', 'crypto', 'deposit', 'cash'] as const

export function AllocationTargetsDialog({
  portfolioId,
  targets,
  onClose,
}: {
  portfolioId: PortfolioScope
  targets: AllocationTarget[]
  onClose: () => void
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const mutation = useReplaceAllocationTargets(portfolioId)
  const [texts, setTexts] = useState(() =>
    CLASSES.map((c) => formatDecimalInput(targets.find((x) => x.asset_class === c)?.weight ?? '')),
  )

  const weights = texts.map((text) => (text.trim() === '' ? '0' : parseDecimalInput(text)))
  const sum = weights.reduce<string>((total, weight) => addDecimal(total, weight ?? '0'), '0')
  const canSave = weights.every((weight) => weight !== null) && compareDecimal(sum, '100') === 0

  function submit(event: FormEvent) {
    event.preventDefault()
    if (!canSave) return
    const set = CLASSES.flatMap((assetClass, i) => {
      const weight = unitsToDecimal(decimalUnits(weights[i] as string))
      return compareDecimal(weight, '0') > 0 ? [{ asset_class: assetClass, weight }] : []
    })
    mutation.mutate(set, { onSuccess: onClose })
  }

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <form
        onSubmit={submit}
        onChange={() => mutation.isError && mutation.reset()}
        className={sheetPanel('max-h-[92vh] overflow-y-auto p-5 md:w-[28rem]')}
      >
        <h2 id={titleId} className="text-ink text-[15px] font-semibold">
          {t('allocation.targets.title')}
        </h2>
        <p className="text-muted mt-1 text-[13px]">{t('allocation.targets.hint')}</p>
        <div className="mt-4 grid grid-cols-2 gap-4">
          {CLASSES.map((assetClass, i) => (
            <Field
              key={assetClass}
              label={t(`allocation.assetClass.${assetClass}`)}
              error={weights[i] === null ? t('allocation.targets.invalid') : undefined}
            >
              <DecimalInput
                value={texts[i]}
                onChange={(event) =>
                  setTexts(texts.map((text, j) => (j === i ? event.target.value : text)))
                }
              />
            </Field>
          ))}
        </div>
        <p role="status" className="text-ink-2 mt-4 text-[13px] font-medium tabular-nums">
          {t('allocation.targets.sum', { sum: formatNumber(sum, { maximumFractionDigits: 9 }) })}
        </p>
        {mutation.isError && (
          <p role="alert" className="text-loss mt-2 text-[13px]">
            {mutation.error.message}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('transactions.cancel')}
          </Button>
          <Button type="submit" disabled={!canSave} pending={mutation.isPending}>
            {t('transactions.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
