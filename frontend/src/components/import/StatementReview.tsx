import type { MouseEvent } from 'react'
import { ApiError, translateError } from '../../api'
import {
  brokerLabel,
  useImports,
  type Import,
  type ImportResult,
  type PreviewWarning,
  type StatementPreview,
} from '../../api/imports'
import { t, type StringKey } from '../../i18n/strings'
import { formatMarketDate } from '../../lib/date'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { TransactionDialog } from '../TransactionDialog'
import { feeLabelKey, fromTransaction } from '../transactionForm'
import { Button } from '../ui/Button'
import { Notice } from '../ui/Notice'
import type { Refused } from './DropZone'
import { HoldingsPanel } from './HoldingsPanel'
import { ParseError } from './ImportQueue'
import { DialogContext, PreviewRows, UnknownNames } from './PreviewTable'
import { ReviewBar } from './ReviewBar'
import { ReviewFilter, type ReviewFilterValue } from './ReviewFilter'
import { liveReconciliation } from './reconcile'
import { inDateOrder, problemRows } from './reviewRows'
import { asTransaction, toImportRow } from './rows'
import type { Row } from './shared'
import type { StatementReviewState } from './useStatementReview'

// The warning codes of the parse endpoint about an earlier import. Both name
// it by its period, which is how the past-imports list below shows it, never
// by its id.
const warningKey: Record<string, StringKey> = {
  overlap: 'import.warning.overlap',
  same_period: 'import.warning.samePeriod',
}

function warningText(warning: PreviewWarning): string {
  // A period the commit refuses carries the commit's own error.
  if (warning.code === 'period') return translateError(warning.message)
  const key = warningKey[warning.code]
  if (!key) return warning.message
  return t(key, {
    start: formatMarketDate(warning.period_start),
    end: formatMarketDate(warning.period_end),
  })
}

// The review's heading: the statement named by its file, then broker, period
// and currency, and the filter of its rows.
function ReviewHeading({
  file,
  preview,
  total,
  problems,
  filter,
  onFilter,
}: {
  file: string | undefined
  preview: StatementPreview
  total: number
  problems: number
  filter: ReviewFilterValue
  onFilter: (value: ReviewFilterValue) => void
}) {
  // An error-only CSV has no period: an empty date would make
  // formatMarketDate throw.
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        {file && (
          <h2
            id="import-review"
            tabIndex={-1}
            className="text-[15px] font-semibold break-all text-ink"
          >
            {file}
          </h2>
        )}
        <p className="text-[13px] text-muted">
          {[
            brokerLabel(preview.broker),
            preview.period_start &&
              preview.period_end &&
              `${formatMarketDate(preview.period_start)} – ${formatMarketDate(preview.period_end)}`,
            preview.currency,
          ]
            .filter(Boolean)
            .join(' · ')}
        </p>
      </div>
      {/* One filter in place of the two notices that counted the
        same rows twice. */}
      {total > 0 && (
        <ReviewFilter
          value={problems > 0 ? filter : 'all'}
          total={total}
          problems={problems}
          onChange={onFilter}
        />
      )}
    </div>
  )
}

// Every warning but `period` names an earlier import. One undone below
// while this preview is open no longer applies: its notice goes, and a
// same_period block on İçe aktar lifts with it.
function reviewWarnings(preview: StatementPreview, imports: Import[] | undefined) {
  const warnings = (preview.warnings ?? []).filter(
    (warning) =>
      warning.code === 'period' ||
      !imports ||
      imports.some((item) => item.id === warning.import_id),
  )
  const blocked = warnings.some(
    (warning) => warning.code === 'same_period' || warning.code === 'period',
  )
  // A same_period statement's portfolio side already counts the earlier
  // import, so a mismatch there says nothing about missing months. The raw
  // warning is used: until an undo's preview is read again, the
  // reconciliation still counts the undone import.
  const samePeriod = (preview.warnings ?? []).some((warning) => warning.code === 'same_period')
  return { warnings, blocked, samePeriod }
}

// The preview proposes no opening rows while a buy or sell is unknown,
// an error row's unknown symbol included (backend proposeOpenings), so a statement with holdings says they come
// once the symbols are matched.
function awaitsOpenings(preview: StatementPreview, rows: Row[]): boolean {
  return (
    !rows.some((row) => row.opening) &&
    preview.holdings.length > 0 &&
    rows.some(
      (row) =>
        (row.status === 'unknown_instrument' || row.unknown_instrument) &&
        (row.type === 'buy' || row.type === 'sell'),
    )
  )
}

// What the review says before its rows: the files the drop zone refused, and
// the warnings of the preview. A same_period or period warning blocks İçe
// aktar.
function ReviewNotices({
  refused,
  warnings,
  blocked,
}: {
  refused: Refused[]
  warnings: PreviewWarning[]
  blocked: boolean
}) {
  return (
    <>
      {refused.length > 0 && (
        <div className="mt-3">
          <Notice role="alert" tone="warn">
            {refused.map(({ reason }, index) => (
              <p key={index}>{reason}</p>
            ))}
          </Notice>
        </div>
      )}
      {warnings.length > 0 && (
        <div className="mt-3">
          <Notice tone={blocked ? 'error' : 'warn'}>
            {warnings.map((warning) => (
              <p key={`${warning.code}-${warning.import_id}`}>{warningText(warning)}</p>
            ))}
          </Notice>
        </div>
      )}
    </>
  )
}

type StatementReviewProps = {
  portfolioId: number
  // The page's useStatementReview, and its preview, which is not null here.
  state: StatementReviewState
  preview: StatementPreview
  refused: Refused[]
  onCancel: () => void
  // Offered only for a statement of a batch, as Atla.
  onSkip?: () => void
  // `from` is the control the user committed with.
  onCommitted: (result: ImportResult, from: Element | null) => void
}

// A double click's second click (detail 2) would hit the next statement's
// Atla or İçe aktar, opened where the first one moved the batch on from.
// It is dropped only when an earlier click of the same run went
// through: one on a pending button did nothing, so the next one counts. A run
// starts with a mousedown of detail 1, which a pending button still gets. The
// flag outlives the review, as the next statement's is a new one.
let clickedInRun = false
function oncePerRun(action: () => void) {
  return {
    onMouseDown: (event: MouseEvent) => {
      if (event.detail === 1) clickedInRun = false
    },
    onClick: (event: MouseEvent) => {
      if (event.detail >= 2 && clickedInRun) return
      clickedInRun = true
      action()
    },
  }
}

// The review of one statement, step 2 of the import page: its rows to check,
// fix and add to, what the statement and the portfolio say about them, and
// the buttons that import it, skip it or give up. The page holds the review
// (reviewReducer) and moves the batch on; this renders it and changes it.
export function StatementReview({
  portfolioId,
  state,
  preview,
  refused,
  onCancel,
  onSkip,
  onCommitted,
}: StatementReviewProps) {
  const {
    review,
    dispatch,
    file,
    parse,
    commit,
    confirmDialog,
    change,
    pickInstrument,
    applyDialog,
    commitRows,
    failedRow,
    failedKey,
  } = state
  const { rows, dialog, filter, announcement } = review
  const { data: imports } = useImports()
  // İçe aktar, Vazgeç and a batch's Atla share it: either of the last two
  // would move on while a commit runs, before its pending state is in, and
  // drop the commit's outcome.
  // Atla and İçe aktar act once per double click (oncePerRun).
  const once = useOneAtATime()
  const cancel = () => once(async () => onCancel())

  // The rows as İçe aktar would send them, so the comparison below follows
  // every check, edit and added row.
  const committed = rows.flatMap((row) => (row.checked ? (toImportRow(row) ?? []) : []))
  const { reconciliation, cash: cashReconciliation } = liveReconciliation(preview, committed)
  const { warnings, blocked, samePeriod } = reviewWarnings(preview, imports)
  const checkedCount = rows.filter((row) => row.checked).length
  // A CSV prints no period of its own: the commit takes it from the rows
  // sent, so a CSV with none checked is not recorded as an empty import.
  // An error-only one would send an empty period and fail.
  const nothingToImport = preview.broker === 'csv' && checkedCount === 0
  const problems = problemRows(rows)
  const attentionCount = problems.length
  const previewFile = file?.name
  // The rows in date order, as the table and the list show them
  // and as a commit error numbers them. Sorunlu with nothing left to fix
  // shows every row rather than an empty table.
  const ordered = inDateOrder(rows)
  const shown = filter === 'problems' && problems.length > 0 ? inDateOrder(problems) : ordered

  function commitErrorMessage(error: Error): string {
    const n = failedRow ? ordered.findIndex((row) => row.key === failedKey) : -1
    if (!failedRow || n < 0) return error.message
    return t('error.row', {
      n: n + 1,
      message: translateError(failedRow.rest, feeLabelKey(ordered[n].type)),
    })
  }

  const openingPending = awaitsOpenings(preview, rows)
  // A pick requests the preview again; İçe aktar waits for it.
  const refreshing = parse.isPending

  return (
    <>
      <ReviewHeading
        file={previewFile}
        preview={preview}
        total={rows.length}
        problems={problems.length}
        filter={filter}
        onFilter={(value) => dispatch({ type: 'filter', filter: value })}
      />

      <ReviewNotices refused={refused} warnings={warnings} blocked={blocked} />

      {dialog && (
        <TransactionDialog
          key={dialog.key ?? 'new'}
          portfolioId={portfolioId}
          // An added row opens an add dialog, titled İşlem ekle, with
          // "Ekle ve yeni".
          editing={
            dialog.row &&
            asTransaction(dialog.row, portfolioId, preview.period_end, preview.currency)
          }
          initial={
            dialog.row
              ? undefined
              : fromTransaction(
                  asTransaction(null, portfolioId, preview.period_end, preview.currency),
                )
          }
          onClose={() => dispatch({ type: 'dialog', dialog: null })}
          onSubmit={applyDialog}
          context={dialog.row && <DialogContext row={dialog.row} />}
        />
      )}

      {openingPending && (
        <p className="mt-3 text-[12px] text-muted">{t('import.openingPending')}</p>
      )}

      <UnknownNames rows={ordered} onPick={pickInstrument} />

      {rows.length === 0 ? (
        <p className="mt-4 text-[13px] text-muted">{t('import.noRows')}</p>
      ) : (
        <PreviewRows
          shown={shown}
          failedKey={failedKey}
          failed={commit.error}
          onSelectAll={(checked) => change({ type: 'selectAll', checked })}
          onToggle={(key) => change({ type: 'toggle', key })}
          onEdit={(row) => dispatch({ type: 'dialog', dialog: { key: row.key, row } })}
          onPick={pickInstrument}
        />
      )}

      <HoldingsPanel
        holdings={preview.holdings}
        reconciliation={reconciliation}
        cash={cashReconciliation}
      />

      {/* A mismatch is a warning, never a block: an earlier month that
        was never imported is exactly the case this import fixes. */}
      {!samePeriod &&
        (reconciliation.some((entry) => !entry.match) || cashReconciliation?.match === false) && (
          <div className="mt-4">
            <Notice>{t('import.reconcile.warning')}</Notice>
          </div>
        )}

      {/* The preview requested again after a pick failed; the rows
        keep the pick. */}
      {parse.isError && (
        <div className="mt-4">
          <Notice role="alert">
            <ParseError error={parse.error} portfolioId={portfolioId} />
          </Notice>
        </div>
      )}

      {/* The totals and the buttons stay in view over a long review. */}
      <ReviewBar
        selected={checkedCount}
        leftOut={attentionCount}
        onAddRow={() => dispatch({ type: 'dialog', dialog: { key: null, row: null } })}
        notice={
          commit.isError && (
            <div id="import-commit-error">
              {/* A second commit of the same statement (double click, a
                second tab) gets the parse step's notice. */}
              <Notice role="alert">
                {commit.error instanceof ApiError && commit.error.status === 409 ? (
                  <ParseError error={commit.error} portfolioId={portfolioId} />
                ) : (
                  commitErrorMessage(commit.error)
                )}
              </Notice>
            </div>
          )
        }
      >
        <p role="status" className="text-[12px] text-muted">
          {refreshing ? t('import.refreshing') : announcement}
        </p>
        {/* Vazgeç and Atla wait for a commit in flight: moving on
            resets it, so its file would land with no toast, or
            while its batch row read skipped. In a batch Vazgeç
            ends it, skipping every file not yet reviewed. */}
        <Button variant="ghost" onClick={cancel} pending={commit.isPending}>
          {t('confirm.cancel')}
        </Button>
        {onSkip && (
          <Button
            variant="secondary"
            {...oncePerRun(() => once(async () => onSkip()))}
            pending={commit.isPending}
          >
            {t('import.queue.skip')}
          </Button>
        )}
        {confirmDialog}
        {/* Enabled with nothing checked: an empty month is still
            recorded, so it shows under past imports.
            Disabled for a statement already imported, which the
            server refuses until that import is undone,
            and for a period it refuses outright, and
            for a CSV with nothing checked, and while the
            preview is requested again after a pick, as its opening
            rows are not in yet. */}
        <Button
          {...oncePerRun(() => once(() => commitRows(onCommitted)))}
          disabled={blocked || nothingToImport}
          pending={commit.isPending || parse.isPending}
        >
          {commit.isPending ? t('import.importing') : t('import.title')}
        </Button>
      </ReviewBar>
    </>
  )
}
