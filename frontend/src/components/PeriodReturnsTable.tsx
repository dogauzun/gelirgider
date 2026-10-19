import type { PeriodBucket, ReturnMethod } from '../api/history'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { MISSING, signedPercent } from '../lib/format'
import { cellValue, HEAT_MIX, heatStep, periodGrid } from '../lib/periods'

// The year column stays in view while the table scrolls sideways.
export const STICKY = 'sticky left-0 z-[1] bg-surface'

// The periodic returns heatmap: a row per year, newest first, with
// twelve month cells and the year's own cell. A cell prints the chosen
// method's return, or with `excess` the return over the benchmark, and its
// background is the gain or loss colour in five strengths (heatStep), so
// colour is never the only cue. Below md it scrolls sideways inside its card
// with the year column fixed.
export function PeriodReturnsTable({
  months,
  years,
  method,
  excess,
  labelledBy,
}: {
  months: PeriodBucket[]
  years: PeriodBucket[]
  method: ReturnMethod
  excess: boolean
  labelledBy: string
}) {
  const rows = periodGrid(months, years)
  const monthNames = Array.from({ length: 12 }, (_, i) =>
    formatMarketDate(`2001-${String(i + 1).padStart(2, '0')}-01`, { month: 'short' }),
  )
  // relative: the cells' sr-only labels are absolute, so without a positioned
  // scroller they escape its clip and widen the page.
  // The table holds nothing focusable, so the scroller itself is a named tab
  // stop that arrow keys scroll (WCAG 2.1.1).
  return (
    <div
      role="region"
      aria-labelledby={labelledBy}
      tabIndex={0}
      className="relative overflow-x-auto"
    >
      {/* eslint-disable-next-line no-restricted-syntax -- a heatmap of spaced tiles, not a compact table */}
      <table
        aria-labelledby={labelledBy}
        className="w-full border-separate border-spacing-[3px] text-[12px] text-ink tabular-nums"
      >
        <thead>
          <tr>
            <td className={STICKY} />
            {[...monthNames, t('performance.periods.year')].map((name) => (
              <th key={name} scope="col" className="px-1 py-0.5 font-medium text-muted">
                {name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.year}>
              <th scope="row" className={`${STICKY} pr-2 text-left font-medium text-muted`}>
                {row.year}
              </th>
              {row.months.map((bucket, i) => (
                <Cell key={i} bucket={bucket} method={method} excess={excess} />
              ))}
              <Cell bucket={row.total} method={method} excess={excess} total />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Cell({
  bucket,
  method,
  excess,
  total = false,
}: {
  bucket: PeriodBucket | null
  method: ReturnMethod
  excess: boolean
  total?: boolean
}) {
  const base = `rounded-[6px] px-1.5 py-1.5 text-center whitespace-nowrap ${total ? 'font-semibold' : ''}`
  if (!bucket) {
    return (
      <td className={`${base} bg-paper text-muted`}>
        <span aria-hidden="true">·</span>
        <span className="sr-only">{t('performance.periods.noData')}</span>
      </td>
    )
  }
  const value = cellValue(bucket, method, excess)
  const step = heatStep(value)
  // The cell text stays ink: index.css.test.ts holds it to 4.5:1 on each mix.
  const background =
    step === 0
      ? 'var(--color-paper)'
      : `color-mix(in srgb, var(--color-${step > 0 ? 'gain' : 'loss'}) ${HEAT_MIX[Math.abs(step) - 1]}%, var(--color-surface))`
  const title = [
    bucket.partial && t('performance.periods.partial'),
    `${formatMarketDate(bucket.from)} – ${formatMarketDate(bucket.to)}`,
    value !== null && signedPercent(value, { maximumFractionDigits: 4 }),
  ]
    .filter(Boolean)
    .join(' · ')
  return (
    <td data-step={step} title={title} className={base} style={{ backgroundColor: background }}>
      {value === null ? MISSING : signedPercent(value, { minimumFractionDigits: 1 })}
      {bucket.partial && <PartialMark />}
    </td>
  )
}

// The asterisk after a figure of a period still running, with its sr-only
// words.
export function PartialMark() {
  return (
    <>
      <span aria-hidden="true">*</span>
      <span className="sr-only">{` (${t('performance.periods.partial')})`}</span>
    </>
  )
}
