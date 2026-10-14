// A module-level store: a value, `get`/`set` for plain code and `use()` for
// components, no provider and no context. The theme, the privacy mode, the
// locale, the transactions page's pending deletes and the toast
// queue are built on it. `read` gives the first value; `onSet` runs on every `set`,
// before the subscribers re-render, for the store's side effect (the
// `data-theme` or `lang` attribute, the localStorage write).
import { useSyncExternalStore } from 'react'

export function createStore<T>(read: () => T, onSet?: (next: T) => void) {
  let value = read()
  const listeners = new Set<() => void>()
  const get = (): T => value
  const subscribe = (listener: () => void) => {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  }
  return {
    get,
    set(next: T): void {
      value = next
      onSet?.(next)
      listeners.forEach((notify) => notify())
    },
    use: (): T => useSyncExternalStore(subscribe, get),
  }
}

// localStorage that never throws: with storage disabled (private mode,
// blocked site data) nothing reads as stored, and a choice lasts for this
// page load only, in the store's value.
export function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function writeStored(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Storage disabled: see above.
  }
}
