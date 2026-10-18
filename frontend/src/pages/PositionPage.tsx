import { useState } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { useAttribution } from '../api/attribution'
import { useDividendForecast, useDividendTimeline } from '../api/dividends'
import { usePositionHistory } from '../api/positions'
import { usePortfolioSummary } from '../api/summary'
import { useTransactions } from '../api/transactions'
import { LineChart } from '../components/LazyChart'
import { NotFound } from '../components/NotFound'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { PositionActions } from '../components/PositionActions'
import { PositionKpis } from '../components/PositionKpis'
import { PositionReturnCard } from '../components/PositionReturnCard'
import { PositionPriceChart } from '../components/PositionPriceChart'
import { PositionTitle } from '../components/PositionTitle'
import { PositionTransactions } from '../components/PositionTransactions'
import { RangePicker } from '../components/RangePicker'
import { ChartFrame } from '../components/ui/ChartFrame'
import { LoadError } from '../components/ui/LoadError'
import { Notice } from '../components/ui/Notice'
import { Segmented } from '../components/ui/Segmented'
import { Skeleton } from '../components/ui/Skeleton'
import { TAP_Y } from '../components/ui/tap'
import { usePositionActions } from '../components/usePositionActions'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { isHeld, positionLabel } from '../lib/positions'
import { resolveRange } from '../lib/ranges'
import { rangeSearch, useRange, useZoom } from '../lib/useRange'
import { usePortfolioTitle } from '../lib/useTitle'

const CARD_HEAD = 'flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3'

const CHARTS = ['price', 'value', 'return'] as const
type ChartView = (typeof CHARTS)[number]
const CHART_LABEL = {
  price: 'position.priceChart',
  value: 'position.valueVsCost',
  return: 'position.returnChart',
} as const

// One holding of one portfolio (ADR 0016), led by the owner's result on it
// (core pages spec, "Position page"): a title row with the last price and the
// trade buttons; its figures in the reporting currency, from the cached
// summary and the dividends timeline; one chart card switching between its
// price, with the user's trades and splits marked, and its value against its
// cost, both in the instrument's own currency, and its time-weighted return in
// percent, over the selected range; and its own transactions, read-only.
export function PositionPage() {
  const params = useParams()
  const portfolioId = Number(params.id)
  const instrumentId = Number(params.instrumentId)
  const { data, isLoading, isError, error, refetch, isFetching } = usePortfolioSummary(portfolioId)
  const dividends = useDividendTimeline(portfolioId)
  const forecast = useDividendForecast(portfolioId)
  const [range, setRange] = useRange()
  const zoom = useZoom(range, setRange)
  const { search } = useLocation()
  const { from, to } = resolveRange(range, todayIstanbul())
  const attribution = useAttribution(portfolioId, from, to)
  const history = usePositionHistory(portfolioId, instrumentId, from, to)
  const transactions = useTransactions(portfolioId, { instrumentId })
  const [chart, setChart] = useState<ChartView>('price')
  const { openTrade, openPrice, dialogs } = usePositionActions(portfolioId)

  const position = data?.positions.find((p) => p.instrument_id === instrumentId)
  // A summary without this position is the body's not-found state.
  const label = position && positionLabel(position)
  usePortfolioTitle(
    data && !position ? t('notFound.title') : (label ?? t('position.title')),
    portfolioId,
  )

  // The value chart spans the held days (isHeld); the price chart
  // is PositionPriceChart, over the same history query.
  const points = history.data?.points ?? []
  const first = points.findIndex(isHeld)
  const last = points.findLastIndex(isHeld)
  const chartPoints = points.slice(first, last + 1).map((p) => ({
    date: p.date,
    total_value: isHeld(p) ? p.market_value : null,
    cost_basis: isHeld(p) ? p.cost_basis : null,
    cash: '0',
  }))
  // The return view reads twr as the line's fraction; unit="percent" draws
  // total_value that way (ReturnCard's percent mode).
  const returnPoints = points.slice(first, last + 1).map((p) => ({
    date: p.date,
    total_value: p.twr ?? null,
    cost_basis: null,
    cash: '0',
  }))

  const rows = transactions.data ?? []

  return (
    // Once the summary is in, the body has the page's h1; until then, and on
    // an error, the header's portfolio name is the h1. The header's
    // İşlem ekle opens here, with this instrument.
    <PortfolioHeader
      portfolioId={portfolioId}
      ownHeading={data !== undefined}
      onAdd={position ? () => openTrade('buy', position) : undefined}
    >
      {dialogs}
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {isLoading && <Skeleton className="h-40" />}
        {isError && (
          <LoadError
            title={t('summary.error')}
            error={error}
            onRetry={() => refetch()}
            retrying={isFetching}
          />
        )}
        {data && !position && (
          <NotFound
            heading={t('position.notFound')}
            explanation={t('position.notFoundExplanation')}
            to={`/portfolios/${portfolioId}`}
            linkLabel={t('position.backToSummary')}
          />
        )}

        {data && position && (
          <>
            {/* One title row in place of a back link and two headings. */}
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
              <PositionTitle position={position}>
                <Link
                  to={`/portfolios/${portfolioId}${rangeSearch(search)}`}
                  className="text-[13px] text-muted hover:text-ink hover:underline"
                >
                  {`${t('position.back')} ›`}
                </Link>
                <h1 className="text-[20px] font-semibold text-ink">{positionLabel(position)}</h1>
              </PositionTitle>
              <PositionActions
                className="flex shrink-0 gap-2"
                position={position}
                onTrade={openTrade}
                onEditPrice={openPrice}
              />
            </div>

            <PositionKpis
              className="mb-6"
              position={position}
              summary={data}
              dividends={dividends.data}
              forecast={forecast.data?.rows.filter((r) => r.instrument_id === instrumentId)}
              attribution={attribution.data?.positions.find(
                (p) => p.instrument_id === instrumentId,
              )}
            />

            <PositionReturnCard history={history} currency={position.currency} />

            <section className="my-6 card">
              <div className={CARD_HEAD}>
                <Segmented
                  options={CHARTS}
                  value={chart}
                  onChange={setChart}
                  label={(option) => t(CHART_LABEL[option])}
                  aria-label={t('position.chart')}
                  itemClassName={TAP_Y}
                />
                {/* The portfolio's first transaction, as on Özet. */}
                <RangePicker
                  value={range}
                  onChange={zoom.onChange}
                  onClear={zoom.onClear}
                  firstDate={data?.stats?.first_transaction_date ?? undefined}
                />
              </div>
              <div className="p-4">
                {chart === 'price' && (
                  <PositionPriceChart
                    history={history}
                    position={position}
                    transactions={rows}
                    onZoom={zoom.onZoom}
                  />
                )}
                {chart === 'value' && (
                  <ChartFrame query={history}>
                    <LineChart
                      points={chartPoints}
                      labels={{ value: t('position.marketValue'), cost: t('chart.costBasis') }}
                      onZoom={zoom.onZoom}
                    />
                  </ChartFrame>
                )}
                {chart === 'return' && (
                  <ChartFrame query={history}>
                    {returnPoints.some((p) => p.total_value !== null) ? (
                      <LineChart
                        unit="percent"
                        points={returnPoints}
                        labels={{ value: t('position.returnLine') }}
                        onZoom={zoom.onZoom}
                      />
                    ) : (
                      <Notice>{t('chart.return.undefined')}</Notice>
                    )}
                  </ChartFrame>
                )}
              </div>
            </section>

            <section className="overflow-hidden card">
              <div className={CARD_HEAD}>
                <h2 className="text-[14px] font-semibold text-ink">{t('position.transactions')}</h2>
                <Link
                  to={`/portfolios/${portfolioId}/transactions?instrument=${position.instrument_id}&symbol=${encodeURIComponent(positionLabel(position))}`}
                  className="text-[13px] font-medium text-accent hover:underline"
                >
                  {t('position.allTransactions')}
                </Link>
              </div>
              <PositionTransactions transactions={transactions} loadMore inset />
            </section>
          </>
        )}
      </div>
    </PortfolioHeader>
  )
}
