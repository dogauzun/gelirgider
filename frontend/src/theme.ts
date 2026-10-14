// The theme choice is a `createStore` mirrored in localStorage, like the
// locale in `i18n/strings.ts`: no provider, no context, no library.
// `public/theme.js` applies the stored value before first paint, so a dark
// choice never flashes light; this module keeps it in sync afterwards.
import { createStore, readStored, writeStored } from './lib/createStore'

export type Theme = 'system' | 'light' | 'dark'
export const themes: readonly Theme[] = ['system', 'light', 'dark']

// ponytail: per-device, in the browser only, exactly like the locale.
// Multi-device sync, if ever wanted: a `theme` column on the user row.
// Nothing stored, or storage disabled: follow the OS.
function storedTheme(): Theme {
  const stored = readStored('theme')
  return stored === 'light' || stored === 'dark' ? stored : 'system'
}

// `system` means no attribute at all, which leaves the `prefers-color-scheme`
// block in index.css in charge.
function apply(next: Theme): void {
  if (next === 'system') {
    delete document.documentElement.dataset.theme
  } else {
    document.documentElement.dataset.theme = next
  }
}

const store = createStore(storedTheme, (next) => {
  apply(next)
  writeStored('theme', next)
})
apply(store.get())

export const getTheme = store.get
export const setTheme = store.set
export const useTheme = store.use
