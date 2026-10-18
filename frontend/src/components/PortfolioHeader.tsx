import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { flushSync } from 'react-dom'
import { NavLink, useLocation, useNavigate } from 'react-router'
import { ApiError } from '../api'
import { useGoal } from '../api/goal'
import { useDeletePortfolio, usePortfolios, useRenamePortfolio } from '../api/portfolios'
import { progressPercent, useProjection } from '../api/projection'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { useNavState, type NavState } from '../lib/navState'
import { toast } from '../lib/toast'
import { focusIfLost } from '../lib/useFocusLater'
import { rangeSearch } from '../lib/useRange'
import { CreatePortfolioDialog } from './CreatePortfolioDialog'
import { GoalDialog } from './GoalDialog'
import { Button } from './ui/Button'
import { useConfirm } from './ui/useConfirm'
import { NotFound } from './NotFound'
import { inputClass } from './ui/inputClass'
import { Icon } from './ui/Icon'
import { Menu, MenuItem } from './ui/Menu'
import { Pill } from './ui/Pill'
import { Skeleton } from './ui/Skeleton'
import { TAP_ICON } from './ui/tap'

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

// The whole two-step delete behind one call: confirm, let the server
// refuse with 409 plus the transaction count, then confirm again naming that
// count before sending ?force=true. `error` is the message to render inline;
// `dialog` has to be rendered by the caller.
function useDeletePortfolioFlow() {
  const { confirm, dialog } = useConfirm()
  const mutation = useDeletePortfolio()
  const [error, setError] = useState<string | null>(null)

  async function remove(id: number): Promise<void> {
    setError(null)
    const confirmed = await confirm({
      title: t('portfolios.deleteConfirm'),
      confirmLabel: t('portfolios.delete'),
      danger: true,
    })
    if (!confirmed) {
      return
    }
    try {
      await mutation.mutateAsync({ id })
    } catch (refusal) {
      const count =
        refusal instanceof ApiError && refusal.status === 409
          ? Number(refusal.body.transaction_count)
          : NaN
      if (!Number.isInteger(count) || count <= 0) {
        setError(messageOf(refusal))
        return
      }
      const cascade = await confirm({
        title: t('portfolios.deleteConfirm'),
        body: t('portfolios.deleteCascadeConfirm', { count }),
        confirmLabel: t('portfolios.deleteCascadeButton', { count }),
        danger: true,
      })
      if (!cascade) {
        setError(messageOf(refusal))
        return
      }
      try {
        await mutation.mutateAsync({ id, force: true })
      } catch (cascadeError) {
        setError(messageOf(cascadeError))
      }
    }
  }

  return { remove, dialog, error, isPending: mutation.isPending }
}

// Below sm the tabs split the row between them, with the focus ring inside
// the tab, as the row may scroll and would clip it; from sm they sit at their
// natural width on the left. A label never wraps: a row too narrow for them
// scrolls instead.
const tabClass = ({ isActive }: { isActive: boolean }) =>
  `inline-flex h-10 flex-1 items-center justify-center gap-1.5 border-b-2 text-[14px] font-medium whitespace-nowrap max-sm:-outline-offset-2 sm:flex-none sm:justify-start ${
    isActive ? 'border-accent text-ink' : 'border-transparent text-muted hover:text-ink'
  }`

// Every page renders its own header, so a tab change unmounts the focused tab
// with the page and focus dropped to <body>. A tab activated while
// it had focus -- Enter, or a click that focused it -- notes its href here,
// and the header that mounts at that URL focuses its active tab instead.
let focusedTab: string | null = null

const tab = {
  className: tabClass,
  onClick(event: MouseEvent<HTMLAnchorElement>) {
    if (document.activeElement === event.currentTarget) {
      focusedTab = event.currentTarget.getAttribute('href')
    }
  },
}

// Below md the primary add button floats; the classes beat the Button's own
// height, padding, corner and shadow, which carry no variant.
const fabClass =
  'max-md:fixed max-md:right-4 max-md:bottom-4 max-md:z-20 max-md:size-14 max-md:rounded-full max-md:px-0 max-md:shadow-pop'

// The portfolio page header: which portfolio, which tab, and what can be done
// to the portfolio. Every portfolio page renders it, so the tab it sits on
// comes from the URL rather than from a prop. Switching portfolio is the
// sidebar's job.
//
// `onAdd` is the transactions page opening its own dialog. Without
// it -- on the summary page -- the button navigates to the transactions page
// and asks it to open that dialog on arrival. `add` says how it shows. By
// default (`float`) it is primary and, below md, floats at the bottom right.
// `quiet` renders it secondary in the header: an empty summary has
// its own primary add button in the page body. `inline` keeps it
// primary in the header: the import page pins its own action row to the
// bottom of the screen, which a floating button would cover.
//
// `children` is the page body. When the id is not in the user's portfolio
// list (a stale or foreign URL) the header and body give way to a not-found
// message, so no action is offered on a portfolio that is not there.
//
// In the `all` scope (every portfolio together, ADR 0014) the header has no
// portfolio actions but the goal, and its tab row holds Özet and Performans
// only; it offers a new portfolio instead, which opens once created, as
// from the sidebar.
//
// The header and the body share one <main>, so the header block is inside a
// landmark and pages render their body without a <main> of their own. The
// portfolio name is the page's h1, unless `ownHeading` says the
// page body has an h1 of its own: a position page once its summary is in (the
// symbol, or its not-found message), where the name is a plain paragraph, so
// the page has exactly one h1 while it loads or fails too.
export function PortfolioHeader({
  portfolioId,
  onAdd,
  add = 'float',
  ownHeading,
  children,
}: {
  portfolioId: PortfolioScope
  onAdd?: () => void
  add?: 'float' | 'quiet' | 'inline'
  ownHeading?: boolean
  children?: ReactNode
}) {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const { state, clear } = useNavState()
  const { data, isError } = usePortfolios()
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState('')
  // The portfolio a delete was tried on. The redirect to Tüm portföyler
  // remounts this header (App keys each portfolio page by the portfolio id),
  // so the attempt does not carry over there.
  const [removedId, setRemovedId] = useState<PortfolioScope | null>(null)
  const removeTried = removedId === portfolioId
  const [creating, setCreating] = useState(false)
  const [goaling, setGoaling] = useState(false)
  const menuRef = useRef<HTMLButtonElement>(null)
  const addRef = useRef<HTMLButtonElement>(null)
  const tabsRef = useRef<HTMLDivElement>(null)
  const renameMutation = useRenamePortfolio()
  const { remove, dialog, error: deleteError, isPending } = useDeletePortfolioFlow()

  const portfolio = data?.find((p) => p.id === portfolioId)
  // The goal's progress next to the name, a link to the goal card, which only
  // the Özet tab has.
  const onSummary = pathname === `/portfolios/${portfolioId}`
  const hasGoal = Boolean(useGoal(portfolioId, onSummary).data)
  const progress = useProjection(portfolioId, hasGoal && onSummary).data?.progress
  const isAll = portfolioId === 'all'
  const onPosition = pathname.startsWith(`/portfolios/${portfolioId}/positions/`)
  // The Özet tab keeps the chart range from Özet and its position pages, as
  // the sidebar does; İşlemler's from / to is not a chart range.
  const range = onPosition || pathname === `/portfolios/${portfolioId}` ? rangeSearch(search) : ''
  // While the list loads or fails, or the name is being edited, the h1 is
  // there for screen readers only, falling back to the generic label: every
  // state has exactly one h1.
  const heading = isAll ? t('portfolio.all') : (portfolio?.name ?? t('portfolio.label'))
  const named = !renaming && Boolean(portfolio || (isAll && data))
  const Title = ownHeading ? 'p' : 'h1'
  const floatAdd = !isAll && add === 'float'

  // The delete flow only reports failures (through `error`), so success is
  // read as the portfolio leaving the refetched list. Gated on an attempt so
  // a summary 404 still gets to show its own message rather than
  // being redirected away. The header, and the focus it held, unmount on the
  // way, so the state asks Tüm portföyler to place focus.
  useEffect(() => {
    if (removeTried && data && !portfolio) {
      void navigate('/portfolios/all', {
        replace: true,
        state: { focusAfterDelete: true } satisfies NavState,
      })
    }
  }, [removeTried, data, portfolio, navigate])

  // A portfolio created on Tüm portföyler opens here with the create
  // dialog's opener gone, so focus fell to <body>: the add button takes it,
  // once the refetched list names the portfolio, unless focus has moved on;
  // then the state is cleared so a remount does not do it again.
  const focusAfterCreate = Boolean(state?.focusAfterCreate)
  useEffect(() => {
    if (!focusAfterCreate || !portfolio) return
    focusIfLost(addRef.current)
    clear()
  }, [focusAfterCreate, portfolio, clear])

  // A tab row that scrolls sideways shows the open page's tab. It
  // sets the row's scrollLeft rather than calling scrollIntoView, which would
  // scroll the window too. The row's side padding stays beside the tab, so
  // it does not end flush at the screen edge. A row that fits has nothing to
  // scroll.
  useEffect(() => {
    const row = tabsRef.current
    const tab = row?.querySelector('[aria-current="page"]')
    if (!row || !tab) return
    const pad = parseFloat(getComputedStyle(row).paddingLeft) || 0
    const r = row.getBoundingClientRect()
    const a = tab.getBoundingClientRect()
    if (a.right + pad > r.right) row.scrollLeft += a.right + pad - r.right
    else if (a.left - pad < r.left) row.scrollLeft -= r.left - a.left + pad
  }, [pathname])

  useEffect(() => {
    if (focusedTab === pathname + search) {
      focusIfLost(tabsRef.current?.querySelector<HTMLElement>('[aria-current="page"]'))
    }
    focusedTab = null
  }, [pathname, search])

  function save() {
    setRenaming(false)
    const trimmed = name.trim()
    if (portfolio && trimmed && trimmed !== portfolio.name) {
      renameMutation.mutate(
        { id: portfolio.id, name: trimmed },
        { onSuccess: () => toast(t('portfolios.renamed')) },
      )
    }
  }

  // Enter saves through the blur handler; Escape puts the original name back
  // first, so the same blur is a no-op revert. The input unmounts with the
  // save, so focus goes back to the menu the rename came from, not to <body>.
  // The key's default is cancelled, or Enter's activation lands on
  // the newly focused menu button and opens the menu again.
  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key !== 'Enter' && event.key !== 'Escape') return
    event.preventDefault()
    const input = event.currentTarget
    if (event.key === 'Escape') {
      // Committed before the blur, so the blur handler sees the reverted name.
      flushSync(() => setName(portfolio?.name ?? ''))
    }
    flushSync(() => input.blur())
    menuRef.current?.focus()
  }

  if (data && !portfolio && !isAll) {
    // Just deleted from here: the effect above is on its way home.
    if (removeTried) return null
    return (
      <main id="main">
        <NotFound
          heading={t('error.portfolioNotFound')}
          explanation={t('portfolio.notFoundExplanation')}
          to="/"
          linkLabel={t('portfolio.backToList')}
        />
      </main>
    )
  }

  let nameSlot: ReactNode = null
  if (renaming) {
    nameSlot = (
      <input
        autoFocus
        aria-label={t('portfolios.name')}
        value={name}
        onChange={(event) => setName(event.target.value)}
        onBlur={save}
        onKeyDown={handleKeyDown}
        className={`mt-0.5 max-w-xl ${inputClass()}`}
      />
    )
  } else if (!named && isError) {
    // The list failed to load, so there is no name or switcher;
    // say why instead of leaving the slot blank.
    nameSlot = (
      <p role="alert" className="mt-1 text-[13px] text-loss">
        {t('portfolios.error')}
      </p>
    )
  } else if (!named && !data) {
    // Loading; once the list is in and the portfolio is not in it
    // (a 404, or a delete just done) the name is simply gone.
    nameSlot = <Skeleton className="mt-1 h-7 w-44" />
  }

  const menuTrigger = (
    <Button
      ref={menuRef}
      variant="ghost"
      iconOnly
      aria-label={t('portfolio.menu')}
      className={TAP_ICON}
    >
      <Icon name="dots" />
    </Button>
  )
  const goalItem = (
    <MenuItem onSelect={() => setGoaling(true)}>
      <Icon name="target" />
      {t('goal.set')}
    </MenuItem>
  )

  return (
    // Room below the last row for the floating add button.
    <main id="main" className={floatAdd ? 'max-md:pb-24' : undefined}>
      <div className="mx-auto max-w-6xl px-6 pt-6">
        {/* Below md the floating add button leaves the menu alone in the
            actions, so it stays on the title's row, at its end; the title
            truncates and the rename input takes the room left,
            up to a width that fits a long name. Tüm portföyler's one
            button sits there too. Below lg a shown name is for screen readers
            only, leaving the title block empty, so the gap waits for lg and
            stacked actions start at the top. */}
        <div
          className={`flex sm:flex-row sm:items-center sm:justify-between ${
            named ? 'lg:gap-3' : 'gap-3'
          } ${floatAdd || isAll ? 'flex-row items-center justify-between' : 'flex-col'}`}
        >
          <div className="flex min-w-0 flex-1 items-center gap-2">
            {(named || !ownHeading) && (
              <Title
                // Below lg the top bar shows the title, so the page's own is
                // for screen readers only there.
                className={
                  named ? 'truncate text-[20px] font-semibold text-ink max-lg:sr-only' : 'sr-only'
                }
                title={named ? heading : undefined}
              >
                {heading}
              </Title>
            )}
            {nameSlot}
            {named && onSummary && hasGoal && progress !== undefined && (
              <a
                href="#projection"
                aria-label={`${t('projection.title')} ${progressPercent(progress)}`}
                className="shrink-0"
              >
                <Pill tone={progress < 1 ? 'cash' : 'gain'}>{progressPercent(progress)}</Pill>
              </a>
            )}
          </div>

          {isAll && (
            <div className="flex shrink-0 items-center gap-2">
              <Button
                data-new-portfolio
                variant="secondary"
                className="shrink-0"
                onClick={() => setCreating(true)}
              >
                <Icon name="plus" />
                {t('portfolios.new')}
              </Button>
              <Menu trigger={menuTrigger}>{goalItem}</Menu>
            </div>
          )}
          {!isAll && (
            <div className="flex shrink-0 items-center gap-2">
              <Button
                ref={addRef}
                // Where the transactions page sends focus after deleting its
                // last row.
                data-add-transaction
                variant={add === 'quiet' ? 'secondary' : 'primary'}
                onClick={() =>
                  onAdd
                    ? onAdd()
                    : navigate(`/portfolios/${portfolioId}/transactions`, {
                        state: { add: true } satisfies NavState,
                      })
                }
                // Floating: a round `+` named by its hidden label, under the
                // toasts (z-50) and the modal dialogs (top layer).
                className={floatAdd ? fabClass : ''}
                // index.css keeps keyboard focus clear of it.
                data-fab={floatAdd || undefined}
              >
                <Icon name="plus" />
                <span className={floatAdd ? 'max-md:sr-only' : ''}>
                  {t('transactions.addTitle')}
                </span>
              </Button>
              <Menu trigger={menuTrigger}>
                <MenuItem
                  onSelect={() => {
                    setName(portfolio?.name ?? '')
                    setRenaming(true)
                  }}
                >
                  <Icon name="pencil" />
                  {t('portfolios.rename')}
                </MenuItem>
                {goalItem}
                <MenuItem
                  tone="danger"
                  disabled={isPending}
                  onSelect={() => {
                    // Set before the delete, so the refetched list never
                    // flashes the not-found message on the way home.
                    setRemovedId(portfolioId)
                    void remove(portfolioId)
                  }}
                >
                  <Icon name="trash" />
                  {t('portfolios.delete')}
                </MenuItem>
              </Menu>
            </div>
          )}
        </div>

        {renameMutation.isError && (
          <p role="alert" className="mt-1 text-[13px] text-loss">
            {renameMutation.error.message}
          </p>
        )}
        {deleteError && (
          <p role="alert" className="mt-1 text-[13px] text-loss">
            {deleteError}
          </p>
        )}
      </div>
      {/* Below sm the tab row sticks under the top bar (h-12, z-20), so a
          long list can switch tabs without scrolling back up. It is
          a sibling of the page body rather than inside the wrapper above: a
          sticky box only sticks within its parent, and that one ends here.
          Full width, so the rule and the background span the page. A row
          too narrow for the tabs (six in English at 375 px) scrolls sideways
          rather than the page. The gap keeps neighbouring labels
          apart at every width. */}
      <nav
        aria-label={t('portfolio.tabs')}
        data-sticky-tabs
        className="mt-3 border-b border-line max-sm:sticky max-sm:top-12 max-sm:z-10 max-sm:bg-paper"
      >
        <div
          ref={tabsRef}
          className="mx-auto -mb-px flex max-w-6xl px-6 gap-3 max-sm:overflow-x-auto sm:gap-6"
        >
          {/* A position page is a drill-down of the summary, so the summary
              tab stays active under /positions/: dropping `end`
              there lets NavLink's prefix match mark it, aria-current and all. */}
          <NavLink to={`/portfolios/${portfolioId}${range}`} end={!onPosition} {...tab}>
            {t('summary.title')}
          </NavLink>
          <NavLink to={`/portfolios/${portfolioId}/performance`} {...tab}>
            {t('performance.title')}
          </NavLink>
          {!isAll && (
            <>
              <NavLink to={`/portfolios/${portfolioId}/transactions`} {...tab}>
                {t('transactions.title')}
              </NavLink>
              <NavLink to={`/portfolios/${portfolioId}/dividends`} {...tab}>
                {t('dividends.page.title')}
              </NavLink>
              <NavLink to={`/portfolios/${portfolioId}/deposits`} {...tab}>
                {t('deposits.tab')}
              </NavLink>
              <NavLink to={`/portfolios/${portfolioId}/plans`} {...tab}>
                {t('plans.tab')}
              </NavLink>
              <NavLink to={`/portfolios/${portfolioId}/import`} {...tab}>
                {t('import.title')}
              </NavLink>
            </>
          )}
        </div>
      </nav>
      {dialog}
      {creating && <CreatePortfolioDialog onClose={() => setCreating(false)} />}
      {goaling && <GoalDialog portfolioId={portfolioId} onClose={() => setGoaling(false)} />}
      {children}
    </main>
  )
}
