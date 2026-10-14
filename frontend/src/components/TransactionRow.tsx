import type { Transaction } from '../api/transactions'
import { formatMarketDate } from '../lib/date'
import { amountTone, feeText, quantityTimesPrice, signedAmount } from '../lib/transactionCells'
import { TypePill } from './TransactionCells'
import { ROW_44, TD, TD_NUM } from './ui/table'

// One read-only row of a position's transactions in the compact table, as
// on İşlemler; editing stays there. The date carries its year: there are no
// month group rows here.
export function TransactionRow({ tx }: { tx: Transaction }) {
  return (
    <tr className={ROW_44}>
      <td className={TD}>{formatMarketDate(tx.date)}</td>
      <td className={TD}>
        <TypePill tx={tx} />
      </td>
      <td className={`${TD_NUM} max-sm:hidden`}>{quantityTimesPrice(tx)}</td>
      <td className={`${TD_NUM} text-muted max-sm:hidden`}>{feeText(tx)}</td>
      <td className={`${TD_NUM} font-medium ${amountTone(tx)}`}>{signedAmount(tx)}</td>
    </tr>
  )
}
