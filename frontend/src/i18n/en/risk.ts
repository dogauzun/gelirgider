import type { trRisk } from '../tr/risk'
import type { AreaStrings } from '../types'

export const enRisk: AreaStrings<typeof trRisk> = {
  // The Risk card on the Performance tab.
  'risk.title': 'Risk',
  'risk.subtitle': 'from daily returns, annualized',
  'risk.window': 'Window',
  'risk.window.1y': '1Y',
  'risk.window.3y': '3Y',
  'risk.window.all': 'All',
  'risk.volatility': 'Volatility',
  'risk.volatility.hint': 'Standard deviation of the daily returns, annualized by √365',
  'risk.downside': 'Downside deviation',
  'risk.downside.hint': 'Deviation of the losing days only, against 0%',
  'risk.bestDay': 'Best day',
  'risk.worstDay': 'Worst day',
  'risk.days': '{count} daily returns',
  'risk.days.one': '{count} daily return',
  'risk.tooShort': 'Not enough data: at least 30 daily returns are needed.',
  'risk.beta': 'Beta',
  'risk.beta.hint': "The portfolio's average move when {benchmark} moves 1%",
  'risk.correlation': 'Correlation',
  'risk.correlation.hint': 'Between −1 and 1; 1 moves together',
  'risk.benchmarkVolatility': '{benchmark} volatility',
  'risk.noBenchmark': 'Pick a benchmark for beta and correlation.',
  'risk.benchmarkNoData': '{benchmark} has gaps in this window',
  'risk.error': 'Risk figures failed to load',
  'risk.position.hint': 'In {currency}, over the selected range, dividends counted',
  'risk.rolling.title': 'Rolling 1-year return',
  'risk.rolling.hint': 'For each day, the time-weighted return of the 365 days ending on it',
  'risk.rolling.line': 'Portfolio',
  'risk.rolling.empty': 'Less than a year of history: no rolling return yet.',
}
