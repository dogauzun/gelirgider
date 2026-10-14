import type { DividendTimeline } from '../api/dividends'
import type { Position, Summary } from '../api/summary'
import { t } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import { formatQuantity } from '../lib/decimal'
import {
  formatMoney,
  formatNumber,
  formatUnitPrice,
  MISSING,
  plTone,
  signedMoney,
} from '../lib/format'
import { unrealized as unrealizedOf, weightOf, weightTotal } from '../lib/positions'
import { FitFigure, KPI_FIGURE, KPI_WIDE, KpiCell, KpiStrip } from './ui/Kpi'

// One position's figures, in the summary's reporting currency. `dividends`
// is the dividends timeline: undefined while it loads, null to leave the
// dividends cell out (the read-only `all` scope has no dividends route,
// ADR 0014).
export function PositionKpis({
  position,
  summary,
  dividends,
  className = '',
}: {
  position: Position
  summary: Summary
  dividends: DividendTimeline | undefined | null
  className?: string
}) {
  const closed = position.closed === true
  const reporting = summary.currency
  const withDividends = dividends !== null

  // The backend folds each dividend's net into the position's realized_pl
  // (holdings.State.Apply), so the total is unrealized + realized, which
  // holds the dividends once, and the realized cell says it includes them.
  // The server sums the two unrounded and rounds once (GLG-1354); an open
  // position without a price has no total, as it has no unrealized.
  // Unrealized and its percent come from the positions table's
  // unrealized(), so the two never disagree.
  const pl = unrealizedOf(position)
  const unrealized = pl.amount
  const realized = position.realized_pl_reporting
  const total = closed || unrealized !== null ? position.total_pl_reporting : null
  const percent = pl.percent
  // The Temettü page's figure: each dividend at its own date's rate, where
  // realized_pl_reporting converts at today's, so the two are never
  // subtracted; for a foreign position they differ by the rate's move.
  const dividendRow = dividends?.by_instrument.find(
    (r) => r.instrument_id === position.instrument_id,
  )
  const dividendNet = dividends ? (dividendRow?.net ?? '0') : null
  const weight = weightOf(position, weightTotal(summary))

  // A closed position keeps what it left behind: its total, its realized
  // P/L and its dividends. On a phone the total and the last cell span both
  // columns; without the dividends cell the average cost pairs with the
  // realized one instead.
  const columns = closed
    ? withDividends
      ? 'grid-cols-1 sm:grid-cols-3'
      : 'grid-cols-1 sm:grid-cols-2'
    : withDividends
      ? 'grid-cols-2 lg:grid-cols-6'
      : 'grid-cols-2 lg:grid-cols-5'
  return (
    <KpiStrip className={className} columns={columns}>
      <KpiCell label={t('position.total')} className={closed ? '' : KPI_WIDE}>
        <FitFigure
          className={`${KPI_FIGURE} ${plTone(total)}`}
          text={total !== null ? signedMoney(total, reporting) : MISSING}
        />
        <p className="text-[12px] text-muted">{t('position.totalHint')}</p>
      </KpiCell>
      {!closed && (
        <>
          <KpiCell label={t('position.marketValue')}>
            <FitFigure
              className={`${KPI_FIGURE} text-ink`}
              text={
                position.market_value_reporting !== null
                  ? formatMoney(position.market_value_reporting, reporting)
                  : MISSING
              }
            />
            <p className="text-[12px] text-muted">
              {t('position.units', {
                quantity: formatQuantity(position.quantity),
                count: formatQuantity(position.quantity),
              })}
              {weight !== null && ` · ${t('summary.weight')} ${formatNumber(weight, SHARE_FORMAT)}`}
            </p>
          </KpiCell>
          <KpiCell label={t('position.unrealized')}>
            <FitFigure
              className={`${KPI_FIGURE} ${plTone(unrealized)}`}
              text={unrealized !== null ? signedMoney(unrealized, reporting) : MISSING}
            />
            {percent && <p className={`text-[12px] ${plTone(unrealized)}`}>{percent}</p>}
            {/* The part of it from the exchange rate, as Özet's table
                shows it (GLG-537); Özet's phone row leaves it to the
                position page (GLG-774). */}
            {position.fx_pl_reporting != null && (
              <p className={`text-[12px] ${plTone(position.fx_pl_reporting)}`}>
                {`${t('summary.fxPl')} ${signedMoney(position.fx_pl_reporting, reporting)}`}
              </p>
            )}
          </KpiCell>
        </>
      )}
      <KpiCell label={t('position.realized')}>
        <FitFigure
          className={`${KPI_FIGURE} ${plTone(realized)}`}
          text={realized !== null ? signedMoney(realized, reporting) : MISSING}
        />
        <p className="text-[12px] text-muted">{t('position.realizedHint')}</p>
      </KpiCell>
      {withDividends && (
        <KpiCell label={t('position.dividends')}>
          <FitFigure
            className={`${KPI_FIGURE} ${plTone(dividendNet)}`}
            text={dividendNet !== null ? signedMoney(dividendNet, reporting) : MISSING}
          />
          {dividendRow && (
            <p className="text-[12px] text-muted">
              {`${t('dividends.page.paymentsCount', { count: dividendRow.count })} · ${t('position.dividendsHint')}`}
            </p>
          )}
        </KpiCell>
      )}
      {!closed && (
        <KpiCell label={t('position.avgCost')} className={withDividends ? KPI_WIDE : ''}>
          <FitFigure
            className={`${KPI_FIGURE} text-ink`}
            text={formatUnitPrice(position.avg_cost, position.currency, 6)}
          />
        </KpiCell>
      )}
    </KpiStrip>
  )
}
