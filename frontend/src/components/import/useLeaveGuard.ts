import { useContext, useEffect, useRef } from 'react'
import { UNSAFE_NavigationContext, type Navigator } from 'react-router'

type Guard = { when: boolean; ask: () => Promise<boolean>; released: boolean; asking: boolean }

const blocking = (guard: Guard) => guard.when && !guard.released

// The mounted guard's popstate handler. Chromium runs window listeners in the
// order they were added, capture or not, and the router's listener re-renders
// the app, unmounting the page and its listener, before a later one runs.
// This one is added when the module loads, before <BrowserRouter>
// mounts and adds the router's, so it always runs first.
let onPop: ((event: PopStateEvent) => void) | null = null
window.addEventListener('popstate', (event) => onPop?.(event))

// Asks once at a time; a second attempt while the dialog is open is dropped.
async function confirmLeave(guard: Guard, leave: () => void) {
  if (guard.asking) return
  guard.asking = true
  try {
    // leave() goes past the guard on its own: the router's own push, or a
    // redo the popstate listener lets through. The guard stays on for a page
    // that is still mounted after it.
    if (await guard.ask()) leave()
  } finally {
    guard.asking = false
  }
}

// Links, NavLinks and navigate() all go through the router navigator's push;
// returns the undo. A replace is the app's own redirect (after a portfolio
// delete, on logout) and goes through unasked.
function guardNavigator(navigator: Navigator, guard: Guard) {
  const { push } = navigator
  navigator.push = (...args) => {
    if (blocking(guard)) void confirmLeave(guard, () => push(...args))
    else push(...args)
  }
  return () => {
    navigator.push = push
  }
}

// Asks before the user leaves a page while `when` holds: `ask`
// for an in-app link or a push navigate() and for Back or Forward, the browser's own
// prompt for a reload or a tab close. react-router's useBlocker needs a data
// router and the app runs on <BrowserRouter>, so the guard sits on the
// router's navigator and on popstate itself.
// ponytail: no data router; move to useBlocker if the app ever adopts one.
//
// Returns `release`, which lets the page's own way out through, e.g. the
// navigation after a successful commit.
export function useLeaveGuard(when: boolean, ask: () => Promise<boolean>) {
  const { navigator } = useContext(UNSAFE_NavigationContext)
  const latest = useRef<Guard>({ when, ask, released: false, asking: false })
  useEffect(() => {
    latest.current.when = when
    latest.current.ask = ask
  })
  // The history index of this page's entry, to tell how far a popstate went.
  const index = useRef<number | null>(null)
  useEffect(() => {
    // eslint-disable-next-line no-restricted-syntax -- History API state, not router state
    index.current = (window.history.state as { idx?: number } | null)?.idx ?? null
  })

  useEffect(() => guardNavigator(navigator, latest.current), [navigator])

  // Back and Forward have already moved the URL when popstate fires. The
  // listener above runs before the router's own one and keeps it from seeing
  // the move; the move is undone, and redone once the user agrees.
  useEffect(() => {
    const guard = latest.current
    let undoing = false
    let redoing = false
    // The redo of a move the user agreed to before its undo landed.
    let redoLater: (() => void) | null = null
    function onPopState(event: PopStateEvent) {
      if (redoing) {
        redoing = false
        return
      }
      if (undoing) {
        undoing = false
        event.stopImmediatePropagation()
        redoLater?.()
        redoLater = null
        return
      }
      // eslint-disable-next-line no-restricted-syntax -- History API state, not router state
      const to = (event.state as { idx?: number } | null)?.idx
      if (!blocking(guard) || index.current === null || to == null || to === index.current) return
      const delta = to - index.current
      event.stopImmediatePropagation()
      undoing = true
      window.history.go(-delta)
      // The redo waits for the undo to land: two queued traversals can
      // race, and the undo's popstate must not be taken for the redo's.
      const redo = () => {
        redoing = true
        window.history.go(delta)
      }
      void confirmLeave(guard, () => {
        if (undoing) redoLater = redo
        else redo()
      })
    }
    function onBeforeUnload(event: BeforeUnloadEvent) {
      if (blocking(guard)) event.preventDefault()
    }
    onPop = onPopState
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      if (onPop === onPopState) onPop = null
      window.removeEventListener('beforeunload', onBeforeUnload)
    }
  }, [])

  return () => {
    latest.current.released = true
  }
}
