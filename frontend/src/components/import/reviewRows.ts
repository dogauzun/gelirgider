import type { PreviewRow } from '../../api/imports'
import { NEEDS_ATTENTION } from './shared'

// The review's "Sorunlu" filter (GLG-966): the rows the parser could not read
// and the rows that need a fix, each once. An unreadable row is an `error`
// row, so one test of the status counts it once, an unknown symbol included.
// A duplicate or a skipped row needs nothing done, so it is not a problem.
export function problemRows<T extends PreviewRow>(rows: T[]): T[] {
  return rows.filter((row) => NEEDS_ATTENTION.includes(row.status))
}

// The rows by date, oldest first (GLG-966): the preview lists its unreadable
// rows after the others, so one sits at its date here, and one whose date
// could not be read goes last. The sort is stable, so the rows of one day keep
// the preview's order (an opening deposit before its buys).
export function inDateOrder<T extends PreviewRow>(rows: T[]): T[] {
  return rows.toSorted((a, b) => {
    if (!a.date || !b.date) return Number(!a.date) - Number(!b.date)
    return a.date < b.date ? -1 : a.date > b.date ? 1 : 0
  })
}
