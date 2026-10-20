import { expect, it } from 'vitest'
import type { PreviewRow, StatementPreview } from '../../api/imports'
import { liveReconciliation } from './reconcile'

const interest: PreviewRow = {
  key: 'cash-1',
  status: 'ok',
  type: 'dividend',
  date: '2026-10-01',
  amount: '50.00',
  fee: '0',
  currency: 'TRY',
}
const lateBuy: PreviewRow = {
  key: 'trade-0',
  status: 'ok',
  type: 'buy',
  date: '2026-10-01',
  instrument_id: 7,
  quantity: '3',
  price: '10',
  amount: '30.00',
  fee: '0',
  currency: 'TRY',
}
const preview: StatementPreview = {
  broker: 'midas',
  sha256: 'a',
  period_start: '2026-09-01',
  period_end: '2026-09-30',
  currency: 'TRY',
  warnings: [],
  rows: [interest, lateBuy],
  holdings: [],
  reconciliation: [
    {
      symbol: 'TEST',
      instrument_id: 7,
      statement_quantity: '0',
      portfolio_quantity: '0',
      match: true,
    },
  ],
  cash_reconciliation: {
    currency: 'TRY',
    statement_amount: '1050.00',
    portfolio_amount: '1050',
    match: true,
  },
}

it('moves the cash line by an instrument-less row dated after the period end', () => {
  const { cash } = liveReconciliation(preview, [lateBuy])
  expect(cash).toMatchObject({ portfolio_amount: '1000', match: false })
})

it('leaves a row with an instrument dated after the period end out', () => {
  const { reconciliation, cash } = liveReconciliation(preview, [interest])
  expect(reconciliation).toEqual(preview.reconciliation)
  expect(cash).toEqual(preview.cash_reconciliation)
})
