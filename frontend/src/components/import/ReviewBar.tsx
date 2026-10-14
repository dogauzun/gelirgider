import type { ReactNode } from 'react'
import { t } from '../../i18n/strings'
import { bottomBar } from '../../lib/bottomBar'
import { Button } from '../ui/Button'
import { Icon } from '../ui/Icon'

// The review's totals and buttons (GLG-966), at the bottom of the screen at
// every width, as the phone had it (GLG-791). Sticky rather than fixed: it
// stays in the page column beside the sidebar, and once the table ends it
// sits after the last row, so it never covers one and the table needs no
// padding of the bar's height; keyboard focus never scrolls a row under it
// (GLG-1067). `children` are the page's buttons (Vazgeç,
// "Bu ekstreyi atla", İçe aktar) and its status line. `notice` is a failed
// commit's error, on its own line above them so it is in view wherever the
// review is scrolled (GLG-1065).
export function ReviewBar({
  selected,
  leftOut,
  onAddRow,
  notice,
  children,
}: {
  selected: number
  leftOut: number
  onAddRow: () => void
  notice?: ReactNode
  children?: ReactNode
}) {
  return (
    <div
      ref={bottomBar}
      data-testid="preview-actions"
      className="sticky bottom-0 z-10 mt-4 flex flex-wrap items-center justify-between gap-3 card px-4 py-2.5 max-md:-mx-6 max-md:rounded-none max-md:px-6 max-md:pb-[max(0.625rem,env(safe-area-inset-bottom))]"
    >
      {notice && <div className="basis-full">{notice}</div>}
      <div className="flex items-center gap-3">
        {/* Operations the parser dropped entirely -- a stock transfer, a cash
            type it skipped -- are typed in here. "Satır ekle", as the page
            header's "İşlem ekle" adds a transaction, not a row (GLG-966). */}
        <Button variant="secondary" onClick={onAddRow}>
          <Icon name="plus" />
          {t('import.addReviewRow')}
        </Button>
        <p className="text-[12px] text-muted">
          <span>{t('import.selected', { count: selected })}</span>
          {leftOut > 0 && (
            <>
              {' · '}
              <span className="text-warn">{t('import.leftOut', { count: leftOut })}</span>
            </>
          )}
        </p>
      </div>
      {/* ml-auto: wrapped to the bar's second line on a phone, the buttons
          stay on the right (GLG-1055). */}
      <div className="ml-auto flex flex-wrap items-center justify-end gap-2">{children}</div>
    </div>
  )
}
