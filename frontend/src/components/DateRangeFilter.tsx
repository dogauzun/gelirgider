import { Popover } from '@base-ui/react/popover'
import { useRef } from 'react'
import { t } from '../i18n/strings'
import { formatShortMarketDate } from '../lib/date'
import { Button } from './ui/Button'
import { inputClass } from './ui/inputClass'
import { Icon } from './ui/Icon'

// The "Tarih" filter of İşlemler (core pages spec): one button that names the
// active range and opens the two date inputs in a popover, so the range takes
// one slot of the toolbar instead of two inputs. `from` and `to` are the
// URL's values; the page decides what a change does with them.
export function DateRangeFilter({
  from,
  to,
  invalid = false,
  errorId,
  onChange,
  onClear,
}: {
  from?: string
  to?: string
  // `to` is before `from`: the end input is marked and described by errorId.
  invalid?: boolean
  errorId?: string
  onChange: (name: 'from' | 'to', value: string | undefined) => void
  onClear: () => void
}) {
  const fromRef = useRef<HTMLInputElement>(null)
  // A popup inside a modal <dialog> (the phone's filter sheet) has to live in
  // that dialog, else it is under the top layer and inert, as in ui/Select.
  const dialogRef = useRef<HTMLDialogElement | null>(null)
  // Short dates ("01 Oca – 31 Mar", with the year outside this one), so a
  // set range keeps the toolbar on one row at 1280 px.
  const range =
    from || to
      ? `${from ? formatShortMarketDate(from) : ''} – ${to ? formatShortMarketDate(to) : ''}`.trim()
      : t('transactions.filter.all')

  return (
    <Popover.Root>
      <Popover.Trigger
        ref={(el: HTMLButtonElement | null) => {
          dialogRef.current = el?.closest('dialog') ?? null
        }}
        aria-invalid={invalid || undefined}
        // Sized by its text: inputClass is full width by default, for a form field.
        className={`${inputClass({ width: 'auto' })} flex shrink-0 items-center gap-2 text-left whitespace-nowrap`}
      >
        <span>
          <span className="text-muted">{`${t('transactions.date')}:`}</span> {range}
        </span>
        <Icon name="chevronDown" className="shrink-0 text-muted" />
      </Popover.Trigger>
      <Popover.Portal container={dialogRef}>
        {/* Above the sticky app header (z-20), as Select and Menu. */}
        <Popover.Positioner sideOffset={4} align="start" className="z-30">
          {/* Named by the filter's label, as a dialog needs a name (GLG-1091). */}
          <Popover.Popup
            initialFocus={fromRef}
            aria-label={t('transactions.date')}
            className="flex items-center gap-2 rounded-card bg-surface p-3 shadow-pop outline-none"
          >
            <span className="w-36">
              <input
                ref={fromRef}
                type="date"
                aria-label={t('transactions.filter.from')}
                value={from ?? ''}
                onChange={(event) => onChange('from', event.target.value)}
                className={inputClass()}
              />
            </span>
            <span className="w-36">
              <input
                type="date"
                aria-label={t('transactions.filter.to')}
                aria-invalid={invalid || undefined}
                aria-describedby={invalid ? errorId : undefined}
                value={to ?? ''}
                onChange={(event) => onChange('to', event.target.value)}
                className={inputClass()}
              />
            </span>
            {(from || to) && (
              <Button
                variant="ghost"
                iconOnly
                aria-label={t('transactions.filter.clearDates')}
                onClick={() => {
                  onClear()
                  // The clear button unmounts with the dates, which dropped
                  // focus to <body>; a new range starts at the start date
                  // (GLG-620).
                  fromRef.current?.focus()
                }}
              >
                <Icon name="x" />
              </Button>
            )}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
