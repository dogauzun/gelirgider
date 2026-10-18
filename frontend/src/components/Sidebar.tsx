import { useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router'
import { useMe } from '../api/auth'
import { sumPortfolios, usePortfolios } from '../api/portfolios'
import { t } from '../i18n/strings'
import { formatCompactMoney } from '../lib/format'
import { rangeSearch } from '../lib/useRange'
import { setPrivacy, usePrivacy } from '../lib/privacy'
import { AccountMenu } from './AccountMenu'
import { CreatePortfolioDialog } from './CreatePortfolioDialog'
import { Mark } from './Mark'
import { MissingRatesMark } from './MissingRatesMark'
import { Button } from './ui/Button'
import { Icon } from './ui/Icon'
import { Skeleton } from './ui/Skeleton'
import { TAP_ICON } from './ui/tap'

// Everything after /portfolios/:id on the page header's tabs; the summary tab
// is the empty suffix, so it is not in the list. A portfolio row keeps the tab
// the user is on; from a position page it goes to the summary, as
// the instrument may not be held in the other portfolio.
const TAB_SUFFIXES = ['/performance', '/transactions', '/dividends', '/deposits', '/import']

const rowClass = ({ isActive }: { isActive: boolean }) =>
  `flex h-8 items-center gap-2 rounded-ctl px-2 text-[13px] font-medium ${
    isActive ? 'bg-raised text-ink shadow-ctl' : 'text-ink-2 hover:bg-chip'
  }`

const valueClass = 'ml-auto shrink-0 pl-2 text-[11.5px] font-normal text-muted'

// The brand mark and name, linking to Tüm portföyler, the home page.
// The label names the link even where the text is not shown.
function Brand({ onClick }: { onClick?: () => void }) {
  return (
    <Link
      to="/portfolios/all"
      aria-label={t('app.name')}
      onClick={onClick}
      className="flex shrink-0 items-center gap-2 text-[14px] font-semibold tracking-tight text-ink"
    >
      <Mark size={20} />
      <span>{t('app.name')}</span>
    </Link>
  )
}

// Masks every money amount while the app is shown to someone; the
// label names what a click does.
export function PrivacyToggle() {
  const privacy = usePrivacy()
  const label = t(privacy ? 'privacy.show' : 'privacy.hide')
  return (
    <Button
      variant="ghost"
      iconOnly
      aria-label={label}
      title={label}
      onClick={() => setPrivacy(!privacy)}
      className={`shrink-0 ${TAP_ICON}`}
    >
      <Icon name={privacy ? 'eye' : 'eyeOff'} />
    </Button>
  )
}

// The portfolio navigation (ADR 0023): every portfolio with its value, one
// click apart. `onNavigate` fires on every link click, for a drawer to close.
export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const { data: me } = useMe()
  const { data, isLoading } = usePortfolios()
  const { pathname, search } = useLocation()
  const [creating, setCreating] = useState(false)

  const onPosition = /^\/portfolios\/[^/]+\/positions\//.test(pathname)
  const tab = onPosition ? '' : (TAB_SUFFIXES.find((suffix) => pathname.endsWith(suffix)) ?? '')
  // A link to a portfolio's Özet keeps the chart range; the tabs' own
  // from / to (İşlemler's date filter) are not a chart range.
  const range = tab ? '' : rangeSearch(search)
  // In the first portfolio's currency, the sum of the list's totals. It
  // leaves out what any portfolio left out, so its mark carries the union of
  // their missing rates.
  const total =
    data && data.length > 0 ? (
      <span className={valueClass}>
        {formatCompactMoney(
          sumPortfolios(data, (p) => p.total_value),
          data[0].currency,
        )}
        <MissingRatesMark
          currencies={Array.from(new Set(data.flatMap((p) => p.missing_rates))).sort()}
          currency={data[0].currency}
        />
      </span>
    ) : null

  return (
    <nav
      aria-label={t('nav.label')}
      className="flex h-full w-[232px] flex-col gap-0.5 bg-paper px-2.5 py-3"
    >
      <div className="px-2 pb-3">
        <Brand onClick={onNavigate} />
      </div>
      {/* Tüm portföyler has Özet and Performans only. */}
      <NavLink
        to={`/portfolios/all${tab === '/performance' ? tab : ''}${range}`}
        onClick={onNavigate}
        className={rowClass}
      >
        <Icon name="layers" className="shrink-0 text-muted" />
        <span className="min-w-0 truncate">{t('portfolio.all')}</span>
        {total}
      </NavLink>

      <div className="flex items-center justify-between pt-3 pb-1 pl-2">
        <span className="text-[11px] font-medium text-muted">{t('nav.portfolios')}</span>
        <button
          type="button"
          aria-label={t('portfolios.new')}
          title={t('portfolios.new')}
          onClick={() => setCreating(true)}
          className="flex h-6 w-6 items-center justify-center rounded-ctl text-muted hover:bg-chip hover:text-ink"
        >
          <Icon name="plus" />
        </button>
      </div>

      {/* The only part that scrolls, so the bottom block stays in view. The
          inset leaves room for the active row's ring and the focus outline. */}
      <ul className="-mx-1 min-h-0 flex-1 overflow-y-auto px-1 py-0.5">
        {isLoading &&
          [0, 1, 2].map((index) => (
            <li key={index} className="flex h-8 items-center gap-2 px-2">
              <Skeleton className="h-2 w-2 rounded-full" />
              <Skeleton className="h-3 flex-1" />
            </li>
          ))}
        {data?.length === 0 && (
          <li className="px-2 pt-1">
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="text-left text-[13px] font-medium text-accent hover:underline"
            >
              {t('portfolios.emptyCta')}
            </button>
          </li>
        )}
        {data?.map((portfolio, index) => (
          <li key={portfolio.id} className="mb-0.5">
            {/* The link carries the current tab, so NavLink's prefix match
                marks the open portfolio on each of its pages, a position
                page included. */}
            <NavLink
              to={`/portfolios/${portfolio.id}${tab}${range}`}
              onClick={onNavigate}
              className={rowClass}
            >
              <span
                aria-hidden="true"
                className="mx-0.5 h-2 w-2 shrink-0 rounded-full"
                style={{ backgroundColor: `var(--color-cat-${(index % 6) + 1})` }}
              />
              <span className="min-w-0 truncate" title={portfolio.name}>
                {portfolio.name}
              </span>
              <span className={valueClass}>
                {formatCompactMoney(portfolio.total_value, portfolio.currency)}
                <MissingRatesMark
                  currencies={portfolio.missing_rates}
                  currency={portfolio.currency}
                />
              </span>
            </NavLink>
          </li>
        ))}
      </ul>

      <div className="flex flex-col gap-0.5 pt-2">
        {me?.is_admin && (
          <NavLink to="/admin" onClick={onNavigate} className={rowClass}>
            <Icon name="shield" className="shrink-0 text-muted" />
            {t('nav.admin')}
          </NavLink>
        )}
        <div className="flex items-center gap-1">
          <div className="min-w-0 flex-1">
            <AccountMenu wide />
          </div>
          <PrivacyToggle />
        </div>
        {/* Set by the image build; a local build is dev and shows nothing. */}
        {__APP_VERSION__ !== 'dev' && (
          <p className="px-2 text-[11px] text-muted">
            {t('nav.version', { version: __APP_VERSION__ })}
          </p>
        )}
      </div>

      {creating && (
        <CreatePortfolioDialog
          onClose={() => setCreating(false)}
          onCreated={() => onNavigate?.()}
        />
      )}
    </nav>
  )
}
