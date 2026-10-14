import { ApiError, currencyMismatch } from '../../api'
import type { ImportAlias, PreviewRow, PreviewStatus } from '../../api/imports'
import type { Instrument } from '../../api/instruments'
import { buttonClass } from '../ui/buttonClass'

// A preview row plus what the user changes on the page: the check mark, and
// whether the row was typed or corrected by hand. Rows live in one
// state array keyed by `key` rather than in a Set of checked keys, because an
// edit rewrites the row itself, not just its selection. `estimated` marks a
// buy or sell from the dialog, whose amount is only the table's estimate.
// `alias` is the preview symbol of a row the user picked an instrument for
// inline: the commit sends the pick, so the next preview of the broker
// resolves that symbol by itself.
export type Row = PreviewRow & {
  checked: boolean
  manual?: boolean
  estimated?: boolean
  alias?: string
  // The currency of the instrument the user picked inline.
  instrument_currency?: string
}

// "İşlemlerde gör", on the Bitti list and in the history card: one secondary
// button look in both, 44 px tall on a phone.
export const viewImportClass = buttonClass({ variant: 'secondary', className: 'max-md:h-11' })

// One statement of a multi-file import. The broker, period and
// currency come from the upfront parse; `error` is why it was not imported,
// `reason` why the batch skipped it by itself, `count` how many
// transactions it brought in and `id` the import it made, for the link to
// its rows at the end. `undone` is an import undone since, which
// the page derives from the list of imports. `names` are the fund
// names its upfront parse printed, for the batch's other previews.
export type QueueItem = {
  file: File
  status: 'queued' | 'imported' | 'skipped' | 'failed' | 'undone'
  broker?: string
  period_start?: string
  period_end?: string
  currency?: string
  error?: Error
  reason?: string
  count?: number
  id?: number
  names?: ImportAlias[]
}

// The 409 of a parse or commit refused because the user imported this file
// already, into this portfolio or another one.
function alreadyImportedBody(error: unknown): Record<string, unknown> | undefined {
  return error instanceof ApiError &&
    error.status === 409 &&
    error.body.error === 'already_imported'
    ? error.body
    : undefined
}

// A parse refused because this file is imported into this portfolio already:
// nothing to do, so a batch skips it rather than failing it.
export function alreadyImported(error: unknown, portfolioId: number): boolean {
  const body = alreadyImportedBody(error)
  return (
    body !== undefined && (body.portfolio_id === undefined || body.portfolio_id === portfolioId)
  )
}

// The other portfolio a refused file is already imported into: a statement
// is one account, so it is a mistake to name, not a file to skip.
export function importedElsewhere(
  error: unknown,
  portfolioId: number,
): { id: number; name: string } | undefined {
  const body = alreadyImportedBody(error)
  if (typeof body?.portfolio_id !== 'number' || body.portfolio_id === portfolioId) return undefined
  return { id: body.portfolio_id, name: String(body.portfolio_name ?? '') }
}

// Rows that cannot be committed as they are: the user finishes them by hand
// through the edit action or the inline instrument picker.
export const NEEDS_ATTENTION: PreviewStatus[] = [
  'unknown_instrument',
  'unsupported_currency',
  'error',
]

// Choosing an instrument is the whole fix for an unknown symbol: the row's
// own amounts stay as the statement printed them. An unknown CSV row
// without a currency cell previews as TRY, so its currency becomes
// the instrument's, as keeping it failed the whole import on a USD pick.
// A currency the file gave stays, and a pick that contradicts it
// is an error row with the commit's message, as the server's parse with the
// pick has it. One pick fixes every unknown row
// of the statement's symbol and currency, and replaces an earlier
// pick the preview resolved them with. A row the commit would
// refuse for more than its currency gets its instrument but stays an error, left for the edit
// dialog. An ok row keeps its check mark, so a likely duplicate
// stays unchecked.
export function resolveRows(rows: Row[], key: string, instrument: Instrument): Row[] {
  const picked = rows.find((row) => row.key === key)
  return rows.map((row) => {
    if (
      row.key !== key &&
      !(
        (row.status === 'unknown_instrument' || row.unknown_instrument || row.resolved_by) &&
        row.symbol === picked?.symbol &&
        row.currency === picked?.currency
      )
    ) {
      return row
    }
    // A currency mismatch belongs to the instrument picked before: it is
    // checked again against this one.
    const mismatch = row.status === 'error' && currencyMismatch(row.message) !== undefined
    const resolved = {
      ...row,
      instrument_id: instrument.id,
      symbol: instrument.symbol,
      currency: row.currency_fallback ? instrument.currency : row.currency,
      instrument_currency: instrument.currency,
      alias: row.symbol,
      resolved_by: undefined,
      message: mismatch ? undefined : row.message,
      code: mismatch ? undefined : row.code,
    }
    if (row.status === 'error' && !mismatch) return { ...resolved, unknown_instrument: false }
    if (resolved.currency !== instrument.currency) {
      return {
        ...resolved,
        status: 'error' as const,
        code: 'invalid',
        message: `currency: must match instrument currency (${instrument.currency})`,
        checked: false,
      }
    }
    return {
      ...resolved,
      status: 'ok' as const,
      checked: row.status === 'ok' ? row.checked : true,
      manual: true,
    }
  })
}
