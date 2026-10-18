import { useState } from 'react'
import { Link } from 'react-router'
import { useAttribution } from '../api/attribution'
import type { PortfolioScope, Position } from '../api/summary'
import { t } from '../i18n/strings'
import {
  CONTRIBUTION_MODES,
  contributionRows,
  dayRemainder,
  type ContributionInput,
  type ContributionMode,
} from '../lib/contribution'
import { todayIstanbul } from '../lib/date'
import { plTone, signedMoney } from '../lib/format'
import { positionLabel, unrealized } from '../lib/positions'
import { resolveRange } from '../lib/ranges'
import { usePositionPath, useRange } from '../lib/useRange'
import { SignedBar } from './SignedBar'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

// How many of the best and of the worst the card shows before "Tümünü göster".
const LIMIT = 4

// The rows of the `total` and `day` modes, from the open positions.
function positionInputs(positions: Position[], mode: 'total' | 'day'): ContributionInput[] {
  return positions.map((p) => ({
    instrumentId: p.instrument_id,
    symbol: positionLabel(p),
    plain: p.market === 'DEPOSIT',
    amount: mode === 'total' ? unrealized(p).amount : p.day_change_reporting,
  }))
}

// The Katkı card: one bar per open position on a shared zero line,
// a gain to the right and a loss to the left, the longest spanning half the
// track, with the signed amount in the reporting currency at the row's end.
// Plain HTML, not Recharts: the card does not wait for the charts chunk, and
// the list with its numbers is the accessible content. Privacy mode masks the
// amounts; the bars keep their lengths. On the read-only `all` scope there is
// no position page, so the symbols are plain text, as in PositionsTable. The
// `range` mode reads the attribution of the page's range, closed positions
// included; until it answers, or when it fails, the card reads as empty. The
// `day` mode adds an "Other / sold" row with the part of the summary's day
// change that no open row holds, so the card adds up to the figure above it.
export function ContributionCard({
  positions,
  currency,
  portfolioId,
  dayChange,
}: {
  positions: Position[]
  currency: string
  portfolioId: PortfolioScope
  // The summary's day_change, which also holds positions sold out on or after
  // the price date; the `day` mode shows what the open rows leave of it.
  dayChange: string | null
}) {
  const [mode, setMode] = useState<ContributionMode>('total')
  const [all, setAll] = useState(false)
  const positionPath = usePositionPath()
  const [range] = useRange()
  const { from, to } = resolveRange(range, todayIstanbul())
  const attribution = useAttribution(portfolioId, from, to, mode === 'range')
  const inputs =
    mode === 'range'
      ? (attribution.data?.positions ?? []).map((p) => ({
          instrumentId: p.instrument_id,
          symbol: p.symbol,
          amount: p.total,
        }))
      : positionInputs(positions, mode)
  const other = mode === 'day' ? dayRemainder(dayChange, inputs, t('contribution.other')) : null
  const { rows, hidden } = contributionRows(inputs, mode, all ? null : LIMIT, other)

  return (
    <section className="card">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-line px-4 py-3">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[14px] font-semibold text-ink">{t('contribution.title')}</h2>
          <span className="text-xs text-muted">{t('contribution.subtitle')}</span>
        </div>
        <Segmented
          options={CONTRIBUTION_MODES}
          value={mode}
          onChange={setMode}
          label={(option) => t(`contribution.mode.${option}`)}
          aria-label={t('contribution.mode.label')}
          itemClassName={TAP_Y}
        />
      </div>
      <div className="p-4">
        {rows.length === 0 ? (
          <p className="text-sm text-muted">{t('contribution.empty')}</p>
        ) : (
          // A subgrid, so every row's track and zero line line up whatever
          // the symbol and amount widths.
          <ul className="grid grid-cols-[auto_minmax(0,1fr)_auto] gap-x-3 gap-y-2 text-xs">
            {rows.map((row) => (
              <li key={row.instrumentId} className="col-span-3 grid grid-cols-subgrid items-center">
                {portfolioId === 'all' || row.other || row.plain ? (
                  <span className="font-semibold text-ink">{row.symbol}</span>
                ) : (
                  <Link
                    to={positionPath(portfolioId, row.instrumentId)}
                    className="font-semibold text-ink hover:underline"
                  >
                    {row.symbol}
                  </Link>
                )}
                {row.amount === null ? (
                  <span aria-hidden="true" />
                ) : (
                  <SignedBar negative={row.value < 0} fraction={row.fraction} />
                )}
                <span className={`text-right tabular-nums ${plTone(row.amount)}`}>
                  {signedMoney(row.amount, currency)}
                </span>
              </li>
            ))}
          </ul>
        )}
        {hidden > 0 && (
          <button
            type="button"
            onClick={() => setAll(true)}
            className="mt-3 text-[13px] font-medium text-accent hover:underline"
          >
            {t('contribution.showAll')}
          </button>
        )}
      </div>
    </section>
  )
}
