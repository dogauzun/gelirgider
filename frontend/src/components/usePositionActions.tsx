import { useState } from 'react'
import type { Position } from '../api/summary'
import type { TradeType } from './PositionsTable'
import { PriceDialog } from './PriceDialog'
import { TransactionDialog } from './TransactionDialog'
import { tradeForm } from './transactionForm'

// What a position's buy, sell and price actions open, for the position page
// and Özet's positions table: the transaction dialog prefilled from the
// position (GLG-558) and the price dialog, anchored to the element that
// opened it or centred without one. `dialogs` renders whichever is open.
export function usePositionActions(portfolioId: number) {
  const [trade, setTrade] = useState<{ type: TradeType; position: Position } | null>(null)
  const [pricing, setPricing] = useState<{ position: Position; anchor: Element | null } | null>(
    null,
  )
  const dialogs = (
    <>
      {trade && (
        <TransactionDialog
          portfolioId={portfolioId}
          editing={null}
          initial={tradeForm(trade.type, trade.position)}
          onClose={() => setTrade(null)}
        />
      )}
      {pricing && (
        <PriceDialog
          position={pricing.position}
          portfolioId={portfolioId}
          anchor={pricing.anchor}
          onClose={() => setPricing(null)}
        />
      )}
    </>
  )
  return {
    openTrade: (type: TradeType, position: Position) => setTrade({ type, position }),
    openPrice: (position: Position, anchor: Element | null) => setPricing({ position, anchor }),
    dialogs,
  }
}
