import type { ExpectedDividend } from '../api/dividends'
import { formatDecimalInput } from '../lib/decimal'
import { emptyForm, type FormState } from './transactionForm'

// The dividend transaction a suggestion becomes: gross in amount, stopaj in
// fee, dated on the ex-date (ADR 0009). The expected dividends panel and
// Özet's attention card both book it.
export function expectedDividendForm(row: ExpectedDividend): FormState {
  return {
    ...emptyForm('dividend', row.date),
    instrumentId: row.instrument_id,
    instrumentLabel: row.symbol,
    instrumentSymbol: row.symbol,
    instrumentCurrency: row.currency,
    amount: formatDecimalInput(row.gross),
    fee: formatDecimalInput(row.stopaj),
  }
}
