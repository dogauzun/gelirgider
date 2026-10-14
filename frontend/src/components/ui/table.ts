// The UI v2 compact table (core pages spec, "Compact table", and its
// mockup): a header row in small muted type, rows of one fixed height, text
// left-aligned, figures right-aligned and never wrapping, and a tinted group
// row. Alignment is inherited from TABLE, so a header over a figure column
// adds `text-right` without a conflicting class to override.

export const TABLE = 'w-full border-collapse text-left text-[13px] text-ink'

// A header cell; over a figure column add `text-right`.
export const TH =
  'border-b border-line px-2 py-[7px] text-[11px] font-medium whitespace-nowrap text-muted first:pl-4 last:pr-4'

// A body cell, one holding a figure, and one whose text wraps, such as a
// long name in a narrow table. The row sets the height. White-space is set in
// one class per cell: two would conflict, and the generated CSS picks the
// winner, not the class order.
const CELL = 'border-b border-paper px-2 first:pl-4 last:pr-4'
export const TD = `${CELL} whitespace-nowrap`
export const TD_NUM = `${TD} text-right tabular-nums`
export const TD_WRAP = `${CELL} whitespace-normal wrap-anywhere`

// Row heights: 40px on Özet, 44px on İşlemler.
export const ROW_40 = 'h-10'
export const ROW_44 = 'h-11'

// A row heading a group of rows, such as a date on İşlemler; its one cell
// spans the table.
export const GROUP_ROW = 'bg-paper text-[11px] font-medium text-muted [&>td]:px-4 [&>td]:py-1'

// A sortable table's sort: the column and its direction.
export type Sort<K extends string> = { key: K; dir: 'asc' | 'desc' }

// The sort after a click on `key`'s header: the same column flips, another
// starts A to Z if it is text (the symbol, the one text column we sort) and
// largest or newest first if it holds figures or dates.
export function nextSort<K extends string>(sort: Sort<K>, key: K): Sort<K> {
  if (sort.key === key) return { key, dir: sort.dir === 'asc' ? 'desc' : 'asc' }
  return { key, dir: key === 'symbol' ? 'asc' : 'desc' }
}
