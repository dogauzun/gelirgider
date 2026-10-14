import type { TransactionType } from '../api/transactions'
import { t } from '../i18n/strings'
import type { Cells } from '../lib/transactionCells'
import { typeLabelKeyFor } from './transactionForm'
import { Pill } from './ui/Pill'
import { TH } from './ui/table'

const typeTone: Record<TransactionType, 'buy' | 'sell' | 'dividend' | 'cash'> = {
  buy: 'buy',
  sell: 'sell',
  dividend: 'dividend',
  deposit: 'cash',
  withdrawal: 'cash',
  fee: 'cash',
}

// A preview row whose instrument is still unknown passes its symbol: that
// dividend waits for its instrument and is not interest.
export function TypePill({ tx }: { tx: Cells }) {
  if (!tx.type) return null
  return (
    <Pill tone={typeTone[tx.type]}>
      {t(typeLabelKeyFor(tx.type, tx.instrument_id ?? tx.symbol))}
    </Pill>
  )
}

// The six column headers a transaction row shares between İşlemler and the
// statement review.
export function TransactionHeadCells() {
  return (
    <>
      <th className={TH}>{t('transactions.date')}</th>
      <th className={TH}>{t('transactions.type')}</th>
      <th className={TH}>{t('transactions.instrument')}</th>
      <th className={`${TH} text-right`}>{t('transactions.quantityPrice')}</th>
      <th className={`${TH} text-right`}>{t('transactions.deduction')}</th>
      <th className={`${TH} text-right`}>{t('transactions.amount')}</th>
    </>
  )
}
