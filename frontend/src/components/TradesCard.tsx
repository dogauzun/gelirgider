import { useId, useState } from 'react'
import { usePortfolios } from '../api/portfolios'
import type { PortfolioScope } from '../api/summary'
import { useTrades } from '../api/trades'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatQuantity } from '../lib/decimal'
import {
  formatMoney,
  formatNumber,
  MISSING,
  plTone,
  signedMoney,
  signedPercent,
} from '../lib/format'
import { positionLabel } from '../lib/positions'
import { sortRows } from '../lib/sortRows'
import { tradeStats, tradeYears } from '../lib/trades'
import { ChartFrame } from './ui/ChartFrame'
import { FitFigure, KPI_FIGURE, KpiCell, KpiStrip } from './ui/Kpi'
import { FitSelect } from './ui/Select'
import { SortHeader } from './ui/SortHeader'
import { nextSort, ROW_40, TABLE, TD, TD_NUM, TH, type Sort } from './ui/table'

type SortKey = 'date' | 'realized' | 'days'

// The "Gerçekleşen satışlar" card on the Performans tab: every realized sell
// of the scope, newest first, each against the average cost (the server's
// rule). `instrumentId` keeps one instrument's sells and leaves its column
// out; the portfolio column shows on `all` only.
export function TradesCard({
  scope,
  instrumentId,
}: {
  scope: PortfolioScope
  instrumentId?: number
}) {
  const headingId = useId()
  const query = useTrades(scope, instrumentId)
  const { data: portfolios } = usePortfolios()
  const all = query.data?.trades ?? []
  // '' is every year.
  const [year, setYear] = useState('')
  const [sort, setSort] = useState<Sort<SortKey>>({ key: 'date', dir: 'desc' })
  const years = tradeYears(all)
  const filtered = year === '' ? all : all.filter((trade) => trade.date.startsWith(year))
  const stats = tradeStats(filtered)
  // The reporting amounts compare across currencies; a row without one
  // would sort as missing, so then the sells' own amounts stand in.
  const byReporting = filtered.every((trade) => trade.realized_reporting !== null)
  const trades = sortRows(
    filtered,
    (trade) => {
      if (sort.key === 'date') return Date.parse(trade.date)
      if (sort.key === 'days') return trade.days
      return byReporting ? trade.realized_reporting : trade.realized
    },
    sort,
  )
  const showPortfolio = scope === 'all'
  const currency = query.data?.currency ?? ''
  // The id stands in while the portfolio list loads.
  const portfolioName = (id: number) => portfolios?.find((p) => p.id === id)?.name ?? String(id)

  const sortBy = (key: SortKey) => setSort((current) => nextSort(current, key))

  const body = () => {
    if (filtered.length === 0)
      return <p className="px-1 py-2 text-[13px] text-muted">{t('trades.empty')}</p>
    return (
      <>
        <KpiStrip columns="grid-cols-2 sm:grid-cols-4" className="mb-3">
          <KpiCell label={t('trades.count')}>
            <FitFigure className={`${KPI_FIGURE} text-ink`} text={String(stats.count)} />
          </KpiCell>
          <KpiCell label={t('trades.winRate')}>
            <FitFigure
              className={`${KPI_FIGURE} text-ink`}
              text={
                stats.winRate === null
                  ? MISSING
                  : formatNumber(stats.winRate, { style: 'percent', maximumFractionDigits: 0 })
              }
            />
          </KpiCell>
          <KpiCell label={t('trades.total')}>
            <FitFigure
              className={`${KPI_FIGURE} ${plTone(stats.realizedReporting)}`}
              text={
                stats.realizedReporting === null
                  ? MISSING
                  : signedMoney(stats.realizedReporting, currency)
              }
            />
            {stats.missingRates > 0 && (
              <p className="mt-1 text-xs text-muted">
                {t('trades.missingRate', { count: stats.missingRates })}
              </p>
            )}
          </KpiCell>
          <KpiCell label={t('trades.averageDays')}>
            <FitFigure
              className={`${KPI_FIGURE} text-ink`}
              text={
                stats.averageDays === null
                  ? MISSING
                  : t('trades.daysValue', { count: stats.averageDays })
              }
            />
          </KpiCell>
        </KpiStrip>
        {/* Full width of the card, so the cells' own padding lines up with the header. */}
        <div className="relative -mx-3 overflow-x-auto">
          <table className={TABLE} aria-labelledby={headingId}>
            <thead>
              <tr>
                <SortHeader column="date" sort={sort} onSort={sortBy}>
                  {t('trades.date')}
                </SortHeader>
                {instrumentId === undefined && <th className={TH}>{t('trades.instrument')}</th>}
                {showPortfolio && <th className={TH}>{t('trades.portfolio')}</th>}
                <th className={`${TH} text-right`}>{t('trades.quantity')}</th>
                <th className={`${TH} text-right`}>{t('trades.proceeds')}</th>
                <th className={`${TH} text-right`}>{t('trades.cost')}</th>
                <SortHeader column="realized" sort={sort} onSort={sortBy} className="text-right">
                  {t('trades.realized')}
                </SortHeader>
                <SortHeader column="days" sort={sort} onSort={sortBy} className="text-right">
                  {t('trades.days')}
                </SortHeader>
              </tr>
            </thead>
            <tbody>
              {trades.map((trade, i) => (
                <tr key={i} className={ROW_40}>
                  <td className={TD}>{formatMarketDate(trade.date)}</td>
                  {instrumentId === undefined && <td className={TD}>{positionLabel(trade)}</td>}
                  {showPortfolio && <td className={TD}>{portfolioName(trade.portfolio_id)}</td>}
                  <td className={TD_NUM}>{formatQuantity(trade.quantity)}</td>
                  <td className={TD_NUM}>{formatMoney(trade.proceeds, trade.currency)}</td>
                  <td className={TD_NUM}>{formatMoney(trade.cost, trade.currency)}</td>
                  <td className={`${TD_NUM} ${plTone(trade.realized)}`}>
                    {signedMoney(trade.realized, trade.currency)}
                    {trade.realized_pct !== null && (
                      <div className="text-[11px]">{signedPercent(Number(trade.realized_pct))}</div>
                    )}
                  </td>
                  <td className={TD_NUM}>{trade.days ?? t('trades.short')}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </>
    )
  }

  return (
    <section className="card">
      <div className="flex items-start justify-between gap-3 border-b border-line px-4 py-3">
        <div className="flex flex-col gap-0.5">
          <h2 id={headingId} className="text-[14px] font-semibold text-ink">
            {t('trades.title')}
          </h2>
          <p className="text-xs text-muted">{t('trades.hint')}</p>
        </div>
        {years.length > 0 && (
          <FitSelect
            aria-label={t('trades.year')}
            value={year}
            onChange={setYear}
            options={[
              { value: '', label: t('trades.allYears') },
              ...years.map((y) => ({ value: y, label: y })),
            ]}
          />
        )}
      </div>
      <div className="p-3">
        <ChartFrame query={query} height={120} errorTitle={t('trades.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
