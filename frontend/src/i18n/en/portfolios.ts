import type { trPortfolios } from '../tr/portfolios'
import type { AreaStrings } from '../types'

export const enPortfolios: AreaStrings<typeof trPortfolios> = {
  'portfolios.title': 'Portfolios',
  'portfolios.new': 'New portfolio',
  'portfolios.create': 'Create',
  'portfolios.name': 'Name',
  'portfolios.nameHint': 'At most 100 characters. Two portfolios cannot share a name.',
  'portfolios.created': 'Portfolio created',
  'portfolio.label': 'Portfolio',
  'portfolio.all': 'All portfolios',
  'portfolio.backToList': 'Back to portfolios',
  'portfolio.menu': 'Portfolio menu',
  'portfolio.tabs': 'Portfolio tabs',
  'portfolios.rename': 'Rename',
  'portfolios.renamed': 'Portfolio renamed',
  'portfolios.delete': 'Delete',
  'portfolios.deleteConfirm': 'Are you sure you want to delete this portfolio?',
  'portfolios.deleted': 'Portfolio deleted',
  'portfolios.deleteCascadeConfirm':
    'This portfolio has {count} transactions. All of them will be deleted. This cannot be undone. Are you sure?',
  'portfolios.deleteCascadeConfirm.one':
    'This portfolio has {count} transaction. It will be deleted. This cannot be undone. Are you sure?',
  'portfolios.deleteCascadeButton': 'Delete the portfolio and {count} transactions',
  'portfolios.deleteCascadeButton.one': 'Delete the portfolio and {count} transaction',
  'portfolios.share': 'Share',
  'portfolios.trend': 'Trend',
  'portfolios.emptyTitle': 'You have no portfolios yet',
  'portfolios.emptyBody':
    'Create a portfolio and add your first transaction; value and profit/loss are tracked here.',
  'portfolios.emptyCta': 'Create your first portfolio',
  'portfolios.error': 'Could not load portfolios',
  'portfolio.notFoundExplanation': 'This portfolio may have been deleted or never existed.',
}
