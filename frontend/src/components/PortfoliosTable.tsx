import { Link, useLocation, useNavigate } from 'react-router'
import { usePortfolioHistory, type HistoryEntry, type ReturnMethod } from '../api/history'
import type { Portfolio } from '../api/portfolios'
import { usePortfolioSummary } from '../api/summary'
import { t } from '../i18n/strings'
import { decimalUnits } from '../lib/decimal'
import { formatMoney, MISSING, plTone, signedMoney, signedPercent } from '../lib/format'
import { RANGES, resolveRange, type RangeValue } from '../lib/ranges'
import { rangeSearch } from '../lib/useRange'
import { MissingRatesMark } from './MissingRatesMark'
import { todayIstanbul } from '../lib/date'
import { TABLE, TD, TD_NUM, TH, ROW_40 } from './ui/table'
import { WeightBar } from './ui/WeightBar'

// The value over the range as a line, scaled to its own low and high. Only
// a shape: no axis, no amount, so privacy mode leaves it.
function Sparkline({ points }: { points: HistoryEntry[] }) {
  const values = points.map((p) => Number(p.total_value))
  // An empty portfolio's points are all zero: no line to draw.
  if (values.length < 2 || values.every((v) => v === 0)) return null
  const low = Math.min(...values)
  const span = Math.max(...values) - low || 1
  const d = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * 120
      const y = 20 - ((v - low) / span) * 18
      return `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`
    })
    .join(' ')
  return (
    <svg aria-hidden="true" viewBox="0 0 120 22" className="h-[22px] w-[120px]">
      <path d={d} fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  )
}

// One portfolio's row. Its return and sparkline come from its own history
// request, its day change from its own summary: the list carries neither.
// Either failing leaves its cells at MISSING and the rest of the row as is.
function PortfolioRow({
  portfolio,
  index,
  share,
  largest,
  from,
  to,
  method,
  search,
}: {
  portfolio: Portfolio
  index: number
  share: number | null
  largest: number
  from: string
  to: string
  method: ReturnMethod
  search: string
}) {
  const navigate = useNavigate()
  const history = usePortfolioHistory(portfolio.id, from, to)
  const summary = usePortfolioSummary(portfolio.id)
  const href = `/portfolios/${portfolio.id}${search}`
  // The previous range's figures would read as this one's while it loads.
  const current = history.data && !history.isPlaceholderData ? history.data : undefined
  const ret = current?.returns[method] ?? null
  const day = summary.data?.day_change ?? null
  const settled = (q: { isError: boolean; data?: unknown }) => q.isError || q.data !== undefined

  return (
    <tr
      className={`${ROW_40} cursor-pointer hover:bg-raised-hover`}
      // The name is the link; the rest of the row opens it for a mouse too.
      onClick={(event) => {
        if (!(event.target as Element).closest('a')) navigate(href)
      }}
    >
      <td className={`${TD} w-full max-w-0`}>
        <span className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden="true"
            className="h-2 w-2 shrink-0 rounded-full"
            style={{ backgroundColor: `var(--color-cat-${(index % 6) + 1})` }}
          />
          <Link
            to={href}
            data-portfolio-row
            title={portfolio.name}
            className="min-w-0 truncate font-semibold hover:underline"
          >
            {portfolio.name}
          </Link>
        </span>
      </td>
      <td className={`${TD_NUM} font-medium`}>
        {formatMoney(portfolio.total_value, portfolio.currency)}
        <MissingRatesMark currencies={portfolio.missing_rates} currency={portfolio.currency} />
      </td>
      <td className={`${TD_NUM} max-sm:hidden`}>
        {share === null ? MISSING : <WeightBar share={share} largest={largest} />}
      </td>
      <td className={`${TD_NUM} ${plTone(ret)}`}>
        {ret !== null ? signedPercent(Number(ret)) : settled(history) ? MISSING : ''}
      </td>
      <td className={`${TD} w-0 max-md:hidden ${plTone(ret)}`}>
        {current && <Sparkline points={current.points} />}
      </td>
      <td className={`${TD_NUM} max-sm:hidden ${plTone(day)}`}>
        {day !== null ? signedMoney(day, portfolio.currency) : settled(summary) ? MISSING : ''}
      </td>
    </tr>
  )
}

// Tüm portföyler's "Portföyler" table (GLG-935): one row per portfolio with
// its value, its share of the whole, its return over the page's range by the
// page's method with a sparkline, and its day change. A row opens that
// portfolio's Özet in the same range. Below md the sparkline goes, below sm
// the share and the day change (the strip above has the day's total), so the
// table never scrolls sideways and a name keeps room at 375 px.
export function PortfoliosTable({
  portfolios,
  range,
  method,
}: {
  portfolios: Portfolio[]
  range: RangeValue
  method: ReturnMethod
}) {
  const { search } = useLocation()
  const { from, to } = resolveRange(range, todayIstanbul())
  // The share base sums only positive totals, as the allocation card does: a
  // negative portfolio (net negative cash) would push the others past 100%
  // (GLG-670). Such a portfolio has no share.
  const base = Number(
    portfolios.reduce((sum, p) => {
      const units = decimalUnits(p.total_value)
      return units > 0n ? sum + units : sum
    }, 0n),
  )
  const shares = portfolios.map((p) => {
    const units = decimalUnits(p.total_value)
    return units > 0n ? Number(units) / base : null
  })
  const largest = Math.max(0, ...shares.map((s) => s ?? 0))
  const preset = RANGES.find((r) => r.key === range.key)

  return (
    <section className="overflow-hidden card">
      <div className="border-b border-line px-4 py-3">
        <h2 className="text-[14px] font-semibold text-ink">
          {t('portfolios.title')}
          <span className="font-normal text-muted"> · {portfolios.length}</span>
        </h2>
      </div>
      <table className={TABLE}>
        <thead>
          <tr>
            <th className={TH}>{t('portfolio.label')}</th>
            <th className={`${TH} text-right`}>{t('summary.value')}</th>
            <th className={`${TH} text-right max-sm:hidden`}>{t('portfolios.share')}</th>
            <th className={`${TH} text-right`}>
              {t('chart.return.title')}
              {preset && ` · ${t(preset.labelKey)}`}
            </th>
            <th className={`${TH} max-md:hidden`}>
              <span className="sr-only">{t('portfolios.trend')}</span>
            </th>
            <th className={`${TH} text-right max-sm:hidden`}>{t('summary.dayChange')}</th>
          </tr>
        </thead>
        <tbody>
          {portfolios.map((portfolio, index) => (
            <PortfolioRow
              key={portfolio.id}
              portfolio={portfolio}
              index={index}
              share={shares[index]}
              largest={largest}
              from={from}
              to={to}
              method={method}
              search={rangeSearch(search)}
            />
          ))}
        </tbody>
      </table>
    </section>
  )
}
