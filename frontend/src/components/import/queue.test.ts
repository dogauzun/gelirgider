import { expect, it } from 'vitest'
import type { StatementPreview } from '../../api/imports'
import { queueFromParses, skipQueued } from './queue'
import type { QueueItem } from './shared'

const preview = (change: Partial<StatementPreview>): StatementPreview => ({
  broker: 'midas',
  sha256: 'a',
  period_start: '2026-02-01',
  period_end: '2026-02-28',
  currency: 'TRY',
  warnings: [],
  rows: [],
  holdings: [],
  reconciliation: [],
  cash_reconciliation: null,
  ...change,
})

it('skips a second copy of a file and queues the rest by period then currency', () => {
  const files = ['feb.pdf', 'feb-copy.pdf', 'jan-usd.pdf', 'jan-try.pdf'].map(
    (name) => new File([], name),
  )
  const results: PromiseSettledResult<StatementPreview>[] = [
    { status: 'fulfilled', value: preview({ sha256: 'feb' }) },
    { status: 'fulfilled', value: preview({ sha256: 'feb' }) },
    {
      status: 'fulfilled',
      value: preview({ sha256: 'jan-usd', period_start: '2026-01-01', currency: 'USD' }),
    },
    {
      status: 'fulfilled',
      value: preview({ sha256: 'jan-try', period_start: '2026-01-01', currency: 'TRY' }),
    },
  ]
  const refused = { file: new File([], 'notes.txt'), status: 'failed' as const, reason: 'type' }
  const queue = queueFromParses(files, results, [refused], 1)
  expect(queue.map((item) => [item.file.name, item.status])).toEqual([
    ['jan-try.pdf', 'queued'],
    ['jan-usd.pdf', 'queued'],
    ['feb.pdf', 'queued'],
    ['feb-copy.pdf', 'skipped'],
    ['notes.txt', 'failed'],
  ])
  expect(queue[3].reason).toBe('feb.pdf ile aynı dosya')
})

it('skips the statement in review as the user and the ones after it as not reviewed', () => {
  const item = (name: string, status: QueueItem['status']): QueueItem => ({
    file: new File([], name),
    status,
  })
  const items = [
    item('done.pdf', 'imported'),
    item('now.pdf', 'queued'),
    item('later.pdf', 'queued'),
  ]
  const skipped = skipQueued(items, 1)
  expect(skipped.map((i) => i.status)).toEqual(['imported', 'skipped', 'skipped'])
  expect(skipped[1].reason).not.toBe(skipped[2].reason)
})
