import { lazy } from 'react'

// Whatever lazy() accepts as a component, read off its own signature.
type Component = Awaited<ReturnType<Parameters<typeof lazy>[0]>>['default']

// lazy() wants a default export; this picks a named one. The import() stays
// at the call site, so Vite still splits one chunk per module, and the
// component keeps its props type.
export function lazyNamed<M extends Record<K, Component>, K extends string>(
  load: () => Promise<M>,
  name: K,
) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}
