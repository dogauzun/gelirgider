// Privacy mode masks every money amount on screen, for showing the app to
// someone (GLG-564). A `createStore` mirrored in localStorage, like `theme.ts`:
// no provider, no context. The format helpers read it at call time, and App
// subscribes to it so every amount re-renders when it flips, without a
// remount that would drop component state (GLG-1031).
import { createStore, readStored, writeStored } from './createStore'

// ponytail: per-device, in the browser only, like the theme and the locale.
// Nothing stored, or storage disabled: amounts visible.
const store = createStore(
  () => readStored('privacy') === 'on',
  (next) => writeStored('privacy', next ? 'on' : 'off'),
)

export const getPrivacy = store.get
export const setPrivacy = store.set
export const usePrivacy = store.use
