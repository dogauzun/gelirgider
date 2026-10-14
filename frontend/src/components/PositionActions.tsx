import type { Position } from '../api/summary'
import { t } from '../i18n/strings'
import type { TradeType } from './PositionsTable'
import { Button } from './ui/Button'

// A position's Al, Sat and Fiyat gir, as in the positions table's row
// actions (GLG-933); a closed position can only be bought again.
export function PositionActions({
  position,
  onTrade,
  onEditPrice,
  className,
}: {
  position: Position
  onTrade?: (type: TradeType, position: Position) => void
  onEditPrice?: (position: Position, anchor: HTMLElement) => void
  className: string
}) {
  return (
    <div className={className}>
      <Button variant="secondary" onClick={() => onTrade?.('buy', position)}>
        {t('summary.buy')}
      </Button>
      {position.closed !== true && (
        <>
          <Button variant="secondary" onClick={() => onTrade?.('sell', position)}>
            {t('summary.sell')}
          </Button>
          <Button
            variant="secondary"
            onClick={(event) => onEditPrice?.(position, event.currentTarget)}
          >
            {t('prices.enter')}
          </Button>
        </>
      )}
    </div>
  )
}
