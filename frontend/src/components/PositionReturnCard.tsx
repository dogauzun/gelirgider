import type { UseQueryResult } from '@tanstack/react-query'
import type { Benchmark } from '../api/history'
import type { PositionHistoryResponse } from '../api/positions'
import { t } from '../i18n/strings'
import { MISSING, plTone, signedPercent } from '../lib/format'
import { annualizedReturn, excessReturn } from '../lib/returns'
import { benchmarkNames, useReturnMethod } from '../lib/useReturnSettings'
import { BenchmarkPill } from './BenchmarkPill'
import { BenchmarkSelect } from './BenchmarkSelect'
import { FitFigure, KPI_FIGURE } from './ui/Kpi'
import { ReturnMethodSelect } from './ReturnMethodSelect'
import type { SelectOption } from './ui/Select'
import { Skeleton } from './ui/Skeleton'

// Below sm the heading and the two selects stack in a one-column grid, so each
// select keeps its label whole; from sm they share a row, as in Özet's return card.
const CARD_HEAD =
  'grid grid-cols-1 gap-3 border-b border-line px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'
const TWO_DIGITS = { minimumFractionDigits: 2, maximumFractionDigits: 2 }

// The position's return over the selected range by the method the user
// chose on Özet (useReturnMethod), in the instrument's currency with
// dividends counted. A range of a year or more also shows it annualized, as
// Özet's return card does (364 to 366 days apart count as one year). While
// the history loads the body is a skeleton; on an error the card stays out,
// the chart card shows the load error for the same query.
// A pill under the figure gives the chosen benchmark's return by the same
// method and the excess over it, as Özet's return card does; `benchmarks` are
// the options other than none, already labelled.
export function PositionReturnCard({
  history,
  currency,
  benchmark,
  benchmarks,
  onBenchmark,
  Heading = 'h2',
}: {
  Heading?: 'h2' | 'h3'
  history: Pick<UseQueryResult<PositionHistoryResponse>, 'data' | 'isError'>
  currency: string
  benchmark: Benchmark | null
  benchmarks: SelectOption<string>[]
  onBenchmark: (next: Benchmark | null) => void
}) {
  const [method, setMethod] = useReturnMethod()
  const { data } = history
  const ret = data?.returns[method] ?? null
  const lastDate = data?.returns_to
  const retNum = ret === null ? null : Number(ret)
  const annualized = annualizedReturn(retNum, data?.returns_from, lastDate)
  const historyBenchmark = data?.benchmark ?? null
  const benchmarkPct = historyBenchmark?.returns[method] ?? null
  // Points with a null benchmark_twr mean the benchmark has no value in the
  // range (or its twr is undefined), as the pill's no-data line says.
  const benchmarkHasValues = !!data?.points.some((p) => p.benchmark_twr != null)
  const excess =
    benchmarkHasValues && benchmarkPct !== null && retNum !== null
      ? excessReturn(retNum, Number(benchmarkPct))
      : null
  const benchmarkEstimated =
    benchmarkHasValues &&
    benchmarkPct !== null &&
    !!historyBenchmark?.estimated_from &&
    !!lastDate &&
    historyBenchmark.estimated_from <= lastDate
  const { label: benchmarkLabel, excess: excessLabel } = benchmarkNames(
    historyBenchmark?.code,
    benchmarks,
  )

  return (
    <section className="card">
      <div className={CARD_HEAD}>
        <Heading className="text-[14px] font-semibold text-ink">
          {t('position.return.title')}
        </Heading>
        <div className="flex gap-2 max-sm:flex-col">
          <BenchmarkSelect benchmark={benchmark} benchmarks={benchmarks} onChange={onBenchmark} />
          <ReturnMethodSelect method={method} onChange={setMethod} />
        </div>
      </div>
      <div className="p-4">
        {!data && !history.isError && <Skeleton className="h-16" />}
        {data && (
          <>
            <span title={ret === null ? t('chart.return.undefined') : undefined}>
              <FitFigure
                className={`${KPI_FIGURE} ${plTone(ret)}`}
                text={ret === null ? MISSING : signedPercent(Number(ret), TWO_DIGITS)}
              />
            </span>
            {historyBenchmark && (
              <p className="mt-1.5">
                <BenchmarkPill
                  label={benchmarkLabel}
                  excessLabel={excessLabel}
                  pct={benchmarkPct === null ? null : Number(benchmarkPct)}
                  excess={excess}
                  estimated={benchmarkEstimated}
                  hasValues={benchmarkHasValues}
                  digits={TWO_DIGITS}
                />
              </p>
            )}
            {annualized !== null && Number.isFinite(annualized) && (
              <p className={`mt-0.5 text-[12px] ${plTone(annualized)}`}>
                {t('position.return.annualized', {
                  percent: signedPercent(annualized, TWO_DIGITS),
                })}
              </p>
            )}
            <p className="mt-0.5 text-[12px] text-muted">
              {t('position.return.hint', { currency })}
            </p>
          </>
        )}
      </div>
    </section>
  )
}
