import { expect, it } from 'vitest'
import { fromTransaction, rowCurrency, toInput } from '../transactionForm'
import { noReview, reviewReducer } from './review'
import { asTransaction, toImportRow } from './rows'
import { resolveRows, type Row } from './shared'

// A pick takes the instrument's currency only for a row whose
// currency is the TRY fallback. A row whose file said TRY stays an
// error with the commit's message, as the server's parse with the pick does.
it('switches only a fallback currency to the picked instrument', () => {
  const row = {
    status: 'unknown_instrument',
    checked: false,
    type: 'buy',
    date: '2026-01-05',
    symbol: 'BTC-USD',
    quantity: '0.5',
    price: '100000',
    amount: '50000',
    fee: '0',
    currency: 'TRY',
  } as const
  const rows: Row[] = [
    { ...row, key: 'trade-0' },
    { ...row, key: 'trade-1', currency_fallback: true },
  ]
  const btc = { id: 7, symbol: 'BTC-USD', market: 'CRYPTO', name: 'Bitcoin', currency: 'USD' }

  const [given, fallback] = resolveRows(rows, 'trade-0', btc)

  expect(given).toMatchObject({
    status: 'error',
    code: 'invalid',
    message: 'currency: must match instrument currency (USD)',
    currency: 'TRY',
    instrument_id: 7,
    checked: false,
  })
  expect(fallback).toMatchObject({ status: 'ok', currency: 'USD', checked: true, manual: true })
})

// The edit dialog of such a row shows and books the instrument's
// currency, so Kaydet cannot turn it into an ok row the commit refuses.
it('edits a contradicting row in the instrument currency', () => {
  const row: Row = {
    key: 'trade-0',
    status: 'unknown_instrument',
    checked: false,
    type: 'buy',
    date: '2026-09-06',
    symbol: 'ZZZQ',
    quantity: '5',
    price: '10',
    amount: '50',
    fee: '0',
    currency: 'TRY',
  }
  const btc = { id: 7, symbol: 'BTC-USD', market: 'CRYPTO', name: 'Bitcoin', currency: 'USD' }
  const [picked] = resolveRows([row], 'trade-0', btc)

  const form = fromTransaction(asTransaction(picked, 1, '', 'TRY'))
  expect(form.instrumentCurrency).toBe('USD')
  const [saved] = reviewReducer(
    { ...noReview, rows: [picked] },
    {
      type: 'applyDialog',
      key: 'trade-0',
      row: picked,
      currency: 'TRY',
      input: { ...toInput(form), currency: rowCurrency(form) },
      symbol: 'BTC-USD',
    },
  ).rows
  expect(toImportRow(saved)).toMatchObject({ instrument_id: 7, currency: 'USD' })
})

// A server row resolved by an earlier pick to a USD instrument, its TRY cell
// a mismatch, re-picked to a TRY instrument through Değiştir: the old
// "(USD)" message goes, the row is ok, and its dialog opens in TRY.
it('re-picks a contradicting row against the new instrument', () => {
  const row: Row = {
    key: 'trade-0',
    status: 'error',
    code: 'invalid',
    message: 'currency: must match instrument currency (USD)',
    checked: false,
    resolved_by: 'alias',
    instrument_id: 7,
    instrument_symbol: 'BTC-USD',
    type: 'buy',
    date: '2026-09-06',
    symbol: 'ZZZQ',
    quantity: '5',
    price: '10',
    amount: '50',
    fee: '0',
    currency: 'TRY',
  }
  const thyao = { id: 3, symbol: 'THYAO', market: 'BIST', name: 'THY', currency: 'TRY' }
  const [repicked] = resolveRows([row], 'trade-0', thyao)

  expect(repicked).toMatchObject({ status: 'ok', checked: true, instrument_id: 3 })
  expect(repicked.message).toBeUndefined()
  expect(fromTransaction(asTransaction(repicked, 1, '', 'TRY')).instrumentCurrency).toBe('TRY')
  // And back to a USD one: a mismatch again, naming the new currency.
  const btc = { id: 7, symbol: 'BTC-USD', market: 'CRYPTO', name: 'Bitcoin', currency: 'USD' }
  expect(resolveRows([repicked], 'trade-0', btc)[0]).toMatchObject({
    status: 'error',
    message: 'currency: must match instrument currency (USD)',
  })
})
