import { useState } from 'react'
import { useCreateInstrument, useUpdateInstrument, type Instrument } from '../../api/instruments'
import { marketLabel, sectorLabel, t } from '../../i18n/strings'
import { SECTOR_KEYS } from '../../lib/sectors'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { InstrumentSearchInput } from '../InstrumentSearchInput'
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

// Adds an instrument missing from the seed lists.
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
        <SectorForm />
      </div>
    </section>
  )
}

// "Provider value" is the empty key: it sends null and clears the override.
const PROVIDER_VALUE = ''

// Pins an instrument's sector over the provider's, or clears the pin.
function SectorForm() {
  const [text, setText] = useState('')
  const [picked, setPicked] = useState<Instrument | null>(null)
  const [sector, setSector] = useState(PROVIDER_VALUE)
  const update = useUpdateInstrument()
  const once = useOneAtATime()
  const saved = update.data

  return (
    <div className="border-line mt-6 border-t pt-4">
      <h3 className="text-ink text-[14px] font-semibold">{t('admin.instruments.sectorTitle')}</h3>
      <p className="text-muted mt-1 text-[13px]">{t('admin.instruments.sectorDescription')}</p>
      <form
        className="mt-4 grid max-w-2xl gap-3 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault()
          if (!picked) return
          once(() =>
            update.mutateAsync({
              id: picked.id,
              sector: sector === PROVIDER_VALUE ? null : sector,
            }),
          )
        }}
      >
        <Field label={t('admin.instruments.sectorInstrument')}>
          <InstrumentSearchInput
            value={text}
            selected={picked !== null}
            onChange={(next) => {
              setText(next)
              setPicked(null)
            }}
            onSelect={(instrument) => {
              setPicked(instrument)
              setText(`${instrument.symbol} · ${instrument.name}`)
              setSector(instrument.sector_override ?? PROVIDER_VALUE)
            }}
          />
        </Field>
        <Field label={t('admin.instruments.sector')}>
          <Select
            value={sector}
            onChange={setSector}
            options={[
              { value: PROVIDER_VALUE, label: t('admin.instruments.sectorProvider') },
              ...SECTOR_KEYS.map((key) => ({ value: key as string, label: sectorLabel(key) })),
            ]}
          />
        </Field>
        <div className="sm:col-span-2">
          <Button type="submit" disabled={!picked} pending={update.isPending}>
            {t('admin.instruments.sectorSubmit')}
          </Button>
        </div>
      </form>
      {update.isError && (
        <p role="alert" className="text-loss mt-3 text-[13px]">
          {update.error.message}
        </p>
      )}
      {saved && (
        <p role="status" className="text-ink-2 mt-3 text-[13px]">
          {t('admin.instruments.sectorSaved', {
            symbol: saved.symbol,
            sector: saved.sector_override
              ? sectorLabel(saved.sector_override)
              : t('admin.instruments.sectorProvider'),
          })}
        </p>
      )}
    </div>
  )
}
