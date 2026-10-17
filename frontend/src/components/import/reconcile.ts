import type {
  CashReconciliation,
  ImportRow,
  Reconciliation,
  StatementPreview,
} from '../../api/imports'
import { decimalUnits, unitsToDecimal } from '../../lib/decimal'
import { isCashIn } from '../../lib/transactionCells'

// A cent in 1e-9 units: the cash line matches within it, as on the server.
const CENT = 10_000_000n

type Booked = Partial<ImportRow>

// What rows dated up to the period end add to each instrument's quantity and
// to the cash in `currency`, in 1e-9 units, by the holdings engine's rules
// (backend/internal/holdings Apply). A trade without an amount, as the page
// sends an edited one, books quantity × price, as the server derives it.
function effects(rows: Booked[], periodEnd: string, currency: string | undefined) {
  const quantities = new Map<number, bigint>()
  let cash = 0n
  for (const row of rows) {
    if (!row.type || !row.date || row.date > periodEnd) continue
    const quantity = decimalUnits(row.quantity ?? '0')
    if (row.instrument_id !== undefined && (row.type === 'buy' || row.type === 'sell')) {
      // eslint-disable-next-line no-restricted-syntax -- share counts, not money
      const held = quantities.get(row.instrument_id) ?? 0n
      quantities.set(row.instrument_id, held + (row.type === 'buy' ? quantity : -quantity))
    }
    if (row.currency !== currency) continue
    const amount =
      row.amount !== undefined
        ? decimalUnits(row.amount)
        : (quantity * decimalUnits(row.price ?? '0')) / 1_000_000_000n
    const fee = decimalUnits(row.fee ?? '0')
    if (isCashIn(row.type)) {
      cash += amount - fee
    } else {
      cash -= amount + fee
    }
  }
  return { quantities, cash }
}

// The parse response compares the statement with the rows the preview starts
// checked (backend reconcile), so without a closing sell. The page moves each
// line by what the rows the user would commit now change against those: a check, an edit, a priced
// opening row or an added row. Only the lines the server sent can
// move, as only it knows the portfolio's other positions; a line the
// statement is silent about that the edits close goes, as a closed position
// is not listed. A line nothing moved keeps the server's verdict.
export function liveReconciliation(
  preview: StatementPreview,
  committed: Booked[],
): { reconciliation: Reconciliation[]; cash: CashReconciliation | null } {
  const currency = preview.cash_reconciliation?.currency
  const defaults = preview.rows.filter(
    (row) => row.status === 'ok' && !row.duplicate_of && !row.closing,
  )
  const before = effects(defaults, preview.period_end, currency)
  const after = effects(committed, preview.period_end, currency)

  const reconciliation = preview.reconciliation.flatMap((entry): Reconciliation[] => {
    const id = entry.instrument_id
    const delta =
      // eslint-disable-next-line no-restricted-syntax -- share counts, not money
      id === undefined ? 0n : (after.quantities.get(id) ?? 0n) - (before.quantities.get(id) ?? 0n)
    if (delta === 0n) return [entry]
    const held = decimalUnits(entry.portfolio_quantity) + delta
    if (entry.statement_quantity === null && held === 0n) return []
    return [
      {
        ...entry,
        portfolio_quantity: unitsToDecimal(held),
        match: entry.statement_quantity !== null && decimalUnits(entry.statement_quantity) === held,
      },
    ]
  })

  let cash = preview.cash_reconciliation
  const moved = after.cash - before.cash
  if (cash && moved !== 0n) {
    const held = decimalUnits(cash.portfolio_amount) + moved
    const gap = decimalUnits(cash.statement_amount) - held
    cash = {
      ...cash,
      portfolio_amount: unitsToDecimal(held),
      match: gap <= CENT && gap >= -CENT,
    }
  }
  return { reconciliation, cash }
}
