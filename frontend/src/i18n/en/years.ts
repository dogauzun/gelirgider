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
  'years.view': 'View',
  'years.view.summary': 'Summary',
  'years.view.split': 'Split',
  'years.price': 'Price',
  'years.income': 'Income',
  'years.fx': 'Currency',
  'years.cashFx': 'Cash currency',
  'years.inflation': 'TÜFE',
  'years.splitHint': 'The four parts explain the gain; rounding can leave the sum a few cents off.',
}
