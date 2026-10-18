import type { RefObject } from 'react'
import { importLabel, type Import } from '../../api/imports'
import type { Transaction } from '../../api/transactions'
import { t } from '../../i18n/strings'
import { formatMarketDate } from '../../lib/date'
import { positionLabel } from '../../lib/positions'
import {
  amountTone,
  feeText,
  quantityTimesPrice,
  signedAmount,
  type Cells,
} from '../../lib/transactionCells'
import { useIsDesktop } from '../../lib/useIsDesktop'
import { SymbolLabel, TransactionHeadCells, TypePill } from '../TransactionCells'
import { feeLabelKey, rowLabel } from '../transactionForm'
import { ActionsTh } from '../ui/ActionsTh'
import { Button } from '../ui/Button'
import { Icon } from '../ui/Icon'
import { Pill } from '../ui/Pill'
import { GROUP_ROW, ROW_44, TABLE, TD, TD_NUM, TH } from '../ui/table'

// A term deposit's rows change only on the Mevduat tab: the server refuses
// the generic routes for them (ADR 0024), so the list offers no action.
function isDepositRow(tx: Transaction): boolean {
  return tx.instrument_market === 'DEPOSIT'
}

// A row as the shared cells read it: the symbol is the instrument's, `#id`
// while it is not loaded, and a cash row has none. A term deposit reads by
// its label, never its generated symbol (ADR 0024).
function cellsOf(tx: Transaction): Cells {
  if (tx.instrument_id === null) return tx
  if (tx.instrument_market === 'DEPOSIT') {
    const position = {
      market: tx.instrument_market,
      symbol: tx.instrument_symbol ?? '',
      name: tx.instrument_name ?? '',
      currency: tx.currency,
    }
    return { ...tx, symbol: positionLabel(position) }
  }
  return { ...tx, symbol: tx.instrument_symbol ?? `#${tx.instrument_id}` }
}

interface MonthGroup {
  key: string
  label: string
  rows: Transaction[]
}

// The rows already arrive newest first, so one pass that starts a group
// whenever the month changes keeps that order.
function groupByMonth(rows: Transaction[]): MonthGroup[] {
  const groups: MonthGroup[] = []
  for (const tx of rows) {
    const key = tx.date.slice(0, 7)
    let group = groups[groups.length - 1]
    if (!group || group.key !== key) {
      group = {
        key,
        label: formatMarketDate(tx.date, { month: 'long', year: 'numeric' }),
        rows: [],
      }
      groups.push(group)
    }
    group.rows.push(tx)
  }
  return groups
}

// The phone list's month header; the table uses GROUP_ROW.
const monthHeaderClass =
  'border-b border-line bg-paper px-4 py-1.5 text-left text-[11px] font-medium text-muted uppercase'

// Where a row came from: the import's pill, named by broker and period, or
// "Elle" for a row entered by hand (core pages spec, "Kaynak"). An import the
// list has not loaded yet reads as a plain "içe aktarıldı". An imported row
// is edited and deleted like any other; the pill only says where it came
// from.
function Source({ tx, imports }: { tx: Transaction; imports: Map<number, Import> }) {
  if (!tx.import_id) return <span className="text-muted">{t('transactions.source.manual')}</span>
  const item = imports.get(tx.import_id)
  return <Pill tone="cash">{item ? importLabel(item) : t('transactions.imported')}</Pill>
}

function EditableTransactionRow({
  tx,
  imports,
  isEditing,
  onEdit,
  onClone,
  onDelete,
}: {
  tx: Transaction
  imports: Map<number, Import>
  isEditing: boolean
  onEdit: () => void
  onClone: () => void
  onDelete: (event: React.MouseEvent) => void
}) {
  const managed = isDepositRow(tx)
  const rowClass = managed
    ? ROW_44
    : `${ROW_44} cursor-pointer ${isEditing ? 'bg-paper' : 'hover:bg-paper'}`
  // A click anywhere on the row edits it, as a tap does on the phone list.
  // Clicks on the row's own buttons keep their behaviour; the
  // keyboard reaches edit through the pencil button, so the row takes no
  // focus itself.
  return (
    <tr
      className={rowClass}
      onClick={(event) => {
        if (managed || isEditing || (event.target as HTMLElement).closest('button, a')) return
        onEdit()
      }}
    >
      {/* Day and month only: the group row above gives the year. */}
      <td className={TD}>{formatMarketDate(tx.date, { day: '2-digit', month: 'short' })}</td>
      <td className={TD}>
        <TypePill tx={tx} />
      </td>
      {/* The column that gives: max-w-0 with w-full makes it take the room
          the others leave and cut its text there, so the table never gets
          wider than its card. */}
      <td className={`${TD} w-full max-w-0`}>
        <span className="flex min-w-0 items-center gap-1.5">
          {tx.instrument_id !== null && (
            <span className="shrink-0">
              <SymbolLabel tx={cellsOf(tx)} />
            </span>
          )}
          <span className="min-w-0 truncate">
            {tx.instrument_id !== null && !isDepositRow(tx) && tx.instrument_name && (
              <span className="text-muted" title={tx.instrument_name}>
                {tx.instrument_name}
              </span>
            )}
            {tx.note && (
              <span className="ml-1.5 text-muted first:ml-0" title={tx.note}>
                {tx.note}
              </span>
            )}
          </span>
        </span>
      </td>
      <td className={TD_NUM}>{quantityTimesPrice(tx)}</td>
      <td className={`${TD_NUM} text-muted`}>{feeText(tx)}</td>
      <td className={`${TD_NUM} font-medium ${amountTone(tx)}`}>{signedAmount(tx)}</td>
      <td className={TD}>
        <Source tx={tx} imports={imports} />
      </td>
      {/* Always shown: on hover only, they were out of reach on a touch
          laptop and invisible to anyone scanning the row. */}
      <td className={TD}>
        {!managed && (
          <div className="flex justify-end">
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('transactions.edit', { what: rowLabel(cellsOf(tx)) })}
              onClick={onEdit}
              disabled={isEditing}
            >
              <Icon name="pencil" />
            </Button>
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('transactions.clone', { what: rowLabel(cellsOf(tx)) })}
              onClick={onClone}
              disabled={isEditing}
            >
              <Icon name="copy" />
            </Button>
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('transactions.delete', { what: rowLabel(cellsOf(tx)) })}
              data-delete-tx={tx.id}
              onClick={onDelete}
              disabled={isEditing}
            >
              <Icon name="trash" />
            </Button>
          </div>
        )}
      </td>
    </tr>
  )
}

// The phone row: eight columns have no honest layout under md, so the table is
// replaced by a list rather than a horizontally scrolling table (ADR 0005).
// Tapping the row edits it; clone and delete keep their own buttons, since
// there is no hover to reveal them on a phone.
function TransactionListItem({
  tx,
  imports,
  onEdit,
  onClone,
  onDelete,
}: {
  tx: Transaction
  imports: Map<number, Import>
  onEdit: () => void
  onClone: () => void
  onDelete: (event: React.MouseEvent) => void
}) {
  const fee = feeText(tx)
  const managed = isDepositRow(tx)
  // A named month, not dd/MM: the numeric pattern reads 12/09 in tr, unlike
  // the rest of the app, and the group header above already gives the year.
  const details = [formatMarketDate(tx.date, { day: '2-digit', month: 'short' })]
  const trade = quantityTimesPrice(tx)
  if (trade) details.push(trade)
  if (tx.note) details.push(tx.note)

  // The row button spans the whole li, so the details line runs under the
  // clone and delete buttons too. They sit over the button's first line,
  // which keeps their 104px free (2 × 44px and the gaps) and is at least
  // their height, so their tap targets never cover the details line. Beside
  // the button, the line got the li minus those 104px and cut
  // "0,004 × ₺2.100.000,00" at 360px.
  return (
    <li className="relative border-b border-line px-4 last:border-0">
      <button
        type="button"
        onClick={onEdit}
        disabled={managed}
        className="block w-full py-2 text-left"
      >
        {/* The row button says nothing about what tapping it does, so a hidden
            prefix names it. It prefixes rather than aria-labels the
            button, which would hide the row's own amount and quantity. */}
        {!managed && <span className="sr-only">{`${t('transactions.editTitle')}:`}</span>}
        <span className="flex min-h-11 flex-wrap items-start gap-x-3 gap-y-0.5 pr-26">
          {/* The pills wrap inside their own column, and the widest one is the
              column's floor (no min-w-0), so it never shrinks under the amount.
              When one pill and the amount do not fit side
              by side, the amount wraps under the pills, right-aligned, instead
              of overlapping them. */}
          <span className="flex-1">
            <span className="flex flex-wrap items-center gap-1.5">
              <TypePill tx={tx} />
              {tx.instrument_id !== null && <SymbolLabel tx={cellsOf(tx)} />}
              {tx.import_id ? <Source tx={tx} imports={imports} /> : null}
            </span>
          </span>
          <span className="ml-auto shrink-0 text-right">
            <span className={`block font-medium ${amountTone(tx)}`}>{signedAmount(tx)}</span>
            {fee && (
              <span className="block text-[12px] text-muted">
                {`${t(feeLabelKey(tx.type, true))} ${fee}`}
              </span>
            )}
          </span>
        </span>
        <span className="mt-0.5 block truncate text-[12px] text-muted">{details.join(' · ')}</span>
      </button>
      {!managed && (
        <div className="absolute top-2 right-4 flex gap-2">
          <Button
            variant="ghost"
            iconOnly
            // 44px touch target on a phone; this list renders only below md.
            className="max-md:h-11 max-md:w-11"
            aria-label={t('transactions.clone', { what: rowLabel(cellsOf(tx)) })}
            onClick={onClone}
          >
            <Icon name="copy" />
          </Button>
          <Button
            variant="ghost"
            iconOnly
            className="max-md:h-11 max-md:w-11"
            aria-label={t('transactions.delete', { what: rowLabel(cellsOf(tx)) })}
            data-delete-tx={tx.id}
            onClick={onDelete}
          >
            <Icon name="trash" />
          </Button>
        </div>
      )}
    </li>
  )
}

// The loaded rows of İşlemler, grouped by month: a table on the desktop, a
// list below md. Edit, clone and delete are the page's.
export function TransactionList({
  rows,
  imports,
  editingId,
  cardRef,
  onEdit,
  onClone,
  onDelete,
}: {
  rows: Transaction[]
  imports: Map<number, Import>
  editingId: number | undefined
  cardRef: RefObject<HTMLDivElement | null>
  onEdit: (tx: Transaction) => void
  onClone: (tx: Transaction) => void
  onDelete: (tx: Transaction, keyboard: boolean) => void
}) {
  const desktop = useIsDesktop()
  const groups = groupByMonth(rows)
  // relative: the actions header's sr-only label is absolute, so without a
  // positioned card it escapes the scroll clip and widens the page.
  return (
    <div ref={cardRef} className="relative overflow-x-auto card">
      {desktop ? (
        <table className={TABLE}>
          <thead>
            <tr>
              <TransactionHeadCells />
              <th className={TH}>{t('transactions.source')}</th>
              <ActionsTh />
            </tr>
          </thead>
          {groups.map((group) => (
            <tbody key={group.key}>
              <tr className={GROUP_ROW}>
                <th colSpan={8} scope="colgroup" className="px-4 py-1 text-left font-medium">
                  {group.label}
                </th>
              </tr>
              {group.rows.map((tx) => (
                <EditableTransactionRow
                  key={tx.id}
                  tx={tx}
                  imports={imports}
                  isEditing={editingId === tx.id}
                  onEdit={() => onEdit(tx)}
                  onClone={() => onClone(tx)}
                  onDelete={(event) => onDelete(tx, event.detail === 0)}
                />
              ))}
            </tbody>
          ))}
        </table>
      ) : (
        <div>
          {groups.map((group) => (
            <div key={group.key}>
              <p className={monthHeaderClass}>{group.label}</p>
              <ul>
                {group.rows.map((tx) => (
                  <TransactionListItem
                    key={tx.id}
                    tx={tx}
                    imports={imports}
                    onEdit={() => onEdit(tx)}
                    onClone={() => onClone(tx)}
                    onDelete={(event) => onDelete(tx, event.detail === 0)}
                  />
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
