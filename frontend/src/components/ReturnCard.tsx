import type { UseQueryResult } from '@tanstack/react-query'
import { useId, useMemo } from 'react'
import { type Benchmark, type HistoryResponse, type ReturnMethod } from '../api/history'
import { t } from '../i18n/strings'
import { rangeDelta } from '../lib/chart'
import { formatMarketDate, todayIstanbul } from '../lib/date'
import { currencySymbol, MISSING, plTone, signedMoney, signedPercent } from '../lib/format'
import { RANGES, resolveRange, type RangeValue } from '../lib/ranges'
import { annualizedReturn, excessReturn } from '../lib/returns'
import { useZoom } from '../lib/useRange'
import { benchmarkNames } from '../lib/useReturnSettings'
import { BenchmarkPill } from './BenchmarkPill'
import { BenchmarkSelect } from './BenchmarkSelect'
import { LineChart } from './LazyChart'
import { RangePicker } from './RangePicker'
import { ReturnMethodSelect } from './ReturnMethodSelect'
import { useStoredChoice } from '../lib/useStoredChoice'
import { ChartFrame } from './ui/ChartFrame'
import { Notice } from './ui/Notice'
import type { SelectOption } from './ui/Select'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

// The chart's "% | ₺" switch, kept in localStorage as chartMode;
// it is LineChart's unit.
type ChartMode = 'percent' | 'money'
const CHART_MODES: ChartMode[] = ['percent', 'money']

export type HistoryQuery = Pick<
  UseQueryResult<HistoryResponse>,
  'data' | 'isError' | 'error' | 'refetch' | 'isFetching' | 'isPlaceholderData'
>

// The return card at the top of Özet (layout A): the return of the
// chosen range by the chosen method, with the change in value (rangeDelta)
// and the range in words; a pill with the benchmark's return and the
// difference, coloured by the difference's sign; the benchmark and method
// selects; the range picker; the chart and the range's notices.
// `benchmarks` are the benchmark options other than none, already labelled.
// The chart opens in percent: the portfolio's twr and the
// benchmark's benchmark_twr from 0%, without the cost line, so a deposit does
// not bend it. The line is time-weighted whatever the method, and with
// another method its legend entry says so and why. "₺" is the value chart.
// With twr undefined for the range (null on every point, ADR 0011) the card
// shows the value chart, disables the switch and says why.
export function ReturnCard({
  currency,
  query,
  range,
  onRange,
  firstDate,
  benchmark,
  benchmarks,
  onBenchmark,
  method,
  onMethod,
}: {
  currency: string
  query: HistoryQuery
  range: RangeValue
  onRange: (next: RangeValue) => void
  firstDate?: string
  benchmark: Benchmark | null
  benchmarks: SelectOption<string>[]
  onBenchmark: (next: Benchmark | null) => void
  method: ReturnMethod
  onMethod: (next: ReturnMethod) => void
}) {
  const { data: history, isPlaceholderData: stale } = query
  const [storedMode, setChartMode] = useStoredChoice('chartMode', CHART_MODES, 'percent')
  const undefinedHint = useId()
  const twrUndefined = !!history && !history.points.some((p) => p.twr != null)
  const chartMode = twrUndefined ? 'money' : storedMode
  // Memoised: LineChart derives its rows from the array's identity.
  const points = useMemo(
    () =>
      chartMode === 'percent'
        ? (history?.points ?? []).map((p) => ({
            ...p,
            total_value: p.twr ?? null,
            cost_basis: null,
            invested: null,
            benchmark_value: p.benchmark_twr ?? undefined,
          }))
        : (history?.points ?? []),
    [history, chartMode],
  )
  // The money mode's label is the reporting currency's symbol, ₺ or $.
  const symbol = currencySymbol(currency)
  const zoom = useZoom(range, onRange)
  const today = todayIstanbul()
  const { from, to } = resolveRange(range, today)
  const preset = RANGES.find((r) => r.key === range.key)
  // A year range reads as the year, a custom one as its two dates.
  let rangeLabel: string = range.key
  if (preset) rangeLabel = t(preset.deltaKey)
  else if (range.key === 'custom') {
    rangeLabel = `${from ? formatMarketDate(from) : '…'} – ${formatMarketDate(to)}`
  }

  // The previous range's chart stays up while a new one loads, but
  // its change and returns would read as the new range's, so they wait.
  const delta = history && !stale ? rangeDelta(history.points, history.net_flow) : null
  const pct = history && delta !== null ? history.returns[method] : null
  // The benchmark's return by the same method, and the excess over it
  // (excessReturn). Percentages are ratios.
  const historyBenchmark = history?.benchmark ?? null
  const benchmarkPct = historyBenchmark?.returns[method] ?? null
  // Values with a null return mean the method is undefined for the range
  // (ADR 0011), not that the benchmark is missing.
  const benchmarkHasValues = !!history?.points.some((p) => p.benchmark_value !== undefined)
  const benchmarkHasData = benchmarkPct !== null && benchmarkHasValues
  const excess =
    benchmarkHasData && pct !== null ? excessReturn(Number(pct), Number(benchmarkPct)) : null
  const lastDate = history?.points[history.points.length - 1]?.date
  const benchmarkEstimated =
    benchmarkHasData &&
    !!historyBenchmark?.estimated_from &&
    !!lastDate &&
    historyBenchmark.estimated_from <= lastDate
  // The real return after TÜFE inflation, from the portfolio's twr. The pill
  // already shows it when TÜFE is the benchmark.
  const real =
    history?.inflation != null && history.returns.twr !== null && historyBenchmark?.code !== 'TUFE'
      ? excessReturn(Number(history.returns.twr), Number(history.inflation))
      : null
  const realEstimated =
    !!history?.inflation_estimated_from &&
    !!lastDate &&
    history.inflation_estimated_from <= lastDate
  const { label: benchmarkLabel, excess: excessLabel } = benchmarkNames(
    historyBenchmark?.code,
    benchmarks,
  )
  // A range of a year or more also shows the returns annualized (see
  // annualizedReturn), over the days between the first and last point.
  const firstPoint = history?.points[0]?.date
  const annualized = annualizedReturn(pct === null ? null : Number(pct), firstPoint, lastDate)
  const benchmarkAnnualized =
    annualized !== null && benchmarkHasData
      ? annualizedReturn(Number(benchmarkPct), firstPoint, lastDate)
      : null

  const moneyLabels = { value: t('chart.totalValue'), invested: t('chart.invested') }
  const returnLabels =
    method === 'twr'
      ? { value: t('chart.return.portfolio'), excess: excessLabel }
      : {
          value: t('chart.return.portfolioTwr'),
          valueHint: t('chart.return.portfolioTwr.hint'),
          excess: excessLabel,
        }

  return (
    <section className="min-w-0 card">
      {/* From sm the heading and the selects share the first row and the
          figures have the card's whole width under them, so a long benchmark
          pill stays on the figures' line in Turkish and English.
          Below sm the selects come last, one under the other, so each keeps its
          label whole. */}
      <div className="grid grid-cols-1 gap-x-3 gap-y-1 px-4 pt-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
        <h2 className="text-[11px] font-medium tracking-wide text-muted uppercase">
          {`${t('chart.return.title')} · ${rangeLabel}`}
        </h2>
        <div className="flex gap-2 max-sm:order-last max-sm:mt-1 max-sm:flex-col">
          <BenchmarkSelect benchmark={benchmark} benchmarks={benchmarks} onChange={onBenchmark} />
          <ReturnMethodSelect method={method} onChange={onMethod} />
        </div>
        <div className="min-w-0 sm:col-span-2">
          {delta !== null && (
            <p
              data-testid="return-headline"
              className="flex flex-wrap items-center gap-x-2 gap-y-1"
            >
              <span
                className={`text-[24px] leading-tight font-semibold tracking-tight tabular-nums ${plTone(pct)}`}
                title={
                  pct !== null ? t(`chart.return.${method}.hint`) : t('chart.return.undefined')
                }
              >
                {/* A method null for the range (ADR 0011) shows a
                    placeholder rather than vanishing. */}
                {pct !== null ? signedPercent(Number(pct)) : MISSING}
              </span>
              <span className={`text-[13px] font-medium tabular-nums ${plTone(delta)}`}>
                {signedMoney(delta, currency)}
              </span>
              {historyBenchmark && (
                <BenchmarkPill
                  label={benchmarkLabel}
                  excessLabel={excessLabel}
                  pct={benchmarkPct === null ? null : Number(benchmarkPct)}
                  excess={excess}
                  estimated={benchmarkEstimated}
                  hasValues={benchmarkHasValues}
                />
              )}
            </p>
          )}
          {annualized !== null && Number.isFinite(annualized) && (
            <p className="mt-0.5 text-[12px] text-muted">
              <span className={plTone(annualized)}>
                {t('chart.annualized', { value: signedPercent(annualized) })}
              </span>
              {benchmarkAnnualized !== null &&
                Number.isFinite(benchmarkAnnualized) &&
                ` · ${benchmarkLabel} ${t('chart.annualized', {
                  value: signedPercent(benchmarkAnnualized),
                })}`}
            </p>
          )}
          {!stale && real !== null && (
            <p
              data-testid="real-return"
              className="mt-0.5 text-[12px] text-muted"
              title={t('chart.return.realHint')}
            >
              <span className={plTone(real)}>
                {`${t('chart.return.real')} ${signedPercent(real)}`}
              </span>
              {realEstimated && ` (${t('chart.benchmark.estimated')})`}
            </p>
          )}
        </div>
      </div>
      <div className="flex items-start gap-2 px-4 pt-2">
        <div className="min-w-0 flex-1">
          <RangePicker
            value={range}
            onChange={zoom.onChange}
            onClear={zoom.onClear}
            firstDate={firstDate}
          />
        </div>
        {/* As tall as the range Select below sm (h-10), so the two centre on
            one line; a custom range's dates wrap under them. */}
        <div className="flex shrink-0 items-center gap-2 max-sm:h-10">
          {twrUndefined && (
            <span id={undefinedHint} className="text-[12px] text-muted">
              {t('chart.return.undefined')}
            </span>
          )}
          <Segmented
            options={CHART_MODES}
            value={chartMode}
            onChange={setChartMode}
            label={(option) => (option === 'percent' ? '%' : symbol)}
            aria-label={t('chart.return.unit')}
            aria-describedby={twrUndefined ? undefinedHint : undefined}
            disabled={twrUndefined}
            className={twrUndefined ? 'opacity-50' : ''}
            itemClassName={TAP_Y}
          />
        </div>
      </div>
      <div className="p-4 pt-3">
        <ChartFrame query={query} height={220}>
          <LineChart
            unit={chartMode}
            points={points}
            height={220}
            benchmarkLabel={benchmarkLabel}
            estimatedFrom={historyBenchmark?.estimated_from}
            labels={chartMode === 'money' ? moneyLabels : returnLabels}
            onZoom={zoom.onZoom}
          />
        </ChartFrame>
        {/* Non-empty means some points leave a currency out, which is what
            makes the line jump on the day rates start. */}
        {(history?.missing_rates ?? []).length > 0 && (
          <div className="mt-3">
            <Notice>
              {t('chart.missingRates', {
                currency,
                currencies: history!.missing_rates.join(', '),
              })}
            </Notice>
          </div>
        )}
        {/* Holdings with no price yet on some days count at cost there; the
            jump when the first price lands is kept out of the change via
            net_flow. */}
        {(history?.missing_prices ?? []).length > 0 && (
          <div className="mt-3">
            <Notice>
              {t('chart.missingPrices', { symbols: history!.missing_prices.join(', ') })}
            </Notice>
          </div>
        )}
      </div>
    </section>
  )
}
