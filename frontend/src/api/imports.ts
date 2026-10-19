import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { apiFetch, apiUpload } from '../api'
import { formatMarketDate } from '../lib/date'
import type { Instrument } from './instruments'
import { invalidatePortfolios, type TransactionType } from './transactions'

// Statement import, the two-step flow of ADR 0006: parse returns a preview and
// writes nothing, commit takes the rows the user kept.

// Per-row verdict of the parse endpoint. Only `ok` rows can be committed;
// `unknown_instrument` and `unsupported_currency` need a human,
// `skipped` is a row the parser deliberately dropped and `error` one it could
// not read.
export type PreviewStatus =
  'ok' | 'unknown_instrument' | 'skipped' | 'error' | 'unsupported_currency'

// One proposed operation. Everything but `key` and `status` is optional: a
// skipped row carries only a reason, an error row only its raw cells.
export interface PreviewRow {
  key: string
  status: PreviewStatus
  type?: TransactionType
  date?: string
  symbol?: string
  // The row's market hint; `TEFAS` lets the picker look a fund code up.
  market?: string
  instrument_id?: number
  quantity?: string
  price?: string
  amount?: string
  fee?: string
  currency?: string
  // The file gave no currency, so `currency` is the instrument's, TRY when
  // none: a pick switches it to the picked instrument's.
  currency_fallback?: boolean
  description?: string
  reason?: string
  section?: string
  code?: string
  value?: string
  message?: string
  raw?: string[]
  // Date of a hand-entered transaction this row likely repeats. The page
  // warns and starts the row unchecked.
  duplicate_of?: string
  // An error row whose symbol is unknown as well.
  unknown_instrument?: boolean
  // "alias" when no instrument matched the symbol and the instrument is the
  // one the user picked for it in an earlier commit; "name" when it
  // is the fund an earlier statement's balances printed under this name.
  resolved_by?: 'alias' | 'name'
  // The instrument such a row resolved to, as `symbol` stays the statement's
  // own.
  instrument_symbol?: string
  instrument_name?: string
  // A row the preview proposes for what the account held before the period:
  // a buy dated the day before period_start, or the deposit paying for those
  // buys. `approximate_price` marks such a buy priced with the statement's
  // period-end price, as no stored price existed for its date;
  // `cost_price` one priced with the average cost the statement prints for
  // the holding.
  opening?: boolean
  approximate_price?: boolean
  cost_price?: boolean
  // A sell the preview proposes, dated the day before period_start, for a
  // position the portfolio holds and the statement does not; it starts
  // unchecked, as the gap may be months not imported.
  closing?: boolean
}

// The statement's own period-end positions. Reconciliation only, never
// imported (ADR 0006). `avg_cost` is "0" when the statement prints none.
export interface Holding {
  symbol: string
  quantity: string
  avg_cost: string
  currency: string
}

// One symbol of the period-end comparison between the statement's snapshot
// and the positions the portfolio would hold once the preview's `ok` rows are
// committed. `statement_quantity` is null for a position the
// statement does not mention. Average cost is not compared. `instrument_id`
// is the instrument compared, absent for a holding that resolves to none; the
// page moves the line by it as the user edits the rows.
export interface Reconciliation {
  symbol: string
  instrument_id?: number
  statement_quantity: string | null
  portfolio_quantity: string
  match: boolean
}

// The period-end cash comparison in the statement currency: the balance the
// statement prints against the cash the portfolio would hold once the
// preview's `ok` rows are committed. `match` allows up to 0.01 of difference.
export interface CashReconciliation {
  currency: string
  statement_amount: string
  portfolio_amount: string
  match: boolean
}

// A notice about an earlier import. `code` is stable and translated by the
// page; `message` is the server's English text for an unknown code. An
// `overlap` does not block the commit; `same_period` is the same statement
// again, which the server refuses until that import is undone.
// `period` is a statement period the server refuses outright: `message` is
// the commit's error and `import_id` is 0.
export interface PreviewWarning {
  code: string
  message: string
  import_id: number
  period_start: string
  period_end: string
}

export interface StatementPreview {
  broker: string
  sha256: string
  period_start: string
  period_end: string
  currency: string
  warnings: PreviewWarning[]
  rows: PreviewRow[]
  holdings: Holding[]
  reconciliation: Reconciliation[]
  // Null when the statement printed no cash balance or no currency.
  cash_reconciliation: CashReconciliation | null
  // The fund names the statement printed next to a code, sent back as the
  // commit's names so later previews resolve them.
  names?: ImportAlias[]
}

// A checked preview row in transaction shape, which is all the commit
// endpoint knows about -- no symbols, no statuses, no broker specifics.
export interface ImportRow {
  type: TransactionType
  date: string
  instrument_id?: number
  quantity?: string
  price?: string
  // Left out for a trade edited by hand, so the server derives it.
  amount?: string
  fee: string
  currency: string
}

export interface ImportRequest {
  broker: string
  portfolio_id: number
  sha256: string
  // The preview's statement currency: with the period, it is what tells a
  // re-downloaded month from a new one.
  currency: string
  period_start: string
  period_end: string
  rows: ImportRow[]
  // The instruments the user picked for preview symbols that matched none;
  // the server remembers each for the broker's later previews, replacing an
  // earlier pick.
  aliases?: ImportAlias[]
  // The preview's names, learned without replacing a pick.
  names?: ImportAlias[]
}

export interface ImportAlias {
  symbol: string
  instrument_id: number
}

export interface ImportResult {
  id: number
  transaction_count: number
  period_start: string
  period_end: string
}

// `picks` are the instruments the user picked in this preview so far: the
// server resolves their symbols with them for this request only, so the
// opening rows and the reconciliation follow them.
function parseStatement(
  portfolioId: number,
  broker: string,
  file: File,
  {
    picks,
    names,
    signal,
  }: {
    picks?: ImportAlias[]
    // Fund names the batch's other statements print, taken as learned names
    // for this preview only.
    names?: ImportAlias[]
    signal?: AbortSignal
  } = {},
): Promise<StatementPreview> {
  const body = new FormData()
  body.append('broker', broker)
  body.append('portfolio_id', String(portfolioId))
  body.append('file', file)
  if (picks && picks.length > 0) body.append('picks', JSON.stringify(picks))
  if (names && names.length > 0) body.append('names', JSON.stringify(names))
  return apiUpload<StatementPreview>('/api/imports/parse', body, signal)
}

// One mutation serves every parse of the page, and only its newest call's
// answer is applied: each call aborts the one before, so quick picks never
// hold the browser's few connections to the host with parses nobody reads.
// The aborted call's AbortError reaches no one, as the mutation
// has moved on to the newer call.
export function useParseStatement(portfolioId: number) {
  const inFlight = useRef<AbortController | null>(null)
  return useMutation({
    mutationFn: ({
      broker,
      file,
      picks,
      names,
    }: {
      broker: string
      file: File
      picks?: ImportAlias[]
      names?: ImportAlias[]
    }) => {
      inFlight.current?.abort()
      const controller = new AbortController()
      inFlight.current = controller
      return parseStatement(portfolioId, broker, file, {
        picks,
        names,
        signal: controller.signal,
      })
    },
  })
}

const PARSES_IN_FLIGHT = 2

// Promise.allSettled over items, with at most PARSES_IN_FLIGHT calls of run
// pending at once, in item order.
async function settleLimited<T, R>(
  items: T[],
  run: (item: T) => Promise<R>,
): Promise<PromiseSettledResult<R>[]> {
  const results: PromiseSettledResult<R>[] = []
  let taken = 0
  async function worker() {
    while (taken < items.length) {
      const i = taken++
      results[i] = await run(items[i]).then(
        (value) => ({ status: 'fulfilled', value }),
        (reason: unknown) => ({ status: 'rejected', reason }),
      )
    }
  }
  await Promise.all(Array.from({ length: Math.min(PARSES_IN_FLIGHT, items.length) }, worker))
  return results
}

// Every file of a batch parsed once, results in file order. A single mutation
// cannot track parallel calls, so this is a plain function, not useMutation.
// At most PARSES_IN_FLIGHT at a time: each parse may fetch prices and write
// them, and the server runs those one at a time, so more only queue there.
export function useParseStatements(portfolioId: number) {
  return (broker: string, files: File[]) =>
    settleLimited(files, (file) => parseStatement(portfolioId, broker, file))
}

export function useCommitImport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (request: ImportRequest) =>
      apiFetch<ImportResult>('/api/imports', { method: 'POST', body: JSON.stringify(request) }),
    onSuccess: () => invalidateAfterImport(queryClient),
  })
}

// The picker's way out for a fund row named by its name: the server
// returns the TEFAS fund with this code, creating it when TEFAS confirms it,
// and answers 404 `fund not found` otherwise. Every instrument search is
// invalidated so the new fund shows up at once.
export function useLookupFund() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (code: string) =>
      apiFetch<Instrument>('/api/imports/funds', {
        method: 'POST',
        body: JSON.stringify({ code }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instruments'] }),
  })
}

// Broker keys the parse endpoint knows (ADR 0006). A proper name, so no i18n.
// `csv` is not a broker but a file format; it rides the same key.
// The form also offers `auto`, the default: the server picks the PDF parser
// that reads each file and answers with its key. Its label is
// translated, so it is not listed here.
export const BROKERS = [
  { key: 'midas', label: 'Midas' },
  { key: 'garanti', label: 'Garanti BBVA' },
  { key: 'garanti_yatirim', label: 'Garanti BBVA Yatırım' },
  { key: 'csv', label: 'CSV' },
]

// A past import carries the broker key the statement was parsed with; an
// unknown key (an import made by an older build) reads as the key itself.
export function brokerLabel(key: string): string {
  return BROKERS.find((broker) => broker.key === key)?.label ?? key
}

// What İşlemler calls an import (core pages spec): the broker and the
// statement's month, "Midas · Haz 2026", or its first and last month when it
// spans more. A statement in another currency says so, as the TRY and USD
// statements of a month are otherwise told apart by nothing.
export function importLabel(item: Import): string {
  const format = (iso: string) => formatMarketDate(iso, { month: 'short', year: 'numeric' })
  const start = format(item.period_start)
  const end = format(item.period_end)
  return [
    brokerLabel(item.broker),
    start === end ? end : `${start} – ${end}`,
    item.currency !== 'TRY' && item.currency,
  ]
    .filter(Boolean)
    .join(' · ')
}

// One past import, as `GET /api/imports` lists it. The endpoint
// answers with every import of the user, across portfolios.
export interface Import {
  id: number
  broker: string
  portfolio_id: number
  period_start: string
  period_end: string
  // The statement's account currency, "" when unknown.
  currency: string
  transaction_count: number
  created_at: string
}

export const importsQueryKey = ['imports'] as const

export function useImports() {
  return useQuery({
    queryKey: importsQueryKey,
    queryFn: () => apiFetch<Import[]>('/api/imports'),
  })
}

// Undo: the server deletes the import and every transaction it created, so
// the cache moves exactly as it did on the commit.
// `onDeleted` runs as soon as the import is gone, before the refreshes below,
// which can take a while: a preview open above reads itself again from there.
export function useDeleteImport(onDeleted?: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) => {
      await apiFetch<void>(`/api/imports/${id}`, { method: 'DELETE' })
      onDeleted?.()
    },
    onSuccess: () => invalidateAfterImport(queryClient),
  })
}

// An import is a batch of transactions, so it moves everything a single
// create moves: the list, the summary, the value history and the
// portfolio cards' P/L and cash -- plus the list of imports itself.
function invalidateAfterImport(queryClient: ReturnType<typeof useQueryClient>) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: importsQueryKey }),
    invalidatePortfolios(queryClient),
  ])
}
