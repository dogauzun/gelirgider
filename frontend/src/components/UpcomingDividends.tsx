import { useUpcomingDividends, type ExpectedDividend } from '../api/dividends'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { netTotal } from '../lib/decimal'
import { formatMoney } from '../lib/format'
import { SymbolLabel } from './TransactionCells'
import { Badge } from './ui/Badge'

// The rows by month of the ex-date ("2026-10"), months in the order they
// first appear. The API sends the rows soonest first, so months and the rows
// in each are ascending.
function byMonth(rows: ExpectedDividend[]): [string, ExpectedDividend[]][] {
  const months = new Map<string, ExpectedDividend[]>()
  for (const row of rows) {
    const month = row.date.slice(0, 7)
    const group = months.get(month)
    if (group) group.push(row)
    else months.set(month, [row])
  }
  return [...months]
}

// Dividends with an ex-date still ahead, as a list by month, right after the
// confirm card. A list, not a grid: İş Yatırım lists only a few weeks ahead,
// so most months would be empty. Renders nothing while there are none.
export function UpcomingDividends({ portfolioId }: { portfolioId: number }) {
  const { data } = useUpcomingDividends(portfolioId)
  if (!data || data.length === 0) return null

  return (
    <section className="mb-6 card">
      <h2 className="border-b border-line px-4 py-3 text-[14px] font-semibold text-ink">
        {t('dividends.upcoming.title')}
      </h2>
      {byMonth(data).map(([month, rows]) => (
        <div key={month}>
          <div className="flex flex-wrap items-baseline justify-between gap-x-3 border-b border-line bg-chip px-4 py-2">
            <h3 className="text-[13px] font-semibold text-ink">
              {formatMarketDate(`${month}-01`, { month: 'long', year: 'numeric' })}
            </h3>
            <span className="text-[12px] text-muted">
              {t('dividends.upcoming.monthNet')} {netTotal(rows)}
            </span>
          </div>
          <ul>
            {rows.map((row) => (
              <li
                key={`${row.instrument_id}-${row.date}`}
                className="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-line px-4 py-2.5 last:border-0"
              >
                <span className="flex items-baseline gap-3">
                  <span className="text-[13px] text-muted">{formatMarketDate(row.date)}</span>
                  <SymbolLabel tx={row} badge={row.market} />
                  <Badge>{t('dividends.upcoming.planned')}</Badge>
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
