import type { Position } from '../api/summary'
import type { Transaction, TransactionInput, TransactionType } from '../api/transactions'
import { t, type StringKey } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import {
  compareDecimal,
  formatDecimalInput,
  formatQuantity,
  isAmbiguousThousands,
  parseDecimalInput,
  decimalUnits,
  tooManyDigits,
  unitsToDecimal,
} from '../lib/decimal'
import { isCashIn } from '../lib/transactionCells'
import type { TradeType } from './PositionsTable'

// The form's types: the API's, plus 'interest', which the API stores as a
// dividend with no instrument.
export type FormType = TransactionType | 'interest'

export const TYPES: FormType[] = [
  'buy',
  'sell',
  'dividend',
  'interest',
  'deposit',
  'withdrawal',
  'fee',
]
export const TYPES_WITH_INSTRUMENT = new Set<FormType>(['buy', 'sell', 'dividend'])
export const TYPES_WITH_QUANTITY_PRICE = new Set<FormType>(['buy', 'sell'])
export const TYPES_WITH_AMOUNT = new Set<FormType>([
  'dividend',
  'interest',
  'deposit',
  'withdrawal',
  'fee',
])
// Types whose fee comes out of the amount, so the fee must not exceed it --
// mirrors the server check. Buy and sell are absent: there the fee
// adds to the cost instead.
const TYPES_WITH_FEE_FROM_AMOUNT = new Set<FormType>([
  'dividend',
  'interest',
  'deposit',
  'withdrawal',
])
export const CURRENCIES = ['TRY', 'USD', 'EUR'] as const

const typeLabelKey: Record<FormType, StringKey> = {
  buy: 'transactions.type.buy',
  sell: 'transactions.type.sell',
  dividend: 'transactions.type.dividend',
  interest: 'transactions.type.interest',
  deposit: 'transactions.type.deposit',
  withdrawal: 'transactions.type.withdrawal',
  fee: 'transactions.type.fee',
}

// A dividend that names no instrument is interest income, like Midas's
// monthly nema. `instrument` is the row's instrument as an id, or as the
// statement's symbol on an import preview row whose instrument is still
// unknown: that one is a dividend waiting for its instrument, not interest.
function formType(type: TransactionType, instrument: number | string | null | undefined): FormType {
  return type === 'dividend' && (instrument === null || instrument === undefined)
    ? 'interest'
    : type
}

// The label of a saved or previewed row, so an instrument-less dividend reads
// "Faiz/Nema" rather than "Temettü".
export function typeLabelKeyFor(
  type: TransactionType,
  instrument: number | string | null | undefined,
): StringKey {
  return typeLabelKey[formType(type, instrument)]
}

// A dividend's fee is the withholding tax (stopaj), not a commission: the
// amount is gross and the fee is the tax taken from it (ADR 0006).
// Interest is taxed the same way. `short` picks the compact prefix used next
// to a row's amount.
export function feeLabelKey(type: FormType | undefined, short = false): StringKey {
  if (type === 'dividend' || type === 'interest') return 'transactions.withholdingTax'
  return short ? 'transactions.commissionShort' : 'transactions.fee'
}

// The segmented control has seven options on one row, so deposit and
// withdrawal use short labels there. The rest are already short enough.
export const typeShortLabelKey: Record<FormType, StringKey> = {
  ...typeLabelKey,
  deposit: 'transactions.type.short.deposit',
  withdrawal: 'transactions.type.short.withdrawal',
}

export interface FormState {
  type: FormType
  date: string
  instrumentId: number | null
  instrumentLabel: string
  instrumentSymbol: string | null
  instrumentCurrency: string | null
  quantity: string
  price: string
  amount: string
  fee: string
  currency: string
  note: string
}

export function emptyForm(type: FormType = 'buy', date: string = todayIstanbul()): FormState {
  return {
    type,
    date,
    instrumentId: null,
    instrumentLabel: '',
    instrumentSymbol: null,
    instrumentCurrency: null,
    quantity: '',
    price: '',
    amount: '',
    fee: '0',
    currency: 'TRY',
    note: '',
  }
}

// The trade dialog's form for a buy or a sell of a held position, as the
// positions table's row menu opens it.
export function tradeForm(type: TradeType, position: Position): FormState {
  return {
    ...emptyForm(type),
    instrumentId: position.instrument_id,
    instrumentLabel: `${position.symbol} - ${position.name}`,
    instrumentSymbol: position.symbol,
    instrumentCurrency: position.currency,
    quantity: type === 'sell' ? formatDecimalInput(position.quantity) : '',
  }
}

// Buy, sell and dividend always take their currency from the selected
// instrument, set by the server (see
// backend/internal/transaction/handlers.go checkInstrument) - never a select,
// since the server rejects a currency that contradicts the instrument.
// Only interest/deposit/withdrawal/fee let the user choose.
export function needsCurrencySelect(form: FormState): boolean {
  return !TYPES_WITH_INSTRUMENT.has(form.type)
}

function instrumentLabelFor(tx: Transaction): string {
  if (tx.instrument_id === null) return ''
  if (!tx.instrument_symbol) return `#${tx.instrument_id}`
  // The name can be missing on a row that is not a saved transaction (the
  // import preview builds one from a statement row); the symbol on
  // its own is still the right label then.
  return tx.instrument_name
    ? `${tx.instrument_symbol} - ${tx.instrument_name}`
    : tx.instrument_symbol
}

// A dividend with no instrument, saved or a preview row (a Midas nema),
// reopens as interest.
export function fromTransaction(tx: Transaction): FormState {
  return {
    type: formType(tx.type, tx.instrument_id ?? tx.instrument_symbol),
    date: tx.date,
    instrumentId: tx.instrument_id,
    instrumentLabel: instrumentLabelFor(tx),
    instrumentSymbol: tx.instrument_id !== null ? tx.instrument_symbol : null,
    instrumentCurrency: tx.instrument_id !== null ? tx.currency : null,
    quantity: formatDecimalInput(tx.quantity ?? ''),
    price: formatDecimalInput(tx.price ?? ''),
    amount: TYPES_WITH_AMOUNT.has(tx.type) ? formatDecimalInput(tx.amount) : '',
    fee: formatDecimalInput(tx.fee),
    currency: tx.currency,
    note: tx.note ?? '',
  }
}

function isPositive(text: string): boolean {
  const parsed = parseDecimalInput(text)
  return parsed !== null && Number(parsed) > 0
}

// notANumber is true for text that is no number at all ("1e3", "abc"), as
// opposed to empty, zero or negative, which the > 0 checks cover.
// parseDecimalInput rejects a minus, so a leading one is stripped first: "-5"
// is a number, just not a positive one.
function notANumber(text: string): boolean {
  const trimmed = text.trim()
  return trimmed !== '' && parseDecimalInput(trimmed.replace(/^-/, '')) === null
}

// A fee row's amount is the fee itself, so it takes no fee of its own:
// the field is hidden, and a value typed under another type before
// switching to fee is ignored rather than sent.
function feeInput(form: FormState): string {
  return form.type === 'fee' ? '' : form.fee
}

// The input a validation error belongs to; null for a server error, which
// stays under the form.
export type FormField = 'instrument' | 'quantity' | 'price' | 'amount' | 'fee'
export type FormError = { field: FormField | null; message: string }

// validate mirrors the server rules (backend/internal/transaction/handlers.go fromRequest).
export function validate(form: FormState): FormError | null {
  if (TYPES_WITH_INSTRUMENT.has(form.type) && form.instrumentId === null) {
    return { field: 'instrument', message: t('transactions.error.instrumentRequired') }
  }
  // Before the > 0 checks, which would otherwise say "must be greater than 0"
  // for a "10.000" that parseDecimalInput rejects as ambiguous, for
  // a "1e3" that is no number at all and for a positive value with
  // more digits than the server stores.
  const typed: [FormField, StringKey, string][] = []
  if (TYPES_WITH_QUANTITY_PRICE.has(form.type)) {
    typed.push(
      ['quantity', 'transactions.quantity', form.quantity],
      ['price', 'transactions.price', form.price],
    )
  }
  if (TYPES_WITH_AMOUNT.has(form.type)) typed.push(['amount', 'transactions.amount', form.amount])
  typed.push(['fee', feeLabelKey(form.type), feeInput(form)])
  const ambiguous = typed.find(([, , text]) => isAmbiguousThousands(text))
  if (ambiguous) return { field: ambiguous[0], message: t('decimal.error.ambiguousThousands') }
  const invalid = typed.find(([, , text]) => notANumber(text))
  if (invalid) {
    return {
      field: invalid[0],
      message: t('transactions.error.notANumber', { field: t(invalid[1]) }),
    }
  }
  const long = typed.find(([, , text]) => tooManyDigits(parseDecimalInput(text) ?? '', 9))
  if (long) {
    return {
      field: long[0],
      message: t('decimal.error.tooManyDigits', { field: t(long[1]), fraction: '9' }),
    }
  }
  if (TYPES_WITH_QUANTITY_PRICE.has(form.type)) {
    if (!isPositive(form.quantity)) {
      return { field: 'quantity', message: t('transactions.error.quantityRequired') }
    }
    // A buy at price 0 records units received for free: staking, airdrops,
    // gifted shares (ADR 0007). A sell still needs a positive price.
    if (form.type === 'buy') {
      if (parseDecimalInput(form.price) === null) {
        return { field: 'price', message: t('transactions.error.priceNonNegative') }
      }
    } else if (!isPositive(form.price)) {
      return { field: 'price', message: t('transactions.error.priceRequired') }
    }
  }
  if (TYPES_WITH_AMOUNT.has(form.type) && !isPositive(form.amount)) {
    return { field: 'amount', message: t('transactions.error.amountRequired') }
  }
  if (feeInput(form).trim() !== '' && parseDecimalInput(feeInput(form)) === null) {
    return {
      field: 'fee',
      message: t('transactions.error.feeInvalid', { field: t(feeLabelKey(form.type)) }),
    }
  }
  // Amount is already known valid and positive here: every type in
  // TYPES_WITH_FEE_FROM_AMOUNT is also in TYPES_WITH_AMOUNT.
  if (TYPES_WITH_FEE_FROM_AMOUNT.has(form.type)) {
    const fee = form.fee.trim() === '' ? 0 : Number(parseDecimalInput(form.fee))
    if (fee > Number(parseDecimalInput(form.amount))) {
      return {
        field: 'fee',
        message: t('transactions.error.feeExceedsAmount', { field: t(feeLabelKey(form.type)) }),
      }
    }
  }
  return null
}

// `original` is the transaction being edited. A trade whose quantity and price
// are unchanged sends its stored amount back: an imported one holds the
// statement's rounded İşlem Tutarı, which the server would otherwise replace
// with quantity × price.
export function toInput(form: FormState, original?: Transaction | null): TransactionInput {
  const input: TransactionInput = {
    type: form.type === 'interest' ? 'dividend' : form.type,
    date: form.date,
    fee: parseDecimalInput(feeInput(form)) ?? '0',
    note: form.note,
  }
  if (TYPES_WITH_INSTRUMENT.has(form.type) && form.instrumentId !== null) {
    input.instrument_id = form.instrumentId
  }
  if (TYPES_WITH_QUANTITY_PRICE.has(form.type)) {
    input.quantity = parseDecimalInput(form.quantity) ?? form.quantity
    input.price = parseDecimalInput(form.price) ?? form.price
    if (
      original?.quantity != null &&
      original.price != null &&
      compareDecimal(input.quantity, original.quantity) === 0 &&
      compareDecimal(input.price, original.price) === 0
    ) {
      input.amount = original.amount
    }
  }
  if (TYPES_WITH_AMOUNT.has(form.type)) {
    input.amount = parseDecimalInput(form.amount) ?? form.amount
  }
  if (needsCurrencySelect(form)) {
    input.currency = form.currency
  }
  return input
}

// The live cash strip under the form. Display only -- the request still
// carries the exact decimal strings the user typed.
// ponytail: Number arithmetic is enough for a preview; a decimal library only
// if this figure ever becomes something other than a hint.
export function cashEffect(form: FormState): { out: boolean; amount: number } | null {
  const feeText = feeInput(form)
  const fee = feeText.trim() === '' ? 0 : Number(parseDecimalInput(feeText) ?? NaN)
  if (Number.isNaN(fee)) return null

  let gross: number
  if (TYPES_WITH_QUANTITY_PRICE.has(form.type)) {
    const quantity = Number(parseDecimalInput(form.quantity) ?? NaN)
    const price = Number(parseDecimalInput(form.price) ?? NaN)
    if (Number.isNaN(quantity) || Number.isNaN(price)) return null
    gross = quantity * price
  } else {
    gross = Number(parseDecimalInput(form.amount) ?? NaN)
    if (Number.isNaN(gross)) return null
  }
  // The engine nets the fee out of an incoming movement and adds it to an
  // outgoing one, so the preview does the same.
  // Interest is stored as a dividend, so it moves cash the same way.
  const out = !isCashIn(form.type === 'interest' ? 'dividend' : form.type)
  const net = out ? gross + fee : gross - fee
  // A fee above the proceeds turns an incoming movement into an outgoing one:
  // a sell of 1 at 1 with fee 5 takes 4 out of cash.
  return net < 0 ? { out: !out, amount: -net } : { out, amount: net }
}

// sellExceedsHeld returns the quantity held when a sell asks for more than
// that, else null. A warning only: an oversell is accepted on purpose, since
// months may be entered out of order. Held is the summary's current
// quantity, not the quantity on the sell's date. The row being edited is
// already in the summary, so it is taken out again first. A split after that
// row makes this add-back approximate; a hint can live with it.
export function sellExceedsHeld(
  form: FormState,
  positions: Position[] | undefined,
  original: Transaction | null,
): string | null {
  if (form.type !== 'sell' || form.instrumentId === null || !positions) return null
  const quantity = parseDecimalInput(form.quantity)
  if (quantity === null) return null
  const position = positions.find((p) => p.instrument_id === form.instrumentId)
  let held = decimalUnits(position?.quantity ?? '0')
  if (original?.instrument_id === form.instrumentId && original.quantity != null) {
    if (original.type === 'sell') held += decimalUnits(original.quantity)
    if (original.type === 'buy') held -= decimalUnits(original.quantity)
  }
  if (decimalUnits(quantity) <= held) return null
  return formatQuantity(unitsToDecimal(held))
}

// Instrument rows carry the instrument's currency, cash rows the chosen one.
export function rowCurrency(form: FormState): string {
  return TYPES_WITH_INSTRUMENT.has(form.type) ? (form.instrumentCurrency ?? 'TRY') : form.currency
}
