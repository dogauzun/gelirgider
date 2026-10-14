import { useState } from 'react'
import { RETURN_METHODS, type Benchmark, type ReturnMethod } from '../api/history'
import type { Summary } from '../api/summary'
import { t } from '../i18n/strings'
import { useStoredChoice } from './useStoredChoice'

// The return method and the benchmark that Özet's return card and the
// Performans tab share (GLG-955), each kept in localStorage.

// Benchmarks for the return comparison (ADR 0012). USD and EUR are hidden
// when they are the reporting currency, where they would be a flat line.
// A position of the scope can be the benchmark too, as instrument:<id>
// (GLG-702); the select lists those under their own group.
type FixedBenchmark = Exclude<Benchmark, `instrument:${number}`>
const BENCHMARKS: FixedBenchmark[] = ['USD', 'EUR', 'XU100', 'TUFE', 'GOLD']
const isInstrumentBenchmark = (code: string): code is `instrument:${number}` =>
  /^instrument:[1-9]\d*$/.test(code)

// The return method (ADR 0011); the default mwr is closest to the Dietz
// figure the header showed before.
export function useReturnMethod(): [ReturnMethod, (next: ReturnMethod) => void] {
  return useStoredChoice('returnMethod', RETURN_METHODS, 'mwr')
}

// The benchmark of a scope whose summary is `summary`, with the labelled
// options other than none for its select.
export function useBenchmark(summary: Summary | undefined) {
  const [stored, setStored] = useState<Benchmark | null>(() => {
    try {
      const value = localStorage.getItem('benchmark')
      if (value && (BENCHMARKS.includes(value as FixedBenchmark) || isInstrumentBenchmark(value)))
        return value as Benchmark
    } catch {
      // Storage disabled (private mode, blocked site data): use the default.
    }
    return null
  })
  const setBenchmark = (next: Benchmark | null) => {
    setStored(next)
    try {
      if (next) localStorage.setItem('benchmark', next)
      else localStorage.removeItem('benchmark')
    } catch {
      // Storage disabled: the choice lasts until the page is left.
    }
  }
  const fixed = BENCHMARKS.filter((b) => b !== summary?.currency)
  // The scope's open positions by symbol, from the summary already loaded.
  const positions = (summary?.positions ?? [])
    .filter((p) => !p.closed)
    .sort((a, b) => a.symbol.localeCompare(b.symbol))
    .map((p) => ({ code: `instrument:${p.instrument_id}` as Benchmark, symbol: p.symbol }))
  // A stored choice hidden for this portfolio's currency, or a position this
  // scope does not hold, counts as none. Until the summary loads no position
  // is known, so a stored position is not requested from the wrong scope.
  const benchmark =
    stored && (fixed.some((b) => b === stored) || positions.some((p) => p.code === stored))
      ? stored
      : null
  const options = [
    ...fixed.map((b) => ({ value: b as string, label: t(`chart.benchmark.${b}`) })),
    ...positions.map((p) => ({
      value: p.code as string,
      label: p.symbol,
      group: t('chart.benchmark.positions'),
    })),
  ]
  return { benchmark, options, setBenchmark }
}
