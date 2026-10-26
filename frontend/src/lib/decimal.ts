import { getLocale } from '../i18n/strings'
import { formatMoney, formatNumber } from './format'

// parseDecimalInput accepts user-typed decimals with either a comma or a dot
// as the decimal separator ("1,5" or "1.5"), and returns the dot form the
// server expects, or null when the text is not a plain decimal.
//
// When both "." and "," appear, the last one is treated as the decimal
// separator and the other is stripped as a thousands separator:
// "100.000,50" -> "100000.50", "100,000.50" -> "100000.50". The groups must
// be three digits: "1.2.3,4" is not a number.
// When only one kind appears, it is the decimal separator. The exception is
// a value in the UI's own grouping shape: tr shows "10.000" and en "10,000",
// so there a single-separator value in thousands shape is rejected as
// ambiguous rather than read as 10; callers show the hint
// via isAmbiguousThousands.
export function parseDecimalInput(text: string): string | null {
  const trimmed = text.trim()
  if (isAmbiguousThousands(trimmed)) return null
  const hasDot = trimmed.includes('.')
  const hasComma = trimmed.includes(',')

  let normalized: string
  if (hasDot && hasComma) {
    const decimalSep = trimmed.lastIndexOf('.') > trimmed.lastIndexOf(',') ? '.' : ','
    const thousandsSep = decimalSep === '.' ? ',' : '.'
    // The integer part must be in 3-digit groups, or a typo like "1.2.3,4"
    // would be read as 123.4, and the first group must not start
    // with 0, or "0.234,5" would be read as 234.5.
    const grouped = decimalSep === '.' ? /^[1-9]\d{0,2}(,\d{3})+$/ : /^[1-9]\d{0,2}(\.\d{3})+$/
    if (!grouped.test(trimmed.slice(0, trimmed.lastIndexOf(decimalSep)))) return null
    normalized = trimmed.split(thousandsSep).join('').replace(decimalSep, '.')
  } else {
    normalized = trimmed.replace(',', '.')
  }

  return /^\d+(\.\d+)?$/.test(normalized) ? normalized : null
}

// tooManyDigits reports a parseDecimalInput result with more digits than the
// server stores: 15 integer digits, and 9 fraction digits for a transaction
// (backend/internal/transaction/handlers.go) or 8 for a manual price
// (backend/internal/price/price.go). Without it the server's rejection reads
// as "must be greater than 0" for a positive value.
export function tooManyDigits(value: string, maxFraction: number): boolean {
  const [integer, fraction = ''] = value.split('.')
  return integer.length > 15 || fraction.length > maxFraction
}

// isAmbiguousThousands reports an input in the locale's grouping shape that
// could be grouping or a decimal: "10.000" or "1.000.000" in tr, "1,000" or
// "1,000,000" in en. "1.5", "12.50" and "0.0003" (or their comma forms) do not
// match, so fund and crypto prices still parse.
export function isAmbiguousThousands(text: string): boolean {
  const pattern = getLocale() === 'tr' ? /^[1-9]\d{0,2}(\.\d{3})+$/ : /^[1-9]\d{0,2}(,\d{3})+$/
  return pattern.test(text.trim())
}

// formatDecimalInput turns an API decimal ("22.5") into what a form field
// prefilled with it should show: the locale's decimal separator ("22,5" in
// tr), no thousands grouping (a tr "10.000" would trip isAmbiguousThousands),
// and exactly the API's digits, nothing rounded or padded.
// parseDecimalInput reads it back to the same string.
export function formatDecimalInput(value: string): string {
  return getLocale() === 'tr' ? value.replace('.', ',') : value
}

// Quantities keep source precision: TEFAS fund units routinely carry 6
// decimals, Midas fractional shares 9, and the API accepts 9, so format to 9
// instead of letting Intl.NumberFormat round to its default 3.
export function formatQuantity(value: string): string {
  return formatNumber(value, { maximumFractionDigits: 9 })
}

// A decimal string (a quantity or an amount of money) as an exact count of
// 1e-9 units, the API's finest step, so comparing and adding never goes
// through float.
export function decimalUnits(text: string): bigint {
  const [whole, fraction = ''] = text.replace('-', '').split('.')
  const units = BigInt(whole + fraction.padEnd(9, '0').slice(0, 9))
  return text.startsWith('-') ? -units : units
}

// unitsToDecimal is decimalUnits read back: a count of 1e-9 units as an
// exact decimal string, trailing fraction zeros trimmed. Built from the
// bigint, since Number() loses cents on large sums.
export function unitsToDecimal(units: bigint): string {
  const sign = units < 0n ? '-' : ''
  const abs = units < 0n ? -units : units
  const fraction = String(abs % 1_000_000_000n)
    .padStart(9, '0')
    // eslint-disable-next-line no-restricted-syntax -- the one trailing-zero trim
    .replace(/0+$/, '')
  return `${sign}${abs / 1_000_000_000n}${fraction ? `.${fraction}` : ''}`
}

// The items' amounts added exactly per currency, each sum formatted, in the
// order the currencies first appear: ["₺1.264,79", "$12,34"].
export function moneyByCurrency(items: { currency: string; amount: string }[]): string[] {
  const sums = new Map<string, bigint>()
  for (const { currency, amount } of items) {
    // eslint-disable-next-line no-restricted-syntax -- the one shared copy
    sums.set(currency, (sums.get(currency) ?? 0n) + decimalUnits(amount))
  }
  return [...sums].map(([currency, units]) => formatMoney(unitsToDecimal(units), currency))
}

// The rows' net total, added exactly per currency: "₺1.264,79", or
// "₺1.264,79 + $12,34" when BIST and US dividends mix, since the panel has no
// FX rate to put them in one currency.
export const netTotal = (rows: { currency: string; net: string }[]) =>
  moneyByCurrency(rows.map((row) => ({ currency: row.currency, amount: row.net }))).join(' + ')

// The values added exactly, as a count of 1e-9 units: 0n for an empty list.
export function sumUnits(values: string[]): bigint {
  return values.reduce((sum, value) => sum + decimalUnits(value), 0n)
}

// a + b and a - b of two API decimal strings, exact: Number() would round a
// large amount's cents. Scales may differ ("1.5" and "0.25").
export function addDecimal(a: string, b: string): string {
  // eslint-disable-next-line no-restricted-syntax -- the one shared copy
  return unitsToDecimal(decimalUnits(a) + decimalUnits(b))
}

export function subDecimal(a: string, b: string): string {
  // eslint-disable-next-line no-restricted-syntax -- the one shared copy
  return unitsToDecimal(decimalUnits(a) - decimalUnits(b))
}

// a × b of two API decimal strings, exact at any number of fraction digits:
// the product's scale is the sum of the two scales ("0.1" × "3" = "0.3").
export function mulDecimal(a: string, b: string): string {
  const scale = (text: string) => (text.split('.')[1] ?? '').length
  const digits = (text: string) => BigInt(text.replace('-', '').replace('.', ''))
  const places = scale(a) + scale(b)
  const product = digits(a) * digits(b)
  const padded = String(product).padStart(places + 1, '0')
  const fraction = padded
    .slice(padded.length - places)
    // eslint-disable-next-line no-restricted-syntax -- the one trailing-zero trim
    .replace(/0+$/, '')
  const sign = a.startsWith('-') !== b.startsWith('-') && product !== 0n ? '-' : ''
  return `${sign}${padded.slice(0, padded.length - places)}${fraction ? `.${fraction}` : ''}`
}

// compareDecimal orders two API decimal strings exactly: -1, 0 or 1, as a
// sort comparator expects.
export function compareDecimal(a: string, b: string): number {
  return compareUnits(decimalUnits(a), decimalUnits(b))
}

// compareDecimal for amounts already in units.
export function compareUnits(x: bigint, y: bigint): number {
  if (x < y) return -1
  return x > y ? 1 : 0
}
