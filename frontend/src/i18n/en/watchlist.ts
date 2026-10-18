import type { trWatchlist } from '../tr/watchlist'
import type { AreaStrings } from '../types'

export const enWatchlist: AreaStrings<typeof trWatchlist> = {
  // The Watchlist page.
  'watchlist.title': 'Watchlist',
  'watchlist.add': 'Add',
  'watchlist.loading': 'Loading the watchlist...',
  'watchlist.loadError': 'Could not load the watchlist',
  'watchlist.empty': 'Your watchlist is empty. Add an instrument to follow.',
  'watchlist.instrument': 'Instrument',
  'watchlist.lastPrice': 'Last price',
  'watchlist.day': 'Day',
  'watchlist.target': 'Target',
  'watchlist.note': 'Note',
  'watchlist.above': 'above',
  'watchlist.below': 'below',
  'watchlist.reached': 'Target reached',
  'watchlist.editTarget': 'Edit target for {symbol}',
  'watchlist.remove': 'Remove {symbol}',
  'watchlist.removeConfirm': 'Remove {symbol} from the watchlist?',
  'watchlist.removeButton': 'Remove',
  'watchlist.added': 'Added to the watchlist',
  'watchlist.targetSaved': 'Target saved',
  'watchlist.removed': 'Removed from the watchlist',
  'watchlist.removeFailed': 'Could not remove it. {error}',
  'watchlist.addDialog.title': 'Add to the watchlist',
  'watchlist.addDialog.submit': 'Add',
  'watchlist.targetDialog.title': '{symbol} target',
  'watchlist.targetDialog.field': 'Target price · {currency}',
  'watchlist.targetDialog.save': 'Save',
  'watchlist.targetDialog.clear': 'Clear target',
  // The Dikkat card's group of reached targets.
  'attention.group.watchlist': 'Watchlist',
  'attention.watch.above': 'above its {target} target: {last}',
  'attention.watch.below': 'below its {target} target: {last}',
  'attention.watch.open': 'Open list',
}
