import { useState } from 'react'
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
import { useDepositSubmit } from './depositForm'
import { DecimalInput } from './ui/Inputs'
import { Field } from './ui/Field'
import { FormDialog } from './ui/FormDialog'

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
  const mutation = useReplaceAllocationTargets(portfolioId)
  const [texts, setTexts] = useState(() =>
    CLASSES.map((c) => formatDecimalInput(targets.find((x) => x.asset_class === c)?.weight ?? '')),
  )

  const weights = texts.map((text) => (text.trim() === '' ? '0' : parseDecimalInput(text)))
  const sum = weights.reduce<string>((total, weight) => addDecimal(total, weight ?? '0'), '0')
  const canSave = weights.every((weight) => weight !== null) && compareDecimal(sum, '100') === 0

  const { error, setError, submit } = useDepositSubmit<'weights', AllocationTarget[]>({
    mutation,
    validate: () =>
      canSave ? null : { field: 'weights', message: t('allocation.targets.invalid') },
    toInput: () =>
      CLASSES.flatMap((assetClass, i) => {
        const weight = unitsToDecimal(decimalUnits(weights[i] as string))
        return compareDecimal(weight, '0') > 0 ? [{ asset_class: assetClass, weight }] : []
      }),
    doneKey: 'allocation.targets.saved',
    onClose,
  })

  return (
    <FormDialog
      title={t('allocation.targets.title')}
      widthClass="md:w-[28rem]"
      error={error}
      pending={mutation.isPending}
      submitLabel={t('transactions.save')}
      submitDisabled={!canSave}
      onSubmit={submit}
      onEdit={() => setError(null)}
      onClose={onClose}
    >
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
    </FormDialog>
  )
}
