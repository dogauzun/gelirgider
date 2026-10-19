import { Fragment, useId, useState, type ReactNode } from 'react'
import type { ReturnMethod, Benchmark } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { useYears, type YearRow } from '../api/years'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { MISSING, formatMoney, plTone, signedMoney, signedPercent } from '../lib/format'
import { PartialMark, STICKY } from './PeriodReturnsTable'
import { ChartFrame } from './ui/ChartFrame'
import { Segmented } from './ui/Segmented'
import { ROW_40, TABLE, TD, TD_NUM, TH } from './ui/table'
import { TAP_Y } from './ui/tap'

const VIEWS = ['summary', 'split'] as const
type View = (typeof VIEWS)[number]

type Column = { key: string; header: ReactNode; cell: (row: YearRow) => ReactNode }

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
  const [view, setView] = useState<View>('summary')
  const query = useYears(scope, benchmark)
  const { data } = query
  const hasRows = !!data && data.years.length > 0

  const body = () => {
    if (!data) return null
    if (data.years.length === 0)
      return <p className="px-1 py-2 text-[13px] text-muted">{t('transactions.empty')}</p>
    const { currency } = data
    const money = (
      key: string,
      header: string,
      value: (row: YearRow) => string | null,
    ): Column => ({
      key,
      header,
      cell: (row) => {
        const v = value(row)
        return v === null ? (
          <td className={TD_NUM}>{MISSING}</td>
        ) : (
          <td className={`${TD_NUM} ${plTone(v)}`}>{signedMoney(v, currency)}</td>
        )
      },
    })
    const gain = money('gain', t('years.gain'), (row) => row.gain)
    const ratioColumn: Column = {
      key: 'return',
      header: t('years.return'),
      cell: (row) => {
        const ratio = row.returns[method]
        return (
          <td className={`${TD_NUM} ${plTone(ratio)}`}>
            {ratio === null ? MISSING : signedPercent(Number(ratio), { minimumFractionDigits: 1 })}
          </td>
        )
      },
    }
    const columns: Column[] =
      view === 'summary'
        ? [
            {
              key: 'opening',
              header: t('years.opening'),
              cell: (row) => <td className={TD_NUM}>{formatMoney(row.opening, currency)}</td>,
            },
            {
              key: 'netFlow',
              header: t('years.netFlow'),
              cell: (row) => <td className={TD_NUM}>{signedMoney(row.net_flow, currency)}</td>,
            },
            gain,
            {
              key: 'closing',
              header: t('years.closing'),
              cell: (row) => <td className={TD_NUM}>{formatMoney(row.closing, currency)}</td>,
            },
            ratioColumn,
          ]
        : [
            money('price', t('years.price'), (row) => row.price),
            money('income', t('years.income'), (row) => row.income),
            money('fx', t('years.fx'), (row) => row.fx),
            money('cashFx', t('years.cashFx'), (row) => row.cash_fx),
            gain,
            ratioColumn,
          ]
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
                {columns.map((column) => (
                  <th key={column.key} scope="col" className={`${TH} text-right`}>
                    {column.header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[...data.years].reverse().map((row) => (
                <tr key={row.year} className={ROW_40}>
                  <th
                    scope="row"
                    title={`${formatMarketDate(row.from)} – ${formatMarketDate(row.to)}`}
                    className={`${TD} ${STICKY} font-medium`}
                  >
                    {row.year}
                    {row.partial && <PartialMark />}
                  </th>
                  {columns.map((column) => (
                    <Fragment key={column.key}>{column.cell(row)}</Fragment>
                  ))}
                </tr>
              ))}
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
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <h2 id={headingId} className="text-[14px] font-semibold text-ink">
            {t('years.title')}
          </h2>
          {/* Nothing to switch between without a row. */}
          {hasRows && (
            <Segmented
              options={VIEWS}
              value={view}
              onChange={setView}
              label={(option) => t(`years.view.${option}`)}
              aria-label={t('years.view')}
              itemClassName={TAP_Y}
            />
          )}
        </div>
        <p className="text-xs text-muted">{t('years.subtitle')}</p>
        <p className="text-xs text-muted">
          {t(view === 'split' ? 'years.splitHint' : 'years.hint')}
        </p>
      </div>
      <div className="p-3">
        <ChartFrame query={query} height={160} errorTitle={t('years.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
