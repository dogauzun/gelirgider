import type { ImportAlias, StatementPreview } from '../../api/imports'
import { t } from '../../i18n/strings'
import { alreadyImported, type QueueItem } from './shared'

export const alreadyImportedSkip = (): Pick<QueueItem, 'status' | 'reason'> => ({
  status: 'skipped',
  reason: t('import.queue.alreadyImported'),
})

// The queue of a batch from its upfront parses: the files still to review
// first, by period then currency, then the ones the batch dropped, then the
// ones the drop zone refused.
//
// A file already imported into this portfolio and a second copy of a file
// are skipped, each with its reason; a file imported into another
// portfolio fails, naming it. A statement is one broker, period
// and currency, as at the commit. Two different files of one
// statement, a corrected re-issue or a fuller re-download, both stay queued,
// each naming the other, for the user to skip one at its turn:
// the later one opens with the same_period warning once the first is in.
// Two CSVs of the same dates are named alike, but the later one opens with
// the non-blocking overlap warning instead: a CSV is never the same
// statement, only its rows can repeat.
export function queueFromParses(
  selected: File[],
  results: PromiseSettledResult<StatementPreview>[],
  refused: QueueItem[],
  portfolioId: number,
): QueueItem[] {
  const kept: { name: string; preview: StatementPreview }[] = []
  const items = results.map((result, i): QueueItem => {
    const file = selected[i]
    if (result.status === 'rejected') {
      return alreadyImported(result.reason, portfolioId)
        ? { file, ...alreadyImportedSkip() }
        : { file, status: 'failed', error: result.reason }
    }
    const parsed = result.value
    const item: QueueItem = {
      file,
      status: 'queued',
      broker: parsed.broker,
      period_start: parsed.period_start,
      period_end: parsed.period_end,
      currency: parsed.currency,
      names: parsed.names,
    }
    const sameFile = kept.find((other) => other.preview.sha256 === parsed.sha256)
    if (sameFile) {
      return {
        ...item,
        status: 'skipped',
        reason: t('import.queue.sameFile', { name: sameFile.name }),
      }
    }
    kept.push({ name: file.name, preview: parsed })
    return item
  })
  const statement = (item: QueueItem) =>
    `${item.broker} ${item.period_start} ${item.period_end} ${item.currency}`
  const noted = items.map((item): QueueItem => {
    if (item.status !== 'queued') return item
    const other = items.find(
      (o) => o !== item && o.status === 'queued' && statement(o) === statement(item),
    )
    return other
      ? { ...item, reason: t('import.queue.samePeriod', { name: other.file.name }) }
      : item
  })
  const key = (item: QueueItem) => `${item.period_start} ${item.currency}`
  return [
    ...noted
      .filter((item) => item.status === 'queued')
      .sort((a, b) => key(a).localeCompare(key(b))),
    ...noted.filter((item) => item.status !== 'queued'),
    ...refused,
  ]
}

// The parse endpoint's limit on names.
const maxNames = 200

// The fund names the batch's statements of broker print, each with the
// instrument of its code, for a preview to take as learned: a
// fund renamed on TEFAS is named in an early statement only by its old name,
// which a later statement prints next to the code. A name two statements
// give two instruments is left out, as the server leaves one it learned so.
// ponytail: past maxNames distinct names the rest are dropped; a year of
// statements prints a few dozen.
export function batchNames(items: QueueItem[], broker: string | undefined): ImportAlias[] {
  const byName = new Map<string, number>()
  for (const item of items) {
    if (item.broker !== broker) continue
    for (const { symbol, instrument_id } of item.names ?? []) {
      const seen = byName.get(symbol)
      byName.set(symbol, seen === undefined || seen === instrument_id ? instrument_id : 0)
    }
  }
  return Array.from(byName, ([symbol, instrument_id]) => ({ symbol, instrument_id }))
    .filter((name) => name.instrument_id !== 0)
    .slice(0, maxNames)
}
