import { useRef } from 'react'

// isPending reaches a component a tick after the click that started the
// request (TanStack's notifyManager flushes on setTimeout(0)), so the second
// click of a double click still found the button enabled and sent the request
// twice. `once(request)` sends the request unless the
// last one it sent is still running. Pass mutateAsync: as with mutate, a
// failure is left to the mutation's error state and onError.
export function useOneAtATime() {
  const running = useRef(false)
  return (request: () => Promise<unknown>) => {
    if (running.current) return
    // eslint-disable-next-line no-restricted-syntax -- the guard itself
    running.current = true
    request()
      .catch(() => {})
      .finally(() => {
        running.current = false
      })
  }
}
