import { useId } from 'react'
import { usePortfolios } from '../api/portfolios'
import type { PortfolioScope } from '../api/summary'
import { useTrades } from '../api/trades'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatQuantity } from '../lib/decimal'
import { formatMoney, plTone, signedMoney, signedPercent } from '../lib/format'
import { positionLabel } from '../lib/positions'
import { ChartFrame } from './ui/ChartFrame'
import { ROW_40, TABLE, TD, TD_NUM, TH } from './ui/table'

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
  const trades = query.data?.trades ?? []
  const showPortfolio = scope === 'all'
  const missingRates = trades.filter((trade) => trade.realized_reporting === null).length
  // The id stands in while the portfolio list loads.
  const portfolioName = (id: number) => portfolios?.find((p) => p.id === id)?.name ?? String(id)

  const body = () => {
    if (trades.length === 0)
      return <p className="px-1 py-2 text-[13px] text-muted">{t('trades.empty')}</p>
    return (
      <>
        {/* Full width of the card, so the cells' own padding lines up with the header. */}
        <div className="relative -mx-3 overflow-x-auto">
          <table className={TABLE} aria-labelledby={headingId}>
            <thead>
              <tr>
                <th className={TH}>{t('trades.date')}</th>
                {instrumentId === undefined && <th className={TH}>{t('trades.instrument')}</th>}
                {showPortfolio && <th className={TH}>{t('trades.portfolio')}</th>}
                <th className={`${TH} text-right`}>{t('trades.quantity')}</th>
                <th className={`${TH} text-right`}>{t('trades.proceeds')}</th>
                <th className={`${TH} text-right`}>{t('trades.cost')}</th>
                <th className={`${TH} text-right`}>{t('trades.realized')}</th>
                <th className={`${TH} text-right`}>{t('trades.days')}</th>
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
        {missingRates > 0 && (
          <p className="px-1 pt-2 text-xs text-muted">
            {t('trades.missingRate', { count: missingRates })}
          </p>
        )}
      </>
    )
  }

  return (
    <section className="card">
      <div className="flex flex-col gap-0.5 border-b border-line px-4 py-3">
        <h2 id={headingId} className="text-[14px] font-semibold text-ink">
          {t('trades.title')}
        </h2>
        <p className="text-xs text-muted">{t('trades.hint')}</p>
      </div>
      <div className="p-3">
        <ChartFrame query={query} height={120} errorTitle={t('trades.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
