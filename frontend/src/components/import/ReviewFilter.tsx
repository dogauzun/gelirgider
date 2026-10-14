import { t } from '../../i18n/strings'
import { Segmented } from '../ui/Segmented'

export type ReviewFilterValue = 'all' | 'problems'

const VALUES: ReviewFilterValue[] = ['all', 'problems']

// "Tümü N | Sorunlu N" above the review's rows (GLG-966): one filter in place
// of the two notices that counted the same rows twice. `problems` is
// problemRows(rows).length.
export function ReviewFilter({
  value,
  total,
  problems,
  onChange,
}: {
  value: ReviewFilterValue
  total: number
  problems: number
  onChange: (value: ReviewFilterValue) => void
}) {
  const count = { all: total, problems }
  return (
    <Segmented
      options={VALUES}
      value={value}
      onChange={onChange}
      label={(option) =>
        t(option === 'all' ? 'import.filter.all' : 'import.filter.problems', {
          count: count[option],
        })
      }
      aria-label={t('import.filter.label')}
      className="shrink-0 self-start"
      itemClassName="whitespace-nowrap max-md:min-h-11"
    />
  )
}
