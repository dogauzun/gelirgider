import { useDividendForecast, type DividendForecast } from '../api/dividends'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatMoney } from '../lib/format'
import { SymbolLabel } from './TransactionCells'
import { Badge } from './ui/Badge'

function byPeriod({ months, rows }: DividendForecast) {
  return months
    .map(({ period, net }) => ({
      period,
      net,
      rows: rows.filter((row) => row.period === period),
    }))
    .filter((month) => month.rows.length > 0)
}

export function UpcomingDividends({ portfolioId }: { portfolioId: number }) {
  const { data } = useDividendForecast(portfolioId)
  if (!data || data.rows.length === 0) return null

  return (
    <section className="mb-6 card">
      <h2 className="border-b border-line px-4 py-3 text-[14px] font-semibold text-ink">
        {t('dividends.upcoming.title')}
      </h2>
      {byPeriod(data).map(({ period, net, rows }) => (
        <div key={period}>
          <div className="flex flex-wrap items-baseline justify-between gap-x-3 border-b border-line bg-chip px-4 py-2">
            <h3 className="text-[13px] font-semibold text-ink">
              {formatMarketDate(`${period}-01`, { month: 'long', year: 'numeric' })}
            </h3>
            <span className="text-[12px] text-muted">
              {t('dividends.upcoming.monthNet')} {formatMoney(net, data.currency)}
            </span>
          </div>
          <ul>
            {rows.map((row, i) => (
              <li
                key={`${row.instrument_id}-${row.date ?? row.period}-${i}`}
                className="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-line px-4 py-2.5 last:border-0"
              >
                <span className="flex items-baseline gap-3">
                  {row.date && (
                    <span className="text-[13px] text-muted">{formatMarketDate(row.date)}</span>
                  )}
                  <SymbolLabel tx={row} badge={row.market} />
                  <Badge>
                    {t(row.planned ? 'dividends.upcoming.planned' : 'dividends.upcoming.estimated')}
                  </Badge>
                </span>
                {/* Under the symbol below sm, right-aligned beside it above. */}
                <span className="flex flex-wrap gap-x-4 text-[12px] text-muted max-sm:basis-full sm:ml-auto">
                  <span>
                    {t('dividends.expected.gross')} {formatMoney(row.gross, row.currency)}
                  </span>
                  <span>
                    {t('transactions.withholdingTax')} {formatMoney(row.stopaj, row.currency)}
                  </span>
                  <span className="text-[14px] font-medium text-plum">
                    {t('dividends.expected.net')} {formatMoney(row.net, row.currency)}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </section>
  )
}
