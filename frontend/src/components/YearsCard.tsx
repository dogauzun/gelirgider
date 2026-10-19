import { useId } from 'react'
import type { ReturnMethod, Benchmark } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { useYears } from '../api/years'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { MISSING, formatMoney, plTone, signedMoney, signedPercent } from '../lib/format'
import { PartialMark, STICKY } from './PeriodReturnsTable'
import { ChartFrame } from './ui/ChartFrame'
import { ROW_40, TABLE, TD, TD_NUM, TH } from './ui/table'

// "Yıllık özet": a row per calendar year, newest first, from the year's
// opening value to its closing value (ADR 0038).
export function YearsCard({
  scope,
  benchmark,
  method,
}: {
  scope: PortfolioScope
  benchmark: Benchmark | null
  method: ReturnMethod
}) {
  const headingId = useId()
  const query = useYears(scope, benchmark)
  const { data } = query

  const body = () => {
    if (!data) return null
    if (data.years.length === 0)
      return <p className="px-1 py-2 text-[13px] text-muted">{t('transactions.empty')}</p>
    const { currency } = data
    return (
      <>
        {/* The table holds nothing focusable, so the scroller itself is a named tab stop (WCAG 2.1.1). */}
        <div
          role="region"
          aria-labelledby={headingId}
          tabIndex={0}
          className="relative -mx-3 overflow-x-auto"
        >
          <table className={TABLE} aria-labelledby={headingId}>
            <thead>
              <tr>
                <th scope="col" className={`${TH} ${STICKY}`}>
                  {t('years.year')}
                </th>
                {(['opening', 'netFlow', 'gain', 'closing', 'return'] as const).map((key) => (
                  <th key={key} scope="col" className={`${TH} text-right`}>
                    {t(`years.${key}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[...data.years].reverse().map((row) => {
                const ratio = row.returns[method]
                return (
                  <tr key={row.year} className={ROW_40}>
                    <th
                      scope="row"
                      title={`${formatMarketDate(row.from)} – ${formatMarketDate(row.to)}`}
                      className={`${TD} ${STICKY} font-medium`}
                    >
                      {row.year}
                      {row.partial && <PartialMark />}
                    </th>
                    <td className={TD_NUM}>{formatMoney(row.opening, currency)}</td>
                    <td className={TD_NUM}>{signedMoney(row.net_flow, currency)}</td>
                    <td className={`${TD_NUM} ${plTone(row.gain)}`}>
                      {signedMoney(row.gain, currency)}
                    </td>
                    <td className={TD_NUM}>{formatMoney(row.closing, currency)}</td>
                    <td className={`${TD_NUM} ${plTone(ratio)}`}>
                      {ratio === null
                        ? MISSING
                        : signedPercent(Number(ratio), { minimumFractionDigits: 1 })}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        {data.missing_rates.length > 0 && (
          <p className="mt-2 text-xs text-muted">
            {t('chart.missingRates', { currency, currencies: data.missing_rates.join(', ') })}
          </p>
        )}
      </>
    )
  }

  return (
    <section className="card">
      <div className="flex flex-col gap-0.5 border-b border-line px-4 py-3">
        <h2 id={headingId} className="text-[14px] font-semibold text-ink">
          {t('years.title')}
        </h2>
        <p className="text-xs text-muted">{t('years.subtitle')}</p>
        <p className="text-xs text-muted">{t('years.hint')}</p>
      </div>
      <div className="p-3">
        <ChartFrame query={query} height={160} errorTitle={t('years.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
