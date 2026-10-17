import { useId, type KeyboardEvent } from 'react'
import { Link } from 'react-router'
import { useDividendTimeline } from '../api/dividends'
import { usePositionHistory } from '../api/positions'
import type { PortfolioScope, Position, Summary } from '../api/summary'
import { useTransactions } from '../api/transactions'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { resolveRange, type RangeValue } from '../lib/ranges'
import { usePositionPath } from '../lib/useRange'
import { PositionActions } from './PositionActions'
import { PositionKpis } from './PositionKpis'
import { PositionPriceChart } from './PositionPriceChart'
import type { TradeType } from './PositionsTable'
import { PositionTitle } from './PositionTitle'
import { PositionTransactions } from './PositionTransactions'
import { Button } from './ui/Button'
import { Icon } from './ui/Icon'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'

const RECENT = 5

// The chart and the last transactions of one position of one portfolio.
// Keyed by the instrument, so moving to the next position starts empty
// instead of showing the previous one's rows while the new ones load.
function PortfolioDetail({
  portfolioId,
  position,
  range,
}: {
  portfolioId: number
  position: Position
  range: RangeValue
}) {
  const { from, to } = resolveRange(range, todayIstanbul())
  const history = usePositionHistory(portfolioId, position.instrument_id, from, to)
  const transactions = useTransactions(portfolioId, { instrumentId: position.instrument_id })
  const rows = transactions.data ?? []
  return (
    <>
      {/* The chart's own arrow keys move its tooltip, not the position. */}
      <div className="mt-4" onKeyDown={(event) => event.stopPropagation()}>
        <PositionPriceChart history={history} position={position} transactions={rows} />
      </div>
      <h3 className="mt-5 mb-1 text-[14px] font-semibold text-ink">
        {t('position.dialog.recent')}
      </h3>
      <PositionTransactions transactions={transactions} limit={RECENT} />
    </>
  )
}

// One position of the positions card in a dialog, so the owner
// stays on Özet or Tüm portföyler and flips through `positions`, the table's
// rows in its order, by the arrows or ←/→. Its title row, figures, price
// chart over the summary's range, and last transactions, as on the position
// page. The caller owns Al, Sat and Fiyat gir, whose dialogs open on top of
// this one. The read-only `all` scope has no position page, no chart, no
// transactions and no dividends route (ADR 0014): there it is the title and
// the summary row's figures alone. A native modal <dialog>, full screen on a
// phone; mounted only while open.
export function PositionDialog({
  scope,
  summary,
  positions,
  position,
  range,
  onChange,
  onTrade,
  onEditPrice,
  onClose,
}: {
  scope: PortfolioScope
  summary: Summary
  positions: Position[]
  position: Position
  range: RangeValue
  onChange: (position: Position) => void
  onTrade?: (type: TradeType, position: Position) => void
  onEditPrice?: (position: Position, anchor: HTMLElement) => void
  onClose: () => void
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const positionPath = usePositionPath()
  const dividends = useDividendTimeline(scope)
  const index = positions.findIndex((p) => p.instrument_id === position.instrument_id)
  const prev = index > 0 ? positions[index - 1] : null
  const next = index >= 0 && index < positions.length - 1 ? positions[index + 1] : null

  function handleKeyDown(event: KeyboardEvent<HTMLDialogElement>) {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    const target = event.key === 'ArrowLeft' ? prev : next
    if (!target) return
    event.preventDefault()
    onChange(target)
  }

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onKeyDown={handleKeyDown} onClose={onClose}>
      {/* Full screen on phones, not a bottom sheet, so not sheetPanel. */}
      {/* eslint-disable-next-line no-restricted-syntax -- a full-screen panel on phones, not a sheetPanel */}
      <div className="absolute inset-0 overflow-y-auto bg-surface p-4 shadow-pop md:inset-auto md:top-1/2 md:left-1/2 md:max-h-[90vh] md:w-[60rem] md:max-w-[calc(100%-2rem)] md:-translate-x-1/2 md:-translate-y-1/2 md:rounded-card md:p-5">
        <div className="flex items-start justify-between gap-3">
          <PositionTitle position={position}>
            <h2 id={titleId} className="text-[18px] font-semibold text-ink">
              {scope === 'all' ? (
                position.symbol
              ) : (
                <Link to={positionPath(scope, position.instrument_id)} className="hover:underline">
                  {position.symbol}
                </Link>
              )}
            </h2>
          </PositionTitle>
          <div className="flex shrink-0 gap-1">
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('position.dialog.previous')}
              disabled={!prev}
              onClick={() => prev && onChange(prev)}
            >
              <Icon name="chevronLeft" />
            </Button>
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('position.dialog.next')}
              disabled={!next}
              onClick={() => next && onChange(next)}
            >
              <Icon name="chevronRight" />
            </Button>
            <Button
              variant="ghost"
              iconOnly
              aria-label={t('position.dialog.close')}
              onClick={onClose}
            >
              <Icon name="x" />
            </Button>
          </div>
        </div>

        {/* The position page's actions. */}
        {scope !== 'all' && (
          <PositionActions
            className="mt-3 flex flex-wrap gap-2"
            position={position}
            onTrade={onTrade}
            onEditPrice={onEditPrice}
          />
        )}

        <PositionKpis
          className="mt-4"
          position={position}
          summary={summary}
          dividends={scope === 'all' ? null : dividends.data}
        />

        {scope !== 'all' && (
          <PortfolioDetail
            key={position.instrument_id}
            portfolioId={scope}
            position={position}
            range={range}
          />
        )}
      </div>
    </Modal>
  )
}
