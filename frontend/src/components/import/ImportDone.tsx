import { Link } from 'react-router'
import { t } from '../../i18n/strings'
import { Button } from '../ui/Button'
import { Pill } from '../ui/Pill'
import { ParseError } from './ImportQueue'
import { viewImportClass, type QueueItem } from './shared'

// Step 3 of a multi-file import, "Bitti" (GLG-967): each imported statement
// with its rows and a link to İşlemler filtered to it, each skipped or failed
// file with its reason. With nothing imported there is no success line.
export function ImportDone({
  items,
  portfolioId,
  onRestart,
}: {
  items: QueueItem[]
  portfolioId: number
  onRestart: () => void
}) {
  const imported = items.filter((item) => item.status === 'imported').length
  return (
    <section aria-labelledby="import-done" className="card p-4">
      <h2 id="import-done" tabIndex={-1} className="text-[14px] font-semibold text-ink">
        {imported > 0
          ? t('import.queue.done', { count: imported })
          : t('import.queue.noneImported')}
      </h2>
      <ul className="mt-3">
        {items.map((item, index) => (
          <li
            key={index}
            className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-line py-2.5 last:border-0"
          >
            <span className="min-w-0">
              <span className="block text-[13px] font-medium break-all text-ink">
                {item.file.name}
              </span>
              <span className="block text-[12px] text-muted">
                {item.status === 'imported' ? (
                  t('transactions.count', { count: item.count ?? 0 })
                ) : item.error ? (
                  <ParseError error={item.error} portfolioId={portfolioId} />
                ) : (
                  item.reason
                )}
              </span>
            </span>
            {item.status === 'imported' ? (
              <Link
                className={viewImportClass}
                to={`/portfolios/${portfolioId}/transactions?import=${item.id}`}
              >
                {t('import.history.view')}
              </Link>
            ) : (
              <Pill tone={item.status === 'failed' ? 'loss' : 'cash'}>
                {t(`import.queue.${item.status}`)}
              </Pill>
            )}
          </li>
        ))}
      </ul>
      <Button variant="secondary" className="mt-3" onClick={onRestart}>
        {t('import.queue.again')}
      </Button>
    </section>
  )
}
