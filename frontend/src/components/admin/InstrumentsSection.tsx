import { useState } from 'react'
import { useCreateInstrument } from '../../api/instruments'
import { marketLabel, t } from '../../i18n/strings'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { inputClass } from '../ui/inputClass'
import { Select } from '../ui/Select'

// The market+currency pairs POST /api/admin/instruments accepts (the backend's
// seedSources). Only CRYPTO has a choice.
const currencies: Record<string, string[]> = {
  BIST: ['TRY'],
  US: ['USD'],
  TEFAS: ['TRY'],
  CRYPTO: ['TRY', 'USD'],
  GOLD: ['TRY'],
}

// Adds an instrument missing from the seed lists (GLG-562).
export function InstrumentsSection() {
  const [market, setMarket] = useState('BIST')
  const [currency, setCurrency] = useState('TRY')
  const [symbol, setSymbol] = useState('')
  const [name, setName] = useState('')
  const create = useCreateInstrument()
  const once = useOneAtATime()
  const saved = create.data

  return (
    <section id="admin-instruments" className="card scroll-mt-20">
      <h2 className="text-ink border-line border-b px-4 py-3 text-[14px] font-semibold">
        {t('admin.instruments.title')}
      </h2>
      <div className="p-4">
        <p className="text-muted text-[13px]">{t('admin.instruments.description')}</p>
        <form
          className="mt-4 grid max-w-2xl gap-3 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            once(() =>
              create.mutateAsync(
                { market, symbol, name, currency },
                {
                  onSuccess: () => {
                    setSymbol('')
                    setName('')
                  },
                },
              ),
            )
          }}
        >
          <Field label={t('admin.instruments.market')}>
            <Select
              value={market}
              onChange={(next) => {
                setMarket(next)
                setCurrency(currencies[next][0])
              }}
              options={Object.keys(currencies).map((code) => ({
                value: code,
                label: marketLabel(code),
              }))}
            />
          </Field>
          <Field label={t('admin.instruments.currency')}>
            <Select
              value={currency}
              disabled={currencies[market].length === 1}
              onChange={setCurrency}
              options={currencies[market].map((code) => ({ value: code, label: code }))}
            />
          </Field>
          <Field label={t('admin.instruments.symbol')}>
            <input
              required
              value={symbol}
              onChange={(event) => setSymbol(event.target.value)}
              className={inputClass()}
            />
          </Field>
          <Field label={t('admin.instruments.name')}>
            <input
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
              className={inputClass()}
            />
          </Field>
          <div className="sm:col-span-2">
            <Button type="submit" pending={create.isPending}>
              {t('admin.instruments.submit')}
            </Button>
          </div>
        </form>
        {create.isError && (
          <p role="alert" className="text-loss mt-3 text-[13px]">
            {create.error.message}
          </p>
        )}
        {saved && (
          <p role="status" className="text-ink-2 mt-3 text-[13px]">
            {t('admin.instruments.saved', {
              symbol: saved.symbol,
              name: saved.name,
              market: marketLabel(saved.market),
              currency: saved.currency,
            })}
          </p>
        )}
      </div>
    </section>
  )
}
