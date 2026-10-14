import { useState } from 'react'
import type { Position } from '../api/summary'
import { t } from '../i18n/strings'
import {
  ALLOCATION_VIEWS,
  allocationRows,
  SHARE_FORMAT,
  type AllocationView,
} from '../lib/allocation'
import { decimalUnits } from '../lib/decimal'
import { formatCompactMoney, formatMoney, formatNumber } from '../lib/format'
import { AllocationDonut } from './LazyChart'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

// Allocation renders the portfolio's allocation as a donut beside a legend
// list (GLG-947): one row per position (or per market, or per currency, see
// AllocationView) and one for cash, from allocationRows. The legend is the
// accessible content, with each row's amount and share; the donut is
// decorative. Hovering a slice or a legend row highlights the pair.
// Positions without a reporting value are left out of the rows, so the
// shares always sum to 100% -- but a note below reports how many positions
// were left out for missing an FX rate, so the panel doesn't look like it
// covers the whole portfolio when it doesn't.
//
// `cash` is the reporting-currency total (already excludes any currency in
// `missingRates`, same as positions above); `cashByCurrency` is only used to
// name which currencies' cash got left out of that total, for the note.
// `cashByCurrencyReporting` is `cash` split by currency, for the currency
// view. `currency` is the reporting currency the amounts are in.
export function Allocation({
  positions,
  cash,
  cashByCurrency,
  cashByCurrencyReporting,
  missingRates,
  currency,
}: {
  positions: Position[]
  cash: string
  cashByCurrency: Record<string, string>
  cashByCurrencyReporting: Record<string, string>
  missingRates: string[]
  currency: string
}) {
  const [view, setView] = useState<AllocationView>('symbol')
  // The highlighted row, shared by the donut and the legend.
  const [active, setActive] = useState<string | null>(null)
  const { rows, total, other } = allocationRows({ positions, cash, cashByCurrencyReporting, view })

  // Positions with no FX rate for their currency (Summary.missing_rates)
  // have market_value_reporting: null and can't be compared to anything, so
  // they stay out of the donut -- but dropping them silently would make the
  // remaining slices sum to 100% over only part of the portfolio. Surface
  // the count instead.
  const omittedCount = positions.filter((p) => p.market_value_reporting === null).length

  // Same defect, cash side: a currency in missingRates has no rate to convert
  // it into `cash` (the reporting total), so it's silently missing from that
  // total. Name the currencies instead of trying to sum amounts that aren't
  // in the same unit.
  const omittedCashCurrencies = Object.entries(cashByCurrency)
    .filter(([code, amount]) => missingRates.includes(code) && Number(amount) !== 0)
    .map(([code]) => code)

  if (rows.length === 0 && omittedCount === 0 && omittedCashCurrencies.length === 0) return null

  // The header count names every holding, not the folded legend.
  const itemCount = rows.length + Math.max(other.length - 1, 0)
  // Only positive cash is in the donut (GLG-662), so the header claims cash
  // only then (GLG-1323). The currency view folds each currency's cash into
  // that currency's row, so there any positive currency counts.
  const cashInDonut =
    view === 'currency'
      ? Object.values(cashByCurrencyReporting).some((amount) => decimalUnits(amount) > 0n)
      : rows.some((row) => row.key === 'cash')

  return (
    // The header of the other Özet cards (GLG-934).
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-line px-4 py-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('allocation.title')}</h2>
          {rows.length > 0 && (
            <span className="text-xs text-muted">
              {t(cashInDonut ? 'allocation.count' : 'allocation.countNoCash', {
                count: itemCount,
              })}
            </span>
          )}
        </div>
        <Segmented
          options={ALLOCATION_VIEWS}
          value={view}
          onChange={setView}
          label={(option) => t(`allocation.view.${option}`)}
          aria-label={t('allocation.view.label')}
          itemClassName={TAP_Y}
        />
      </div>
      <div className="@container p-4">
        {rows.length === 0 ? (
          <p className="text-sm text-muted">{t('allocation.unavailable')}</p>
        ) : (
          // The legend goes under the donut until the card itself is 28rem
          // wide (GLG-1027): from lg the card is half the page, and a
          // viewport breakpoint left the names 0 px wide beside the donut.
          <div className="flex flex-col items-center gap-4 @md:flex-row">
            <AllocationDonut
              rows={rows}
              currency={currency}
              totalLabel={formatCompactMoney(total, currency)}
              active={active}
              onActive={setActive}
            />
            <ul className="w-full min-w-0 flex-1 text-xs">
              {rows.map((row) => (
                <li
                  key={row.key}
                  title={row.key === 'other' ? other.join(', ') : undefined}
                  onMouseEnter={() => setActive(row.key)}
                  onMouseLeave={() => setActive(null)}
                  className={`flex items-center gap-2 rounded px-2 py-1 ${active === row.key ? 'bg-well' : ''}`}
                >
                  <span
                    aria-hidden="true"
                    className="size-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: row.color }}
                  />
                  <span className="min-w-0 flex-1 truncate text-ink-2">{row.label}</span>
                  <span className="text-muted tabular-nums">
                    {formatMoney(row.amount, currency)}
                  </span>
                  <span className="w-12 text-right font-medium text-ink tabular-nums">
                    {formatNumber(row.share, SHARE_FORMAT)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
        {omittedCount > 0 && (
          <p className="mt-3 text-xs text-muted">
            {t('allocation.missingRates', { count: omittedCount })}
          </p>
        )}
        {omittedCashCurrencies.length > 0 && (
          <p className="mt-1 text-xs text-muted">
            {t('allocation.missingRatesCash', { currencies: omittedCashCurrencies.join(', ') })}
          </p>
        )}
      </div>
    </section>
  )
}
