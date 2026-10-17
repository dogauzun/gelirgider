import type { ReactNode } from 'react'
import type { Position } from '../api/summary'
import { marketLabel, t } from '../i18n/strings'
import { formatShortMarketDate } from '../lib/date'
import { formatUnitPrice, signedPercent, signTone } from '../lib/format'
import { dayRatio } from '../lib/positions'
import { Pill } from './ui/Pill'

// A position's title row, shared by the position page and the position
// dialog: the caller's heading (`children`), then the name, market, closed
// pill, last price and day change.
export function PositionTitle({ position, children }: { position: Position; children: ReactNode }) {
  const day = dayRatio(position)
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      {children}
      <span className="max-w-full min-w-0 truncate text-[13px] text-muted" title={position.name}>
        {position.name}
      </span>
      <span className="text-[13px] text-muted">{marketLabel(position.market)}</span>
      {position.closed === true && <Pill tone="cash">{t('position.closed')}</Pill>}
      {position.has_price && position.last_price !== null && (
        <span className="ml-1 text-[15px] font-semibold text-ink tabular-nums">
          {formatUnitPrice(position.last_price, position.currency)}
        </span>
      )}
      {day !== null && position.prev_price_date && position.price_date && (
        <span
          title={t('summary.dayChangeTitle', {
            from: formatShortMarketDate(position.prev_price_date),
            to: formatShortMarketDate(position.price_date),
          })}
        >
          <Pill tone={signTone(day)}>{t('position.today', { change: signedPercent(day) })}</Pill>
        </span>
      )}
    </div>
  )
}
