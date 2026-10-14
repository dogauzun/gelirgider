import { t } from '../../i18n/strings'
import { Icon } from '../ui/Icon'

// The three steps of an import under the tabs (GLG-965). `batch.index` is the
// statement in review counted from 0, like the page's `turn`.
export function ImportSteps({
  step,
  batch,
}: {
  step: 1 | 2 | 3
  batch?: { index: number; total: number }
}) {
  const count = batch && { n: batch.index + 1, total: batch.total }
  // Below `sm` a batch shows only the counter so the steps stay on one line (GLG-1054).
  const labels = [
    t('import.steps.choose'),
    count ? (
      <>
        <span className="sm:hidden">
          {t('import.steps.review')} {t('import.steps.batchCounter', count)}
        </span>
        <span className="hidden sm:inline">{t('import.steps.reviewBatch', count)}</span>
      </>
    ) : (
      t('import.steps.review')
    ),
    t('import.steps.done'),
  ]
  return (
    <ol className="mb-4 flex flex-wrap items-center gap-x-1.5 gap-y-2 text-[13px] text-muted">
      {labels.map((label, i) => {
        const n = i + 1
        const done = n < step
        const current = n === step
        return (
          <li
            key={n}
            aria-current={current ? 'step' : undefined}
            className={`flex items-center gap-1.5 ${current ? 'font-medium text-ink' : ''}`}
          >
            {i > 0 && <span aria-hidden className="mr-1.5 h-px w-3 bg-line sm:w-8" />}
            <span
              aria-hidden
              className={`inline-grid size-5 place-items-center rounded-full text-[11px] font-semibold ${
                current
                  ? 'bg-accent text-on-ink'
                  : done
                    ? 'bg-gain-soft text-gain'
                    : 'bg-chip text-muted'
              }`}
            >
              {done ? <Icon name="check" className="size-3" /> : n}
            </span>
            {label}
            {done && <span className="sr-only"> {t('import.steps.finished')}</span>}
          </li>
        )
      })}
    </ol>
  )
}
