import { useState } from 'react'
import { useCashFlows } from '../api/cashflows'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { streaks, windowTotals } from '../lib/cashflows'
import { addDecimal } from '../lib/decimal'
import { todayIstanbul } from '../lib/date'
import { formatMoney } from '../lib/format'
import { CashFlowBars } from './LazyChart'
import { ChartFrame } from './ui/ChartFrame'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

type Span = '12' | '24' | 'all'
const SPANS: Span[] = ['12', '24', 'all']

// The "Nakit akışı" card on the Performans tab: the scope's money in and out
// per month as grouped bars (ADR 0035). "12 ay | 24 ay | Tümü" cuts the bars
// to the last months; it opens on 24 ay and is not stored. A scope with no
// transaction has no months. The header holds the saving streak and the longest
// run over the whole record, and a line under the bars sums the shown window.
export function CashFlowCard({ portfolioId }: { portfolioId: PortfolioScope }) {
  const [span, setSpan] = useState<Span>('24')
  const query = useCashFlows(portfolioId)
  const { data } = query
  const months = data?.months ?? []
  const shown = span === 'all' ? months : months.slice(-Number(span))
  const run = streaks(months, todayIstanbul())
  const figure = (label: string, count: number) => (
    <span className="flex items-baseline gap-1.5">
      <span className="text-muted">{label}</span>
      <b className="font-semibold tabular-nums text-ink">{t('cashflow.streakMonths', { count })}</b>
    </span>
  )

  // What the chart's place holds once the months are in.
  const body = () => {
    if (!data) return null
    if (data.months.length === 0) {
      return <p className="text-[13px] text-muted">{t('transactions.empty')}</p>
    }
    const totals = windowTotals(shown)
    const money = (amount: string) => formatMoney(amount, data.currency)
    return (
      <>
        <CashFlowBars months={shown} currency={data.currency} />
        <p data-testid="cashflow-totals" className="mt-2 text-xs text-muted tabular-nums">
          {t('cashflow.totals', {
            window: t(`cashflow.window.${span}`),
            deposits: money(totals.deposits),
            withdrawals: money(totals.withdrawals),
            net: money(totals.net),
            income: money(addDecimal(totals.dividends, totals.interest)),
            fees: money(totals.fees),
          })}
        </p>
        {data.missing_rates.length > 0 && (
          <p className="mt-2 text-xs text-muted">
            {t('cashflow.missingRates', { currencies: data.missing_rates.join(', ') })}
          </p>
        )}
      </>
    )
  }

  return (
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 pt-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('cashflow.title')}</h2>
          <span className="text-xs text-muted">{t('cashflow.subtitle')}</span>
        </div>
        {months.length > 0 && (
          <p data-testid="cashflow-figures" className="flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
            {figure(t('cashflow.streak'), run.current)}
            {figure(t('cashflow.longest'), run.longest)}
          </p>
        )}
        <Segmented
          options={SPANS}
          value={span}
          onChange={setSpan}
          label={(option) => t(`cashflow.window.${option}`)}
          aria-label={t('cashflow.window')}
          itemClassName={TAP_Y}
        />
      </div>
      <div className="p-4 pt-3">
        <ChartFrame query={query} height={200} errorTitle={t('cashflow.error')}>
          {body()}
        </ChartFrame>
      </div>
    </section>
  )
}
