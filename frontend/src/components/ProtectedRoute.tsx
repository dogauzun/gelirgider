import { useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { loggedOutByUser, useMe } from '../api/auth'
import { t } from '../i18n/strings'
import type { NavState } from '../lib/navState'
import { LoadError } from './ui/LoadError'

// `guest` is what a signed-out visitor gets instead of the login redirect:
// the signed-out not-found page on an unknown URL (GLG-882).
export function ProtectedRoute({ children, guest }: { children: ReactNode; guest?: ReactNode }) {
  const { data: me, isError, failureCount, refetch, isFetching } = useMe()
  const location = useLocation()
  // Whether this page has seen a session. If it then turns null, the session
  // expired under the user, so the login page says why (GLG-801); a visitor
  // who was never signed in, or who just logged out, gets no notice (GLG-833).
  const [hadSession, setHadSession] = useState(false)
  if (me && !hadSession) {
    setHadSession(true)
  }

  if (me) {
    return children
  }
  // Only a 401 (fetchMe returns null) means logged out. A 5xx or network
  // error is not a logout: redirecting would drop a valid session onto the
  // login form (GLG-330). This is also where an expired session lands
  // (GLG-379); `from` brings the user back here after login (GLG-380). After
  // a logout the user chose there is nothing to resume, and the next login
  // may be someone else's, so it starts at home (GLG-843).
  if (me === null) {
    if (guest !== undefined) {
      return guest
    }
    if (loggedOutByUser()) {
      return <Navigate to="/login" replace />
    }
    const notice = hadSession ? 'auth.login.sessionExpired' : undefined
    return <Navigate to="/login" replace state={{ from: location, notice } satisfies NavState} />
  }
  // Still loading, or /api/me failed. A failing /api/me is retried for about
  // seven seconds with no data, so once an attempt has failed say why instead
  // of rendering nothing (GLG-154). After that nothing refetches it but a tab
  // switch, so offer a retry (GLG-1263).
  if (isError || failureCount > 0) {
    return (
      <LoadError
        title={t('health.error')}
        error={null}
        onRetry={() => refetch()}
        retrying={isFetching}
        className="m-6"
      />
    )
  }
  // While /api/me loads, assistive technology hears that the session is
  // loading (GLG-810); the screen stays on the plain ground, as the header
  // this bar once mimicked is gone (GLG-875).
  return (
    <div role="status" className="sr-only">
      {t('session.loading')}
    </div>
  )
}
