import { useGoal } from '../api/goal'
import { progressPercent, useProjection } from '../api/projection'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatMoney } from '../lib/format'
import { ProjectionChart } from './LazyChart'
import { ChartFrame } from './ui/ChartFrame'

// The goal card: the fan of the scope's projection under one sentence that
// answers it, the month the median path reaches the goal or the extra monthly
// amount it needs. Shown only when the scope has a goal. A real or USD goal
// says so under the title, as the chart's amounts are in that unit; the extra
// monthly amount is always in the reporting currency, as the server computes it.
export function ProjectionCard({ portfolioId }: { portfolioId: PortfolioScope }) {
  const goal = useGoal(portfolioId).data
  const query = useProjection(portfolioId, Boolean(goal))
  if (!goal) return null
  const p = query.data

  const sentence = () => {
    if (!p) return null
    if (p.months.length === 0) return t('projection.tooLittleHistory')
    if (p.reached_at) {
      return t('projection.reached', {
        month: formatMarketDate(`${p.reached_at}-01`, { month: 'long', year: 'numeric' }),
      })
    }
    return t('projection.gap', { amount: formatMoney(p.monthly_gap, p.currency) })
  }

  return (
    <section id="projection" className="card scroll-mt-16">
      <div className="flex items-baseline justify-between gap-x-4 px-4 pt-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('projection.title')}</h2>
          {goal.unit !== 'nominal' && (
            <span className="text-xs text-muted">{t(`goal.unit.${goal.unit}`)}</span>
          )}
        </div>
        {p && (
          <b className="text-[14px] font-semibold text-ink tabular-nums">
            {progressPercent(p.progress)}
          </b>
        )}
      </div>
      <div className="p-4 pt-3">
        <ChartFrame query={query} height={200} errorTitle={t('projection.error')}>
          {p && (
            <>
              <p className="mb-3 text-[13px] text-ink-2">{sentence()}</p>
              {p.months.length > 0 && (
                <ProjectionChart months={p.months} amount={Number(p.amount)} />
              )}
            </>
          )}
        </ChartFrame>
      </div>
    </section>
  )
}
