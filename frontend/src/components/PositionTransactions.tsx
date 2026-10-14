import type { useTransactions } from '../api/transactions'
import { t } from '../i18n/strings'
import { TransactionRow } from './TransactionRow'
import { Button } from './ui/Button'
import { LoadError } from './ui/LoadError'
import { Skeleton } from './ui/Skeleton'
import { TABLE, TH } from './ui/table'

// One position's transactions with their loading, error and empty states,
// shared by the position page and the position dialog. `limit` caps the rows
// (the dialog's last few), `loadMore` adds the next-page button, and `inset`
// pads the states inside a card.
export function PositionTransactions({
  transactions,
  limit,
  loadMore = false,
  inset = false,
}: {
  transactions: ReturnType<typeof useTransactions>
  limit?: number
  loadMore?: boolean
  inset?: boolean
}) {
  const rows = transactions.data ?? []
  return (
    <>
      {transactions.isLoading && <Skeleton className={inset ? 'm-4 h-14' : 'h-14'} />}
      {transactions.isError && (
        <LoadError
          title={t('transactions.error')}
          error={transactions.error}
          onRetry={() => transactions.refetch()}
          retrying={transactions.isFetching}
          className={inset ? 'm-4' : undefined}
        />
      )}
      {transactions.data && rows.length === 0 && (
        <p className={inset ? 'p-4 text-sm text-muted' : 'text-sm text-muted'}>
          {t('transactions.empty')}
        </p>
      )}
      {rows.length > 0 && (
        <table className={TABLE}>
          <thead>
            <tr>
              <th className={TH}>{t('transactions.date')}</th>
              <th className={TH}>{t('transactions.type')}</th>
              {/* Below sm the date, type and amount fill the card at 375px;
                  İşlemler has the rest. */}
              <th className={`${TH} text-right max-sm:hidden`}>
                {t('transactions.quantityPrice')}
              </th>
              <th className={`${TH} text-right max-sm:hidden`}>{t('transactions.deduction')}</th>
              <th className={`${TH} text-right`}>{t('transactions.amount')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, limit).map((tx) => (
              <TransactionRow key={tx.id} tx={tx} />
            ))}
          </tbody>
        </table>
      )}
      {loadMore && transactions.hasNextPage && (
        <div className="flex justify-center border-t border-line p-3">
          <Button
            variant="secondary"
            onClick={() => transactions.fetchNextPage()}
            pending={transactions.isFetchingNextPage}
          >
            {transactions.isFetchingNextPage
              ? t('transactions.loadingMore')
              : t('transactions.loadMore')}
          </Button>
        </div>
      )}
    </>
  )
}
