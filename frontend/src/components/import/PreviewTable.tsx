import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'
import { translateError } from '../../api'
import type { Instrument } from '../../api/instruments'
import type { PreviewRow } from '../../api/imports'
import { t, type StringKey } from '../../i18n/strings'
import { formatMarketDate, formatShortMarketDate } from '../../lib/date'
import { formatMoney } from '../../lib/format'
import { useIsWide } from '../../lib/useIsDesktop'
import { feeText, quantityTimesPrice } from '../../lib/transactionCells'
import { TransactionHeadCells, TypePill } from '../TransactionCells'
import { feeLabelKey, typeLabelKeyFor } from '../transactionForm'
import { ActionsTh } from '../ui/ActionsTh'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Checkbox } from '../ui/Checkbox'
import { Icon } from '../ui/Icon'
import { Pill } from '../ui/Pill'
import { GROUP_ROW, TABLE, TD, TD_WRAP, TH } from '../ui/table'
import { InstrumentPicker } from './InstrumentPicker'
import type { Row } from './shared'

// The reasons the Midas parser (ADR 0006) and the Garanti Yatırım parser
// emit. An unknown reason is shown as the parser wrote it rather
// than swallowed.
const reasonKey: Record<string, StringKey> = {
  'fx conversion': 'import.reason.fxConversion',
  'not executed': 'import.reason.notExecuted',
  'stock transfer': 'import.reason.stockTransfer',
  'opening balance': 'import.reason.openingBalance',
  'IPO refund': 'import.reason.ipoRefund',
  'IPO payment, booked by the allotment': 'import.reason.ipoPayment',
  'fractional share cash-out, booked from the ledger': 'import.reason.fractionalCashOut',
  'unsupported asset type': 'import.reason.unsupportedAssetType',
}

// The row error codes the statement parsers emit. An unknown code shows
// the server's English message, like an unknown reason above.
const errorKey: Record<string, StringKey> = {
  cell_count: 'import.error.cellCount',
  net_mismatch: 'import.error.netMismatch',
  bad_date: 'import.error.unreadableDate',
  bad_number: 'import.error.badNumber',
  ambiguous_number: 'import.error.ambiguousNumber',
  unknown_type: 'import.error.unknownType',
  unknown_market: 'import.error.unknownMarket',
  missing_field: 'import.error.missingField',
  fee_on_fee: 'import.error.feeOnFee',
  // Both Garanti fund and Garanti Yatırım statements emit these, ledger and
  // trade-list rows alike, so the text names neither. A bad_row shows no value.
  unknown_row: 'import.error.unknownRow',
  bad_row: 'import.error.badRow',
  // A transfer in without a cost; the edit dialog takes one.
  unknown_cost: 'import.error.unknownCost',
  // An opening buy no price was found for; the edit dialog takes one.
  opening_price: 'import.error.openingPrice',
  // A closing sell no price was found for.
  closing_price: 'import.error.closingPrice',
}

// A row error with one of these codes names its field by key; the field is
// shown by its name in the UI locale. Any other value, such as an unknown
// type cell, is shown as the parser wrote it, even when it reads like a
// field name.
const columnCodes = new Set(['bad_number', 'ambiguous_number', 'missing_field'])

const fieldKey: Record<string, StringKey> = {
  symbol: 'import.field.symbol',
  quantity: 'import.field.quantity',
  price: 'import.field.price',
  amount: 'import.field.amount',
  fee: 'import.field.fee',
  avg_cost: 'import.field.avgCost',
  gross: 'import.field.gross',
  tax: 'import.field.tax',
  net: 'import.field.net',
}

function fieldName(code: string, value: string): string {
  const key = columnCodes.has(code) ? fieldKey[value] : undefined
  return key ? t(key) : value
}

function statusText(row: PreviewRow): string {
  switch (row.status) {
    case 'unknown_instrument':
      return t('import.status.unknownInstrument', { symbol: row.symbol ?? '' })
    case 'unsupported_currency':
      return t('import.status.unsupportedCurrency', { currency: row.currency ?? '' })
    case 'skipped': {
      const reason = row.reason ?? ''
      const key = reasonKey[reason]
      return key ? t(key) : reason
    }
    case 'error': {
      // A row the commit would refuse carries the commit's own error,
      // after its unknown symbol when it has one.
      if (row.code === 'invalid') {
        const error = translateError(row.message, feeLabelKey(row.type))
        return row.unknown_instrument
          ? t('import.status.unknownAndInvalid', { symbol: row.symbol ?? '', error })
          : error
      }
      const key = errorKey[row.code ?? '']
      return key
        ? t(key, { value: fieldName(row.code ?? '', row.value ?? '') })
        : (row.message ?? '')
    }
    default: {
      if (row.approximate_price) return t('import.approximatePrice')
      if (row.cost_price) return t('import.costPrice')
      if (!row.duplicate_of) return ''
      const date = formatMarketDate(row.duplicate_of)
      return row.type === 'dividend'
        ? t('import.status.duplicateDividend', { date })
        : t('import.status.duplicate', { date })
    }
  }
}

function money(value: string | undefined, currency: string | undefined): string {
  if (!value || !currency) return ''
  return formatMoney(value, currency)
}

// Date, symbol, type, quantity × price and amount -- whichever the row has --
// so eight row controls read apart to a screen reader,
// even two same-day deposits in TRY and USD.
function rowWhat(row: Row): string {
  return [
    row.date && formatMarketDate(row.date),
    row.symbol,
    row.type && t(typeLabelKeyFor(row.type, row.instrument_id ?? row.symbol)),
    quantityTimesPrice(row),
    money(row.amount, row.currency),
  ]
    .filter(Boolean)
    .join(' ')
}

function RowCheckbox({ row, onToggle }: { row: Row; onToggle: () => void }) {
  // Skipped and error rows have nothing to commit, so they get no control at
  // all; unknown instruments get a disabled one, so the column stays aligned
  // and the row reads as "blocked" rather than "missing".
  if (row.status === 'skipped' || row.status === 'error') return null
  return (
    <Checkbox
      aria-label={t('import.selectRow', { what: rowWhat(row) })}
      checked={row.checked}
      disabled={row.status !== 'ok'}
      onChange={onToggle}
    />
  )
}

// Checked when every committable row is, mixed when some are.
function SelectAll({ rows, onChange }: { rows: Row[]; onChange: (checked: boolean) => void }) {
  const ok = rows.filter((row) => row.status === 'ok')
  if (ok.length === 0) return null
  const checked = ok.every((row) => row.checked)
  const some = ok.some((row) => row.checked)
  return (
    <Checkbox
      aria-label={t('import.selectAll')}
      checked={checked}
      indeterminate={some && !checked}
      onChange={() => onChange(!checked)}
    />
  )
}

// The cells the parser could not use, as one line. A skipped or error row
// carries them so the statement can be checked against what was read.
// Readable text, so `muted` rather than `faint`, which misses WCAG AA
// (ADR 0005).
function rawLine(row: PreviewRow): string | null {
  return row.raw && row.raw.length > 0 ? row.raw.join(' | ') : null
}

// Breaks at the spaces between cells first, inside a cell only when one
// alone is wider than the line: `break-all` split "300" into "3" / "00" on a
// phone.
const rawClass = 'text-[11px] wrap-anywhere text-muted'

function StatusText({ row }: { row: PreviewRow }) {
  const text = statusText(row)
  if (!text) return null
  const tone =
    row.status === 'error'
      ? 'text-loss'
      : row.duplicate_of || row.approximate_price
        ? 'text-warn'
        : 'text-muted'
  return <span className={tone}>{text}</span>
}

// The status with the raw line under it, for the edit dialog. The table puts
// the raw line on its own full-width row instead -- the Durum column
// is far too narrow for a statement line -- and the table and the phone list
// open it on demand.
function StatusCell({ row }: { row: PreviewRow }) {
  const raw = rawLine(row)
  return (
    <>
      <StatusText row={row} />
      {raw && <span className={`mt-0.5 block ${rawClass}`}>{raw}</span>}
    </>
  )
}

// The statement line being fixed, in the edit dialog that would otherwise
// cover it. Nothing for a row the parser read cleanly.
export function DialogContext({ row }: { row: PreviewRow }) {
  if (!statusText(row) && !rawLine(row)) return null
  return (
    <div className="rounded-lg bg-paper px-3 py-2 text-[12px]">
      <StatusCell row={row} />
    </div>
  )
}

function SymbolLabel({ row }: { row: PreviewRow }) {
  if (!row.symbol) return null
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <span className="text-[13px] font-semibold text-ink">{row.symbol}</span>
      {row.currency && row.currency !== 'TRY' && <Badge>{row.currency}</Badge>}
    </span>
  )
}

// The symbol cell: the symbol, also while the instrument is unknown, whose
// picker is in the block above the table. A symbol resolved by the
// user's earlier pick, or by a fund name an earlier statement printed,
// says so and names the instrument it resolved to. A
// wrong one can be replaced through Değiştir, which shows the
// picker; an empty picker on a matched row read as unmatched.
// The caller owns Değiştir's open state; `children` is the picker shown in
// the cell, left out by a caller that renders it on its own line, as the
// phone list does.
function InstrumentCell({
  row,
  changing,
  onChanging,
  children,
}: {
  row: Row
  changing: boolean
  onChanging: (changing: boolean) => void
  children?: ReactNode
}) {
  if (row.resolved_by) {
    return (
      <span className="block">
        <SymbolLabel row={row} />
        <span className="mb-1 flex flex-wrap items-baseline gap-1.5 text-[12px] text-muted">
          <span>{t(row.resolved_by === 'name' ? 'import.learnedName' : 'import.earlierPick')}</span>
          {row.instrument_symbol && (
            <>
              <span aria-hidden="true">→</span>
              <span className="font-semibold text-ink">{row.instrument_symbol}</span>
              {row.instrument_name && <span>{row.instrument_name}</span>}
            </>
          )}
          <button
            type="button"
            aria-expanded={changing}
            aria-label={t('import.changeMatch', { symbol: row.symbol ?? '' })}
            onClick={() => onChanging(!changing)}
            className="font-medium text-ink-2 underline underline-offset-2 hover:text-ink"
          >
            {t(changing ? 'confirm.cancel' : 'import.change')}
          </button>
        </span>
        {changing && children}
      </span>
    )
  }
  return <SymbolLabel row={row} />
}

// Değiştir's picker: a pick closes it.
function ChangePicker({
  row,
  onChanging,
  onPick,
}: {
  row: Row
  onChanging: (changing: boolean) => void
  onPick: (instrument: Instrument) => void
}) {
  return (
    <InstrumentPicker
      row={row}
      autoFocus
      onPick={(instrument) => {
        onChanging(false)
        onPick(instrument)
      }}
    />
  )
}

function EditButton({ row, onEdit }: { row: Row; onEdit: () => void }) {
  // A skipped row may carry none of the parts; it keeps the plain name.
  const what = rowWhat(row)
  return (
    <Button
      variant="ghost"
      iconOnly
      aria-label={what ? t('import.editRow', { what }) : t('import.editRowPlain')}
      data-edit-row={row.key}
      onClick={onEdit}
    >
      <Icon name="pencil" />
    </Button>
  )
}

function Markers({ row }: { row: Row }) {
  return (
    <>
      {row.opening && <Pill tone="warn">{t('import.opening')}</Pill>}
      {row.closing && <Pill tone="warn">{t('import.closing')}</Pill>}
      {row.manual && <Pill tone="cash">{t('import.manual')}</Pill>}
    </>
  )
}

function TypeCell({ row }: { row: Row }) {
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <TypePill tx={row} />
      <Markers row={row} />
    </span>
  )
}

// The raw line of an unreadable row opens under it on demand, so
// every row stays short and the statement text is one click away.
function RawToggle({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-expanded={open}
      onClick={onToggle}
      className="block text-[12px] font-medium text-accent hover:underline"
    >
      {t(open ? 'import.hideRaw' : 'import.showRaw')}
    </button>
  )
}

// A row the commit refused: tinted, described by the error in the
// review bar, and scrolled to, as the table shows no row numbers to count by.
// Once per error: a row remounted by the filter or the breakpoint stays put.
const scrolled = new WeakSet<Error>()
function useFailed<T extends HTMLElement>(failed: Error | null) {
  const ref = useRef<T>(null)
  useEffect(() => {
    if (!failed || scrolled.has(failed)) return
    scrolled.add(failed)
    ref.current?.scrollIntoView({ block: 'center' })
  }, [failed])
  return {
    ref,
    'aria-describedby': failed ? 'import-commit-error' : undefined,
    className: failed ? 'bg-loss-soft' : '',
  }
}

// The compact table of ui/table.ts in nine columns that fit the card at
// 1280 px: quantity and price share a cell, and the one fee field
// (a commission, or a dividend's withholding) is "Kesinti", as on İşlemler.
// Below that (`wide` false) the card is 720 to 1000 px and the fixed-width
// Durum column pushed the table past it, so the status goes under the
// instrument, in the column that gives, and the table has eight.
export function PreviewTableRow({
  row,
  wide = true,
  failed = null,
  onToggle,
  onEdit,
  onPick,
}: {
  row: Row
  wide?: boolean
  failed?: Error | null
  onToggle: () => void
  onEdit: () => void
  onPick: (instrument: Instrument) => void
}) {
  const { className: mark, ...failedProps } = useFailed<HTMLTableRowElement>(failed)
  const [open, setOpen] = useState(false)
  const [changing, setChanging] = useState(false)
  const raw = rawLine(row)
  const picker = <ChangePicker row={row} onChanging={setChanging} onPick={onPick} />
  // Below xl Değiştir's picker gets its own line under the row, as in the
  // phone list: in the instrument cell its 12rem min width spilled
  // over "Adet × fiyat".
  const marked = row.opening || row.closing || row.manual
  const status = (
    <>
      {marked && (
        <span className="mr-1.5 inline-flex gap-1.5 align-middle">
          <Markers row={row} />
        </span>
      )}
      <StatusText row={row} />
      {raw && <RawToggle open={open} onToggle={() => setOpen(!open)} />}
    </>
  )
  const tone = row.status === 'skipped' ? 'text-muted' : ''
  const cell = `${TD} py-2 align-top`
  return (
    <>
      <tr {...failedProps} className={`${tone} ${mark}`}>
        <td className={cell}>
          <RowCheckbox row={row} onToggle={onToggle} />
        </td>
        <td className={cell}>{row.date ? formatMarketDate(row.date) : ''}</td>
        {/* The type alone: beside it the opening and manual markers stacked
            in this narrow column, or widened the table past its card; they
            go to the fixed-width Durum column. */}
        <td className={cell}>
          <TypePill tx={row} />
        </td>
        {/* The column that gives: it takes the room the others leave and cuts
            a description there, so the table never gets wider than its card.
            Its text wraps: a statement's fund name, kept
            as the symbol of a row an earlier pick resolved, ran over
            "Adet × fiyat". */}
        <td className={`${TD_WRAP} w-full max-w-0 py-2 align-top`}>
          <InstrumentCell row={row} changing={changing} onChanging={setChanging}>
            {wide && picker}
          </InstrumentCell>
          {row.description && (
            <span className="mt-0.5 block truncate text-[12px] text-muted" title={row.description}>
              {row.description}
            </span>
          )}
          {!wide && (marked || raw || statusText(row)) && (
            <span className="mt-0.5 block text-[12px]">{status}</span>
          )}
        </td>
        <td className={`${cell} text-right tabular-nums`}>{quantityTimesPrice(row)}</td>
        <td className={`${cell} text-right text-muted tabular-nums`}>{feeText(row)}</td>
        <td className={`${cell} text-right font-medium tabular-nums`}>
          {money(row.amount, row.currency)}
        </td>
        {wide && (
          <td className={cell}>
            <span className="block w-44 text-[12px] whitespace-normal">{status}</span>
          </td>
        )}
        <td className={`${TD} py-1 align-top`}>
          <EditButton row={row} onEdit={onEdit} />
        </td>
      </tr>
      {!wide && changing && row.resolved_by && (
        <tr className={`${tone} ${mark}`}>
          <td colSpan={3} className="border-b border-paper" />
          <td colSpan={5} className="border-b border-paper pr-4 pb-2 pl-2">
            {picker}
          </td>
        </tr>
      )}
      {raw && open && (
        <tr className={tone}>
          <td colSpan={wide ? 9 : 8} className={`border-b border-paper px-4 pb-2 ${rawClass}`}>
            {raw}
          </td>
        </tr>
      )}
    </>
  )
}

// Ten columns have no honest phone layout, so below md the table becomes a
// list -- the same trade the transactions page makes (ADR 0005).
export function PreviewListItem({
  row,
  failed = null,
  onToggle,
  onEdit,
  onPick,
}: {
  row: Row
  failed?: Error | null
  onToggle: () => void
  onEdit: () => void
  onPick: (instrument: Instrument) => void
}) {
  const details = []
  if (row.date) details.push(formatShortMarketDate(row.date))
  const trade = quantityTimesPrice(row)
  if (trade) details.push(trade)
  if (row.description) details.push(row.description)
  const fee = feeText(row)
  const [open, setOpen] = useState(false)
  const [changing, setChanging] = useState(false)
  const raw = rawLine(row)
  // Değiştir's picker gets its own line under the row: the middle column
  // beside the amount was ~142 px at 375 px and cut the placeholder.
  const { className: mark, ...failedProps } = useFailed<HTMLLIElement>(failed)

  return (
    <li
      {...failedProps}
      className={`flex flex-wrap items-start gap-3 border-b border-line px-4 py-2.5 last:border-0 ${
        row.status === 'skipped' ? 'text-muted' : ''
      } ${mark}`}
    >
      <span className="pt-1">
        <RowCheckbox row={row} onToggle={onToggle} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-1.5">
          <TypeCell row={row} />
          <InstrumentCell row={row} changing={changing} onChanging={setChanging} />
        </span>
        {/* Wraps rather than truncates: the cut fell inside the price. */}
        {details.length > 0 && (
          <span className="mt-0.5 block text-[12px] wrap-anywhere text-muted">
            {details.join(' · ')}
          </span>
        )}
        <span className="mt-0.5 block text-[12px]">
          <StatusText row={row} />
          {raw && <RawToggle open={open} onToggle={() => setOpen(!open)} />}
          {raw && open && <span className={`mt-0.5 block ${rawClass}`}>{raw}</span>}
        </span>
      </span>
      <span className="shrink-0 text-right">
        <span className="block font-medium">{money(row.amount, row.currency)}</span>
        {fee && (
          <span className="block text-[12px] text-muted">
            {`${t(feeLabelKey(row.type, true))} ${fee}`}
          </span>
        )}
      </span>
      <span className="shrink-0">
        <EditButton row={row} onEdit={onEdit} />
      </span>
      {changing && row.resolved_by && (
        <div data-testid="picker-line" className="basis-full">
          <ChangePicker row={row} onChanging={setChanging} onPick={onPick} />
        </div>
      )}
    </li>
  )
}

// The unknown names of a statement, each once with one picker and the number
// of rows it covers, above the table: one picker per row made a
// wall of identical pickers, and only the first pick showed that it fixes
// every row of its name. A name is a symbol in one currency, as resolveRows
// fixes them; `onPick` gets the first row of the name.
export function UnknownNames({
  rows,
  onPick,
}: {
  rows: Row[]
  onPick: (key: string, instrument: Instrument) => void
}) {
  const names = new Map<string, Row[]>()
  for (const row of rows) {
    if (row.status !== 'unknown_instrument' && !row.unknown_instrument) continue
    const name = `${row.symbol ?? ''}\u0000${row.currency ?? ''}`
    const list = names.get(name)
    if (list) list.push(row)
    else names.set(name, [row])
  }
  if (names.size === 0) return null
  return (
    <section aria-labelledby="import-unknown-names" className="mt-3 card px-4 py-3">
      <h3 id="import-unknown-names" className="text-[13px] font-semibold text-ink">
        {t('import.unknownNames', { count: names.size })}
      </h3>
      <ul className="mt-1">
        {Array.from(names, ([name, [first, ...rest]]) => (
          <li
            key={name}
            className="flex flex-wrap items-start gap-x-3 gap-y-1 border-b border-line py-2 last:border-0"
          >
            <span className="min-w-0 flex-1 basis-48 pt-1.5">
              <SymbolLabel row={first} />
              <span className="block text-[12px] text-muted">
                {t('import.unknownNameRows', { count: rest.length + 1 })}
              </span>
            </span>
            <span className="max-md:basis-full md:w-72">
              <InstrumentPicker
                row={first}
                onPick={(instrument) => onPick(first.key, instrument)}
              />
              {/* A fund row may name a fund TEFAS has renamed since by its old
                  name, which only a later statement in the batch links to the
                  code. */}
              {first.market === 'TEFAS' && (
                <span className="mt-1 block text-[12px] text-muted">
                  {t('import.unknownNameRenamedFund')}
                </span>
              )}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

// The rows of a statement's review, a table from md up and a list below,
// the opening and closing rows each under a line that says what
// they are. `failedKey` is the row a commit error names.
export function PreviewRows({
  shown,
  desktop,
  failedKey,
  failed,
  onSelectAll,
  onToggle,
  onEdit,
  onPick,
}: {
  shown: Row[]
  desktop: boolean
  failedKey: string | undefined
  failed: Error | null
  onSelectAll: (checked: boolean) => void
  onToggle: (key: string) => void
  onEdit: (row: Row) => void
  onPick: (key: string, instrument: Instrument) => void
}) {
  // The review table's Durum column only from xl up.
  const wide = useIsWide()
  // The opening rows come first by their date, the day before the period;
  // one line above the first of them says what they are.
  const firstOpening = shown.find((row) => row.opening)?.key
  // The closing sells follow them, under a line of their own.
  const firstClosing = shown.find((row) => row.closing)?.key
  // The line above the opening rows names their price only when every
  // proposed buy has the same source: the statement's average
  // cost, or the price of the day before the period. Otherwise it names none
  // and each row priced from the statement says so itself. A buy left
  // without a price, or edited, has no source to name.
  const openingBuys = shown.filter(
    (row) => row.opening && row.type === 'buy' && row.status === 'ok' && !row.manual,
  )
  const openingHint =
    openingBuys.length === 0
      ? t('import.openingHintMixed')
      : openingBuys.every((row) => row.cost_price)
        ? t('import.openingHintCost')
        : openingBuys.every((row) => !row.cost_price && !row.approximate_price)
          ? t('import.openingHint')
          : t('import.openingHintMixed')
  // What a row needs in both layouts, the table row and the list item.
  const rowProps = (row: Row) => ({
    failed: row.key === failedKey ? failed : null,
    onToggle: () => onToggle(row.key),
    onEdit: () => onEdit(row),
    onPick: (instrument: Instrument) => onPick(row.key, instrument),
  })

  return (
    // relative: the select and actions headers' sr-only labels are
    // absolute, so without a positioned card they escape the scroll
    // clip and widen the page.
    <div className="relative mt-3 overflow-x-auto card">
      {desktop ? (
        <table className={TABLE}>
          <thead>
            <tr>
              <th className={TH}>
                <SelectAll rows={shown} onChange={onSelectAll} />
              </th>
              <TransactionHeadCells />
              {wide && <th className={TH}>{t('import.statusColumn')}</th>}
              <ActionsTh />
            </tr>
          </thead>
          <tbody>
            {shown.map((row) => (
              <Fragment key={row.key}>
                {(row.key === firstOpening || row.key === firstClosing) && (
                  <tr className={GROUP_ROW}>
                    <td colSpan={wide ? 9 : 8} className="whitespace-normal">
                      {row.key === firstOpening ? openingHint : t('import.closingHint')}
                    </td>
                  </tr>
                )}
                <PreviewTableRow row={row} wide={wide} {...rowProps(row)} />
              </Fragment>
            ))}
          </tbody>
        </table>
      ) : (
        <ul>
          {shown.some((row) => row.status === 'ok') && (
            <li className="border-b border-line px-4 py-2 text-[12px] text-muted">
              <label className="flex items-center gap-3">
                <SelectAll rows={shown} onChange={onSelectAll} />
                {t('import.selectAll')}
              </label>
            </li>
          )}
          {shown.map((row) => (
            <Fragment key={row.key}>
              {(row.key === firstOpening || row.key === firstClosing) && (
                <li className="border-b border-line px-4 py-2 text-[12px] text-muted">
                  {row.key === firstOpening ? openingHint : t('import.closingHint')}
                </li>
              )}
              <PreviewListItem row={row} {...rowProps(row)} />
            </Fragment>
          ))}
        </ul>
      )}
    </div>
  )
}
