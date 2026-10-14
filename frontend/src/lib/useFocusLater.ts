import { useEffect, useRef } from 'react'

// Focus `target`, unless the user has already moved focus elsewhere: it moves
// only when focus was lost to <body>, or is still on `from`, the control the
// user acted on. A control that leaves the page with its row drops focus to
// <body>.
export function focusIfLost(target: HTMLElement | null | undefined, from: Element | null = null) {
  const active = document.activeElement
  // eslint-disable-next-line no-restricted-syntax -- the one focus recovery helper
  if (active === null || active === document.body || active === from) target?.focus()
}

// The key that takes over from `key` once its row is gone: the next of `keys`
// still there, else the previous one, else undefined.
export function neighbourOf<K>(keys: K[], key: K, stillThere: (key: K) => boolean = () => true) {
  const index = keys.indexOf(key)
  return keys.slice(index + 1).find(stillThere) ?? keys.slice(0, index).reverse().find(stillThere)
}

// An action whose control leaves the page with it -- a batch step, a fixed
// row leaving Sorunlu -- dropped focus to <body>. `focusLater`
// moves focus, after the render the action's state change causes, to the
// first of `selectors` on the page. The target may come later, as the next
// file's review waits for its parse, so the request waits until one is there.
// Focus moves only when it was lost, or is still on `from`, the control the
// user acted on, which React may keep for the next file's review: an answer
// that lands after the user moved on (a parse, a TEFAS lookup) leaves focus
// wherever they went. `from` is taken at the action, never when the answer
// lands.
export function useFocusLater() {
  const pending = useRef<{ selectors: string[]; from: Element | null } | null>(null)
  useEffect(() => {
    const request = pending.current
    if (!request) return
    const target = request.selectors
      .map((selector) => document.querySelector<HTMLElement>(selector))
      .find((element) => element !== null)
    if (!target) return
    pending.current = null
    focusIfLost(target, request.from)
  })
  return (selectors: string[], from: Element | null = null) => {
    pending.current = { selectors, from }
  }
}
