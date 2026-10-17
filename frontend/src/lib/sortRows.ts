import { getLocale } from '../i18n/strings'
import { compareDecimal } from './decimal'

// The rows in `sort` order, one rule for every sortable table. `pick` gives
// the row's value for a column: the symbol column is text, compared in the
// app's locale so "İ" sorts as the UI shows it; any other string is an API
// decimal, compared exactly; a number (a count, a ratio, a date) is compared
// as is. A missing value sorts last in either direction, and the sort is
// stable, so equal rows keep their order.
export function sortRows<T>(
  rows: T[],
  pick: (row: T) => string | number | null,
  sort: { key: string; dir: 'asc' | 'desc' },
): T[] {
  const sign = sort.dir === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const x = pick(a)
    const y = pick(b)
    if (x === null || y === null) {
      if (x === y) return 0
      return x === null ? 1 : -1
    }
    if (typeof x === 'number' || typeof y === 'number') return sign * (Number(x) - Number(y))
    return sign * (sort.key === 'symbol' ? x.localeCompare(y, getLocale()) : compareDecimal(x, y))
  })
}
