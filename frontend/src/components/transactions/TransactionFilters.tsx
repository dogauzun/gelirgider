import { useState } from 'react'
import { importLabel, type Import } from '../../api/imports'
import type { Instrument } from '../../api/instruments'
import type { ImportFilter } from '../../api/transactions'
import { t } from '../../i18n/strings'
import { DateRangeFilter } from '../DateRangeFilter'
import { InstrumentSearchInput } from '../InstrumentSearchInput'
import { Button } from '../ui/Button'
import { Icon } from '../ui/Icon'
import { Modal } from '../ui/Modal'
import { Segmented } from '../ui/Segmented'
import { Select, type SelectOption } from '../ui/Select'
import { useModal } from '../ui/useModal'
import { filterParam, LIST_FILTERS } from './filters'

// The phone's filters (core pages spec, "Phone"): the toolbar keeps one
// "Filtrele" button and the controls open in this bottom sheet. Each control
// applies at once, as on the desktop toolbar, so the sheet only closes.
export function FilterSheet({
  onClose,
  children,
}: {
  onClose: () => void
  children: React.ReactNode
}) {
  const dialogRef = useModal('[data-open-filters]')
  return (
    <Modal ref={dialogRef} aria-labelledby="transactions-filter-title" onClose={onClose}>
      <div className="absolute inset-x-0 bottom-0 flex flex-col gap-3 rounded-t-xl bg-surface p-5 shadow-pop">
        <h2 id="transactions-filter-title" className="text-[15px] font-semibold text-ink">
          {t('transactions.filter.title')}
        </h2>
        {children}
        <Button className="mt-2 self-end" onClick={onClose}>
          {t('transactions.filter.done')}
        </Button>
      </div>
    </Modal>
  )
}

// The search text is held here, not by the page, so a keystroke renders the
// input alone and not every loaded row (GLG-1168). Picking an instrument
// swaps this for the chip, so the text starts empty the next time it shows.
function InstrumentFilter({
  onSelect,
  autoFocus,
}: {
  onSelect: (instrument: Instrument) => void
  autoFocus: boolean
}) {
  const [query, setQuery] = useState('')
  return (
    <InstrumentSearchInput
      value={query}
      onChange={setQuery}
      onSelect={onSelect}
      autoFocus={autoFocus}
      aria-label={t('transactions.filter.instrument')}
    />
  )
}

type ListFilter = (typeof LIST_FILTERS)[number]

// The toolbar's filters, shown in the row on the desktop and in FilterSheet
// below md. Each control writes its query parameter through the page.
export function FilterControls({
  desktop,
  filter,
  from,
  to,
  rangeValid,
  importFilter,
  imports,
  instrumentSymbol,
  focusInstrumentFilter,
  onSelectInstrument,
  setParam,
  clearDates,
}: {
  desktop: boolean
  filter: ListFilter
  from: string | undefined
  to: string | undefined
  rangeValid: boolean
  importFilter: ImportFilter | undefined
  imports: Import[]
  instrumentSymbol: string | null
  focusInstrumentFilter: boolean
  onSelectInstrument: (instrument: Instrument | undefined) => void
  setParam: (name: string, value: string | undefined) => void
  clearDates: () => void
}) {
  // Tümü, Elle, then this portfolio's imports, newest first as the API lists
  // them. An import id the list does not hold (still loading, or undone) is
  // kept as an option, so the select still shows what the URL filters by.
  const sourceOptions: SelectOption<string>[] = [
    { value: 'all', label: t('transactions.filter.all') },
    { value: 'none', label: t('transactions.source.manual') },
    ...imports.map((item) => ({ value: String(item.id), label: importLabel(item) })),
  ]
  if (typeof importFilter === 'number' && !imports.some((item) => item.id === importFilter)) {
    sourceOptions.push({ value: String(importFilter), label: `#${importFilter}` })
  }

  const instrumentControl = instrumentSymbol ? (
    <Button
      variant="secondary"
      aria-label={t('transactions.filter.clearInstrument', { symbol: instrumentSymbol })}
      onClick={() => onSelectInstrument(undefined)}
      autoFocus={focusInstrumentFilter}
      className="shrink-0"
    >
      <span className="font-semibold">{instrumentSymbol}</span>
      <Icon name="x" />
    </Button>
  ) : (
    <div className={desktop ? 'w-40 shrink-0' : 'w-full'}>
      <InstrumentFilter onSelect={onSelectInstrument} autoFocus={focusInstrumentFilter} />
    </div>
  )

  return (
    <>
      {instrumentControl}
      <Segmented
        options={LIST_FILTERS}
        value={filter}
        onChange={(f) => setParam('type', filterParam(f.types))}
        optionKey={(f) => filterParam(f.types) || 'all'}
        label={(f) => t(f.labelKey)}
        aria-label={t('transactions.filter.label')}
        className="shrink-0 self-start"
        itemClassName="max-md:min-h-11"
      />
      <DateRangeFilter
        from={from}
        to={to}
        invalid={!rangeValid}
        errorId="transactions-date-error"
        onChange={setParam}
        onClear={clearDates}
      />
      {/* A fixed width, so a long import name truncates rather than pushes
        the count and the export off the row. */}
      <div className={desktop ? 'w-52 shrink-0' : ''}>
        <Select
          id="transactions-source"
          aria-label={t('transactions.source')}
          prefix={`${t('transactions.source')}:`}
          value={importFilter === undefined ? 'all' : String(importFilter)}
          onChange={(value) => setParam('import', value === 'all' ? undefined : value)}
          options={sourceOptions}
        />
      </div>
    </>
  )
}
