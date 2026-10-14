import { describe, expect, it } from 'vitest'
import type { Transaction } from '../api/transactions'
import { feeText, quantityTimesPrice, signedAmount } from './transactionCells'

function tx(over: Partial<Transaction>): Transaction {
  return {
    id: 1,
    portfolio_id: 1,
    instrument_id: null,
    instrument_symbol: null,
    instrument_name: null,
    type: 'deposit',
    date: '2026-01-02',
    quantity: null,
    price: null,
    amount: '100',
    fee: '0',
    currency: 'TRY',
    note: null,
    created_at: '2026-01-02T00:00:00Z',
    import_id: null,
    ...over,
  }
}

describe('transactionCells', () => {
  it('signs cash in with + and cash out with the typographic minus', () => {
    expect(signedAmount(tx({ type: 'deposit' }))).toMatch(/^\+/)
    expect(signedAmount(tx({ type: 'buy', quantity: '1', price: '100' }))).toMatch(/^−/)
  })

  // The transactions list and the import preview share these (GLG-1167): a
  // quantity without a price, a preview's unpriced opening position, shows
  // alone, and a zero or missing fee is an empty cell.
  it('shows a price-less quantity alone and leaves a zero fee empty', () => {
    expect(quantityTimesPrice(tx({ type: 'buy', quantity: '1', price: null }))).toBe('1')
    expect(
      quantityTimesPrice({ type: 'buy', quantity: '5', price: '10', currency: 'TRY' }),
    ).toMatch(/^5 × /)
    expect(quantityTimesPrice(tx({}))).toBe('')
    expect(feeText(tx({ fee: '0' }))).toBe('')
    expect(feeText({ type: 'buy', currency: 'TRY' })).toBe('')
    expect(feeText(tx({ type: 'buy', fee: '1.5' }))).toMatch(/1,50/)
  })
})
