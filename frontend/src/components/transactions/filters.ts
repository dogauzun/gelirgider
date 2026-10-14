import type { ImportFilter, TransactionType } from '../../api/transactions'
import type { StringKey } from '../../i18n/strings'

// Options of the list's type filter (GLG-173). `all` sends no types at all;
// `cash` folds the three non-trade types into one segment, so the control
// stays at five options. The joined types are what goes in ?type= and in the
// request, so the URL reads ?type=deposit,withdrawal,fee.
export const LIST_FILTERS: { types: TransactionType[]; labelKey: StringKey }[] = [
  { types: [], labelKey: 'transactions.filter.all' },
  { types: ['buy'], labelKey: 'transactions.type.buy' },
  { types: ['sell'], labelKey: 'transactions.type.sell' },
  { types: ['dividend'], labelKey: 'transactions.type.dividend' },
  { types: ['deposit', 'withdrawal', 'fee'], labelKey: 'transactions.filter.cash' },
]

// The ?type= value of a filter, and back. An unrecognised value (a hand-typed
// or stale URL) reads as the unfiltered option rather than a request the
// server would reject.
export function filterParam(types: TransactionType[]): string {
  return types.join(',')
}

export function filterFromParam(param: string) {
  return LIST_FILTERS.find((f) => filterParam(f.types) === param) ?? LIST_FILTERS[0]
}

// The ?instrument= value of the instrument filter (GLG-333). Anything but a
// positive integer reads as no filter, like an unrecognised ?type=.
export function instrumentFromParam(param: string | null): number | undefined {
  const id = Number(param)
  return Number.isInteger(id) && id > 0 ? id : undefined
}

// The ?from= and ?to= values of the date range filter (GLG-602). Anything but
// a YYYY-MM-DD date reads as no bound, so a hand-typed URL cannot send the
// server a value it rejects.
export function dateFromParam(param: string | null): string | undefined {
  return param && /^\d{4}-\d{2}-\d{2}$/.test(param) ? param : undefined
}

// The ?import= value of the Kaynak filter: an import's id, or `none` for the
// rows entered by hand. Anything else reads as no filter, like ?instrument=.
export function importFromParam(param: string | null): ImportFilter | undefined {
  if (param === 'none') return 'none'
  const id = Number(param)
  return Number.isInteger(id) && id > 0 ? id : undefined
}
