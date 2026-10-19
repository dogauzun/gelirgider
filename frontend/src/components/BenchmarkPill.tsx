import { t } from '../i18n/strings'
import { MISSING, signedPercent, signTone } from '../lib/format'
import { Pill } from './ui/Pill'

// The benchmark line under a return figure, as `span`s so a card can put it
// inline or in its own paragraph: a pill with the benchmark's return and the
// excess over it, coloured by the excess's sign, or the no-data line when the
// benchmark has no values in the range. `pct` and `excess` are ratios; a null
// `pct` means the method is undefined for the range.
export function BenchmarkPill({
  label,
  excessLabel,
  pct,
  excess,
  estimated,
  hasValues,
  digits,
}: {
  label: string | undefined
  excessLabel: string
  pct: number | null
  excess: number | null
  estimated: boolean
  hasValues: boolean
  digits?: Intl.NumberFormatOptions
}) {
  // The select would name a benchmark that draws nothing, so the card says so
  // in place of the pill.
  if (!hasValues) {
    return (
      <span className="text-[12px] text-muted">{`${label} · ${t('chart.benchmark.noData')}`}</span>
    )
  }
  const text = [
    `${label} ${pct !== null ? signedPercent(pct, digits) : MISSING}${
      estimated ? ` (${t('chart.benchmark.estimated')})` : ''
    }`,
    excess !== null && `${excessLabel} ${signedPercent(excess, digits)}`,
  ]
    .filter(Boolean)
    .join(' · ')
  return (
    <span title={pct !== null ? undefined : t('chart.return.undefined')}>
      <Pill tone={signTone(excess)}>{text}</Pill>
    </span>
  )
}
