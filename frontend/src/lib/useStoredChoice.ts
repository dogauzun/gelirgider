import { useState } from 'react'

// A choice among `allowed` remembered in localStorage under `key`: the
// return method, the chart's "% | ₺" switch, the dividends grouping. A
// stored value outside `allowed` counts as `fallback`.
export function useStoredChoice<T extends string>(
  key: string,
  allowed: readonly T[],
  fallback: T,
): [T, (next: T) => void] {
  const [value, setValue] = useState<T>(() => {
    try {
      const stored = localStorage.getItem(key)
      if (allowed.includes(stored as T)) return stored as T
    } catch {
      // Storage disabled (private mode, blocked site data): use the default.
    }
    return fallback
  })
  const set = (next: T) => {
    setValue(next)
    try {
      localStorage.setItem(key, next)
    } catch {
      // Storage disabled: the choice lasts until the page is left.
    }
  }
  return [value, set]
}
