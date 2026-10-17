import { useState } from 'react'
import { Link } from 'react-router'
import type { PortfolioScope, Position } from '../api/summary'
import { t } from '../i18n/strings'
import { CONTRIBUTION_MODES, contributionRows, type ContributionMode } from '../lib/contribution'
import { plTone, signedMoney } from '../lib/format'
import { usePositionPath } from '../lib/useRange'
import { Segmented } from './ui/Segmented'
import { TAP_Y } from './ui/tap'

// How many of the best and of the worst the card shows before "Tümünü göster".
const LIMIT = 4

// The Katkı card: one bar per open position on a shared zero line,
// a gain to the right and a loss to the left, the longest spanning half the
// track, with the signed amount in the reporting currency at the row's end.
// Plain HTML, not Recharts: the card does not wait for the charts chunk, and
// the list with its numbers is the accessible content. Privacy mode masks the
// amounts; the bars keep their lengths. On the read-only `all` scope there is
// no position page, so the symbols are plain text, as in PositionsTable.
export function ContributionCard({
  positions,
  currency,
  portfolioId,
}: {
  positions: Position[]
  currency: string
  portfolioId: PortfolioScope
}) {
  const [mode, setMode] = useState<ContributionMode>('total')
  const [all, setAll] = useState(false)
  const positionPath = usePositionPath()
  const { rows, hidden } = contributionRows(positions, mode, all ? null : LIMIT)

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
                {portfolioId === 'all' ? (
                  <span className="font-semibold text-ink">{row.symbol}</span>
                ) : (
                  <Link
                    to={positionPath(portfolioId, row.instrumentId)}
                    className="font-semibold text-ink hover:underline"
                  >
                    {row.symbol}
                  </Link>
                )}
                <span aria-hidden="true" className="relative h-3.5">
                  <span className="absolute -inset-y-0.5 left-1/2 w-px bg-line-strong" />
                  <span
                    data-bar
                    className={`absolute inset-y-0 rounded-[3px] ${
                      row.value < 0 ? 'right-1/2 bg-loss' : 'left-1/2 bg-gain'
                    }`}
                    style={{ width: `${row.fraction * 50}%` }}
                  />
                </span>
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
