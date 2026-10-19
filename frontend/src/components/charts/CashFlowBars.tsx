import type { CashFlowBucket } from '../../api/cashflows'
import { t } from '../../i18n/strings'
import { formatMarketDate } from '../../lib/date'
import { formatMoney } from '../../lib/format'
import { GroupedBars } from './GroupedBars'

// CashFlowBars draws the cash flow months as grouped money bars on a zero
// line: deposits up in the gain color, withdrawals down in the loss color
// (ADR 0035). The tooltip lists the month's six fields from the exact
// strings; Number() is for drawing only.
export interface CashFlowBarsProps {
  months: CashFlowBucket[]
  currency: string
  height?: number
}

export function CashFlowBars({ months, currency, height = 200 }: CashFlowBarsProps) {
  const byPeriod = new Map(months.map((m) => [m.period, m]))
  const groups = months.map((m) => ({
    key: m.period,
    label: formatMarketDate(`${m.period}-01`, { month: 'short', year: '2-digit' }),
    title: formatMarketDate(`${m.period}-01`, { month: 'long', year: 'numeric' }),
    a: Number(m.deposits),
    // 0 - n, not -n: a month without withdrawals is 0, never -0.
    b: 0 - Number(m.withdrawals),
  }))
  const money = (v: string) => formatMoney(v, currency)

  return (
    <GroupedBars
      groups={groups}
      series={{
        a: {
          label: t('cashflow.deposits'),
          color: 'var(--color-gain)',
          className: 'series-deposits',
        },
        b: {
          label: t('cashflow.withdrawals'),
          color: 'var(--color-loss)',
          className: 'series-withdrawals',
        },
      }}
      unit="money"
      currency={currency}
      height={height}
      summaryKey="cashflow.chart.summary"
      tooltipRows={(g) => {
        const m = byPeriod.get(g.key)!
        return [
          { label: t('cashflow.deposits'), value: money(m.deposits), series: 'a' },
          { label: t('cashflow.withdrawals'), value: money(m.withdrawals), series: 'b' },
          { label: t('cashflow.net'), value: money(m.net) },
          { label: t('cashflow.dividends'), value: money(m.dividends) },
          { label: t('cashflow.interest'), value: money(m.interest) },
          { label: t('cashflow.fees'), value: money(m.fees) },
        ]
      }}
    />
  )
}
