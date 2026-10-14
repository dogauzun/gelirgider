import { useId, useState, type CSSProperties, type KeyboardEvent } from 'react'
import { useSetPrice } from '../api/prices'
import type { Position } from '../api/summary'
import { t } from '../i18n/strings'
import {
  formatDecimalInput,
  isAmbiguousThousands,
  parseDecimalInput,
  tooManyDigits,
} from '../lib/decimal'
import { useIsDesktop } from '../lib/useIsDesktop'
import { todayIstanbul } from '../lib/date'
import { toast } from '../lib/toast'
import { useOneAtATime } from '../lib/useOneAtATime'
import { Button } from './ui/Button'
import { inputClass } from './ui/inputClass'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'

// The panel's own height, about: below an anchor that leaves less room than
// this under it, the panel opens above the anchor instead.
const PANEL_HEIGHT = 180

// Where the panel sits: under the anchor (a positions-table row), its right
// edge on the row's, or above the row near the bottom of the screen. Read
// once, when the dialog opens; a modal dialog keeps the page from changing
// under it.
function anchorStyle(anchor: Element): CSSProperties {
  const rect = anchor.getBoundingClientRect()
  const right = window.innerWidth - rect.right
  return rect.bottom + PANEL_HEIGHT < window.innerHeight
    ? { top: rect.bottom + 4, right }
    : { bottom: window.innerHeight - rect.top + 4, right }
}

// The manual price editor (GLG-134), opened from a positions-table row's
// Fiyat gir (GLG-933): a price and a date, Kaydet and Vazgeç. Enter saves,
// Escape cancels. A native modal <dialog> as ConfirmDialog, anchored to the
// row from md up and a bottom sheet below it; without an anchor it is centred.
// Mounted only while open; useModal hands focus back to its opener.
export function PriceDialog({
  position,
  portfolioId,
  anchor,
  onClose,
}: {
  position: Position
  portfolioId: number
  anchor: Element | null
  onClose: () => void
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const desktop = useIsDesktop()
  const [style] = useState(() => (anchor && desktop ? anchorStyle(anchor) : undefined))
  const [price, setPrice] = useState(() => formatDecimalInput(position.last_price ?? ''))
  const [date, setDate] = useState(todayIstanbul)
  const [error, setError] = useState<string | null>(null)
  const mutation = useSetPrice(portfolioId)
  const once = useOneAtATime()
  // Non-null only when the price in effect is this user's own manual entry.
  const manualDate = position.price_source === 'manual' ? position.price_date : null

  function save() {
    // Kaydet ignores clicks while pending, but Enter in a field comes straight
    // here: a second press while the save runs would send it twice (GLG-994).
    if (mutation.isPending) return
    setError(null)
    if (isAmbiguousThousands(price)) {
      setError(t('decimal.error.ambiguousThousands'))
      return
    }
    const close = parseDecimalInput(price)
    if (close !== null && tooManyDigits(close, 8)) {
      setError(t('decimal.error.tooManyDigits', { field: t('transactions.price'), fraction: '8' }))
      return
    }
    if (close === null || !(Number(close) > 0)) {
      setError(t('prices.error.invalid'))
      return
    }
    // The date is a path segment: empty (cleared, or a partial date the input
    // reports as '') it would PUT .../prices/, which only the mux 404s (GLG-478).
    if (!date) {
      setError(t('error.field.date'))
      return
    }
    // Re-saving the manual price already in effect changes nothing: no
    // request, no toast (GLG-813).
    if (manualDate === date && Number(close) === Number(position.last_price)) {
      onClose()
      return
    }
    once(() =>
      mutation.mutateAsync(
        { instrumentId: position.instrument_id, date, close },
        {
          onSuccess: () => {
            onClose()
            toast(t('prices.saved'))
          },
          onError: (mutationError) => setError(mutationError.message),
        },
      ),
    )
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'Enter') {
      event.preventDefault()
      save()
    } else if (event.key === 'Escape') {
      onClose()
    }
  }

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <div
        style={style}
        className={
          style
            ? 'absolute w-full rounded-card bg-surface p-4 shadow-pop md:w-auto'
            : sheetPanel('p-4 md:w-auto')
        }
      >
        <h2 id={titleId} className="text-[14px] font-semibold text-ink">
          {t('prices.enterLabel', { symbol: position.symbol })}
        </h2>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {/* The shared input class is full width, so the width lives on a
              wrapper instead of fighting it with a second w-* utility. */}
          <span className="inline-block w-28">
            <input
              autoFocus
              type="text"
              inputMode="decimal"
              aria-label={t('transactions.price')}
              value={price}
              onChange={(event) => setPrice(event.target.value)}
              onKeyDown={handleKeyDown}
              // Read-only, not disabled: Chromium blurs a disabled field, and
              // Enter in it is how the save usually starts (GLG-994).
              readOnly={mutation.isPending}
              className={`${inputClass()} text-right`}
            />
          </span>
          <span className="inline-block w-40">
            <input
              type="date"
              aria-label={t('transactions.date')}
              value={date}
              min="1990-01-01"
              max={todayIstanbul()}
              onChange={(event) => setDate(event.target.value)}
              onKeyDown={handleKeyDown}
              readOnly={mutation.isPending}
              className={inputClass()}
            />
          </span>
          <Button onClick={save} pending={mutation.isPending}>
            {t('transactions.save')}
          </Button>
          <Button variant="secondary" onClick={onClose} pending={mutation.isPending}>
            {t('transactions.cancel')}
          </Button>
        </div>
        {error && (
          <p role="alert" className="mt-2 text-[12px] text-loss">
            {error}
          </p>
        )}
      </div>
    </Modal>
  )
}
