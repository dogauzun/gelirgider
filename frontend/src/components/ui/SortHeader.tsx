import type { ReactNode } from 'react'
import { TH, type Sort } from './table'

// The one sortable header cell: a button that sorts by `column`, the arrow
// of the active direction, and aria-sort, `none` on the inactive columns.
// Over a figure column pass `text-right`.
export function SortHeader<K extends string>({
  column,
  sort,
  onSort,
  className = '',
  children,
}: {
  column: K
  sort: Sort<K>
  onSort: (key: K) => void
  className?: string
  children: ReactNode
}) {
  const active = sort.key === column
  return (
    <th
      // eslint-disable-next-line no-restricted-syntax
      aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      className={`${TH} ${className}`}
    >
      <button
        type="button"
        onClick={() => onSort(column)}
        className={`inline-flex items-center gap-0.5 hover:text-ink ${active ? 'text-ink' : ''}`}
      >
        {children}
        {active && <span aria-hidden="true">{sort.dir === 'asc' ? '↑' : '↓'}</span>}
      </button>
    </th>
  )
}
