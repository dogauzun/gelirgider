import { useSyncExternalStore } from 'react'

// One media query as a store for useSyncExternalStore. Pages with a table
// from a breakpoint up and a phone list below render only the matching one:
// hiding the other with CSS still builds both, doubling
// the DOM.
function mediaStore(media: string) {
  return {
    subscribe(onChange: () => void) {
      const query = window.matchMedia(media)
      query.addEventListener('change', onChange)
      return () => query.removeEventListener('change', onChange)
    },
    matches: () => window.matchMedia(media).matches,
  }
}

// Tailwind's md breakpoint.
const desktop = mediaStore('(min-width: 48rem)')

export function useIsDesktop() {
  return useSyncExternalStore(desktop.subscribe, desktop.matches)
}

// Tailwind's xl breakpoint: from here up the content card is 1000 px wide
// whatever the sidebar does, room enough for the import review's Durum
// column.
const wide = mediaStore('(min-width: 80rem)')

export function useIsWide() {
  return useSyncExternalStore(wide.subscribe, wide.matches)
}
