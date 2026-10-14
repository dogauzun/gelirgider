import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { loginRedirectPath, useMe } from '../api/auth'
import { t } from '../i18n/strings'
import { LoadError } from './ui/LoadError'

// The inverse of ProtectedRoute: the login and register forms are for logged
// out visitors only. Without this a signed-in user reaching /login or
// /register (bookmark, back button, the footer links) can open a second
// session, or register a second account whose cookie silently replaces the
// first one.
export function GuestRoute({ children }: { children: ReactNode }) {
  const { data: me, isError, failureCount, refetch, isFetching } = useMe()
  const location = useLocation()

  if (me) {
    // Same target as LoginPage: a successful login refetches me, and this
    // redirect must not race the deep link back to "/".
    return <Navigate to={loginRedirectPath(location.state)} replace />
  }
  // Only a 401 (fetchMe returns null) means logged out. On a 5xx or network
  // error the visitor may hold a valid session, so do not offer the form.
  if (me === null) {
    return children
  }
  // A failing /api/me is retried for about seven seconds (shouldRetryQuery)
  // and has no data for that whole window, so rendering null leaves a blank
  // page. Once the first attempt has failed, say why instead, with
  // a retry, as nothing else refetches it but a tab switch.
  return isError || failureCount > 0 ? (
    <LoadError
      title={t('health.error')}
      error={null}
      onRetry={() => refetch()}
      retrying={isFetching}
      className="m-6"
    />
  ) : null
}
