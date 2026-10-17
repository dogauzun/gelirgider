import { t, type StringKey } from './i18n/strings'

// Server error strings (see backend writeError/validate calls) -> i18n keys.
// Field-prefixed validation messages ("fee: must be ...") are matched on the
// field name alone, everything else on the full message.
const messageKeys: Record<string, StringKey> = {
  'internal error': 'error.internal',
  unauthorized: 'error.unauthorized',
  'invalid request body': 'error.invalidRequestBody',
  'invalid email or password': 'error.invalidCredentials',
  'email already registered': 'error.emailRegistered',
  'registration closed': 'error.registrationClosed',
  'invalid setup token': 'error.invalidSetupToken',
  'invalid credentials': 'error.invalidCredentials',
  'setup already done': 'error.setupDone',
  setup_required: 'error.setupRequired',
  'invalid email': 'error.invalidEmail',
  'email must be at most 254 characters': 'error.emailTooLong',
  'password must be at least 8 characters': 'error.passwordTooShort',
  'password must be at most 72 bytes': 'error.passwordTooLong',
  'password must not be only whitespace': 'error.passwordBlank',
  'admin only': 'error.adminOnly',
  'last admin': 'error.lastAdmin',
  'cannot delete yourself': 'error.cannotDeleteSelf',
  'user not found': 'error.userNotFound',
  'unknown provider': 'error.unknownProvider',
  'not configured': 'error.providerNotConfigured',
  'stored key unreadable': 'error.providerKeyUnreadable',
  'already running': 'error.alreadyRunning',
  'unknown source': 'error.unknownSource',
  'source has no backfill': 'error.sourceHasNoBackfill',
  'backfill_from: must not be in the future': 'error.backfillFromFuture',
  'backfill_from: must be on or after 1990-01-01': 'transactions.error.dateTooEarly',
  'too many requests': 'error.tooManyRequests',
  'current password is incorrect': 'error.currentPasswordIncorrect',
  'portfolio not found': 'error.portfolioNotFound',
  'portfolio has transactions': 'error.portfolioHasTransactions',
  'name must be 1-100 characters': 'error.portfolioName',
  'name must not contain control characters': 'error.nameControl',
  'note: must not contain control characters': 'error.noteControl',
  'name already used': 'error.portfolioNameTaken',
  'instrument not found': 'error.instrumentNotFound',
  'market and currency must be a supported pair': 'error.instrumentPair',
  'symbol is required': 'error.instrumentSymbol',
  'name is required': 'error.instrumentName',
  'symbol must be at most 32 characters': 'error.instrumentSymbolLength',
  'name must be at most 300 characters': 'error.instrumentNameLength',
  'symbol must not contain control characters': 'error.instrumentSymbolControl',
  'transaction not found': 'error.transactionNotFound',
  'price not found': 'error.priceNotFound',
  'close must be a positive decimal': 'error.priceClose',
  'fee: must not exceed amount': 'transactions.error.feeExceedsAmount',
  'fee: must be 0 for a fee transaction': 'transactions.error.feeOnFee',
  'amount: must be 0 when price is 0': 'transactions.error.freeBuyAmount',
  'date must be YYYY-MM-DD': 'error.field.date',
  'date: must be on or after 1990-01-01': 'transactions.error.dateTooEarly',
  'date must be on or after 1990-01-01': 'transactions.error.dateTooEarly',
  'date: must not be in the future': 'transactions.error.dateFuture',
  'date must not be in the future': 'transactions.error.dateFuture',
  'from must be YYYY-MM-DD': 'error.dateRange',
  'to must be YYYY-MM-DD': 'error.dateRange',
  'from must not be after to': 'error.dateRange',
  'range exceeds 3660 days': 'error.dateRange',
  'limit must be a positive integer': 'error.limit',
  'file: larger than 5 MB': 'error.fileTooLarge',
  'file: required': 'error.fileRequired',
  'file: not a PDF': 'error.field.file',
  'file: not a readable PDF': 'error.field.file',
  // The Midas parser's only rejection (ADR 0006); fileNotStatement is the
  // broker-neutral fallback for any other broker.
  'file: no period row found: not a Midas statement': 'error.fileNotMidasStatement',
  // Automatic mode: no PDF parser accepted the file, and the user chose no
  // broker to check.
  'file: not a supported statement': 'error.fileNotSupportedStatement',
  // The CSV upload's own errors.
  'file: not a CSV': 'error.fileNotCsv',
  'file: not a readable CSV': 'error.fileNotCsv',
  'file: not a csv statement': 'error.fileNotCsvStatement',
  'file: no rows': 'error.fileNoRows',
  'file: more than 5000 rows': 'error.fileTooManyRows',
  'file: invalid upload': 'error.fileUnreadable',
  // The statement period checks of the import commit.
  'period_start: must be on or after 1990-01-01': 'error.periodTooEarly',
  'period_end: must not be in the future': 'error.periodFuture',
  'period_start: must not be in the future': 'error.periodFuture',
  'period_end: period must be shorter than a year': 'error.periodTooLong',
}

const fieldKeys: Record<string, StringKey> = {
  // Any other file error is a broker parser rejecting the document (ADR 0006).
  file: 'error.fileNotStatement',
  type: 'error.field.type',
  date: 'error.field.date',
  fee: 'error.field.fee',
  instrument_id: 'error.field.instrument_id',
  quantity: 'error.field.quantity',
  price: 'error.field.price',
  amount: 'error.field.amount',
  reporting_currency: 'error.field.reporting_currency',
  stale_manual_days: 'error.attentionRule',
  position_share: 'error.attentionRule',
  cash_share: 'error.attentionRule',
  try_cash_share: 'error.attentionRule',
  currency_share: 'error.attentionRule',
  movers: 'error.attentionRule',
  note: 'error.field.note',
  at: 'error.field.at',
  backfill_from: 'error.field.date',
  period_start: 'error.field.period',
  period_end: 'error.field.period',
}

// Field labels for the digit-limit message; fee uses feeLabel. close is the
// manual price endpoint.
const digitLabels: Record<string, StringKey> = {
  quantity: 'transactions.quantity',
  price: 'transactions.price',
  amount: 'transactions.amount',
  fee: 'transactions.fee',
  close: 'transactions.price',
}

// The instrument's currency in a "currency contradicts the instrument" error,
// the server's (backend checkInstrument) or the import page's own.
export function currencyMismatch(message: string | undefined): string | undefined {
  return /^currency: must match instrument currency \((\w+)\)$/.exec(message ?? '')?.[1]
}

// A commit error about one row (POST /api/imports): `rows[i].<field>: <message>`,
// where i counts the rows sent. Null for any other message.
export function rowError(message: string): { index: number; rest: string } | null {
  const match = /^rows\[(\d+)\]\.(.*)$/.exec(message)
  return match ? { index: Number(match[1]), rest: match[2] } : null
}

// Maps a server `error` string to its Turkish UI message. Unknown messages
// get a generic fallback so raw English never reaches the screen. `feeLabel`
// names the fee field: a dividend's fee is the withholding tax.
export function translateError(
  message: string | undefined,
  feeLabel: StringKey = 'transactions.fee',
): string {
  if (!message) return t('error.unknown')
  // The row error names the row by its index; the rest is an ordinary field
  // message.
  const row = rowError(message)
  if (row) return t('error.row', { n: row.index + 1, message: translateError(row.rest, feeLabel) })
  const period = /^date: must be within (.*)$/.exec(message)
  if (period) return t('error.dateOutsidePeriod', { period: period[1] })
  const currency = currencyMismatch(message)
  if (currency) return t('error.currencyMismatch', { currency })
  // A value past the digit limit names the limit, not "must be greater than
  // 0".
  const digits = /^(\w+): must have at most \d+ integer and (\d+) fraction digits$/.exec(message)
  if (digits && digits[1] in digitLabels) {
    const label = digits[1] === 'fee' ? feeLabel : digitLabels[digits[1]]
    return t('decimal.error.tooManyDigits', { field: t(label), fraction: digits[2] })
  }
  const key = messageKeys[message] ?? fieldKeys[message.split(': ')[0]]
  return t(key ?? 'error.unknown', { field: t(feeLabel) })
}

// Thrown by apiFetch on non-2xx responses. Carries the HTTP status so
// callers (e.g. the QueryClient's retry logic) can tell a 4xx -- which won't
// change on retry -- from a transient 5xx or network failure, plus the parsed
// error body for the endpoints that put detail next to `error` (the 409 from
// DELETE /api/portfolios/{id} carries `transaction_count`).
export class ApiError extends Error {
  status: number
  body: Record<string, unknown>

  constructor(message: string, status: number, body: Record<string, unknown> = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

// Generic JSON fetch wrapper for /api/*. Always sends/reads cookies, always
// JSON in/out, throws with the translated server `error` message on non-2xx.
export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  return (await apiFetchWithHeaders<T>(path, init)).data
}

// apiFetch, plus the response headers, for the endpoints that put paging
// metadata there (X-Next-Cursor on the transaction list).
export async function apiFetchWithHeaders<T>(
  path: string,
  init: RequestInit = {},
): Promise<{ data: T; headers: Headers }> {
  const res = await send(path, {
    ...init,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...init.headers },
  })

  if (!res.ok) throw await apiError(res)

  // Some endpoints (e.g. 204 No Content, or 200 with no body) return no
  // JSON at all -- parsing an empty string would throw, so treat it as T.
  const text = await res.text()
  return { data: (text ? JSON.parse(text) : undefined) as T, headers: res.headers }
}

// Multipart POST for the endpoints that take a file (the statement upload).
// Same cookies and same error translation as apiFetch, minus the
// JSON content type: the browser has to set it itself so the multipart
// boundary matches the body it builds from the FormData.
export async function apiUpload<T>(
  path: string,
  formData: FormData,
  signal?: AbortSignal,
): Promise<T> {
  const res = await send(path, { method: 'POST', credentials: 'include', body: formData, signal })
  if (!res.ok) throw await apiError(res)
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

// fetch, with its rejection (the browser's raw English "Failed to fetch"
// TypeError when the server is unreachable) turned into a translated
// ApiError with status 0, which shouldRetryQuery still retries.
// An aborted request keeps its AbortError so cancellation stays a cancel.
async function send(path: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(path, init)
  } catch (err) {
    if ((err as { name?: string } | null)?.name === 'AbortError') throw err
    throw new ApiError(t('error.network'), 0)
  }
}

// Called when a request comes back 401 "unauthorized", the auth middleware's
// answer to a missing or expired session. App points it at the cached
// /api/me so ProtectedRoute sends the user to the login page.
// Keyed on the message, not just the status: a wrong password on login or on
// the password change is also a 401, but it is not a lost session.
let onSessionExpired = () => {}

export function setSessionExpiredHandler(handler: () => void) {
  onSessionExpired = handler
}

// Called when an admin route answers 403 "admin only": another admin removed
// this user's admin role, which the server applies at once (ADR 0010). App
// points it at the cached /api/me so the admin page and its link go.
let onAdminRevoked = () => {}

export function setAdminRevokedHandler(handler: () => void) {
  onAdminRevoked = handler
}

async function apiError(res: Response): Promise<ApiError> {
  const body = (await res.json().catch(() => ({}))) as { error?: string }
  if (res.status === 401 && body.error === 'unauthorized') onSessionExpired()
  if (res.status === 403 && body.error === 'admin only') onAdminRevoked()
  // A 502/503/504 with no JSON `error` is the vite or nginx proxy saying the
  // backend is down: in every supported setup a stopped server looks like
  // this, not like a rejected fetch.
  if (!body.error && [502, 503, 504].includes(res.status)) {
    return new ApiError(t('error.network'), res.status, body)
  }
  return new ApiError(translateError(body.error), res.status, body)
}

// Default retry policy for the app's QueryClient (see main.tsx). A 4xx means
// the request itself is wrong (not found, unauthorized, bad input) and will
// fail the same way every time, so retrying it just delays the error by
// several seconds of exponential backoff for nothing. Anything else (5xx,
// network failure) keeps TanStack Query's normal default of up to 3 retries.
export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false
  return failureCount < 3
}
