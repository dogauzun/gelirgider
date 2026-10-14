import { getLocale } from '../i18n/strings'
import { getPrivacy } from './privacy'

// What privacy mode shows in place of a money amount. Quantities
// and percentages stay visible, so formatNumber and signedPercent never mask.
export const MASK = '•••'

// What every page shows for a value it does not have, never a zero.
export const MISSING = '—'

// Display formatters that follow the active locale. A module-level
// `const formatter = new Intl.NumberFormat(...)` is evaluated once at import
// and never notices a runtime locale switch, so formatters are kept per
// locale and options instead: building one per call was a large part of the
// transactions page's render time at a thousand rows.
const formatters = new Map<string, Intl.NumberFormat>()

function formatter(options?: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = JSON.stringify([getLocale(), options])
  let f = formatters.get(key)
  if (!f) {
    // eslint-disable-next-line no-restricted-syntax -- the per-locale cache itself
    f = new Intl.NumberFormat(getLocale(), options)
    formatters.set(key, f)
  }
  return f
}

export function formatNumber(value: string | number, options?: Intl.NumberFormatOptions): string {
  // A string goes in as is: Intl formats it as an exact decimal, where
  // Number() would round a large amount's cents.
  return formatter(options).format(typeof value === 'string' ? (value as `${number}`) : value)
}

// Formats with the value's own currency, so a USD figure carries a $ and a
// TRY figure a ₺ -- never a bare number with no currency shown. narrowSymbol:
// the `en` locale has no TRY symbol and would print "TRY 1,234.50".
// signDisplay 'negative': an amount that rounds to zero, such as a sub-cent
// difference, reads "$0,00", never "-$0,00".
export function formatMoney(value: string | number, currency: string): string {
  if (getPrivacy()) return MASK
  return formatNumber(value, {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    signDisplay: 'negative',
  })
}

// A short money figure for a narrow slot, the sidebar's portfolio rows:
// "₺1,28 Mn" in tr. Intl's compact currency output depends on the
// runtime's CLDR data: ICU 77 prints "1,28 Mn ₺" and "842,00 B ₺".
// So the number is formatted compactly on its own and put where formatMoney
// puts the digits, which keeps the sign and symbol where formatMoney has them.
// `rounding` overrides the 2-decimal cap; `{}` keeps Intl's compact default,
// which BarChart's narrow y-axis ticks need.
const NUMBER_PARTS = new Set(['integer', 'group', 'decimal', 'fraction'])

export function formatCompactMoney(
  value: string | number,
  currency: string,
  rounding: Intl.NumberFormatOptions = { maximumFractionDigits: 2 },
): string {
  if (getPrivacy()) return MASK
  const amount = formatNumber(value, {
    notation: 'compact',
    signDisplay: 'never',
    ...rounding,
  })
  const parts = formatter({
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
  }).formatToParts(typeof value === 'string' ? (value as `${number}`) : value)
  let placed = false
  return parts
    .map((part) => {
      if (!NUMBER_PARTS.has(part.type)) return part.value
      if (placed) return ''
      placed = true
      return amount
    })
    .join('')
}

// The currency's own symbol, "₺" or "$", for a label that shows no amount.
export function currencySymbol(currency: string): string {
  return (
    formatter({ style: 'currency', currency, currencyDisplay: 'narrowSymbol' })
      .formatToParts(0)
      .find((part) => part.type === 'currency')?.value ?? currency
  )
}

// Every P/L figure carries its sign, so color is never the only cue
// (ADR 0005). Zero is neither a gain nor a loss, so it stays unsigned.
export function signedMoney(value: string | number, currency: string): string {
  if (getPrivacy()) return MASK
  return formatNumber(value, {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    signDisplay: 'exceptZero',
  })
}

// A P/L ratio as a percent (0.059 -> "+%5,9"), signed like signedMoney.
// Callers override the digits; a share of a whole is not P/L and stays on
// formatNumber.
export function signedPercent(ratio: number, options?: Intl.NumberFormatOptions): string {
  return formatNumber(ratio, {
    style: 'percent',
    signDisplay: 'exceptZero',
    maximumFractionDigits: 1,
    ...options,
  })
}

// plTone colors a profit/loss figure green when positive, red when negative,
// and leaves zero -- or an unknown value -- in the default ink.
export function plTone(value: string | number | null): string {
  const n = Number(value)
  if (n > 0) return 'text-gain'
  if (n < 0) return 'text-loss'
  return 'text-ink'
}

// Unit prices (avg cost, last price, a transaction's price) keep source
// precision: TEFAS NAVs carry 6 decimals and often sit below 1 TL, so 2
// decimals would misstate them. Money totals stay on formatMoney.
// An average cost is a division result, so callers cap it at 6.
// Below 1 the cap counts from the first nonzero digit, or a coin at
// 0.00000874 would round to 0.000009. Intl allows 100 at most.
export function formatUnitPrice(value: string | number, currency: string, maxDigits = 9): string {
  if (getPrivacy()) return MASK
  const n = Math.abs(Number(value))
  const leadingZeros = n > 0 && n < 1 ? -Math.floor(Math.log10(n)) - 1 : 0
  return formatNumber(value, {
    style: 'currency',
    currency,
    currencyDisplay: 'narrowSymbol',
    minimumFractionDigits: 2,
    maximumFractionDigits: Math.min(maxDigits + leadingZeros, 100),
  })
}
