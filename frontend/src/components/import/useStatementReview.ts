import { useEffect, useReducer, useRef, useState } from 'react'
import { ApiError, rowError } from '../../api'
import {
  useCommitImport,
  useParseStatement,
  type ImportAlias,
  type ImportResult,
  type StatementPreview,
} from '../../api/imports'
import type { Instrument } from '../../api/instruments'
import type { TransactionInput } from '../../api/transactions'
import { t } from '../../i18n/strings'
import { toast } from '../../lib/toast'
import { useFocusLater } from '../../lib/useFocusLater'
import { useConfirm } from '../ui/useConfirm'
import { noReview, reviewReducer, type ReviewAction } from './review'
import { inDateOrder, problemRows } from './reviewRows'
import { picksOf, toImportRow } from './rows'
import { resolveRows } from './shared'

// The statement in review and everything that changes it: the review
// (reviewReducer), the parse that fills and refreshes it, the commit that
// ends it, and the confirm dialog and focus handover both use. The page
// holds it for the whole batch and moves the batch on; StatementReview
// renders it. `file` is the statement in review, `names` the batch's fund
// names every parse of it sends.
export function useStatementReview(
  portfolioId: number,
  file: File | undefined,
  names: ImportAlias[] | undefined,
) {
  const [review, dispatch] = useReducer(reviewReducer, noReview)
  const { preview, rows, dialog } = review
  const { confirm, dialog: confirmDialog } = useConfirm()
  const parse = useParseStatement(portfolioId)
  const commit = useCommitImport()
  // Where focus goes when an action takes its own control away.
  const focusLater = useFocusLater()
  // The keys of the rows the last commit sent, in order: its `rows[i]` error
  // names the row on screen through these.
  const [sentKeys, setSentKeys] = useState<string[]>([])
  // What is on screen, for an undo that lands renders after it started.
  const live = useRef({ file, preview, rows })
  useEffect(() => {
    live.current = { file, preview, rows }
  })

  function show(result: StatementPreview) {
    dispatch({ type: 'show', preview: result })
  }

  function clear() {
    dispatch({ type: 'clear' })
    parse.reset()
    commit.reset()
  }

  // An import undone may be what the open preview's warnings and
  // likely duplicates point at, and only the server knows which: the preview
  // is requested again and merged as after a pick. This runs when
  // the undo is back, so it reads the page as it is by then, with any pick
  // since, and leaves it alone once the batch has moved to another file.
  function reread() {
    const now = live.current
    if (!now.preview || !file || now.file !== file) return
    parse.mutate(
      { broker: now.preview.broker, file, picks: picksOf(now.rows), names },
      {
        onSuccess: (result) => dispatch({ type: 'merge', preview: result }),
      },
    )
  }

  // Skipping a statement throws away the user's changes to its preview too.
  async function askSkip() {
    return (
      !review.touched ||
      (await confirm({
        title: t('import.skipConfirm'),
        body: t('import.cancelConfirmEdits'),
        confirmLabel: t('import.queue.skip'),
        danger: true,
      }))
    )
  }

  // Vazgeç throws away the user's changes to this preview and, in a batch,
  // skips every file not yet reviewed (`later` of them); either loss is asked
  // about first.
  function askDiscard(later: number) {
    const body = [
      review.touched && t('import.cancelConfirmEdits'),
      later > 0 && t('import.cancelConfirmQueued', { count: later }),
    ]
      .filter(Boolean)
      .join(' ')
    return confirm({
      title: t('import.cancelConfirm'),
      body,
      confirmLabel: t('import.cancelConfirmButton'),
      danger: true,
    })
  }

  // A change to the rows (each sets `touched`, so Vazgeç asks first from now
  // on), and a failed commit's error, which may name a row now
  // fixed, goes. Only a failed one: resetting a commit in flight
  // drops its onSuccess.
  function change(action: ReviewAction) {
    dispatch(action)
    if (commit.isError) commit.reset()
  }

  // A fixed row keeps focus on its pencil; one that left Sorunlu hands it to
  // the next row still to fix, else the one before, else the review heading.
  function focusAfterFix(key: string) {
    const byDate = inDateOrder(rows)
    const at = byDate.findIndex((row) => row.key === key)
    const keys = [
      key,
      ...problemRows(byDate.slice(at + 1)).map((row) => row.key),
      ...problemRows(byDate.slice(0, Math.max(at, 0)))
        .map((row) => row.key)
        .reverse(),
    ]
    focusLater([...keys.map((k) => `[data-edit-row="${k}"]`), '#import-review'])
  }

  // Choosing an instrument is the whole fix for an unknown symbol
  // (resolveRows). The preview is then requested again with every pick so far, so its
  // opening rows and reconciliation follow the picks. Only the
  // latest request's answer is applied, and it is merged into the rows as
  // they are by then (mergeRows).
  function pickInstrument(key: string, instrument: Instrument) {
    if (!preview) return
    const updated = resolveRows(rows, key, instrument)
    change({ type: 'pick', rows: updated })
    focusAfterFix(key)
    if (!file) return
    parse.mutate(
      { broker: preview.broker, file, picks: picksOf(updated), names },
      {
        onSuccess: (result) => {
          const added =
            result.rows.filter((row) => row.opening).length -
            rows.filter((row) => row.opening).length
          dispatch({
            type: 'merge',
            preview: result,
            announcement: added > 0 ? t('import.openingAdded', { count: added }) : '',
          })
        },
      },
    )
  }

  function applyDialog(input: TransactionInput, symbol: string | undefined) {
    if (!dialog || !preview) return
    const { key, row } = dialog
    change({ type: 'applyDialog', key, row, currency: preview.currency, input, symbol })
    // An edited row the dialog fixed leaves Sorunlu, as a picked one does.
    if (key !== null) focusAfterFix(key)
    // The dialog closes itself, or stays open for "Ekle ve yeni".
  }

  // Rows that still need fixing are left out of the commit: asked about
  // once, and named in the toast, so they are never dropped silently.
  // `from` is the control the user committed with.
  async function commitRows(onCommitted: (result: ImportResult, from: Element | null) => void) {
    if (!preview) return
    const from = document.activeElement
    const leftOut = problemRows(rows).length
    if (
      leftOut > 0 &&
      !(await confirm({
        title: t('import.leftOut', { count: leftOut }),
        body: t('import.leftOut.confirmBody'),
        confirmLabel: t('import.leftOut.confirm'),
      }))
    ) {
      return
    }
    const checked = rows.filter((row) => row.checked)
    const aliases = picksOf(checked)
    const sent = checked.flatMap((row) => {
      const out = toImportRow(row)
      return out ? [{ key: row.key, out }] : []
    })
    setSentKeys(sent.map((item) => item.key))
    return commit.mutateAsync(
      {
        broker: preview.broker,
        portfolio_id: portfolioId,
        sha256: preview.sha256,
        currency: preview.currency,
        period_start: preview.period_start,
        period_end: preview.period_end,
        rows: sent.map((item) => item.out),
        ...(aliases.length > 0 && { aliases }),
        names: preview.names,
      },
      {
        onSuccess: (result) => {
          const count = result.transaction_count
          toast(
            leftOut > 0
              ? t('import.doneLeftOut', { count, left: leftOut })
              : t('import.done', { count }),
          )
          onCommitted(result, from)
        },
        // The refused row is a checked ok row, which Sorunlu never shows:
        // Tümü puts it on screen at the number the error names.
        onError: (error) => {
          if (error instanceof ApiError && rowError(String(error.body.error))) {
            dispatch({ type: 'filter', filter: 'all' })
          }
        },
      },
    )
  }

  // A commit row error counts the rows sent, which skip the unchecked ones,
  // so `rows[i]` is mapped back to the row's place in the preview.
  const failedRow =
    commit.error instanceof ApiError ? rowError(String(commit.error.body.error)) : null
  // The row a commit error names, marked by the error and scrolled to.
  const failedKey = failedRow ? sentKeys[failedRow.index] : undefined

  return {
    review,
    dispatch,
    file,
    parse,
    commit,
    confirmDialog,
    askSkip,
    askDiscard,
    focusLater,
    show,
    clear,
    reread,
    change,
    pickInstrument,
    applyDialog,
    commitRows,
    failedRow,
    failedKey,
  }
}

export type StatementReviewState = ReturnType<typeof useStatementReview>
