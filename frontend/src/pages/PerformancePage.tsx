import { useId, useState } from 'react'
import { useParams } from 'react-router'
import { usePeriodReturns, type Benchmark, type ReturnMethod } from '../api/history'
import { usePortfolioSummary, type PortfolioScope } from '../api/summary'
import { CashFlowCard } from '../components/CashFlowCard'
import { CashGapNotice } from '../components/CashGapNotice'
import { DrawdownCard } from '../components/DrawdownCard'
import { ReturnBars } from '../components/LazyChart'
import { PeriodReturnsTable } from '../components/PeriodReturnsTable'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { ReturnMethodSelect } from '../components/ReturnMethodSelect'
import { RiskCard } from '../components/RiskCard'
import { TradesCard } from '../components/TradesCard'
import { ChartFrame } from '../components/ui/ChartFrame'
import { Segmented } from '../components/ui/Segmented'
import { FitSelect } from '../components/ui/Select'
import { TAP_Y } from '../components/ui/tap'
import { t } from '../i18n/strings'
import { benchmarkNames, useBenchmark, useReturnMethod } from '../lib/useReturnSettings'
import { usePortfolioTitle } from '../lib/useTitle'

// The Performans tab, for a portfolio and for `all`: the benchmark
// and method selects, stored as Özet stores them, over one card per view.
export function PerformancePage() {
  const params = useParams()
  const portfolioId: PortfolioScope = params.id === 'all' ? 'all' : Number(params.id)
  usePortfolioTitle(t('performance.title'), portfolioId)
  // The summary names the scope's currency and positions, which decide the
  // benchmark options, as on Özet.
  const { data: summary } = usePortfolioSummary(portfolioId)
  const { benchmark, options, setBenchmark } = useBenchmark(summary)
  const [method, setMethod] = useReturnMethod()
  const names = benchmarkNames(benchmark ?? undefined, options)

  return (
    <PortfolioHeader portfolioId={portfolioId}>
      <div className="mx-auto flex max-w-6xl flex-col gap-4 px-6 pt-6 pb-12">
        <div className="flex gap-2 sm:justify-end">
          <FitSelect
            aria-label={t('chart.benchmark.label')}
            // '' is none, as on the return card.
            value={benchmark ?? ''}
            onChange={(next) => setBenchmark((next as Benchmark) || null)}
            options={[{ value: '', label: t('chart.benchmark.none') }, ...options]}
          />
          <ReturnMethodSelect method={method} onChange={setMethod} />
        </div>
        <PeriodReturnsCard
          portfolioId={portfolioId}
          benchmark={benchmark}
          benchmarkLabel={options.find((o) => o.value === benchmark)?.label}
          method={method}
        />
        <DrawdownCard portfolioId={portfolioId} />
        <RiskCard portfolioId={portfolioId} benchmark={benchmark} benchmarkLabel={names.label} />
        <TradesCard scope={portfolioId} />
        <CashFlowCard portfolioId={portfolioId} />
      </div>
    </PortfolioHeader>
  )
}

type View = 'portfolio' | 'excess'
const VIEWS: View[] = ['portfolio', 'excess']
type Shape = 'heatmap' | 'bars'
const SHAPES: Shape[] = ['heatmap', 'bars']

// "Dönemsel getiri": the heatmap of every month and year. With a benchmark,
// "Portföy | Fark" switches the cells to the excess over it. "Isı haritası |
// Çubuk" switches to the last 24 months as bars, portfolio and benchmark
// side by side; it opens on the heatmap and is not stored.
function PeriodReturnsCard({
  portfolioId,
  benchmark,
  benchmarkLabel,
  method,
}: {
  portfolioId: PortfolioScope
  benchmark: Benchmark | null
  benchmarkLabel?: string
  method: ReturnMethod
}) {
  const headingId = useId()
  const [view, setView] = useState<View>('portfolio')
  const [shape, setShape] = useState<Shape>('heatmap')
  const query = usePeriodReturns(portfolioId, benchmark)
  const { data } = query
  const excess = view === 'excess' && benchmark !== null
  // No bucket at all is a scope with no transactions yet, not a return that
  // cannot be computed.
  const buckets = data ? [...data.months, ...data.years] : []
  const empty = data !== undefined && buckets.length === 0
  const undefinedReturns = buckets.length > 0 && buckets.every((b) => b.returns[method] === null)
  // The whole scope is one day, the day of the first transaction: too little
  // data, as the drawdown card says, not a return that cannot be computed.
  const oneDay = buckets.length > 0 && buckets[0].from === buckets[buckets.length - 1].to
  // Against TÜFE the excess is the real return, as on the return card.
  const viewLabel = (option: View) =>
    option === 'portfolio'
      ? t('performance.periods.portfolio')
      : t(benchmark === 'TUFE' ? 'performance.periods.real' : 'performance.periods.excess')

  // What the card's body holds once the periods are in.
  const body = () => {
    if (!data) return null
    if (empty) return <p className="px-1 py-2 text-[13px] text-muted">{t('transactions.empty')}</p>
    if (undefinedReturns && oneDay)
      return <p className="px-1 py-2 text-[13px] text-muted">{t('chart.empty')}</p>
    if (undefinedReturns)
      return (
        <div className="px-1 py-2">
          <CashGapNotice scope={portfolioId} />
        </div>
      )
    if (shape === 'bars')
      return <ReturnBars buckets={data.months} method={method} benchmarkLabel={benchmarkLabel} />
    return (
      <PeriodReturnsTable
        months={data.months}
        years={data.years}
        method={method}
        excess={excess}
        labelledBy={headingId}
      />
    )
  }

  return (
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-line px-4 py-3">
        <h2 id={headingId} className="text-[14px] font-semibold text-ink">
          {t('performance.periods.title')}
        </h2>
        {/* Nothing to switch between without a bucket. */}
        {!empty && (
          <div className="flex flex-wrap gap-2">
            <Segmented
              options={SHAPES}
              value={shape}
              onChange={setShape}
              label={(option) => t(`chart.returnBars.view.${option}`)}
              aria-label={t('chart.returnBars.view')}
              itemClassName={TAP_Y}
            />
            {/* The bars show the benchmark beside the portfolio: no excess. */}
            {benchmark !== null && shape === 'heatmap' && (
              <Segmented
                options={VIEWS}
                value={view}
                onChange={setView}
                label={viewLabel}
                aria-label={t('performance.periods.view')}
                itemClassName={TAP_Y}
              />
            )}
          </div>
        )}
      </div>
      <div className="p-3">
        <ChartFrame query={query} height={160} errorTitle={t('performance.periods.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
