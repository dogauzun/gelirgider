import type { HistoryEntry } from '../api/history'
import { t } from '../i18n/strings'

// What a history card's body holds with fewer than two points: a scope with no
// transactions (today, all zero) or the day of its first one, not an undefined
// return. Null with two or more points.
export function HistoryEmpty({ points }: { points: HistoryEntry[] }) {
  if (points.length >= 2) return null
  const noTransactions = points.every((p) =>
    [p.total_value, p.cash, p.cost_basis].every((v) => Number(v) === 0),
  )
  return (
    <p className="text-[13px] text-muted">
      {t(noTransactions ? 'transactions.empty' : 'chart.empty')}
    </p>
  )
}
