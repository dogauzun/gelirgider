import { describe, expect, it } from 'vitest'
import { emptyForm, rowLabel, validate } from './transactionForm'

describe('rowLabel', () => {
  it('puts the symbol first and signs the amount, and leaves out what the row lacks', () => {
    expect(
      rowLabel({
        symbol: 'VTI',
        date: '2025-12-26',
        type: 'buy',
        quantity: '2',
        price: '331.82',
        amount: '663.64',
        currency: 'USD',
      }),
    ).toBe('VTI 26.12.2025 Alış 2 × $331,82 −$663,64')
    expect(rowLabel({ date: '2025-12-09', type: 'deposit', amount: '100', currency: 'TRY' })).toBe(
      '09.12.2025 Para yatırma +₺100,00',
    )
    expect(rowLabel({})).toBe('')
  })
})

describe('validate fee against amount', () => {
  const form = (fee: string, amount: string) => ({
    ...emptyForm('deposit', '2025-12-26'),
    amount,
    fee,
  })

  it('compares exactly: a fee of 100000000.000000001 exceeds an amount of 100000000.000000000, which share a double', () => {
    expect(validate(form('100000000.000000001', '100000000.000000000'))?.field).toBe('fee')
  })

  it('accepts an equal fee and an empty fee', () => {
    expect(validate(form('100000000.000000000', '100000000.000000000'))).toBeNull()
    expect(validate(form('', '1'))).toBeNull()
  })
})
