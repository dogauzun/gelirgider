import { useAttribution } from '../api/attribution'
import type { PortfolioScope } from '../api/summary'
import { t, type StringKey } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { plTone, signedMoney } from '../lib/format'
import { resolveRange, type RangeValue } from '../lib/ranges'
import { MissingRatesMark } from './MissingRatesMark'
import { SignedBar } from './SignedBar'
import { ChartFrame } from './ui/ChartFrame'

const ROWS = [
  { key: 'price', label: 'summary.returnSplit.price', hint: null },
  { key: 'income', label: 'summary.returnSplit.income', hint: 'summary.returnSplit.incomeHint' },
  { key: 'fx', label: 'summary.returnSplit.fx', hint: null },
  { key: 'cash_fx', label: 'summary.returnSplit.cashFx', hint: null },
] as const satisfies readonly { key: string; label: StringKey; hint: StringKey | null }[]

// The "Getiri dağılımı" card under the return card: how the range's return
// splits into price, income, currency and cash currency (ADR 0027), one
// SignedBar per bucket with its signed amount in the reporting currency. The
// longest bar spans half the track. A bucket with no rate to convert it shows
// MISSING and the missing-rates mark. Privacy mode masks the amounts; the bars
// keep their lengths. A range with no holdings reads four zeros.
export function ReturnSplitCard({ scope, range }: { scope: PortfolioScope; range: RangeValue }) {
  const { from, to } = resolveRange(range, todayIstanbul())
  const query = useAttribution(scope, from, to)
  const data = query.data
  const max = data
    ? Math.max(0, ...ROWS.map((row) => Math.abs(Number(data.total[row.key] ?? 0))))
    : 0

  return (
    <section className="card">
      <div className="border-b border-line px-4 py-3">
        <h2 className="text-[14px] font-semibold text-ink">{t('summary.returnSplit.title')}</h2>
      </div>
      <div className="p-4">
        <ChartFrame query={query} height={110} errorTitle={t('summary.returnSplit.error')}>
          {data && (
            // A subgrid, as in ContributionCard, so the zero lines line up.
            <ul className="grid grid-cols-[auto_minmax(0,1fr)_auto] gap-x-3 gap-y-2 text-xs">
              {ROWS.map((row) => {
                const amount = data.total[row.key]
                return (
                  <li key={row.key} className="col-span-3 grid grid-cols-subgrid items-center">
                    <span className="flex flex-col">
                      <span className="font-semibold text-ink">{t(row.label)}</span>
                      {row.hint && <span className="text-muted">{t(row.hint)}</span>}
                    </span>
                    <SignedBar
                      negative={Number(amount) < 0}
                      fraction={max > 0 ? Math.abs(Number(amount)) / max : 0}
                    />
                    <span className={`text-right tabular-nums ${plTone(amount)}`}>
                      {signedMoney(amount, data.currency)}
                      {amount === null && (
                        <MissingRatesMark
                          currencies={data.missing_rates}
                          currency={data.currency}
                        />
                      )}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </ChartFrame>
      </div>
    </section>
  )
}
