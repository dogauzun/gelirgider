import { useState } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import {
  expectedNetOf,
  useDividendForecast,
  useDividendTimeline,
  type DividendInstrumentTotal,
} from '../api/dividends'
import { usePortfolioSummary } from '../api/summary'
import { BarChart } from '../components/LazyChart'
import { ExpectedDividends } from '../components/ExpectedDividends'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { UpcomingDividends } from '../components/UpcomingDividends'
import { TransactionDialog } from '../components/TransactionDialog'
import { emptyForm } from '../components/transactionForm'
import { Button } from '../components/ui/Button'
import { FitFigure, KPI_FIGURE, KpiCell, KpiStrip } from '../components/ui/Kpi'
import { LoadError } from '../components/ui/LoadError'
import { Notice } from '../components/ui/Notice'
import { Segmented } from '../components/ui/Segmented'
import { SkeletonRows } from '../components/ui/Skeleton'
import { SortHeader } from '../components/ui/SortHeader'
import { nextSort, ROW_44, TABLE, TD, TD_NUM, TH, type Sort } from '../components/ui/table'
import { t, type StringKey } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import type { Bar } from '../lib/chart'
import { formatMarketDate, todayIstanbul } from '../lib/date'
import { decimalUnits, unitsToDecimal } from '../lib/decimal'
import { sortRows } from '../lib/sortRows'
import { formatMoney, formatNumber, MISSING } from '../lib/format'
import { usePortfolioTitle } from '../lib/useTitle'
import { usePositionPath } from '../lib/useRange'
import { useStoredChoice } from '../lib/useStoredChoice'

const GROUP_KEYS = ['month', 'year', 'forecast'] as const
type Group = (typeof GROUP_KEYS)[number]

const GROUPS: { key: Group; labelKey: StringKey }[] = [
  { key: 'month', labelKey: 'dividends.page.monthly' },
  { key: 'year', labelKey: 'dividends.page.yearly' },
  { key: 'forecast', labelKey: 'dividends.page.forecast' },
]

type SortKey = 'symbol' | 'count' | 'net' | 'expected' | 'last_date'

// The instrument table's columns. Text sorts A to Z first, figures and dates
// largest or newest first. Pay, the share of the net, sorts as net does, so
// it has no sort of its own. Below `sm` the table must fit a 375px screen:
// Pay is left out, and the payment count moves under the symbol in
// place of the name.
const COLUMNS: { key: SortKey | 'share'; labelKey: StringKey; className: string }[] = [
  { key: 'symbol', labelKey: 'transactions.instrument', className: '' },
  { key: 'count', labelKey: 'dividends.page.payments', className: 'text-right max-sm:hidden' },
  { key: 'share', labelKey: 'dividends.page.share', className: 'text-right max-sm:hidden' },
  { key: 'net', labelKey: 'dividends.page.net', className: 'text-right' },
  {
    key: 'expected',
    labelKey: 'dividends.page.expectedAnnual',
    className: 'text-right max-sm:hidden',
  },
  { key: 'last_date', labelKey: 'dividends.page.lastPayment', className: 'text-right' },
]

// A ratio as a percent with one decimal: 0.14 -> "%14,0".
const percent = (ratio: number) => formatNumber(ratio, SHARE_FORMAT)

// The sort value of a row's column; `expected` is the row's expected annual
// net, null when it has no forecast. The API's order (net largest first, then
// symbol) is kept for equal values.
function pickValue(
  key: SortKey,
  row: DividendInstrumentTotal,
  expected: string | null,
): string | number | null {
  if (key === 'symbol') return row.symbol
  if (key === 'count') return row.count
  if (key === 'net') return row.net
  if (key === 'expected') return expected
  return Date.parse(row.last_date)
}

// The `count` months ("YYYY-MM") ending with `last`, oldest first.
function monthsBack(last: string, count: number): string[] {
  const [year, month] = last.split('-').map(Number)
  const start = year * 12 + month - 1 - (count - 1)
  return Array.from({ length: count }, (_, i) => {
    const m = start + i
    return `${Math.floor(m / 12)}-${String((m % 12) + 1).padStart(2, '0')}`
  })
}

// The chart's bars: the last 24 months, or every year from the first bucket
// to this one, each zero-filled where the API has no bucket (it sends only
// non-empty ones).
function barsFor(
  group: Group,
  source: Record<Group, { period: string; net: string }[]>,
  today: string,
) {
  const { year: years, forecast } = source
  const net = new Map(source[group].map((b) => [b.period, b.net]))
  let periods: string[]
  if (group === 'forecast') {
    periods = forecast.map((b) => b.period)
  } else if (group === 'month') {
    periods = monthsBack(today.slice(0, 7), 24)
  } else {
    const last = Number(today.slice(0, 4))
    const first = years.length > 0 ? Math.min(Number(years[0].period), last) : last
    periods = Array.from({ length: last - first + 1 }, (_, i) => String(first + i))
  }
  return periods.map((period): Bar => ({
    key: period,
    label:
      group === 'year'
        ? period
        : formatMarketDate(`${period}-01`, { month: 'short', year: '2-digit' }),
    value: net.get(period) ?? '0',
  }))
}

// The Temettü tab: dividends received by month or year, by
// instrument, and the expected ones still to book. Per portfolio only, like
// İşlemler; the `all` scope goes back to its summary.
export function DividendsPage() {
  const params = useParams()
  if (params.id === 'all') return <Navigate to="/portfolios/all" replace />
  return <DividendsBody portfolioId={Number(params.id)} />
}

function EmptyCard({ onAdd }: { onAdd: () => void }) {
  return (
    <section className="mb-6 flex flex-col items-center gap-3 card px-6 py-12 text-center">
      <p className="text-[16px] font-semibold text-ink">{t('dividends.page.empty')}</p>
      <Button onClick={onAdd}>{t('dividends.page.emptyAdd')}</Button>
    </section>
  )
}

function DividendsBody({ portfolioId }: { portfolioId: number }) {
  usePortfolioTitle(t('dividends.page.title'), portfolioId)
  const positionPath = usePositionPath()
  const { data, isLoading, isError, error, refetch, isFetching } = useDividendTimeline(portfolioId)
  const summary = usePortfolioSummary(portfolioId)
  const forecast = useDividendForecast(portfolioId)
  const [group, setGroup] = useStoredChoice('dividendsGroup', GROUP_KEYS, 'month')
  const [adding, setAdding] = useState(false)
  // The API already sends the rows net largest first, so the table starts
  // there and says so.
  const [sort, setSort] = useState<Sort<SortKey>>({ key: 'net', dir: 'desc' })
  const toggleSort = (key: SortKey) => setSort((cur) => nextSort(cur, key))
  const today = todayIstanbul()
  const selected = GROUPS.find((g) => g.key === group) ?? GROUPS[0]
  // Nothing received and nothing ahead is the empty state. With a forecast
  // alone the body still has the forecast KPIs and chart to show, so with
  // nothing received the page waits for the forecast before deciding; a
  // failed forecast counts as none unless an earlier answer is still cached.
  const forecastRows = forecast.data?.rows ?? (forecast.isError ? [] : undefined)
  const isEmpty = data?.years.length === 0 && forecastRows?.length === 0
  const undecided = data?.years.length === 0 && forecastRows === undefined
  const missingRates = [
    ...(data?.missing_rates ?? []),
    ...(forecast.data?.missing_rates ?? []).filter((c) => !data?.missing_rates.includes(c)),
  ]

  let body = null
  if (isEmpty) {
    body = <EmptyCard onAdd={() => setAdding(true)} />
  } else if (undecided) {
    body = <SkeletonRows label={t('dividends.page.loading')} />
  } else if (data) {
    const { currency, months, years } = data
    const last12 = data.last_12_months.net
    // Last 12 months net over what the open positions cost; nothing to
    // divide by when they cost nothing (all closed, or cash only).
    // The open positions' cost in the reporting currency, summed and rounded
    // once on the server; a position without a rate is left out.
    const cost = Number(summary.data?.cost_basis ?? 0)
    const yieldOnCost = cost > 0 ? percent(Number(last12) / cost) : MISSING
    const forecastMonths = forecast.data?.months ?? []
    const forecastNet = forecast.data
      ? unitsToDecimal(forecastMonths.reduce((sum, m) => sum + decimalUnits(m.net), 0n))
      : null
    const positionsValue = Number(summary.data?.positions_value ?? 0)
    const forwardYield =
      forecastNet !== null && positionsValue > 0
        ? percent(Number(forecastNet) / positionsValue)
        : MISSING
    const money = (key: StringKey, value: string) => (
      <KpiCell label={t(key)}>
        <FitFigure className={`${KPI_FIGURE} text-ink`} text={formatMoney(value, currency)} />
      </KpiCell>
    )
    const netUnits = data.by_instrument.reduce((sum, row) => sum + decimalUnits(row.net), 0n)
    // Each instrument's expected annual net; instruments only the forecast
    // knows are not rows of this table.
    const expectedNet = (id: number) =>
      expectedNetOf((forecast.data?.rows ?? []).filter((r) => r.instrument_id === id))
    const share = (net: string) =>
      netUnits > 0n ? percent(Number(net) / Number(unitsToDecimal(netUnits))) : MISSING
    body = (
      <>
        <KpiStrip columns="grid-cols-2 sm:grid-cols-4" className="mb-6">
          {money('dividends.page.last12', last12)}
          <KpiCell label={t('dividends.page.yieldOnCost')}>
            <FitFigure className={`${KPI_FIGURE} text-ink`} text={yieldOnCost} />
            <p className="mt-1 text-[11px] text-muted">{t('dividends.page.yieldOnCostHint')}</p>
          </KpiCell>
          <KpiCell label={t('dividends.page.forecast12')}>
            <FitFigure
              className={`${KPI_FIGURE} text-ink`}
              text={forecastNet === null ? MISSING : formatMoney(forecastNet, currency)}
            />
          </KpiCell>
          <KpiCell label={t('dividends.page.forwardYield')}>
            <FitFigure className={`${KPI_FIGURE} text-ink`} text={forwardYield} />
            <p className="mt-1 text-[11px] text-muted">{t('dividends.page.forwardYieldHint')}</p>
          </KpiCell>
          {money(
            'dividends.page.thisYear',
            years.find((b) => b.period === today.slice(0, 4))?.net ?? '0.00',
          )}
          {money('dividends.page.totalNet', data.total.net)}
          {money('dividends.page.totalTax', data.total.tax)}
          {money('dividends.page.interest', data.total.interest)}
        </KpiStrip>

        <section className="mb-6 card">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3">
            <h2 className="text-[14px] font-semibold text-ink">{t('dividends.page.received')}</h2>
            <Segmented
              options={GROUPS}
              value={selected}
              onChange={(g) => setGroup(g.key)}
              optionKey={(g) => g.key}
              label={(g) => t(g.labelKey)}
              aria-label={t('dividends.page.group')}
              itemClassName="max-md:min-h-11"
            />
          </div>
          <div className="p-4">
            <BarChart
              bars={barsFor(group, { month: months, year: years, forecast: forecastMonths }, today)}
              currency={currency}
            />
            {group === 'forecast' && (
              <p className="mt-3 text-[11px] text-muted">{t('dividends.page.forecastHint')}</p>
            )}
          </div>
        </section>

        <section className="relative mb-6 overflow-x-auto card">
          <h2 className="border-b border-line px-4 py-3 text-[14px] font-semibold text-ink">
            {t('dividends.page.byInstrument')}
          </h2>
          {data.by_instrument.length === 0 ? (
            <p className="px-4 py-6 text-[13px] text-muted">{t('dividends.page.empty')}</p>
          ) : (
            <table className={TABLE}>
              <thead>
                <tr>
                  {COLUMNS.map(({ key, labelKey, className }) => {
                    if (key === 'share') {
                      return (
                        <th key={key} className={`${TH} ${className}`}>
                          {t(labelKey)}
                        </th>
                      )
                    }
                    return (
                      <SortHeader
                        key={key}
                        column={key}
                        sort={sort}
                        onSort={toggleSort}
                        className={className}
                      >
                        {t(labelKey)}
                      </SortHeader>
                    )
                  })}
                </tr>
              </thead>
              <tbody>
                {sortRows(
                  data.by_instrument,
                  (row) => pickValue(sort.key, row, expectedNet(row.instrument_id)),
                  sort,
                ).map((row) => (
                  <tr key={row.instrument_id} className={ROW_44}>
                    <td className={TD}>
                      <Link
                        to={positionPath(portfolioId, row.instrument_id)}
                        className="font-semibold text-ink hover:underline"
                      >
                        {row.symbol}
                      </Link>
                      {/* Truncated, so a long name never widens the table. */}
                      <span
                        title={row.name}
                        className="block max-w-56 truncate text-[11px] text-muted max-sm:hidden"
                      >
                        {row.name}
                      </span>
                      <span className="block text-[11px] text-muted sm:hidden">
                        {t('dividends.page.paymentsCount', { count: row.count })}
                      </span>
                    </td>
                    <td className={`${TD_NUM} max-sm:hidden`}>{row.count}</td>
                    <td className={`${TD_NUM} max-sm:hidden`}>{share(row.net)}</td>
                    <td className={`${TD_NUM} font-medium text-plum`}>
                      {formatMoney(row.net, currency)}
                    </td>
                    <td className={`${TD_NUM} max-sm:hidden`}>
                      {formatMoney(expectedNet(row.instrument_id), currency)}
                    </td>
                    <td className={TD_NUM}>
                      {formatMarketDate(row.last_date, {
                        day: '2-digit',
                        month: 'short',
                        year: 'numeric',
                      })}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </>
    )
  }

  return (
    // An empty Temettü's one primary action is the card's own.
    <PortfolioHeader portfolioId={portfolioId} add={isEmpty ? 'quiet' : 'float'}>
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {adding && (
          <TransactionDialog
            portfolioId={portfolioId}
            editing={null}
            initial={emptyForm('dividend', today)}
            onClose={() => setAdding(false)}
          />
        )}
        {/* The one thing here that asks for an action leads. */}
        <ExpectedDividends portfolioId={portfolioId} />
        {/* Outside body, like the card above: a user with nothing received
            still sees what is ahead. */}
        <UpcomingDividends portfolioId={portfolioId} />
        {isLoading && <SkeletonRows label={t('dividends.page.loading')} />}
        {isError && (
          <LoadError
            title={t('dividends.page.error')}
            error={error}
            onRetry={() => refetch()}
            retrying={isFetching}
            className="mb-6"
          />
        )}
        {/* Also over the empty state: rows left out for a missing rate can be
            all there is. */}
        {data && missingRates.length > 0 && (
          <div className="mb-6">
            <Notice>
              {t('summary.missingRates', {
                currency: data.currency,
                currencies: missingRates.join(', '),
              })}
            </Notice>
          </div>
        )}
        {body}
      </div>
    </PortfolioHeader>
  )
}
