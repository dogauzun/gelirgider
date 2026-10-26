import { useState } from 'react'
import { useRemoveWatchItem, useWatchlist, type WatchItem } from '../api/watchlist'
import { PageHeader } from '../components/PageHeader'
import { WatchAddDialog } from '../components/WatchAddDialog'
import { WatchTargetDialog } from '../components/WatchTargetDialog'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { EmptyCard } from '../components/ui/EmptyCard'
import { ActionsTh } from '../components/ui/ActionsTh'
import { Icon } from '../components/ui/Icon'
import { LoadError } from '../components/ui/LoadError'
import { Pill } from '../components/ui/Pill'
import { SkeletonRows } from '../components/ui/Skeleton'
import { ROW_44, TABLE, TD, TD_NUM, TH } from '../components/ui/table'
import { useConfirm } from '../components/ui/useConfirm'
import { marketLabel, t, type StringKey } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatUnitPrice, MISSING, plTone, signedMoney, signedPercent } from '../lib/format'
import { returnRatio } from '../lib/positions'
import { useConfirmedDelete } from '../lib/useConfirmedDelete'
import { useTitle } from '../lib/useTitle'

const COLUMNS: { labelKey: StringKey; className: string }[] = [
  { labelKey: 'watchlist.instrument', className: '' },
  { labelKey: 'watchlist.lastPrice', className: 'text-right' },
  { labelKey: 'watchlist.day', className: 'text-right' },
  { labelKey: 'watchlist.target', className: 'text-right' },
  { labelKey: 'watchlist.note', className: '' },
]

function WatchRow({
  item,
  onEdit,
  onRemove,
}: {
  item: WatchItem
  onEdit: () => void
  onRemove: () => void
}) {
  const { currency, symbol } = item
  const day = returnRatio(item.day_change, item.prev_close)
  return (
    <tr className={ROW_44}>
      <td className={TD}>
        <div className="flex items-center gap-2">
          <span className="font-semibold">{symbol}</span>
          <Badge>{marketLabel(item.market)}</Badge>
        </div>
        <div className="max-w-64 truncate text-[12px] text-muted" title={item.name}>
          {item.name}
        </div>
      </td>
      <td className={TD_NUM}>
        {item.last_price !== null && item.price_date !== null ? (
          <>
            <div>{formatUnitPrice(item.last_price, currency)}</div>
            <div className="text-[12px] text-muted">{formatMarketDate(item.price_date)}</div>
          </>
        ) : (
          MISSING
        )}
      </td>
      <td className={TD_NUM}>
        {day !== null ? (
          <div className={plTone(item.day_change)}>
            <div>{signedMoney(item.day_change, currency)}</div>
            <div className="text-[12px]">{signedPercent(day, { maximumFractionDigits: 2 })}</div>
          </div>
        ) : (
          MISSING
        )}
      </td>
      <td className={TD_NUM}>
        {item.target_price !== null && item.target_side !== null ? (
          <>
            <div>
              {formatUnitPrice(item.target_price, currency)}{' '}
              <span className="text-[12px] text-muted">{t(`watchlist.${item.target_side}`)}</span>
            </div>
            {item.target_reached && <Pill tone="gain">{t('watchlist.reached')}</Pill>}
          </>
        ) : (
          MISSING
        )}
      </td>
      <td className={TD}>
        <div className="max-w-64 truncate text-ink-2" title={item.note}>
          {item.note}
        </div>
      </td>
      <td className={`${TD} w-0 whitespace-nowrap`}>
        <Button
          variant="ghost"
          iconOnly
          aria-label={t('watchlist.editTarget', { symbol })}
          onClick={onEdit}
        >
          <Icon name="pencil" />
        </Button>
        <Button
          variant="ghost"
          iconOnly
          aria-label={t('watchlist.remove', { symbol })}
          data-delete-row={item.id}
          onClick={onRemove}
        >
          <Icon name="trash" />
        </Button>
      </td>
    </tr>
  )
}

// The İzleme listesi page: the instruments the user follows, each with its
// last close, the day's change and an optional target price.
export function WatchlistPage() {
  useTitle(t('watchlist.title'))
  const query = useWatchlist()
  const { data } = query
  const { confirm, dialog } = useConfirm()
  const removeItem = useConfirmedDelete(useRemoveWatchItem(), {
    done: 'watchlist.removed',
    failed: 'watchlist.removeFailed',
  })
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<WatchItem | null>(null)

  const remove = (item: WatchItem) =>
    removeItem(
      (data ?? []).map((row) => row.id),
      item.id,
      () =>
        confirm({
          title: t('watchlist.removeConfirm', { symbol: item.symbol }),
          confirmLabel: t('watchlist.removeButton'),
          danger: true,
        }),
    )

  const addButton = (
    <Button data-list-add onClick={() => setAdding(true)}>
      <Icon name="plus" />
      {t('watchlist.add')}
    </Button>
  )

  let body = null
  if (data?.length === 0) {
    body = <EmptyCard title={t('watchlist.empty')}>{addButton}</EmptyCard>
  } else if (data) {
    body = (
      <section className="relative overflow-x-auto card">
        <table className={TABLE}>
          <thead>
            <tr>
              {COLUMNS.map(({ labelKey, className }) => (
                <th key={labelKey} className={`${TH} ${className}`}>
                  {t(labelKey)}
                </th>
              ))}
              <ActionsTh />
            </tr>
          </thead>
          <tbody>
            {data.map((item) => (
              <WatchRow
                key={item.id}
                item={item}
                onEdit={() => setEditing(item)}
                onRemove={() => void remove(item)}
              />
            ))}
          </tbody>
        </table>
      </section>
    )
  }

  return (
    <main id="main">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        <PageHeader title={t('watchlist.title')} actions={addButton} />
        {/* Below lg the h1 is sr-only, so the wrapper's top padding is the gap. */}
        <div className="lg:mt-6">
          {query.isLoading && <SkeletonRows label={t('watchlist.loading')} />}
          {query.isError && (
            <LoadError
              title={t('watchlist.loadError')}
              error={query.error}
              onRetry={() => query.refetch()}
              retrying={query.isFetching}
            />
          )}
          {body}
        </div>
      </div>
      {adding && <WatchAddDialog onClose={() => setAdding(false)} />}
      {editing && <WatchTargetDialog item={editing} onClose={() => setEditing(null)} />}
      {dialog}
    </main>
  )
}
