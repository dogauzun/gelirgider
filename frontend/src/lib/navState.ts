import { useCallback } from 'react'
import { useLocation, useNavigate } from 'react-router'
import type { StringKey } from '../i18n/strings'

// What rides along in react-router's location.state, one type for every
// writer (`satisfies NavState`) and reader: the add dialog (GLG-172), the
// import banner, focus after a portfolio is created or deleted (GLG-1324,
// GLG-857), and the login page's notice and deep link (GLG-400).
export type NavState = {
  add?: boolean
  imported?: number
  focusAfterCreate?: boolean
  focusAfterDelete?: boolean
  notice?: StringKey
  from?: unknown
} | null

// The state, and clear() to drop it in place so a remount does not act on it
// again. The caller picks the moment: the focus flags wait for a refetch.
export function useNavState() {
  const { pathname, search, state } = useLocation()
  const navigate = useNavigate()
  const clear = useCallback(() => {
    navigate(pathname + search, { replace: true, state: null })
  }, [navigate, pathname, search])
  // eslint-disable-next-line no-restricted-syntax -- the one read of router state
  return { state: state as NavState, clear }
}
