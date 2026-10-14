import { expect, it } from 'vitest'
import type { StatementPreview } from '../../api/imports'
import { noReview, reviewReducer, type Review } from './review'

const preview = { currency: 'TRY', rows: [] } as unknown as StatementPreview

it('adds a dialog row at the end in Tümü, and a cleared review keeps the filter', () => {
  const open: Review = {
    ...noReview,
    preview,
    rows: [{ key: 'trade-0', status: 'ok', checked: true }],
    filter: 'problems',
    dialog: { key: null, row: null },
    announcement: 'x',
  }
  const added = reviewReducer(open, {
    type: 'applyDialog',
    key: null,
    row: null,
    currency: 'TRY',
    input: { type: 'deposit', date: '2026-07-01', amount: '10' },
    symbol: undefined,
  })
  expect(added.rows.map((row) => row.key)).toEqual(['trade-0', 'manual-0'])
  expect(added).toMatchObject({ touched: true, filter: 'all' })
  expect(reviewReducer({ ...added, filter: 'problems' }, { type: 'clear' })).toEqual({
    ...noReview,
    filter: 'problems',
  })
})
