import { Fragment, Suspense, useLayoutEffect, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Outlet, Routes, Route, useParams } from 'react-router'
import { setAdminRevokedHandler, setSessionExpiredHandler, shouldRetryQuery } from './api'
import { dropDataOnUserChange, meQueryKey, type Me } from './api/auth'
import { ErrorBoundary } from './components/ErrorBoundary'
import { GuestRoute } from './components/GuestRoute'
import { ProtectedRoute } from './components/ProtectedRoute'
import { lazyNamed } from './lib/lazyNamed'
import { toast } from './lib/toast'
import { focusIfLost } from './lib/useFocusLater'
import { t, useLocale } from './i18n/strings'
import { usePrivacy } from './lib/privacy'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage, SignedOutNotFoundPage } from './pages/NotFoundPage'
import { RegisterPage } from './pages/RegisterPage'
import { SetupGate, SetupPage } from './pages/SetupPage'

// The signed-in pages load as one chunk each, on first visit (GLG-1242), so
// the login page does not ship them. A navigation is a transition, so the
// page on screen stays until the next one's chunk arrives; only a cold load
// shows the fallback. The same ponytail as LazyChart.tsx: a failed fetch
// stays failed until a reload, and an ErrorBoundary shows it.
// The app shell and setup step 2 load the same way, so the login page ships
// neither the shell's account menu (Base UI Menu, floating-ui) nor the admin
// sections (GLG-1259).
const AppShell = lazyNamed(() => import('./components/AppShell'), 'AppShell')
const SetupConfigurePage = lazyNamed(
  () => import('./pages/SetupConfigurePage'),
  'SetupConfigurePage',
)
const AccountPage = lazyNamed(() => import('./pages/AccountPage'), 'AccountPage')
const AdminPage = lazyNamed(() => import('./pages/AdminPage'), 'AdminPage')
const DividendsPage = lazyNamed(() => import('./pages/DividendsPage'), 'DividendsPage')
const ImportPage = lazyNamed(() => import('./pages/ImportPage'), 'ImportPage')
const PerformancePage = lazyNamed(() => import('./pages/PerformancePage'), 'PerformancePage')
const PositionPage = lazyNamed(() => import('./pages/PositionPage'), 'PositionPage')
const SummaryPage = lazyNamed(() => import('./pages/SummaryPage'), 'SummaryPage')
const TransactionsPage = lazyNamed(() => import('./pages/TransactionsPage'), 'TransactionsPage')

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: shouldRetryQuery } },
})

// A call that finds the session gone re-checks /api/me: its 401 turns the
// cached me into null and ProtectedRoute redirects to /login. A refetch, not
// a straight null, so a stray 401 cannot log out a session that still works.
setSessionExpiredHandler(() => queryClient.invalidateQueries({ queryKey: meQueryKey }))

// A 403 "admin only" means the user was demoted while an admin page was open
// (GLG-1023). The server answered for the user in me, so drop the flag at
// once: the admin page and its sidebar link go, and the page's own error line
// with them, so the toast says why. Only on the first such answer: the
// admin page's other requests in flight would repeat it. Then re-check me.
setAdminRevokedHandler(() => {
  const me = queryClient.getQueryData<Me | null>(meQueryKey)
  if (me?.is_admin) {
    queryClient.setQueryData<Me>(meQueryKey, { ...me, is_admin: false })
    toast(t('error.adminOnly'), { error: true })
  }
  void queryClient.invalidateQueries({ queryKey: meQueryKey })
})

dropDataOnUserChange(queryClient)

// Where the focused element sits, as child indexes from <body>. The remount
// below rebuilds the same tree, so the same path finds its replacement.
function focusPath(): number[] | null {
  const path: number[] = []
  let node = document.activeElement
  // eslint-disable-next-line no-restricted-syntax
  if (node === null || node === document.body) return null
  // eslint-disable-next-line no-restricted-syntax
  while (node !== document.body) {
    const parent: Element | null = node.parentElement
    if (parent === null) return null
    path.unshift(Array.prototype.indexOf.call(parent.children, node))
    node = parent
  }
  return path
}

function PerPortfolio() {
  return (
    <Fragment key={useParams().id}>
      <Outlet />
    </Fragment>
  )
}

export function App() {
  // Every t() call site is a plain function call evaluated during render, so
  // a locale switch remounts the routes, app shell included (their key
  // changes), to re-run them all. The query cache and the router stay outside
  // the keyed subtree: cached data and the current URL survive the switch,
  // in-progress form input does not.
  const key = useLocale()
  // Privacy mode (GLG-564) is read by the format helpers during render too.
  // Subscribing here re-renders App, and with it every component below (none
  // is memoised), so they re-run without a remount: component state, such as
  // the range before a zoom or the Katkı card's choice, survives (GLG-1031).
  usePrivacy()

  // The remount destroys the focused control, the very button that made the
  // switch, and focus drops to <body> (GLG-681). Note where it was while the
  // old tree is still mounted, and focus its replacement after the commit.
  const [mounted, setMounted] = useState<{ key: string; focus: number[] | null }>({
    key,
    focus: null,
  })
  if (mounted.key !== key) setMounted({ key, focus: focusPath() })
  useLayoutEffect(() => {
    if (mounted.focus === null) return
    let node: Element | undefined = document.body
    for (const index of mounted.focus) node = node?.children[index]
    if (node instanceof HTMLElement) focusIfLost(node)
  }, [mounted])

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Fragment key={key}>
          <ErrorBoundary>
            <SetupGate>
              <Suspense
                fallback={
                  <div role="status" className="sr-only">
                    {t('page.loading')}
                  </div>
                }
              >
                <Routes>
                  <Route path="/setup" element={<SetupPage />} />
                  <Route
                    path="/setup/configure"
                    element={
                      <ProtectedRoute>
                        <SetupConfigurePage />
                      </ProtectedRoute>
                    }
                  />
                  <Route
                    path="/login"
                    element={
                      <GuestRoute>
                        <LoginPage />
                      </GuestRoute>
                    }
                  />
                  <Route
                    path="/register"
                    element={
                      <GuestRoute>
                        <RegisterPage />
                      </GuestRoute>
                    }
                  />
                  {/* Every signed-in page sits in the app shell (ADR 0023). */}
                  <Route
                    element={
                      <ProtectedRoute>
                        <AppShell />
                      </ProtectedRoute>
                    }
                  >
                    {/* Tüm portföyler is the home page (GLG-935). */}
                    <Route path="/" element={<Navigate to="/portfolios/all" replace />} />
                    <Route path="/account" element={<AccountPage />} />
                    <Route path="/admin" element={<AdminPage />} />
                    {/* The sidebar keeps the tab on a portfolio switch
                      (GLG-875), Back and Forward cross portfolios too, and
                      the router keeps the mounted page when only :id changes.
                      Every portfolio page is keyed by it, so a switch starts
                      the page over instead of carrying one portfolio's
                      preview, rows or open dialog into the other (GLG-1062).
                      No child takes the rest of the path, so an unknown
                      sub-path falls to the not-found route below. */}
                    <Route path="/portfolios/:id" element={<PerPortfolio />}>
                      <Route index element={<SummaryPage />} />
                      <Route path="performance" element={<PerformancePage />} />
                      <Route path="transactions" element={<TransactionsPage />} />
                      <Route path="positions/:instrumentId" element={<PositionPage />} />
                      <Route path="dividends" element={<DividendsPage />} />
                      <Route path="import" element={<ImportPage />} />
                    </Route>
                  </Route>
                  {/* An unknown URL: inside the shell when signed in, the
                    signed-out frame for a visitor (GLG-970), nothing while
                    the session loads (GLG-882). The same element as the
                    layout above, at the same place, so React keeps one shell
                    mounted between them. */}
                  <Route
                    element={
                      <ProtectedRoute guest={<SignedOutNotFoundPage />}>
                        <AppShell />
                      </ProtectedRoute>
                    }
                  >
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                </Routes>
              </Suspense>
            </SetupGate>
          </ErrorBoundary>
        </Fragment>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
