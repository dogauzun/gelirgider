import type { Trade } from '../api/trades'
import { addDecimal } from './decimal'

// The figures above the realized trades table. Amounts are summed exactly as
// decimal strings; a sell with no rate (null realized_reporting) is left out
// of the sum and counted in missingRates, so the total can say it is partial.
export function tradeStats(trades: Trade[]) {
  const count = trades.length
  const reporting = trades.flatMap((trade) =>
    trade.realized_reporting === null ? [] : [trade.realized_reporting],
  )
  const days = trades.flatMap((trade) => (trade.days === null ? [] : [trade.days]))
  return {
    count,
    winRate:
      count === 0 ? null : trades.filter((trade) => Number(trade.realized) > 0).length / count,
    realizedReporting: reporting.length === 0 ? null : reporting.reduce(addDecimal),
    missingRates: count - reporting.length,
    averageDays:
      days.length === 0 ? null : Math.round(days.reduce((a, b) => a + b, 0) / days.length),
  }
}

// The distinct years of the sells, newest first.
export function tradeYears(trades: Trade[]): string[] {
  return [...new Set(trades.map((trade) => trade.date.slice(0, 4)))].sort().reverse()
}
