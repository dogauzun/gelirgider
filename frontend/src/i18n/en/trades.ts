import type { trTrades } from '../tr/trades'
import type { AreaStrings } from '../types'

export const enTrades: AreaStrings<typeof trTrades> = {
  // The Realized trades card on the Performance tab.
  'trades.title': 'Realized trades',
  'trades.hint':
    "Each sale against the average cost, converted at the sale's date. Dividends and open positions are not included.",
  'trades.error': 'Trades failed to load',
  'trades.empty': 'No sales yet.',
  'trades.year': 'Year',
  'trades.allYears': 'All',
  'trades.count': 'Sales',
  'trades.winRate': 'Win rate',
  'trades.total': 'Total P/L',
  'trades.averageDays': 'Avg. holding',
  'trades.daysValue': '{count} days',
  'trades.daysValue.one': '{count} day',
  'trades.date': 'Date',
  'trades.instrument': 'Instrument',
  'trades.portfolio': 'Portfolio',
  'trades.quantity': 'Quantity',
  'trades.proceeds': 'Proceeds',
  'trades.cost': 'Cost',
  'trades.realized': 'P/L',
  'trades.days': 'Days',
  'trades.short': 'Short sale',
  'trades.missingRate': '{count} sales have no rate',
  'trades.missingRate.one': '{count} sale has no rate',
}
