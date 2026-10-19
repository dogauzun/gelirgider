import type { trYears } from '../tr/years'
import type { AreaStrings } from '../types'

export const enYears: AreaStrings<typeof trYears> = {
  // The Yearly statement card on the Performance tab.
  'years.title': 'Yearly statement',
  'years.subtitle': "from the year's first day to its last",
  'years.hint':
    "Opening is the value on the year's first day; gain = closing − opening − net flow.",
  'years.error': 'Yearly statement failed to load',
  'years.year': 'Year',
  'years.opening': 'Opening',
  'years.netFlow': 'Net flow',
  'years.gain': 'Gain',
  'years.closing': 'Closing',
  'years.return': 'Return',
}
