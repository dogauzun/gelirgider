import { useCallback } from 'react'
import { useLocation, useNavigate } from 'react-router'
import type { StringKey } from '../i18n/strings'

// What rides along in react-router's location.state, one type for every
// writer (`satisfies NavState`) and reader: the add dialog, the
// import banner, focus after a portfolio is created or deleted, and the login
// page's notice and deep link.
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
    void navigate(pathname + search, { replace: true, state: null })
  }, [navigate, pathname, search])
  // eslint-disable-next-line no-restricted-syntax -- the one navState cast
  return { state: state as NavState, clear }
}
