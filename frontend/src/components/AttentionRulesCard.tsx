import { useState, type FormEvent } from 'react'
import { useAttentionRules, useUpdateAttentionRules } from '../api/auth'
import { ATTENTION, type AttentionRules } from '../lib/attention'
import { t } from '../i18n/strings'
import { toast } from '../lib/toast'
import { Button } from './ui/Button'
import { Checkbox } from './ui/Checkbox'
import { inputClass } from './ui/inputClass'
import { LoadError } from './ui/LoadError'

// The rules the card lists, in display order. A percent rule is stored as a
// 0-1 share and edited as a percent.
const ruleDefs: {
  key: keyof AttentionRules
  label: Parameters<typeof t>[0]
  unit: 'days' | 'percent' | 'count'
  fallback: number
}[] = [
  {
    key: 'stale_manual_days',
    label: 'account.attention.staleManualDays',
    unit: 'days',
    fallback: ATTENTION.staleManualDays,
  },
  {
    key: 'position_share',
    label: 'account.attention.positionShare',
    unit: 'percent',
    fallback: ATTENTION.positionShare,
  },
  {
    key: 'cash_share',
    label: 'account.attention.cashShare',
    unit: 'percent',
    fallback: ATTENTION.cashShare,
  },
  {
    key: 'currency_share',
    label: 'account.attention.currencyShare',
    unit: 'percent',
    fallback: ATTENTION.currencyShare,
  },
  {
    key: 'movers',
    label: 'account.attention.movers',
    unit: 'count',
    fallback: ATTENTION.movers,
  },
]

type Draft = Record<string, { enabled: boolean; value: string }>

// toFixed trims binary noise: 0.07 * 100 is 7.000000000000001.
const toInput = (unit: string, value: number) =>
  String(unit === 'percent' ? Number((value * 100).toFixed(4)) : value)
const fromInput = (unit: string, value: string) =>
  unit === 'percent' ? Number((Number(value) / 100).toFixed(6)) : Number(value)

function AttentionRulesForm({ rules }: { rules: AttentionRules }) {
  const [draft, setDraft] = useState<Draft>(() =>
    Object.fromEntries(
      ruleDefs.map(({ key, unit, fallback }) => [
        key,
        {
          enabled: rules[key]?.enabled !== false,
          value: toInput(unit, rules[key]?.value ?? fallback),
        },
      ]),
    ),
  )
  const mutation = useUpdateAttentionRules(() => toast(t('account.attention.saved')))

  function edit(key: string, change: Partial<Draft[string]>) {
    setDraft((current) => ({ ...current, [key]: { ...current[key], ...change } }))
    if (mutation.isError) mutation.reset()
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // Overrides only: a value equal to its default is left out, which removes
    // an override saved earlier.
    const body: AttentionRules = {}
    for (const { key, unit, fallback } of ruleDefs) {
      const { enabled, value } = draft[key]
      const number = fromInput(unit, value)
      const override = {
        ...(enabled ? {} : { enabled: false }),
        ...(number === fallback ? {} : { value: number }),
      }
      if (Object.keys(override).length > 0) body[key] = override
    }
    mutation.mutate(body)
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-3">
      <ul className="divide-y divide-line">
        {ruleDefs.map(({ key, label, unit }) => (
          <li key={key} className="flex items-center justify-between gap-3 py-1 first:pt-0">
            <Checkbox
              label={t(label)}
              checked={draft[key].enabled}
              onChange={(enabled) => edit(key, { enabled })}
            />
            <span className="flex shrink-0 items-center gap-1.5 text-[13px] text-muted">
              <input
                type="number"
                step="any"
                required
                aria-label={t(label)}
                value={draft[key].value}
                disabled={!draft[key].enabled}
                onChange={(event) => edit(key, { value: event.target.value })}
                className={inputClass({ size: 'sm', width: 'auto' })}
              />
              <span className="w-3">{unit === 'percent' ? '%' : ''}</span>
            </span>
          </li>
        ))}
      </ul>
      {mutation.isError && (
        <p role="alert" className="text-[12px] text-loss">
          {mutation.error.message}
        </p>
      )}
      <Button type="submit" pending={mutation.isPending} className="self-start">
        {t('account.attention.save')}
      </Button>
    </form>
  )
}

// The form mounts once the rules are loaded: saving over a failed load would
// drop the overrides the user cannot see.
export function AttentionRulesCard() {
  const { data, isError, error, refetch, isFetching } = useAttentionRules()
  let body = null
  if (data) body = <AttentionRulesForm rules={data} />
  else if (isError)
    body = (
      <LoadError
        title={t('account.attention.loadError')}
        error={error}
        onRetry={() => void refetch()}
        retrying={isFetching}
      />
    )
  return (
    <section className="card p-5">
      <h2 className="text-[14px] font-semibold text-ink">{t('account.attention')}</h2>
      <p className="mt-1 text-[13px] text-muted">{t('account.attention.help')}</p>
      <div className="mt-4">{body}</div>
    </section>
  )
}
