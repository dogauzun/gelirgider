import type { StatementPreview } from '../../api/imports'
import type { TransactionInput } from '../../api/transactions'
import type { ReviewFilterValue } from './ReviewFilter'
import { problemRows } from './reviewRows'
import { fromPreview, inStep, mergeRows, rowFromInput, withCashRows } from './rows'
import type { Row } from './shared'

// The review of one statement on the import page: its preview, its rows as
// the user changed them, and around them:
// - dialog: the row the open dialog edits, or `key: null` for an operation
//   the statement never had. Closed is `null`.
// - touched: whether the user changed the open preview (a check, a pick, an
//   edit, an added row), so cancelling it asks first.
// - filter: "Tümü | Sorunlu", on Sorunlu when a preview opens with
//   rows to fix, so they come first; the rule of each row is the same either
//   way.
// - announcement: what the last re-requested preview changed, read out by
//   the footer's status line.
export type Review = {
  preview: StatementPreview | null
  rows: Row[]
  dialog: { key: string | null; row: Row | null } | null
  touched: boolean
  filter: ReviewFilterValue
  announcement: string
}

export const noReview: Review = {
  preview: null,
  rows: [],
  dialog: null,
  touched: false,
  filter: 'all',
  announcement: '',
}

export type ReviewAction =
  // A preview of a statement, fresh.
  | { type: 'show'; preview: StatementPreview }
  // A preview requested again, merged into the rows as they are by then
  // (mergeRows).
  | { type: 'merge'; preview: StatementPreview; announcement?: string }
  | { type: 'clear' }
  | { type: 'toggle'; key: string }
  | { type: 'selectAll'; checked: boolean }
  // The rows after an inline instrument pick (resolveRows).
  | { type: 'pick'; rows: Row[] }
  // What the dialog of `key` hands back; `row` is the row it edited and
  // `currency` the preview's.
  | {
      type: 'applyDialog'
      key: string | null
      row: Row | null
      currency: string
      input: TransactionInput
      symbol: string | undefined
    }
  | { type: 'dialog'; dialog: Review['dialog'] }
  | { type: 'filter'; filter: ReviewFilterValue }

export function reviewReducer(state: Review, action: ReviewAction): Review {
  switch (action.type) {
    case 'show':
      return {
        ...state,
        preview: action.preview,
        rows: withCashRows(action.preview.rows.map(fromPreview), 'closing'),
        touched: false,
        filter: problemRows(action.preview.rows).length > 0 ? 'problems' : 'all',
      }
    case 'merge':
      return {
        ...state,
        preview: action.preview,
        rows: mergeRows(action.preview.rows, state.rows),
        announcement: action.announcement ?? state.announcement,
      }
    case 'clear':
      return { ...noReview, filter: state.filter }
    // Every row that can be committed, or none. A likely duplicate
    // is checked too; its note stays next to it.
    case 'selectAll':
      return {
        ...state,
        touched: true,
        rows: inStep(
          state.rows.map((row) =>
            row.status === 'ok' ? { ...row, checked: action.checked } : row,
          ),
        ),
      }
    case 'toggle':
      return {
        ...state,
        touched: true,
        rows: inStep(
          state.rows.map((row) =>
            row.key === action.key ? { ...row, checked: !row.checked } : row,
          ),
        ),
      }
    case 'pick':
      return { ...state, touched: true, rows: action.rows, announcement: '' }
    case 'applyDialog': {
      const { key, input, symbol, currency } = action
      // An added row is an ok row, which Sorunlu never shows: Tümü puts it in
      // view, so it is not added twice.
      if (key !== null) {
        const rows = inStep(
          state.rows.map((row) =>
            row.key === key
              ? rowFromInput(key, input, {
                  symbol,
                  previous: action.row ?? undefined,
                  currency,
                })
              : row,
          ),
        )
        return { ...state, touched: true, rows }
      }
      const manualKey = `manual-${state.rows.filter((row) => row.key.startsWith('manual-')).length}`
      return {
        ...state,
        touched: true,
        filter: 'all',
        rows: [...state.rows, rowFromInput(manualKey, input, { symbol, currency })],
      }
    }
    case 'dialog':
      return { ...state, dialog: action.dialog }
    case 'filter':
      return { ...state, filter: action.filter }
  }
}
