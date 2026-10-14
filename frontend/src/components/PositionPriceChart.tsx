import type { usePositionHistory } from '../api/positions'
import type { Position } from '../api/summary'
import type { Transaction, TransactionType } from '../api/transactions'
import { t } from '../i18n/strings'
import type { ChartMarker } from '../lib/chart'
import { formatUnitPrice } from '../lib/format'
import { isHeld } from '../lib/positions'
import { LineChart } from './LazyChart'
import { ChartFrame } from './ui/ChartFrame'

const MARKED = new Set<TransactionType>(['buy', 'sell', 'dividend'])

// One position's price against its average cost, in the instrument's own
// currency, with the user's trades (from `transactions`) and the
// instrument's splits marked. `history` is the caller's usePositionHistory
// over its range: a second observer mounting later would fetch the same
// range again. onZoom turns on drag to zoom.
export function PositionPriceChart({
  history,
  position,
  transactions,
  onZoom,
}: {
  history: ReturnType<typeof usePositionHistory>
  position: Position
  transactions: Transaction[]
  onZoom?: (from: string, to: string) => void
}) {
  // The chart spans the held days (isHeld), and also keeps the closing day
  // after the last held one, so the sell that closed the position keeps its
  // marker, and draws the price through a closed gap. A closing day's
  // avg_cost is 0 (nothing held); a sale leaves the average cost unchanged,
  // so the line carries the last held day's value into it, and is a gap on
  // the later days nothing is held.
  const points = history.data?.points ?? []
  const first = points.findIndex(isHeld)
  const last = points.findLastIndex(isHeld)
  const priceDays = first < 0 ? [] : points.slice(first, last + 2)
  const pricePoints = priceDays
    .map((p, i) => ({
      date: p.date,
      total_value: p.close,
      cost_basis: isHeld(p)
        ? p.avg_cost
        : i > 0 && isHeld(priceDays[i - 1])
          ? priceDays[i - 1].avg_cost
          : null,
      cash: '0',
    }))
    .filter((p): p is typeof p & { total_value: string } => p.total_value !== null)

  // ponytail: markers only cover the transactions the caller has loaded
  // (100 rows per page); older markers appear after "load more".
  const markers: ChartMarker[] = [
    ...transactions
      .filter((tx) => MARKED.has(tx.type))
      .map((tx) => ({ date: tx.date, kind: tx.type as ChartMarker['kind'] })),
    ...(position.splits ?? []).map((s) => ({ date: s.date, kind: 'split' as const })),
  ]

  return (
    <ChartFrame query={history}>
      <LineChart
        points={pricePoints}
        markers={markers}
        labels={{ value: t('position.price'), cost: t('position.avgCost') }}
        formatValue={(v) => formatUnitPrice(v, history.data?.currency ?? position.currency, 6)}
        onZoom={onZoom}
      />
    </ChartFrame>
  )
}
