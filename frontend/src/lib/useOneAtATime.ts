import { useRef } from 'react'
import { toast } from './toast'

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

// One guard for a click that runs a request and toasts the outcome. `failed`
// words the error toast; without it the raw message is shown.
export function useAction() {
  const once = useOneAtATime()
  return (request: () => Promise<unknown>, done: string, failed?: (message: string) => string) => {
    once(async () => {
      try {
        await request()
        toast(done)
      } catch (error) {
        const message = (error as Error).message
        toast(failed ? failed(message) : message, { error: true })
      }
    })
  }
}
