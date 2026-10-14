import { expect, it } from 'vitest'
import type { PreviewRow } from '../../api/imports'
import { inDateOrder, problemRows } from './reviewRows'

const row = (key: string, change: Partial<PreviewRow> = {}): PreviewRow => ({
  key,
  status: 'ok',
  ...change,
})

it('holds the unreadable rows and the rows to fix, each once', () => {
  const rows = [
    row('ok'),
    row('dup', { duplicate_of: '2026-07-01' }),
    row('skip', { status: 'skipped', reason: 'unsupported_type' }),
    row('unknown', { status: 'unknown_instrument', symbol: 'XYZAB' }),
    row('currency', { status: 'unsupported_currency', currency: 'GBP' }),
    // Unreadable and with an unknown symbol: one problem, not two.
    row('err', { status: 'error', section: 'trades', unknown_instrument: true }),
    row('invalid', { status: 'error', code: 'invalid' }),
  ]
  expect(problemRows(rows).map((r) => r.key)).toEqual(['unknown', 'currency', 'err', 'invalid'])
})

it('puts rows in date order, an unreadable row at its date and an undated one last', () => {
  const rows = [
    row('a', { date: '2026-07-10' }),
    row('b', { date: '2026-07-22' }),
    row('cash', { date: '2026-07-01' }),
    row('undated', { status: 'error' }),
    row('err', { status: 'error', date: '2026-07-14' }),
    row('c', { date: '2026-07-22' }),
  ]
  expect(inDateOrder(rows).map((r) => r.key)).toEqual(['cash', 'a', 'err', 'b', 'c', 'undated'])
  // A new array: the rows' own order is left alone.
  expect(rows[0].key).toBe('a')
})

it('keeps the rows of one day in the order they came', () => {
  const rows = [
    row('open-cash-TRY', { date: '2026-06-30', opening: true }),
    row('open-1', { date: '2026-06-30', opening: true }),
    row('open-2', { date: '2026-06-30', opening: true }),
  ]
  expect(inDateOrder(rows).map((r) => r.key)).toEqual(['open-cash-TRY', 'open-1', 'open-2'])
})
