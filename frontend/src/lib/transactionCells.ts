// The sign, tone, quantity × price and fee rules of a transaction's cells,
// shared by the transactions page, the position page and the import preview,
// which must agree.
import type { Transaction, TransactionType } from '../api/transactions'
import { formatQuantity } from './decimal'
import { formatMoney, formatUnitPrice } from './format'

// Cash-flow sign (ADR 0005): the API amount is unsigned and the sign is
// derived from the type at render time -- sell, dividend and deposit bring
// cash in, buy, withdrawal and fee take it out.
// The one cash-in rule; the form's cash preview and the import reconcile use
// it too.
const CASH_IN = new Set<TransactionType>(['sell', 'dividend', 'deposit'])

export function isCashIn(type: TransactionType): boolean {
  return CASH_IN.has(type)
}

// Intl writes a plain hyphen for a negative; the sign here is the typographic
// minus, the same one the dialog's cash strip uses.
export function signedAmount(tx: Pick<Transaction, 'type' | 'amount' | 'currency'>): string {
  return `${isCashIn(tx.type) ? '+' : '−'}${formatMoney(tx.amount, tx.currency)}`
}

export function amountTone(tx: Transaction): string {
  return isCashIn(tx.type) ? 'text-gain' : 'text-ink'
}

// The fields the cells below read: a saved transaction, or an import preview
// row, whose fields are all optional. `symbol` is the preview row's statement
// symbol, which a transaction does not carry.
export type Cells = {
  type?: TransactionType
  date?: string
  instrument_id?: number | null
  symbol?: string
  quantity?: string | null
  price?: string | null
  fee?: string | null
  amount?: string
  currency?: string
}

// "500 × ₺125,52": quantity and price share a cell, empty for a row without
// them, such as a deposit. A quantity without a price, such as a preview's
// opening position no price was found for, shows alone.
export function quantityTimesPrice(tx: Cells): string {
  if (!tx.quantity) return ''
  const quantity = formatQuantity(tx.quantity)
  return tx.price && tx.currency
    ? `${quantity} × ${formatUnitPrice(tx.price, tx.currency)}`
    : quantity
}

// The one fee field, a commission or a dividend's withholding, is empty, not
// a dash, when zero.
export function feeText(tx: Cells): string {
  return tx.fee && tx.currency && Number(tx.fee) !== 0 ? formatMoney(tx.fee, tx.currency) : ''
}
