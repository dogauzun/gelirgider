import { useEffect, useState } from 'react'
import { useLocation } from 'react-router'
import { useMe } from '../../api/auth'
import { useImports } from '../../api/imports'
import type { QueueItem } from './shared'

// The Bitti list per history entry, so Back from a statement's "İşlemlerde
// gör" returns to it.
// shortcut: module memory, gone on a full reload; history state would have to
// structured-clone the files and ApiErrors. Kept per user, so another
// account in this tab never gets it back. `listed` goes with it, so an
// import undone while the user was away reads undone on return.
const doneByEntry = new Map<
  string,
  {
    user: number | undefined
    portfolioId: number
    items: QueueItem[]
    listed: ReadonlySet<number>
  }
>()

// The batch of the import page: its queue (null for a single file, which
// goes straight to the preview and on to the transactions page), whose turn
// it is, and the queue as the page shows it. `reading` is the upfront parse
// still running.
export function useBatchQueue(portfolioId: number, reading: boolean) {
  const { key: entry } = useLocation()
  const user = useMe().data?.id
  const { data: imports } = useImports()
  const [restored] = useState(() => {
    const saved = doneByEntry.get(entry)
    return saved?.user === user && saved?.portfolioId === portfolioId ? saved : undefined
  })
  const [queue, setQueue] = useState<QueueItem[] | null>(restored?.items ?? null)
  // The file whose turn it is: the first one still queued, -1 when there is
  // no batch or its last file is done.
  const turn = queue ? queue.findIndex((item) => item.status === 'queued') : -1
  // A batch ends on this page at step 3, "Bitti".
  const batchDone = queue !== null && !reading && turn === -1
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
  return { queue, setQueue, turn, batchDone, listed }
}
