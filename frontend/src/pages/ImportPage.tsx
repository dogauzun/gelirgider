import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useParseStatements, type ImportResult } from '../api/imports'
import type { Refused } from '../components/import/DropZone'
import { ImportDone } from '../components/import/ImportDone'
import { ImportQueue } from '../components/import/ImportQueue'
import { ImportSteps } from '../components/import/ImportSteps'
import { PastImports } from '../components/import/PastImports'
import {
  alreadyImportedSkip,
  batchNames,
  queueFromParses,
  skipQueued,
} from '../components/import/queue'
import { alreadyImported, type QueueItem } from '../components/import/shared'
import { StatementReview } from '../components/import/StatementReview'
import { UploadCard } from '../components/import/UploadCard'
import { useBatchQueue } from '../components/import/useBatchQueue'
import { useLeaveGuard } from '../components/import/useLeaveGuard'
import { useStatementReview } from '../components/import/useStatementReview'
import { useStrayDropGuard } from '../components/import/useStrayDropGuard'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { t } from '../i18n/strings'
import type { NavState } from '../lib/navState'
import { usePortfolioTitle } from '../lib/useTitle'

function mark(items: QueueItem[], index: number, change: Partial<QueueItem>): QueueItem[] {
  return items.map((item, i) => (i === index ? { ...item, ...change } : item))
}

export function ImportPage() {
  const params = useParams()
  const portfolioId = Number(params.id)
  usePortfolioTitle(t('import.title'), portfolioId)
  const navigate = useNavigate()
  useStrayDropGuard()
  const [broker, setBroker] = useState('auto')
  const [files, setFiles] = useState<File[]>([])
  // Files the drop zone refused next to a single good one, named in its
  // review since step 1 and its message are gone.
  const [refused, setRefused] = useState<Refused[]>([])
  const [reading, setReading] = useState(false)
  // Files the upfront parse queued; they sort first, so the one in review
  // is number turn + 1 of these.
  const [reviewable, setReviewable] = useState(0)
  const parseAll = useParseStatements(portfolioId)
  const { queue, setQueue, turn, batchDone, listed } = useBatchQueue(portfolioId, reading)
  const currentFile = queue ? queue[turn]?.file : files[0]
  // The batch's fund names every parse of the file in review sends.
  const names = queue && turn !== -1 ? batchNames(queue, queue[turn].broker) : undefined
  // The statement in review.
  const state = useStatementReview(portfolioId, currentFile, names)
  const { parse, focusLater } = state
  const { preview, touched } = state.review
  // The files of the batch after the one in review.
  const later = queue ? queue.filter((item) => item.status === 'queued').length - 1 : 0
  // Leaving the page loses what Vazgeç would, and asks the same.
  const releaseLeave = useLeaveGuard(touched || later > 0, () => state.askDiscard(later))

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
          state.show(result)
          focusLater(['#import-review'])
        },
      },
    )
  }

  // Every file is parsed once up front -- parse writes nothing -- to order the
  // queue by period and to name the files that cannot be imported at all
  // before the first review.
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
    const results = await parseAll(broker, selected)
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
    state.clear()
    focusLater(['#import-review', '#import-done'], from)
    setQueue(items)
    const index = items.findIndex((item) => item.status === 'queued')
    if (index === -1) return
    parse.mutate(
      { broker, file: items[index].file, names: batchNames(items, items[index].broker) },
      {
        onSuccess: state.show,
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

  async function skipPreview(items: QueueItem[]) {
    const from = document.activeElement
    if (!(await state.askSkip())) return
    next(mark(items, turn, { status: 'skipped', reason: t('import.queue.skippedByUser') }), from)
  }

  async function cancelPreview() {
    const from = document.activeElement
    if ((touched || later > 0) && !(await state.askDiscard(later))) return
    if (!queue) {
      state.clear()
      focusLater(['#import-upload'])
      return
    }
    next(skipQueued(queue, turn), from)
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

  let step: 1 | 2 | 3 = 1
  if (batchDone) step = 3
  else if (preview || turn !== -1) step = 2

  return (
    <PortfolioHeader portfolioId={portfolioId} add="inline">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        <ImportSteps
          step={step}
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
            state={state}
            preview={preview}
            refused={queue ? [] : refused}
            onCancel={() => void cancelPreview()}
            onSkip={queue ? () => void skipPreview(queue) : undefined}
            onCommitted={committed}
          />
        )}

        {(preview || turn !== -1 || batchDone) && (
          <div className="mt-8">
            <PastImports portfolioId={portfolioId} onUndone={state.reread} />
          </div>
        )}
      </div>
    </PortfolioHeader>
  )
}
