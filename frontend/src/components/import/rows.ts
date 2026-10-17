import { currencyMismatch } from '../../api'
import type { ImportAlias, ImportRow, PreviewRow } from '../../api/imports'
import type { Transaction, TransactionInput } from '../../api/transactions'
import { todayIstanbul } from '../../lib/date'
import { decimalUnits, unitsToDecimal } from '../../lib/decimal'
import type { Row } from './shared'

// Only `ok` rows are ever checked, and those always carry a type and a date.
// A row without them is dropped rather than sent as a half transaction.
// So is an opening deposit with no opening buy left to pay for, and
// a closing withdrawal with no closing sell checked.
export function toImportRow(row: Row): ImportRow | null {
  if (!row.type || !row.date) return null
  if (row.opening && row.type === 'deposit' && !row.manual && row.amount === '0') return null
  if (row.closing && row.type === 'withdrawal' && !row.manual && row.amount === '0') return null
  const out: ImportRow = {
    type: row.type,
    date: row.date,
    fee: row.fee ?? '0',
    currency: row.currency ?? '',
  }
  // A statement trade keeps its İşlem Tutarı as the amount; an
  // estimated one is left to the server's quantity × price.
  if (!row.estimated) out.amount = row.amount ?? '0'
  if (row.instrument_id !== undefined) out.instrument_id = row.instrument_id
  if (row.quantity !== undefined) out.quantity = row.quantity
  if (row.price !== undefined) out.price = row.price
  return out
}

// An unknown_type row of the HESAP İŞLEMLERİ section is a row of a type the parser does
// not know (Talep Tarihi|İşlem Tarihi|İşlem Tipi|Açıklama|Durum|Tutar). Its
// last cell, "47,93 TRY", still gives the amount and currency.
// Statement text is Turkish format whatever the UI locale, so "1.000 TRY" is
// a thousand: it is read here, not with the UI-locale parseDecimalInput.
function unknownCash(row: Row | null): { amount: string; currency: string } | null {
  if (row?.code !== 'unknown_type' || row.section !== 'HESAP İŞLEMLERİ' || row.raw?.length !== 6)
    return null
  // Not parseDecimalInput: that reads by UI locale, the statement is Turkish
  // in every locale. Same first-group rule though.
  const match = /^([1-9]\d{0,2}(?:\.\d{3})*(?:,\d+)?|\d+(?:,\d+)?) (TRY|USD|EUR)$/.exec(
    row.raw[5].trim(),
  )
  if (!match) return null
  return { amount: match[1].replace(/\./g, '').replace(',', '.'), currency: match[2] }
}

// TransactionDialog prefills from a saved Transaction. A preview row is not
// saved and has no id, but in `onSubmit` mode the dialog only ever reads the
// form fields, so handing it a row-shaped Transaction is enough. `row` is
// null for "add operation", and a parser error row fills in nothing but its
// date, except an unknown cash type, which becomes a deposit of its amount. A
// row the commit would refuse (code invalid) fills in all of it. A row
// without a date starts on the period end, or on today when the preview has
// no period, as for a CSV of error rows alone.
export function asTransaction(
  row: Row | null,
  portfolioId: number,
  date: string,
  currency: string,
): Transaction {
  const cash = unknownCash(row)
  return {
    id: 0,
    portfolio_id: portfolioId,
    instrument_id: row?.instrument_id ?? null,
    instrument_symbol: row?.symbol ?? null,
    instrument_name: null,
    type: cash ? 'deposit' : (row?.type ?? 'buy'),
    date: row?.date || date || todayIstanbul(),
    quantity: row?.quantity ?? null,
    price: row?.price ?? null,
    amount: cash?.amount ?? row?.amount ?? '0',
    fee: row?.fee ?? '0',
    // A row whose currency contradicts its instrument opens in the
    // instrument's, as the dialog shows and books it: the one the
    // user picked, else the one the server's message names. Only a server
    // row has no instrument_currency, and its message is the server's
    // verdict on its own instrument, since mergeRows replaces it on each
    // preview; the preview rows carry no instrument currency of their own.
    currency:
      cash?.currency ??
      row?.instrument_currency ??
      currencyMismatch(row?.message) ??
      row?.currency ??
      currency,
    note: null,
    created_at: '',
    import_id: null,
  }
}

// The dialog hands back a TransactionInput with the row's currency, the
// instrument's for a trade or dividend, and that instrument's symbol
// for the Enstrüman cell, but no amount once quantity or price
// changed: the server derives it from quantity × price (see
// backend/internal/transaction/handlers.go fromRequest). It is filled in here
// for the preview table only, and not sent.
// ponytail: Number arithmetic for that preview amount, like the dialog's cash
// strip -- the figure that is stored is the server's.
export function rowFromInput(
  key: string,
  input: TransactionInput,
  { symbol, previous, currency }: { symbol: string | undefined; previous?: Row; currency: string },
): Row {
  const gross =
    input.quantity !== undefined && input.price !== undefined
      ? String(Number(input.quantity) * Number(input.price))
      : '0'
  return {
    key,
    status: 'ok',
    checked: true,
    manual: true,
    type: input.type,
    date: input.date,
    symbol,
    instrument_id: input.instrument_id,
    quantity: input.quantity,
    price: input.price,
    amount: input.amount ?? gross,
    // An unchanged trade gets its amount back from the dialog: the
    // statement's İşlem Tutarı, or on a row edited before, this estimate again.
    estimated:
      input.amount === undefined || (input.quantity !== undefined && previous?.estimated === true),
    fee: input.fee ?? '0',
    currency: input.currency ?? previous?.currency ?? currency,
    description: previous?.description,
    // An edited opening row stays in the opening group; its price is now the
    // user's, so the approximate-price note goes.
    opening: previous?.opening,
    closing: previous?.closing,
  }
}

// A likely duplicate of a hand-entered transaction starts unchecked, so a
// plain commit does not book it twice. So does a proposed
// closing sell: the gap may be months not imported yet. Its
// withdrawal starts checked and follows the checked sells (withCashRows).
export function fromPreview(row: PreviewRow): Row {
  const sell = row.closing && row.type === 'sell'
  return { ...row, checked: row.status === 'ok' && !row.duplicate_of && !sell }
}

// The rows of a preview requested again with the user's picks,
// with the user's work on the rows before it kept. Rows are matched by key,
// which a statement row takes from its place in the parser's own list
// (trade-3, cash-0), so opening rows coming or going in front of the list do
// not shift it. A row picked or edited in the dialog stays as the user left
// it, an ok row keeps its check mark, and rows added by hand stay at the end.
// Opening rows are keyed by instrument and currency (open-5, open-cash-USD),
// so the new preview decides which there are, and one the user priced or
// unchecked keeps that; the opening deposits then follow them. A
// likely duplicate that is one no more, its import undone, starts checked
// like a fresh row.
export function mergeRows(fresh: PreviewRow[], current: Row[]): Row[] {
  const before = new Map(current.map((row) => [row.key, row]))
  const rows = fresh.map((row): Row => {
    const old = before.get(row.key)
    if (old?.manual || old?.alias) return old
    const next = fromPreview(row)
    return old?.status === 'ok' && !(old.duplicate_of && !row.duplicate_of)
      ? { ...next, checked: next.status === 'ok' && old.checked }
      : next
  })
  // A deposit the page added for a buy priced in the dialog is in
  // no preview; unchecked or edited by the user, it stays so.
  const fresher = new Set(fresh.map((row) => row.key))
  const kept = current.filter(
    (row) =>
      ((row.opening && row.type === 'deposit') || (row.closing && row.type === 'withdrawal')) &&
      (row.manual || !row.checked) &&
      !fresher.has(row.key),
  )
  return inStep([...kept, ...rows, ...current.filter((row) => row.key.startsWith('manual-'))])
}

// The proposed cash rows of one side follow that side's checked, priced
// trades in each currency: quantity × price summed exactly, in 1e-18 units,
// as the preview proposes them (backend proposeOpenings). The opening deposit
// pays for the opening buys, rounded up to the cent; the closing
// withdrawal takes out what the closing sells bring in less their fee,
// rounded down. A trade priced or re-priced in the dialog, checked
// or unchecked, changes that sum, so the cash row follows: added in front of
// the opening rows, or after the closing rows, when the preview had none.
// With no such trade left it stays, at 0 and with its check mark, so a row
// the user unchecked comes back unchecked; toImportRow leaves a 0 one out of
// the commit. A row edited by hand is the user's and is left alone.
export function withCashRows(rows: Row[], side: 'opening' | 'closing'): Row[] {
  const opening = side === 'opening'
  const sums = new Map<string, { units: bigint; date: string }>()
  for (const row of rows) {
    if (!row[side] || row.type !== (opening ? 'buy' : 'sell') || !row.checked) continue
    if (!row.quantity || !row.price || !row.currency) continue
    const sum = sums.get(row.currency)
    const fee = opening ? 0n : decimalUnits(row.fee ?? '0') * 1_000_000_000n
    sums.set(row.currency, {
      units: (sum?.units ?? 0n) + decimalUnits(row.quantity) * decimalUnits(row.price) - fee,
      date: sum?.date ?? row.date ?? '',
    })
  }
  const cent = 10n ** 16n
  const cents = (units: bigint) => {
    let whole = 0n
    if (opening) whole = (units + cent - 1n) / cent
    else if (units > 0n) whole = units / cent
    return unitsToDecimal(whole * 10n ** 7n)
  }
  const type = opening ? 'deposit' : 'withdrawal'
  const cash = (row: Row) => row[side] && row.type === type
  const kept = new Set(rows.filter((row) => cash(row) && row.manual).map((row) => row.currency))
  const out = rows.flatMap((row) => {
    if (!cash(row) || row.manual || !row.currency) return [row]
    kept.add(row.currency)
    const sum = sums.get(row.currency)
    return [{ ...row, amount: sum ? cents(sum.units) : '0' }]
  })
  const added = Array.from(sums)
    .filter(([currency, sum]) => !kept.has(currency) && sum.units >= (opening ? 1n : cent))
    .map(([currency, sum]): Row => ({
      key: `${opening ? 'open' : 'close'}-cash-${currency}`,
      status: 'ok',
      checked: true,
      type,
      date: sum.date,
      amount: cents(sum.units),
      fee: '0',
      currency,
      ...(opening ? { opening: true } : { closing: true }),
    }))
  const at = opening
    ? Math.max(
        0,
        out.findIndex((row) => row.opening),
      )
    : out.findLastIndex((row) => row.closing) + 1
  return [...out.slice(0, at), ...added, ...out.slice(at)]
}

// Every proposed cash row in step with the rows it pays for or pays out.
export function inStep(rows: Row[]): Row[] {
  return withCashRows(withCashRows(rows, 'opening'), 'closing')
}

// One pick per preview symbol; a symbol picked for twice (one per currency)
// keeps the later row's.
export function picksOf(rows: Row[]): ImportAlias[] {
  const picks = new Map<string, number>()
  for (const row of rows) {
    if (row.alias && row.instrument_id !== undefined) picks.set(row.alias, row.instrument_id)
  }
  return Array.from(picks, ([symbol, instrument_id]) => ({ symbol, instrument_id }))
}
