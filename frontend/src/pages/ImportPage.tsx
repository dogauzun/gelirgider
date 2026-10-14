import { useEffect, useReducer, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router'
import { useMe } from '../api/auth'
import {
  parseStatement,
  useCommitImport,
  useImports,
  useParseStatement,
  type ImportResult,
  type StatementPreview,
} from '../api/imports'
import type { Refused } from '../components/import/DropZone'
import { ImportDone } from '../components/import/ImportDone'
import { ImportQueue } from '../components/import/ImportQueue'
import { ImportSteps } from '../components/import/ImportSteps'
import { PastImports } from '../components/import/PastImports'
import { alreadyImportedSkip, batchNames, queueFromParses } from '../components/import/queue'
import { noReview, reviewReducer } from '../components/import/review'
import { picksOf } from '../components/import/rows'
import { alreadyImported, type QueueItem } from '../components/import/shared'
import { StatementReview } from '../components/import/StatementReview'
import { UploadCard } from '../components/import/UploadCard'
import { useFocusLater } from '../lib/useFocusLater'
import { useStrayDropGuard } from '../components/import/useStrayDropGuard'
import { useLeaveGuard } from '../components/import/useLeaveGuard'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { useConfirm } from '../components/ui/useConfirm'
import { t } from '../i18n/strings'
import type { NavState } from '../lib/navState'
import { usePortfolioTitle } from '../lib/useTitle'

// The Bitti list per history entry, so Back from a statement's "İşlemlerde
// gör" returns to it.
// ponytail: module memory, gone on a full reload; history state would have to
// structured-clone the files and ApiErrors. Kept per user, so another
// account in this tab never gets it back. `listed` goes with it, so an
// import undone while the user was away reads undone on return.
const doneByEntry = new Map<
  string,
  {
    user: string | undefined
    portfolioId: number
    items: QueueItem[]
    listed: ReadonlySet<number>
  }
>()

function mark(items: QueueItem[], index: number, change: Partial<QueueItem>): QueueItem[] {
  return items.map((item, i) => (i === index ? { ...item, ...change } : item))
}

export function ImportPage() {
  const params = useParams()
  const portfolioId = Number(params.id)
  usePortfolioTitle(t('import.title'), portfolioId)
  const navigate = useNavigate()
  const { key: entry } = useLocation()
  const user = useMe().data?.id
  useStrayDropGuard()
  const [broker, setBroker] = useState('auto')
  const [files, setFiles] = useState<File[]>([])
  // Several files picked at once; null for a single file, which
  // goes straight to the preview and on to the transactions page as before.
  const [restored] = useState(() => {
    const saved = doneByEntry.get(entry)
    return saved?.user === user && saved?.portfolioId === portfolioId ? saved : undefined
  })
  const [queue, setQueue] = useState<QueueItem[] | null>(restored?.items ?? null)
  // Files the drop zone refused next to a single good one, named in its
  // review since step 1 and its message are gone.
  const [refused, setRefused] = useState<Refused[]>([])
  const [reading, setReading] = useState(false)
  // Files the upfront parse queued; they sort first, so the one in review
  // is number turn + 1 of these.
  const [reviewable, setReviewable] = useState(0)
  // The statement in review (reviewReducer).
  const [review, dispatch] = useReducer(reviewReducer, noReview)
  const { preview, rows, touched } = review
  const { confirm, dialog: confirmDialog } = useConfirm()
  const parse = useParseStatement(portfolioId)
  const commit = useCommitImport()
  const { data: imports } = useImports()
  // The file whose turn it is: the first one still queued, -1 when there is
  // no batch or its last file is done.
  const turn = queue ? queue.findIndex((item) => item.status === 'queued') : -1
  const currentFile = queue ? queue[turn]?.file : files[0]
  // The batch's fund names every parse of the file in review sends.
  const names = queue && turn !== -1 ? batchNames(queue, queue[turn].broker) : undefined
  // A batch ends on this page at step 3, "Bitti".
  const batchDone = queue !== null && !reading && turn === -1
  // What is on screen, for an undo that lands renders after it started.
  const live = useRef({ file: undefined as File | undefined, preview, rows })
  useEffect(() => {
    live.current = { file: currentFile, preview, rows }
  })
  // Every import id the imports list has shown on this page, or on the
  // visit Back returns to. Only one of these can be undone: a commit whose
  // refetch of the list failed is not in it yet.
  const [listedImports, setListedImports] = useState<ReadonlySet<number>>(
    restored?.listed ?? new Set(),
  )
  if (imports && imports.some((item) => !listedImports.has(item.id))) {
    setListedImports(new Set([...listedImports, ...imports.map((item) => item.id)]))
  }
  useEffect(() => {
    if (batchDone) {
      doneByEntry.set(entry, { user, portfolioId, items: queue, listed: listedImports })
    } else doneByEntry.delete(entry)
  }, [batchDone, entry, user, portfolioId, queue, listedImports])
  // An import undone below, or anywhere else, reads as undone in the queue
  // and on Bitti, without its link to rows that are gone. It was
  // listed once and is no longer.
  const listed =
    queue?.map((item): QueueItem =>
      item.status === 'imported' &&
      item.id !== undefined &&
      listedImports.has(item.id) &&
      imports &&
      !imports.some((other) => other.id === item.id)
        ? { ...item, status: 'undone' }
        : item,
    ) ?? null
  // Where focus goes when an action takes its own control away.
  const focusLater = useFocusLater()
  // The files of the batch after the one in review.
  const later = queue ? queue.filter((item) => item.status === 'queued').length - 1 : 0
  // Leaving the page loses what Vazgeç would, and asks the same.
  const releaseLeave = useLeaveGuard(touched || later > 0, askDiscard)

  function showPreview(result: StatementPreview) {
    dispatch({ type: 'show', preview: result })
  }

  // Choosing or dropping files starts the preview at once; a
  // wrong file costs one Vazgeç, as nothing is written before İçe aktar.
  function choose(chosen: File[], dropped: Refused[]) {
    if (chosen.length > 1) {
      setRefused([])
      void startBatch(chosen, dropped)
      return
    }
    setRefused(dropped)
    setFiles(chosen)
    setQueue(null)
    parse.mutate(
      { broker, file: chosen[0] },
      {
        onSuccess: (result) => {
          showPreview(result)
          focusLater(['#import-review'])
        },
      },
    )
  }

  // Every file is parsed once up front -- parse writes nothing -- to order the
  // queue by period and to name the files that cannot be imported at all
  // before the first review. A single mutation cannot track parallel calls,
  // so these go through parseStatement directly.
  // ponytail: no client concurrency cap, the browser queues uploads past ~6
  // per host. Add one if a year of statements ever strains the server.
  async function startBatch(selected: File[], dropped: Refused[]) {
    // A batch's files live in its queue, the refused ones last as not
    // imported with the drop zone's reason.
    const refusedItems = dropped.map(({ file, reason }): QueueItem => ({
      file,
      status: 'failed',
      reason,
    }))
    setFiles([])
    setQueue([...selected.map((file): QueueItem => ({ file, status: 'queued' })), ...refusedItems])
    setReading(true)
    const results = await Promise.allSettled(
      selected.map((file) => parseStatement(portfolioId, broker, file)),
    )
    setReading(false)
    const items = queueFromParses(selected, results, refusedItems, portfolioId)
    setReviewable(items.filter((item) => item.status === 'queued').length)
    next(items)
  }

  // Hands the batch to its first queued file, or ends it. The file is parsed
  // again at its turn rather than reusing the upfront result: its same_period
  // and overlap warnings and its reconciliation depend on the statements
  // committed before it in this batch.
  // `from` is the control the user moved on with: the next review may keep
  // it, so focus moves on from it too.
  function next(items: QueueItem[], from: Element | null = null) {
    clearPreview()
    focusLater(['#import-review', '#import-done'], from)
    setQueue(items)
    const index = items.findIndex((item) => item.status === 'queued')
    if (index === -1) return
    parse.mutate(
      { broker, file: items[index].file, names: batchNames(items, items[index].broker) },
      {
        onSuccess: showPreview,
        onError: (error) =>
          next(
            mark(
              items,
              index,
              alreadyImported(error, portfolioId)
                ? alreadyImportedSkip()
                : { status: 'failed', error },
            ),
            from,
          ),
      },
    )
  }

  // An import undone below may be what the open preview's warnings and
  // likely duplicates point at, and only the server knows which: the preview
  // is requested again and merged as after a pick. This runs when
  // the undo is back, so it reads the page as it is by then, with any pick
  // since, and leaves it alone once the batch has moved to another file.
  function reread() {
    const now = live.current
    if (!now.preview || !currentFile || now.file !== currentFile) return
    parse.mutate(
      { broker: now.preview.broker, file: currentFile, picks: picksOf(now.rows), names },
      {
        onSuccess: (result) => dispatch({ type: 'merge', preview: result }),
      },
    )
  }

  function clearPreview() {
    dispatch({ type: 'clear' })
    parse.reset()
    commit.reset()
  }

  // Skipping a statement throws away the user's changes to its preview too.
  async function skipPreview(items: QueueItem[]) {
    const from = document.activeElement
    if (
      touched &&
      !(await confirm({
        title: t('import.skipConfirm'),
        body: t('import.cancelConfirmEdits'),
        confirmLabel: t('import.queue.skip'),
        danger: true,
      }))
    ) {
      return
    }
    next(mark(items, turn, { status: 'skipped', reason: t('import.queue.skippedByUser') }), from)
  }

  // Vazgeç throws away the user's changes to this preview and, in a batch,
  // skips every file not yet reviewed; either loss is asked about first.
  function askDiscard() {
    const body = [
      touched && t('import.cancelConfirmEdits'),
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

  async function cancelPreview() {
    const from = document.activeElement
    if ((touched || later > 0) && !(await askDiscard())) return
    if (!queue) {
      clearPreview()
      focusLater(['#import-upload'])
      return
    }
    // The statement in review was skipped by the user, the ones after it
    // without a look; both say so.
    next(
      queue.map((item, index): QueueItem =>
        item.status === 'queued'
          ? {
              ...item,
              status: 'skipped',
              reason: t(index === turn ? 'import.queue.skippedByUser' : 'import.queue.notReviewed'),
            }
          : item,
      ),
      from,
    )
  }

  function committed(result: ImportResult, from: Element | null) {
    if (queue) {
      next(
        mark(queue, turn, {
          status: 'imported',
          count: result.transaction_count,
          id: result.id,
        }),
        from,
      )
      return
    }
    // İşlemler opens on this import's rows, with a banner naming it
    // (core pages spec, "After an import").
    releaseLeave()
    void navigate(`/portfolios/${portfolioId}/transactions?import=${result.id}`, {
      state: { imported: result.id } satisfies NavState,
    })
  }

  return (
    <PortfolioHeader portfolioId={portfolioId} add="inline">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        <ImportSteps
          step={batchDone ? 3 : preview || turn !== -1 ? 2 : 1}
          batch={queue && turn !== -1 && !reading ? { index: turn, total: reviewable } : undefined}
        />
        {batchDone && listed && (
          <ImportDone
            items={listed}
            portfolioId={portfolioId}
            onRestart={() => {
              setQueue(null)
              focusLater(['#import-upload'])
            }}
          />
        )}
        {listed && !batchDone && (
          <ImportQueue
            queue={listed}
            current={turn}
            reviewable={reviewable}
            reading={reading}
            portfolioId={portfolioId}
          />
        )}

        {/* A batch hides step 1 until "Başka ekstre yükle" at its end. Upload first,
            the history beside it from lg, stacked below. */}
        {!preview && turn === -1 && !batchDone && (
          <div className="grid items-start gap-4 lg:grid-cols-[5fr_4fr]">
            <UploadCard
              broker={broker}
              onBroker={setBroker}
              onFiles={choose}
              parse={parse}
              portfolioId={portfolioId}
            />
            <PastImports portfolioId={portfolioId} />
          </div>
        )}
        {preview && (
          <StatementReview
            portfolioId={portfolioId}
            review={review}
            preview={preview}
            dispatch={dispatch}
            file={currentFile}
            names={names}
            refused={queue ? [] : refused}
            imports={imports}
            parse={parse}
            commit={commit}
            batch={queue !== null}
            confirm={confirm}
            confirmDialog={confirmDialog}
            focusLater={focusLater}
            onCancel={() => void cancelPreview()}
            onSkip={() => {
              if (queue) void skipPreview(queue)
            }}
            onCommitted={committed}
          />
        )}

        {(preview || turn !== -1 || batchDone) && (
          <div className="mt-8">
            <PastImports portfolioId={portfolioId} onUndone={reread} />
          </div>
        )}
      </div>
    </PortfolioHeader>
  )
}
