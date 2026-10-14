import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { ApiError } from '../../api'
import { brokerLabel } from '../../api/imports'
import { t } from '../../i18n/strings'
import { formatMarketDate } from '../../lib/date'
import { Pill } from '../ui/Pill'
import { alreadyImported, importedElsewhere, type QueueItem } from './shared'

const queueTone = {
  queued: 'cash',
  current: 'buy',
  imported: 'dividend',
  skipped: 'cash',
  failed: 'loss',
  undone: 'cash',
} as const

// The statements of a multi-file import in the order they are reviewed:
// by period, then currency, the ones that could not be read last.
// `current` is the one in review; the page shows "Bitti" (ImportDone) instead
// once the last one is done. `reviewable` is how many files the upfront parse queued for review: a
// duplicate or an unreadable file is never shown, so it is not counted.
export function ImportQueue({
  queue,
  current,
  reviewable,
  reading,
  portfolioId,
}: {
  queue: QueueItem[]
  current: number
  reviewable: number
  reading: boolean
  portfolioId: number
}) {
  // While a statement is in review the full list pushed the preview below
  // the fold on a phone, so it folds to one line naming the file.
  const [expanded, setExpanded] = useState(false)
  const folded = !reading && !expanded
  return (
    <section className="mb-4">
      <h2 id="import-queue" className="sr-only">
        {t('import.queue.title')}
      </h2>
      <p className="text-[13px] text-muted">
        <span role="status">
          {reading
            ? t('import.parsing')
            : t('import.queue.progress', {
                n: current + 1,
                total: reviewable,
              })}
        </span>
        {!reading && (
          <>
            {' · '}
            <span className="font-medium break-all text-ink">{queue[current]?.file.name}</span>
            {/* Midas names its TRY and USD statements alike. */}
            {queue[current]?.currency && ` · ${queue[current].currency}`}
            {/* The same-period note stays in sight while folded. */}
            {queue[current]?.reason && ` · ${queue[current].reason}`}
            {' · '}
            <button
              type="button"
              aria-expanded={expanded}
              aria-controls="import-queue-list"
              onClick={() => setExpanded(!expanded)}
              className="underline underline-offset-2 hover:text-ink"
            >
              {t(expanded ? 'import.queue.hide' : 'import.queue.show')}
            </button>
          </>
        )}
      </p>
      <ol
        id="import-queue-list"
        hidden={folded}
        aria-labelledby="import-queue"
        className="mt-3 overflow-hidden card"
      >
        {queue.map((item, index) => {
          const state = index === current && !reading ? 'current' : item.status
          return (
            <li
              key={index}
              aria-current={state === 'current' ? 'step' : undefined}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2.5 last:border-0"
            >
              <span className="min-w-0">
                <span className="block text-[13px] font-medium break-all text-ink">
                  {item.file.name}
                </span>
                <span className="block text-[12px] text-muted">
                  {item.error ? (
                    <ParseError error={item.error} portfolioId={portfolioId} />
                  ) : (
                    // Broker and currency first, as in the past imports below.
                    [
                      item.broker && brokerLabel(item.broker),
                      item.currency,
                      item.period_start &&
                        item.period_end &&
                        `${formatMarketDate(item.period_start)} – ${formatMarketDate(item.period_end)}`,
                      item.count !== undefined && t('transactions.count', { count: item.count }),
                      item.reason,
                    ]
                      .filter(Boolean)
                      .join(' · ')
                  )}
                </span>
              </span>
              <Pill tone={queueTone[state]}>{t(`import.queue.${state}`)}</Pill>
            </li>
          )
        })}
      </ol>
    </section>
  )
}

// The parse failures worth naming: a re-upload of a statement already in the
// portfolio or in another one, and a file over the endpoint's
// limit. Everything else is the server's own message, already translated by
// apiFetch.
export function ParseError({
  error,
  portfolioId,
}: {
  error: Error
  portfolioId: number
}): ReactNode {
  const elsewhere = importedElsewhere(error, portfolioId)
  if (elsewhere) {
    return (
      <>
        {t('import.alreadyImportedIn', { name: elsewhere.name })}{' '}
        <Link className="underline" to={`/portfolios/${elsewhere.id}/transactions`}>
          {t('import.alreadyImportedLink')}
        </Link>
      </>
    )
  }
  if (alreadyImported(error, portfolioId)) {
    return (
      <>
        {t('import.alreadyImported')}{' '}
        <Link className="underline" to={`/portfolios/${portfolioId}/transactions`}>
          {t('import.alreadyImportedLink')}
        </Link>
      </>
    )
  }
  if (error instanceof ApiError && error.status === 413) return t('import.tooLarge')
  return error.message
}
