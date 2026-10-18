import type { AttributionPosition } from '../api/attribution'
import { expectedNetOf, type DividendForecastRow, type DividendTimeline } from '../api/dividends'
import type { Position, Summary } from '../api/summary'
import { t, type StringKey } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import { formatQuantity } from '../lib/decimal'
import { formatMoney, formatNumber, formatUnitPrice, plTone, signedMoney } from '../lib/format'
import { holdingsTotal, unrealized as unrealizedOf, weightOf } from '../lib/positions'
import { FitFigure, KPI_FIGURE, KPI_WIDE, KpiCell, KpiStrip } from './ui/Kpi'

// A closed position keeps what it left behind: its total, its realized
// P/L and its dividends. On a phone the total and the last cell span both
// columns; without the dividends cell the average cost pairs with the
// realized one instead.
function kpiColumns(closed: boolean, withDividends: boolean) {
  if (closed) return withDividends ? 'grid-cols-1 sm:grid-cols-3' : 'grid-cols-1 sm:grid-cols-2'
  return withDividends ? 'grid-cols-2 lg:grid-cols-6' : 'grid-cols-2 lg:grid-cols-5'
}

const RANGE_ROWS = [
  ['price', 'summary.returnSplit.price'],
  ['income', 'summary.returnSplit.income'],
  ['fx', 'summary.returnSplit.fx'],
] as const satisfies readonly (readonly [keyof AttributionPosition, StringKey])[]

// One position's figures, in the summary's reporting currency. `dividends`
// is the dividends timeline: undefined while it loads, null to leave the
// dividends cell out (the read-only `all` scope has no dividends route,
// ADR 0014). `attribution` is the position's entry in the selected range's
// return split; without one nothing extra shows. `forecast` is the position's
// rows of the twelve-month dividend forecast.
export function PositionKpis({
  position,
  summary,
  dividends,
  attribution,
  forecast = [],
  className = '',
}: {
  position: Position
  summary: Summary
  dividends: DividendTimeline | undefined | null
  attribution?: AttributionPosition
  forecast?: DividendForecastRow[]
  className?: string
}) {
  const closed = position.closed === true
  const reporting = summary.currency
  const withDividends = dividends !== null

  // The backend folds each dividend's net into the position's realized_pl
  // (holdings.State.Apply), so the total is unrealized + realized, which
  // holds the dividends once, and the realized cell says it includes them.
  // The server sums the two unrounded and rounds once; an open
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
  const expected = expectedNetOf(forecast)
  const marketValue = Number(position.market_value_reporting ?? 0)
  const weight = weightOf(position, holdingsTotal(summary))

  const columns = kpiColumns(closed, withDividends)
  return (
    <KpiStrip className={className} columns={columns}>
      <KpiCell label={t('position.total')} className={closed ? '' : KPI_WIDE}>
        <FitFigure
          className={`${KPI_FIGURE} ${plTone(total)}`}
          text={signedMoney(total, reporting)}
        />
        <p className="text-[12px] text-muted">{t('position.totalHint')}</p>
        {attribution && (
          <div className="mt-1 text-[12px]">
            <p className="text-muted">{t('position.rangeSplit')}</p>
            {RANGE_ROWS.map(([key, label]) => (
              <p key={key} className="flex justify-between gap-2">
                <span className="text-muted">{t(label)}</span>
                <span className={`tabular-nums ${plTone(attribution[key])}`}>
                  {signedMoney(attribution[key], reporting)}
                </span>
              </p>
            ))}
          </div>
        )}
      </KpiCell>
      {!closed && (
        <>
          <KpiCell label={t('position.marketValue')}>
            <FitFigure
              className={`${KPI_FIGURE} text-ink`}
              text={formatMoney(position.market_value_reporting, reporting)}
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
              text={signedMoney(unrealized, reporting)}
            />
            {percent && <p className={`text-[12px] ${plTone(unrealized)}`}>{percent}</p>}
            {/* The part of it from the exchange rate, as Özet's table
                shows it; Özet's phone row leaves it to the
                position page. */}
            {position.fx_pl_reporting != null && (
              <p className={`text-[12px] ${plTone(position.fx_pl_reporting)}`}>
                {`${t('position.fxSinceCost')} ${signedMoney(position.fx_pl_reporting, reporting)}`}
              </p>
            )}
          </KpiCell>
        </>
      )}
      <KpiCell label={t('position.realized')}>
        <FitFigure
          className={`${KPI_FIGURE} ${plTone(realized)}`}
          text={signedMoney(realized, reporting)}
        />
        <p className="text-[12px] text-muted">{t('position.realizedHint')}</p>
      </KpiCell>
      {withDividends && (
        <KpiCell label={t('position.dividends')}>
          <FitFigure
            className={`${KPI_FIGURE} ${plTone(dividendNet)}`}
            text={signedMoney(dividendNet, reporting)}
          />
          {dividendRow && (
            <p className="text-[12px] text-muted">
              {`${t('dividends.page.paymentsCount', { count: dividendRow.count })} · ${t('position.dividendsHint')}`}
            </p>
          )}
          {expected !== null && (
            <p className="text-[12px] text-muted">
              {marketValue > 0
                ? t('position.dividendForecast', {
                    amount: formatMoney(expected, reporting),
                    yield: formatNumber(Number(expected) / marketValue, SHARE_FORMAT),
                  })
                : t('position.dividendForecastNoYield', {
                    amount: formatMoney(expected, reporting),
                  })}
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
