import {
  useMutation,
  useQuery,
  useQueryClient,
  type Query,
  type QueryClient,
} from '@tanstack/react-query'
import { apiFetch } from '../api'
import { getLocale } from '../i18n/strings'
import type { AttentionRules } from '../lib/attention'
import { clearToasts, flushToasts } from '../lib/toast'
import { portfoliosQueryKey } from './portfolios'

export interface Me {
  id: number
  email: string
  reporting_currency: string
  is_admin: boolean
  totp_enabled: boolean
}

export const meQueryKey = ['me'] as const

function register(email: string, password: string): Promise<Me> {
  return apiFetch<Me>('/api/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password, locale: getLocale() }),
  })
}

// Public install config the login and register pages read before anyone
// is logged in.
export interface AuthConfig {
  registration_open: boolean
}

export const authConfigQueryKey = ['auth-config'] as const

// Whether an admin exists yet; SetupGate reads it on every page.
export const setupStatusQueryKey = ['setup-status'] as const

export function useAuthConfig() {
  return useQuery({
    queryKey: authConfigQueryKey,
    queryFn: () => apiFetch<AuthConfig>('/api/auth/config'),
  })
}

export function useSetupStatus() {
  return useQuery({
    queryKey: setupStatusQueryKey,
    queryFn: () => apiFetch<{ needs_setup: boolean }>('/api/setup/status'),
  })
}

// Step 1 of the setup wizard (ADR 0010): makes the first admin, or promotes an
// existing account whose password matches, and signs them in. mutate() takes
// no variables, so the password never lands in the mutation cache.
// `onCreated` runs once me and the setup status have refetched.
export function useSetupAdmin(
  admin: { token: string; email: string; password: string },
  onCreated: () => void,
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<Me>('/api/setup/admin', {
        method: 'POST',
        body: JSON.stringify({ ...admin, locale: getLocale() }),
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: meQueryKey }),
        queryClient.invalidateQueries({ queryKey: setupStatusQueryKey }),
      ])
      onCreated()
    },
  })
}

// Admin only.
function setRegistrationOpen(open: boolean): Promise<void> {
  return apiFetch<void>('/api/admin/registration', {
    method: 'PUT',
    body: JSON.stringify({ open }),
  })
}

export function useSetRegistrationOpen() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: setRegistrationOpen,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authConfigQueryKey }),
  })
}

// Where to go after logging in: the page ProtectedRoute sent the visitor away
// from (its location rides along in router state as `from`), search and hash
// included, else home. Only an in-app path: "//host" and "/\host" are read by
// the browser as another origin, and the auth pages would just bounce.
export function loginRedirectPath(state: unknown): string {
  // eslint-disable-next-line no-restricted-syntax -- login state is unknown until narrowed here
  const from = (state as { from?: { pathname?: unknown; search?: unknown; hash?: unknown } } | null)
    ?.from
  const pathname = from?.pathname
  if (
    typeof pathname !== 'string' ||
    !pathname.startsWith('/') ||
    pathname.startsWith('//') ||
    pathname.startsWith('/\\') ||
    pathname === '/login' ||
    pathname === '/register'
  ) {
    return '/'
  }
  const search = typeof from?.search === 'string' ? from.search : ''
  const hash = typeof from?.hash === 'string' ? from.hash : ''
  return pathname + search + hash
}

// Login's answer for a user with two-step verification on: no session yet,
// the token goes to /api/auth/login/totp with a code.
interface TotpChallenge {
  totp_required: true
  token: string
}

export function isTotpChallenge(result: Me | TotpChallenge | undefined): result is TotpChallenge {
  return result !== undefined && 'totp_required' in result
}

function login(email: string, password: string): Promise<Me | TotpChallenge> {
  return apiFetch<Me | TotpChallenge>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

// A challenge answer leaves the page on the code step: `onLoggedIn` runs, once
// me has refetched, only for a real session.
export function useLogin(onLoggedIn: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      login(email, password),
    onSuccess: async (result) => {
      if (isTotpChallenge(result)) return
      await queryClient.invalidateQueries({ queryKey: meQueryKey })
      onLoggedIn()
    },
  })
}

// Second login step. `onLoggedIn` runs once me has refetched.
export function useLoginTotp(onLoggedIn: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ token, code }: { token: string; code: string }) =>
      apiFetch<Me>('/api/auth/login/totp', {
        method: 'POST',
        body: JSON.stringify({ token, code }),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: meQueryKey })
      onLoggedIn()
    },
  })
}

// Register doesn't set a session cookie (only login does), so log the new
// user in with the same credentials right after. If that second call fails
// the account still exists: `onDone` gets false, and the caller sends them to
// the login page. Logged in, it runs once me has refetched.
export function useRegister(onDone: (loggedIn: boolean) => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({ email, password }: { email: string; password: string }) => {
      await register(email, password)
      try {
        await login(email, password)
        return true
      } catch {
        return false
      }
    },
    onSuccess: async (loggedIn) => {
      if (loggedIn) await queryClient.invalidateQueries({ queryKey: meQueryKey })
      onDone(loggedIn)
    },
  })
}

// Whether the last session ended by a logout the user chose. ProtectedRoute
// reads it so the null me that follows is not called an expired session: the
// move to /login is a transition, and the protected page can render that null
// me before it commits. The next signed-in /api/me clears it.
let loggedOut = false

export function loggedOutByUser(): boolean {
  return loggedOut
}

export async function logout(): Promise<void> {
  await apiFetch<void>('/api/auth/logout', { method: 'POST' })
  loggedOut = true
}

// A delete still waiting behind its undo toast is sent first, while the
// session lasts. Then me goes null before `onLoggedOut` runs, so
// GuestRoute does not bounce /login back home; the protected page may still
// see it before the caller's navigation commits, which logout() has told
// ProtectedRoute is no expired session.
export function useLogout(onLoggedOut: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      await flushToasts()
      await logout()
    },
    onSuccess: async () => {
      queryClient.setQueryData(meQueryKey, null)
      onLoggedOut()
      await queryClient.invalidateQueries({ queryKey: meQueryKey })
    },
  })
}

function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  return apiFetch<void>('/api/account/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

function updateReportingCurrency(currency: string): Promise<Me> {
  return apiFetch<Me>('/api/me/reporting-currency', {
    method: 'PUT',
    body: JSON.stringify({ reporting_currency: currency }),
  })
}

// mutate() takes no variables, so the passwords never land in the mutation
// cache. `onChanged` runs on success.
export function useChangePassword(
  currentPassword: string,
  newPassword: string,
  onChanged: () => void,
) {
  return useMutation({
    mutationFn: () => changePassword(currentPassword, newPassword),
    onSuccess: onChanged,
  })
}

export interface TotpSetup {
  secret: string
  uri: string
}

// Starts two-step enrolment (ADR 0025); two-step stays off until confirm. The
// password and code are hook arguments, not mutate() variables, so they never
// land in the mutation cache.
export function useTotpSetup(password: string) {
  return useMutation({
    mutationFn: () =>
      apiFetch<TotpSetup>('/api/account/totp/setup', {
        method: 'POST',
        body: JSON.stringify({ password }),
      }),
  })
}

interface RecoveryCodes {
  recovery_codes: string[]
}

// Confirm and disable change `totp_enabled`: `onDone` runs once me has refetched.
// The recovery codes go straight to `onCodes` and the mutation returns nothing,
// so they never sit in the mutation cache.
export function useTotpConfirm(
  code: string,
  onCodes: (codes: string[]) => void,
  onDone: () => void,
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const { recovery_codes } = await apiFetch<RecoveryCodes>('/api/account/totp/confirm', {
        method: 'POST',
        body: JSON.stringify({ code }),
      })
      onCodes(recovery_codes)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: meQueryKey })
      onDone()
    },
  })
}

// Replaces all recovery codes. `code` is an authenticator or a recovery code.
export function useTotpRegenerate(
  password: string,
  code: string,
  onCodes: (codes: string[]) => void,
) {
  return useMutation({
    mutationFn: async () => {
      const { recovery_codes } = await apiFetch<RecoveryCodes>('/api/account/totp/recovery-codes', {
        method: 'POST',
        body: JSON.stringify({ password, code }),
      })
      onCodes(recovery_codes)
    },
  })
}

export function useTotpDisable(password: string, code: string, onDone: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiFetch<void>('/api/account/totp', {
        method: 'DELETE',
        body: JSON.stringify({ password, code }),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: meQueryKey })
      onDone()
    },
  })
}

// The portfolio list's values are in the reporting currency, and the sidebar
// showing them stays mounted. Exact: the per-portfolio keys share
// the prefix and refetch on their own pages' mount. `onSaved` runs last.
export function useUpdateReportingCurrency(onSaved: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (currency: string) => updateReportingCurrency(currency),
    onSuccess: (updated) => {
      queryClient.setQueryData(meQueryKey, updated)
      // eslint-disable-next-line no-restricted-syntax -- the list only, not the portfolios under it
      void queryClient.invalidateQueries({ queryKey: portfoliosQueryKey, exact: true })
      onSaved()
    },
  })
}

// The user's attention rule overrides, `{}` when there are none. The summary
// page uses the defaults until it arrives or when it fails.
export const attentionRulesQueryKey = ['attention-rules'] as const

export function useAttentionRules() {
  return useQuery({
    queryKey: attentionRulesQueryKey,
    queryFn: () => apiFetch<AttentionRules>('/api/me/attention-rules'),
  })
}

// Replaces the whole override object, so the body holds overrides only.
// `onSaved` runs after the rules query is invalidated.
export function useUpdateAttentionRules(onSaved: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (rules: AttentionRules) =>
      apiFetch<AttentionRules>('/api/me/attention-rules', {
        method: 'PUT',
        body: JSON.stringify(rules),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: attentionRulesQueryKey })
      onSaved()
    },
  })
}

// GET /api/me returns 401 when logged out. That's a normal, expected
// response here, not a query error: report it as `null` instead of throwing.
async function fetchMe(): Promise<Me | null> {
  const res = await fetch('/api/me', { credentials: 'include' })
  if (res.status === 401) {
    return null
  }
  if (!res.ok) {
    throw new Error(`failed to load session: ${res.status}`)
  }
  loggedOut = false
  return res.json() as Promise<Me>
}

export function useMe() {
  return useQuery({ queryKey: meQueryKey, queryFn: fetchMe })
}

// Me, and the public queries: they hold no user's data, are the same for
// everybody, and SetupGate and the login, register and setup pages read them,
// so a change of user leaves them alone.
const keptQueryKeys: readonly unknown[] = [
  meQueryKey[0],
  authConfigQueryKey[0],
  setupStatusQueryKey[0],
]

// Everything else in the cache belongs to the user in me. When that user
// leaves, drop it so the next user never sees it rendered. Logged
// out (logout, an expired session): the protected pages unmount, so remove
// it; a query still on screen goes once its page has unmounted. The page can
// render once more before it does, and a query removed under it would be
// built and fetched again, without a session. Another account
// signed in from another tab: the mounted page stays, so reset it, which
// refetches what is on screen. The kept queries stay.
// Toasts go too, their held-back commits unsent but undone: logout has flushed them
// already, an expired session or another account would only see them fail
// or show them to the wrong user. On a login as well, for one that a late
// response added after the user left, and the cache is reset then
// too: a request sent without a session may still be in flight, and the next
// user's page would join it and get its 401.
export function dropDataOnUserChange(queryClient: QueryClient) {
  // undefined until /api/me first answers, null while signed out.
  let cachedUserId: number | null | undefined
  const queryCache = queryClient.getQueryCache()
  const unseen = (query: Query) =>
    !keptQueryKeys.includes(query.queryKey[0]) && query.getObserversCount() === 0
  queryCache.subscribe((event) => {
    if (event.type === 'observerRemoved' && cachedUserId === null && unseen(event.query)) {
      queryCache.remove(event.query)
    }
    if (event.type !== 'updated' || event.query.queryKey[0] !== meQueryKey[0]) return
    const me = event.query.state.data as Me | null | undefined
    if (me === undefined) return
    const id = me?.id ?? null
    if (id === cachedUserId) return
    clearToasts()
    if (cachedUserId !== undefined) {
      if (id === null) queryClient.removeQueries({ predicate: unseen })
      else
        void queryClient.resetQueries({
          predicate: (query) => !keptQueryKeys.includes(query.queryKey[0]),
        })
    }
    cachedUserId = id
  })
}
