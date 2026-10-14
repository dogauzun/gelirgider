import { expect, it } from 'vitest'
import { inStep } from './rows'
import type { Row } from './shared'

// The proposed cash rows follow the checked opening buys and closing sells:
// the deposit rounds up to the cent, the withdrawal nets
// the fee and rounds down, a hand-edited deposit is the user's, and a new row
// goes in front of the opening rows or after the closing rows.
it('keeps the opening deposits and closing withdrawals in step', () => {
  const row = { status: 'ok', checked: true, fee: '0' } as const
  const rows: Row[] = [
    {
      ...row,
      key: 'trade-0',
      type: 'buy',
      date: '2026-01-10',
      quantity: '1',
      price: '5',
      amount: '5',
      currency: 'USD',
    },
    {
      ...row,
      key: 'open-cash-EUR',
      manual: true,
      opening: true,
      type: 'deposit',
      date: '2026-01-01',
      amount: '7',
      currency: 'EUR',
    },
    {
      ...row,
      key: 'open-1',
      opening: true,
      type: 'buy',
      date: '2026-01-01',
      quantity: '1',
      price: '100',
      amount: '100',
      currency: 'EUR',
    },
    {
      ...row,
      key: 'open-2',
      opening: true,
      type: 'buy',
      date: '2026-01-01',
      quantity: '3',
      price: '10.001',
      amount: '30.003',
      currency: 'USD',
    },
    {
      ...row,
      key: 'close-3',
      closing: true,
      type: 'sell',
      date: '2026-01-31',
      quantity: '4',
      price: '12.3456',
      amount: '49.3824',
      fee: '1.5',
      currency: 'USD',
    },
    {
      ...row,
      key: 'manual-0',
      manual: true,
      type: 'deposit',
      date: '2026-01-15',
      amount: '1',
      currency: 'USD',
    },
  ]

  const out = inStep(rows)

  expect(out.map((r) => r.key)).toEqual([
    'trade-0',
    'open-cash-USD',
    'open-cash-EUR',
    'open-1',
    'open-2',
    'close-3',
    'close-cash-USD',
    'manual-0',
  ])
  // 3 × 10.001 = 30.003, up to 30.01.
  expect(out[1]).toEqual({
    ...row,
    key: 'open-cash-USD',
    type: 'deposit',
    date: '2026-01-01',
    amount: '30.01',
    currency: 'USD',
    opening: true,
  })
  expect(out[2]).toEqual(rows[1])
  // 4 × 12.3456 − 1.5 = 47.8824, down to 47.88.
  expect(out[6]).toEqual({
    ...row,
    key: 'close-cash-USD',
    type: 'withdrawal',
    date: '2026-01-31',
    amount: '47.88',
    currency: 'USD',
    closing: true,
  })
})
