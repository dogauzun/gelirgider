import { Link } from 'react-router'
import { brokerLabel, useDeleteImport, useImports, type Import } from '../../api/imports'
import { t } from '../../i18n/strings'
import { formatMarketDate, formatShortInstant } from '../../lib/date'
import { Button } from '../ui/Button'
import { useConfirm } from '../ui/useConfirm'
import { Notice } from '../ui/Notice'
import { Pill } from '../ui/Pill'
import { useConfirmedDelete } from '../../lib/useConfirmedDelete'
import { viewImportClass } from './shared'

// The statement's months: "Haz 2026", or "Oca 2026 – Mar 2026" for a span.
function period(item: Import): string {
  const format = (iso: string) => formatMarketDate(iso, { month: 'short', year: 'numeric' })
  const start = format(item.period_start)
  const end = format(item.period_end)
  return start === end ? end : `${start} – ${end}`
}

// The browser's own zone, read once: `Intl.DateTimeFormat()` builds a
// formatter even without `new`.
const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone

// `created_at` is an instant: the day it fell on where the user is, with the
// year only when it is not this year.
function importedOn(iso: string): string {
  return formatShortInstant(iso, browserTimeZone)
}

// Past imports of this portfolio, with the undo button that deletes an
// import and every transaction it created. `GET /api/imports` answers with
// every import of the user, newest first, so the portfolio filter is done
// here rather than through a query parameter the endpoint does not have.
// `onUndone` runs once an undo has deleted the import, for a preview open
// above.
export function PastImports({
  portfolioId,
  onUndone,
}: {
  portfolioId: number
  onUndone?: () => void
}) {
  const { data, isError } = useImports()
  const remove = useDeleteImport(onUndone)
  // The mutation's error shows in the notice below, so there is no failed toast.
  const removeImport = useConfirmedDelete(remove, {
    done: 'import.undone',
    rowAttribute: 'data-undo-import',
    fallback: '#past-imports',
  })
  const { confirm, dialog } = useConfirm()
  const imports = (data ?? []).filter((item) => item.portfolio_id === portfolioId)

  async function undo(item: Import) {
    // One undo at a time: a second mutate would take the mutation over and
    // drop the first one's toast, focus move and error.
    if (remove.isPending) return
    await removeImport(
      imports.map((other) => other.id),
      item.id,
      () =>
        confirm({
          title: t('import.undoConfirm'),
          body: t('import.undoConfirmBody', { count: item.transaction_count }),
          confirmLabel: t('import.undo'),
          danger: true,
        }),
    )
  }

  return (
    <section aria-labelledby="past-imports" className="card p-4">
      <h2 id="past-imports" tabIndex={-1} className="text-[14px] font-semibold text-ink">
        {t('import.past')}
      </h2>
      {dialog}
      {remove.isError && (
        <div className="mt-3">
          <Notice role="alert">{remove.error.message}</Notice>
        </div>
      )}
      {isError && (
        <div className="mt-3">
          <Notice role="alert">{t('import.pastError')}</Notice>
        </div>
      )}
      {!isError && imports.length === 0 && (
        <p className="mt-2 text-[13px] text-muted">{t('import.pastEmpty')}</p>
      )}
      {!isError && imports.length > 0 && (
        <ul className="mt-2">
          {imports.map((item) => (
            <li
              key={item.id}
              className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-line py-2 last:border-0"
            >
              <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                <Pill tone="buy">{`${brokerLabel(item.broker)} · ${period(item)}`}</Pill>
                <span className="text-[12px] text-muted">
                  {[
                    t('transactions.count', { count: item.transaction_count }),
                    // The TRY and USD statements of a month are only told
                    // apart by their currency.
                    item.currency,
                    importedOn(item.created_at),
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </span>
              <span className="flex items-center gap-2">
                <Link
                  to={`/portfolios/${portfolioId}/transactions?import=${item.id}`}
                  className={viewImportClass}
                >
                  {t('import.history.view')}
                </Link>
                <Button
                  variant="danger-quiet"
                  className="max-md:h-11"
                  onClick={() => undo(item)}
                  pending={remove.isPending && remove.variables === item.id}
                  data-undo-import={item.id}
                >
                  {t('import.undo')}
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
