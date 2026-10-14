import { useState } from 'react'
import { ApiError } from '../../api'
import { useInstrumentSearch, type Instrument } from '../../api/instruments'
import { useLookupFund } from '../../api/imports'
import { t } from '../../i18n/strings'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { InstrumentSearchInput } from '../InstrumentSearchInput'
import { Button } from '../ui/Button'
import type { Row } from './shared'

// The box starts empty: the statement's symbol is what failed to match, so
// prefilling it only made typing append to a dead query. Its
// placeholder is the search's own, not that symbol, which read like a typed
// value; the status text already names it.
//
// A fund row named by its name may name a fund the app does not have yet. On
// a TEFAS row, a typed fund code the search has no TEFAS match for offers a
// TEFAS lookup, which creates the fund TEFAS confirms and picks it.
export function InstrumentPicker({
  row,
  onPick,
  autoFocus,
}: {
  row: Row
  onPick: (instrument: Instrument) => void
  autoFocus?: boolean
}) {
  const [query, setQuery] = useState('')
  const code = query.trim().toUpperCase()
  const fundCode = row.market === 'TEFAS' && /^[A-Z0-9]{3}$/.test(code) ? code : ''
  const { data: results } = useInstrumentSearch(fundCode)
  const lookup = useLookupFund()
  const once = useOneAtATime()
  const offerLookup =
    !!results && !results.some((i) => i.market === 'TEFAS' && i.symbol === fundCode)
  // In the table, 12rem: room for the placeholder in tr and en at the
  // table's 13px ("Search by symbol or name" needs 190 px with the input's
  // padding and border), which 9rem cut.
  return (
    <div className="min-w-[9rem] md:min-w-[12rem]">
      <InstrumentSearchInput
        value={query}
        onChange={(text) => {
          setQuery(text)
          lookup.reset()
        }}
        onSelect={onPick}
        autoFocus={autoFocus}
        aria-label={t('import.pickInstrument', { symbol: row.symbol ?? '' })}
      />
      {offerLookup && (
        <Button
          variant="ghost"
          size="sm"
          pending={lookup.isPending}
          onClick={() => once(() => lookup.mutateAsync(fundCode, { onSuccess: onPick }))}
          // Wraps and sits flush left under the field, as the plain link did:
          // the label runs past the 9rem cell at phone width. `!` because
          // two utilities for one property apply in stylesheet order.
          className="mt-1 h-auto! justify-start! px-0! py-0.5 text-left whitespace-normal! underline"
        >
          {t('import.lookupFund', { code: fundCode })}
        </Button>
      )}
      {lookup.error && (
        <p role="alert" className="text-loss mt-1 text-[13px]">
          {lookup.error instanceof ApiError && lookup.error.status === 404
            ? t('import.lookupFundNotFound', { code: fundCode })
            : lookup.error.message}
        </p>
      )}
    </div>
  )
}
