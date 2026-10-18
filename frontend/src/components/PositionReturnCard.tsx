import type { UseQueryResult } from '@tanstack/react-query'
import type { PositionHistoryResponse } from '../api/positions'
import { t } from '../i18n/strings'
import { daysBetween } from '../lib/date'
import { MISSING, plTone, signedPercent } from '../lib/format'
import { annualize } from '../lib/returns'
import { useReturnMethod } from '../lib/useReturnSettings'
import { FitFigure, KPI_FIGURE } from './ui/Kpi'
import { ReturnMethodSelect } from './ReturnMethodSelect'
import { Skeleton } from './ui/Skeleton'

const CARD_HEAD = 'flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3'
const TWO_DIGITS = { minimumFractionDigits: 2, maximumFractionDigits: 2 }

// The position's return over the selected range by the method the user
// chose on Özet (useReturnMethod), in the instrument's currency with
// dividends counted. A range of a year or more also shows it annualized, as
// Özet's return card does (364 to 366 days apart count as one year). While
// the history loads the body is a skeleton; on an error the card stays out,
// the chart card shows the load error for the same query.
export function PositionReturnCard({
  history,
  currency,
  Heading = 'h2',
}: {
  Heading?: 'h2' | 'h3'
  history: Pick<UseQueryResult<PositionHistoryResponse>, 'data' | 'isError'>
  currency: string
}) {
  const [method, setMethod] = useReturnMethod()
  const { data } = history
  const ret = data?.returns[method] ?? null
  const lastDate = data?.points[data.points.length - 1]?.date
  const gap = data?.returns_from && lastDate ? daysBetween(data.returns_from, lastDate) : 0
  const annualized =
    gap >= 364 && ret !== null ? annualize(Number(ret), gap <= 366 ? 365 : gap) : null

  return (
    <section className="card">
      <div className={CARD_HEAD}>
        <Heading className="text-[14px] font-semibold text-ink">
          {t('position.return.title')}
        </Heading>
        <ReturnMethodSelect method={method} onChange={setMethod} />
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
