import type { HistoryResponse, ReturnMethod } from '../api/history'
import { t } from '../i18n/strings'
import { rangeDelta } from './chart'
import { daysBetween, formatMarketDate } from './date'
import { RANGES, resolveRange, type RangeValue } from './ranges'

// annualize turns a period return r over `days` calendar days into a yearly
// rate, (1 + r)^(365 / days) - 1 (ADR 0011). MWR is a period IRR, so the same
// formula holds for all three methods. A total loss (r = -1) stays -1; a loss
// beyond the money put in (r < -1) has no real annual rate and gives NaN.
export function annualize(r: number, days: number): number {
  return (1 + r) ** (365 / days) - 1
}

// annualizedReturn is the yearly rate of a period return over the days between
// two market dates: null for a missing return or date, or under 364 days. A
// span of 364 to 366 days counts as one year (a calendar year, 2025-01-01 to
// 2025-12-31, is 364 days apart, 2024 is 365; the 1Y preset is 365, or 366 over
// a leap day), so its annualized return is its period return.
export function annualizedReturn(
  ret: number | null,
  from?: string | null,
  to?: string | null,
): number | null {
  if (ret === null || !from || !to) return null
  const gap = daysBetween(from, to)
  return gap >= 364 ? annualize(ret, gap <= 366 ? 365 : gap) : null
}

// excessReturn is return p over benchmark return b, (1 + p) / (1 + b) - 1:
// the real return when the benchmark is TÜFE (ADR 0012). null when the
// benchmark lost everything or more (1 + b <= 0), where the ratio means nothing.
export function excessReturn(p: number, b: number): number | null {
  // eslint-disable-next-line no-restricted-syntax -- the one place for it
  return 1 + b > 0 ? (1 + p) / (1 + b) - 1 : null
}

// returnFigures derives what the return card shows from the history of a
// range: the range in words, the change in value, the return by `method`, the
// benchmark's return and the excess over it, the real return after TÜFE
// inflation and the annualized returns. `today` is the market date the range
// resolves against. `stale` is the previous range's data kept while a new one
// loads; its change and returns would read as the new range's, so they wait.
export function returnFigures({
  history,
  stale,
  method,
  range,
  today,
}: {
  history: HistoryResponse | undefined
  stale: boolean
  method: ReturnMethod
  range: RangeValue
  today: string
}) {
  const { from, to } = resolveRange(range, today)
  const preset = RANGES.find((r) => r.key === range.key)
  // A year range reads as the year, a custom one as its two dates.
  let rangeLabel: string = range.key
  if (preset) rangeLabel = t(preset.deltaKey)
  else if (range.key === 'custom') {
    rangeLabel = `${from ? formatMarketDate(from) : '…'} – ${formatMarketDate(to)}`
  }

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
  // A range of a year or more also shows the returns annualized (see
  // annualizedReturn), over the days between the first and last point.
  const firstPoint = history?.points[0]?.date
  const annualized = annualizedReturn(pct === null ? null : Number(pct), firstPoint, lastDate)
  const benchmarkAnnualized =
    annualized !== null && benchmarkHasData
      ? annualizedReturn(Number(benchmarkPct), firstPoint, lastDate)
      : null
  return {
    rangeLabel,
    delta,
    pct,
    benchmarkPct,
    benchmarkHasValues,
    excess,
    benchmarkEstimated,
    real,
    realEstimated,
    annualized,
    benchmarkAnnualized,
  }
}
