import { Autocomplete } from '@base-ui/react/autocomplete'
import { useEffect, useRef, useState } from 'react'
import { SEARCH_LIMIT, useInstrumentSearch, type Instrument } from '../api/instruments'
import { Badge } from './ui/Badge'
import { inputClass } from './ui/inputClass'
import { menuItemClass } from './ui/Menu'
import { marketLabel, t } from '../i18n/strings'

// Fully controlled by the parent (value/onChange) so the parent's form reset
// (after a successful add, or switching what's being edited) just flows down
// as a prop instead of needing an effect to re-sync local state.
//
// On Base UI Autocomplete (ADR 0023): focus stays in the input,
// ArrowUp/Down move the highlighted option, Enter picks it, Escape closes the
// list.
//
// `selected` says the text is a picked instrument's label, which the search
// cannot match: focusing it neither searches nor says "no results"; the
// first keystroke clears the pick and searches again.
export function InstrumentSearchInput({
  value,
  selected = false,
  onChange,
  onSelect,
  ...controlProps
}: {
  value: string
  selected?: boolean
  onChange: (text: string) => void
  onSelect: (instrument: Instrument) => void
  id?: string
  placeholder?: string
  autoFocus?: boolean
  'aria-label'?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}) {
  const [debounced, setDebounced] = useState('')
  const [open, setOpen] = useState(false)
  // A modal <dialog> is in the top layer: a popup portaled to <body> would be
  // under it and inert, so it goes into the input's dialog, as ui/Select.
  const dialogRef = useRef<HTMLDialogElement | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  // While the list is open, Base UI 1.8 puts a hidden button for screen
  // readers right before the input and right after the popup, named
  // "Dismiss" in English; no prop renames it or drops it while the input sits
  // outside the popup. Both exist once the popup mounts, and React
  // leaves the attribute alone afterwards, as its own prop never changes.
  const nameDismissButtons = (popup: HTMLElement | null) => {
    for (const button of [inputRef.current?.previousElementSibling, popup?.nextElementSibling]) {
      if (button?.getAttribute('aria-label') === 'Dismiss') {
        button.setAttribute('aria-label', t('search.closeList'))
      }
    }
  }

  const query = selected ? '' : value
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query), 300)
    return () => clearTimeout(timer)
  }, [query])

  const { data, isPending } = useInstrumentSearch(debounced)
  // Until the delay ends the results belong to the previous text; listing
  // them let ArrowDown and Enter pick from the old search.
  const stale = query !== debounced
  const results = stale ? undefined : data
  const listOpen = open && !!results && results.length > 0
  // A full page may hide matches past the cap; say so instead of a count
  // that reads like every match. Exactly SEARCH_LIMIT matches
  // look the same, and keeping on typing is still the way to narrow them.
  const capped = listOpen && results.length >= SEARCH_LIMIT
  // `results` stays undefined until the current query settles (a new query
  // key has no cached data), so an empty array means "searched, found
  // nothing" and not "still loading". While it is in flight the
  // popup says so instead of showing nothing.
  const settledEmpty = debounced.length > 0 && results?.length === 0
  let statusText: string | null = null
  if (open && !stale) {
    if (debounced.length > 0 && isPending) statusText = t('search.loading')
    else if (settledEmpty) statusText = t('search.noResults')
  }
  // Screen readers hear the outcome of each settled search through a polite
  // live region; it stays mounted so changes to it are announced.
  let announcement = ''
  if (open) {
    if (capped) announcement = t('search.resultsCapped', { count: results.length })
    else if (listOpen) announcement = t('search.resultCount', { count: results.length })
    else if (settledEmpty) announcement = t('search.noResults')
  }

  return (
    <Autocomplete.Root
      items={listOpen ? results : []}
      // The API does the matching.
      filter={null}
      value={value}
      // Only typing changes the text, and it opens the list, as Base UI does
      // not for a change without an inputType (autofill). Base UI also writes
      // the picked item into the text, and clears it on Escape with the list
      // closed; the caller sets its own label on select, and Escape only
      // closes.
      onValueChange={(text, details) => {
        if (details.reason !== 'input-change') return
        onChange(text)
        setOpen(true)
      }}
      open={listOpen || statusText !== null}
      onOpenChange={setOpen}
    >
      <Autocomplete.Input
        ref={(el) => {
          inputRef.current = el
          dialogRef.current = el?.closest('dialog') ?? null
        }}
        onFocus={() => setOpen(true)}
        placeholder={t('transactions.instrumentPlaceholder')}
        className={inputClass()}
        {...controlProps}
      />
      <Autocomplete.Status className="sr-only">{announcement}</Autocomplete.Status>
      <Autocomplete.Portal container={dialogRef}>
        {/* Above the sticky app header (z-20), as Menu. Base UI flips it
            above the input when there is no room below and keeps
            it inside the viewport. */}
        <Autocomplete.Positioner sideOffset={4} align="start" collisionPadding={8} className="z-30">
          {/* At least 22rem, so a narrow box (the import table's symbol
              cell) does not cut names to "Vangua…". */}
          <Autocomplete.Popup
            ref={nameDismissButtons}
            className="bg-surface text-ink flex max-h-[min(16rem,var(--available-height))] w-[max(var(--anchor-width),min(22rem,calc(100vw-1rem)))] flex-col rounded-card shadow-pop outline-none"
          >
            {statusText !== null ? (
              <p className="text-ink-2 m-0 px-3 py-1.5 text-[13px]">{statusText}</p>
            ) : (
              <Autocomplete.List className="min-h-0 overflow-y-auto p-1">
                {(instrument: Instrument) => (
                  <Autocomplete.Item
                    key={instrument.id}
                    value={instrument}
                    onClick={() => onSelect(instrument)}
                    // The Menu and Select item look.
                    className={menuItemClass}
                  >
                    <span className="text-ink shrink-0 font-semibold">{instrument.symbol}</span>
                    <span className="text-ink-2 truncate" title={instrument.name}>
                      {instrument.name}
                    </span>
                    <span className="ml-auto shrink-0">
                      <Badge>
                        {marketLabel(instrument.market)} · {instrument.currency}
                      </Badge>
                    </span>
                  </Autocomplete.Item>
                )}
              </Autocomplete.List>
            )}
            {capped && (
              // Not an option: hidden from the accessibility tree, which hears
              // the same text from the status region above.
              <p aria-hidden="true" className="text-ink-2 m-0 px-3 py-1.5 text-[13px]">
                {announcement}
              </p>
            )}
          </Autocomplete.Popup>
        </Autocomplete.Positioner>
      </Autocomplete.Portal>
    </Autocomplete.Root>
  )
}
