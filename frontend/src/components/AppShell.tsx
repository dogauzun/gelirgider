import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { Link, Outlet, useLocation, useMatch, useNavigationType, useParams } from 'react-router'
import { useMe } from '../api/auth'
import { usePortfolios } from '../api/portfolios'
import { t } from '../i18n/strings'
import { AccountMenu } from './AccountMenu'
import { ErrorBoundary } from './ErrorBoundary'
import { Mark } from './Mark'
import { PrivacyToggle, Sidebar } from './Sidebar'
import { Button } from './ui/Button'
import { Icon } from './ui/Icon'
import { TAP_ICON } from './ui/tap'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'

// The signed-in layout (ADR 0023): the sidebar from `lg`; below it a top bar
// with the menu button that opens the same sidebar as a drawer, and
// the routed page. The pages and PortfolioHeader own the <main>, so the shell
// adds none; so does the error fallback, which replaces the page's.
// `children` stands in for the routed page on a route that is not under the
// shell's layout route.
export function AppShell({ children }: { children?: ReactNode }) {
  const { pathname } = useLocation()
  const [open, setOpen] = useState(false)
  // Any navigation closes the drawer, a link in its account menu included;
  // the sidebar's own links close it through onNavigate, the open row too.
  const [openedAt, setOpenedAt] = useState(pathname)
  if (openedAt !== pathname) {
    setOpenedAt(pathname)
    setOpen(false)
  }

  // A link to another page starts it at the top; BrowserRouter
  // keeps the old offset, and its ScrollRestoration needs a data router.
  // Back and forward (POP) keep what the browser restores. Search params or
  // state on the same pathname, and the remount on a language switch, do not
  // scroll. Focus stays where it is.
  const navigationType = useNavigationType()
  const scrolledFor = useRef(pathname)
  useLayoutEffect(() => {
    if (scrolledFor.current === pathname) return
    scrolledFor.current = pathname
    if (navigationType !== 'POP') window.scrollTo(0, 0)
  }, [pathname, navigationType])

  return (
    <>
      {/* First tab stop: jumps past the navigation to the page's <main>. */}
      <a
        href="#main"
        className="sr-only rounded-ctl bg-surface px-3 py-2 text-[14px] font-medium text-ink shadow-pop focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50"
      >
        {t('nav.skip')}
      </a>
      <div className="lg:grid lg:grid-cols-[232px_minmax(0,1fr)]">
        <div className="sticky top-0 hidden h-dvh lg:block">
          <Sidebar />
        </div>
        <header className="sticky top-0 z-20 flex h-12 items-center gap-1 bg-surface px-2 shadow-ctl lg:hidden">
          <Button
            variant="ghost"
            iconOnly
            data-open-menu
            aria-label={t('nav.openMenu')}
            aria-expanded={open}
            onClick={() => setOpen(true)}
            className={`shrink-0 ${TAP_ICON}`}
          >
            <Icon name="menu" />
          </Button>
          <Link to="/portfolios/all" aria-label={t('app.name')} className="shrink-0 px-1">
            <Mark size={20} />
          </Link>
          <PageTitle />
          <PrivacyToggle />
          <AccountMenu />
        </header>
        <div className="min-w-0">
          {/* A page that fails to render leaves the shell, and with it the
              navigation, logout and privacy, in place; resetKey clears the
              error once the user goes to another page, without remounting a
              page that only changed its params (App keys each portfolio page
              by the portfolio id itself). */}
          <ErrorBoundary resetKey={pathname}>{children ?? <Outlet />}</ErrorBoundary>
        </div>
        {open && <Drawer onClose={() => setOpen(false)} />}
      </div>
    </>
  )
}

// Where the user is, in words: the open portfolio, or the page's own title,
// any other URL being the not-found page. A plain span, as the
// page owns the h1.
function PageTitle() {
  // Matched the router's way (any case, a trailing slash). `*` is set only by
  // App's catch-all route, so an unknown sub-path of a portfolio counts too.
  const notFound = useParams()['*'] !== undefined
  const match = useMatch('/portfolios/:id/*')
  const account = useMatch('/account')
  const admin = useMatch('/admin')
  const watchlist = useMatch('/watchlist')
  const { data } = usePortfolios()
  const { data: me } = useMe()
  const id = match?.params.id
  let title: string
  if (notFound) title = t('notFound.title')
  else if (id === 'all') title = t('portfolio.all')
  // An id the loaded list lacks is the not-found page, as in the tab title;
  // the app's name stands in while the list loads or has failed.
  else if (id !== undefined)
    title =
      data?.find((p) => String(p.id) === id)?.name ??
      (data === undefined ? t('app.name') : t('notFound.title'))
  else if (account) title = t('account.heading')
  else if (watchlist) title = t('watchlist.title')
  else if (admin && me?.is_admin) title = t('admin.heading')
  else title = t('notFound.title')
  return (
    <span
      title={title}
      className="mr-auto min-w-0 truncate px-1 text-[14px] font-semibold text-ink"
    >
      {title}
    </span>
  )
}

// The sidebar as a left drawer, mounted only while open. Not hidden from
// `lg` like the bar: a modal <dialog> hidden by CSS would leave the page
// inert after a tablet turns past the breakpoint with the drawer open.
function Drawer({ onClose }: { onClose: () => void }) {
  // The fallback is for an opener that never had focus: Safari does not
  // focus a clicked button.
  const dialogRef = useModal('[data-open-menu]')
  return (
    <Modal ref={dialogRef} aria-label={t('nav.label')} onClose={onClose}>
      <div className="fixed inset-y-0 left-0 w-[264px] bg-paper shadow-pop *:w-full">
        <Sidebar onNavigate={onClose} />
      </div>
    </Modal>
  )
}
