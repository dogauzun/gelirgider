import type { Benchmark } from '../api/history'
import { t } from '../i18n/strings'
import { FitSelect, type SelectOption } from './ui/Select'

// The benchmark select of the return cards: none, then `benchmarks`, already
// labelled.
export function BenchmarkSelect({
  benchmark,
  benchmarks,
  onChange,
}: {
  benchmark: Benchmark | null
  benchmarks: SelectOption<string>[]
  onChange: (next: Benchmark | null) => void
}) {
  return (
    <FitSelect
      aria-label={t('chart.benchmark.label')}
      // '' is none, and Base UI keeps it as a value: its label shows
      // and picking it reports ''.
      value={benchmark ?? ''}
      onChange={(next) => onChange((next as Benchmark) || null)}
      options={[{ value: '', label: t('chart.benchmark.none') }, ...benchmarks]}
    />
  )
}
