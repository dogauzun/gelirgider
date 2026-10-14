// Module-level store like `i18n/strings.ts`: `toast()` is a plain function any
// module can call without a provider, and `<Toaster/>` (ui/Toast)
// subscribes to it. Mounting `<Toaster/>` is the app shell's job.
import { createStore } from './createStore'

export type Toast = {
  id: number
  message: string
  undo?: () => void
  focus?: boolean
  error?: boolean
}

const MAX_VISIBLE = 3
const DISMISS_MS = 3000
// Long enough to read the message, find the button and press it.
const UNDO_MS = 8000

const store = createStore<readonly Toast[]>(() => [])
let nextId = 1
// Per toast: the time left and the running timeout. Pausing (hover or focus
// on a toast) stops every timeout and keeps what was left.
const timers = new Map<
  number,
  { remaining: number; started: number; handle?: ReturnType<typeof setTimeout> }
>()
// Runs when a toast goes without its undo being pressed: on timeout, when a
// newer toast pushes it out, or when the page is left or the user logs out.
// Not when the session has already gone: clearToasts drops it and runs the
// undo instead.
const commits = new Map<number, () => unknown>()
let paused = false

export function removeToast(id: number): void {
  clearTimeout(timers.get(id)?.handle)
  timers.delete(id)
  commits.delete(id)
  store.set(store.get().filter((item) => item.id !== id))
}

function expire(id: number): unknown {
  const commit = commits.get(id)
  removeToast(id)
  return commit?.()
}

function start(id: number): void {
  const timer = timers.get(id)
  if (!timer) return
  timer.started = Date.now()
  timer.handle = setTimeout(() => expire(id), timer.remaining)
}

// The Toaster pauses every timer while a toast is hovered or has focus.
export function pauseToasts(pause: boolean): void {
  if (pause === paused) return
  paused = pause
  for (const [id, timer] of timers) {
    if (pause) {
      clearTimeout(timer.handle)
      timer.remaining -= Date.now() - timer.started
    } else {
      start(id)
    }
  }
}

// `commit` is the action an undo toast holds back, such as a deferred delete:
// it runs unless the undo is pressed. It may return a promise, which
// flushToasts waits for. `focus` moves focus to the undo button, so a
// keyboard user reaches it at once and the timer waits while it has focus.
// `error` is for what failed: it stays until closed and is
// announced assertively, so a user who looked away still learns of it.
export function toast(
  message: string,
  options: { undo?: () => void; commit?: () => unknown; focus?: boolean; error?: boolean } = {},
): void {
  const item: Toast = {
    id: nextId++,
    message,
    undo: options.undo,
    focus: options.focus,
    error: options.error,
  }
  if (!options.error) {
    timers.set(item.id, { remaining: options.undo ? UNDO_MS : DISMISS_MS, started: 0 })
  }
  if (options.commit) commits.set(item.id, options.commit)
  store.set([...store.get(), item])
  // Errors stay out of the count: one arriving does not push out an undo
  // toast and commit its action early, and none is dropped unread.
  const timed = store.get().filter((old) => !old.error)
  if (timed.length > MAX_VISIBLE) expire(timed[0].id)
  if (!paused) start(item.id)
}

// Expires every toast now, running the commits held back, and resolves once
// they have settled. The Toaster calls it when the page is left and logout
// awaits it before ending the session, so a deferred action is not dropped
// or sent without a session. Repeats until none is left: a commit
// that fails adds an error toast, which would otherwise outlive the logout
// into the next session.
export async function flushToasts(): Promise<void> {
  while (store.get().length) {
    await Promise.allSettled(store.get().map((item) => expire(item.id)))
  }
}

// Drops every toast now without running the commits held back. For when
// the user has already gone (an expired session, another account signed in):
// a commit would only be sent without that user's session, and nothing on
// screen is meant for whoever logs in next. A dropped commit was
// never sent, so its undo runs, as if pressed: what the toast hid comes
// back rather than staying hidden for good.
export function clearToasts(): void {
  const undos = store
    .get()
    .filter((item) => commits.has(item.id))
    .map((item) => item.undo)
  timers.forEach((timer) => clearTimeout(timer.handle))
  timers.clear()
  commits.clear()
  store.set([])
  undos.forEach((undo) => undo?.())
}

export const useToasts = store.use
