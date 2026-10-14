import { afterEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n/strings'
import {
  formatCompactMoney,
  formatMoney,
  formatNumber,
  formatUnitPrice,
  MASK,
  plTone,
  signedMoney,
  signedPercent,
} from './format'
import { setPrivacy } from './privacy'

describe('plTone', () => {
  // A zero P/L (a portfolio with no positions) is neither gain nor loss (GLG-240).
  it('leaves zero and unknown in ink', () => {
    expect(plTone(1)).toBe('text-gain')
    expect(plTone('-0.01')).toBe('text-loss')
    expect(plTone(0)).toBe('text-ink')
    expect(plTone(null)).toBe('text-ink')
  })
})

describe('formatMoney', () => {
  // The other tests assert Turkish output, so leave the default locale behind.
  afterEach(() => setLocale('tr'))

  it('follows the active locale', () => {
    expect(formatMoney('1234.5', 'TRY')).toContain('1.234,50')
    setLocale('en')
    expect(formatMoney('1234.5', 'TRY')).toContain('1,234.50')
  })

  // `en` has no TRY symbol in CLDR, so plain 'symbol' falls back to "TRY" (GLG-270).
  it('shows ₺ for lira in English', () => {
    setLocale('en')
    expect(formatMoney('1234.5', 'TRY')).toBe('₺1,234.50')
  })

  // The import preview's cash difference showed "-$0,00" (GLG-1186): an
  // amount that rounds to zero carries no sign, either way.
  it('drops the sign of an amount that rounds to zero', () => {
    expect(formatMoney('-0.004', 'USD')).toBe('$0,00')
    expect(signedMoney('0.004', 'USD')).toBe('$0,00')
    expect(formatMoney('-1.5', 'USD')).toBe('-$1,50')
  })
})

// The sidebar's portfolio values (GLG-875). Intl may put a no-break space
// before the unit, so whitespace is compared as a plain space.
describe('formatCompactMoney', () => {
  afterEach(() => setPrivacy(false))
  const compact = (value: string, currency: string) =>
    formatCompactMoney(value, currency).replace(/\s/g, ' ')

  it('formats compact money in tr', () => {
    expect(compact('1284650.40', 'TRY')).toBe('₺1,28 Mn')
    expect(compact('842000', 'TRY')).toBe('₺842 B')
    expect(compact('0', 'TRY')).toBe('₺0')
    expect(compact('-35200', 'TRY')).toBe('-₺35,2 B')
    expect(compact('12500.5', 'USD')).toBe('$12,5 B')
  })

  it('masks compact money in privacy mode', () => {
    setPrivacy(true)
    expect(formatCompactMoney('1284650.40', 'TRY')).toBe(MASK)
  })
})

describe('formatUnitPrice', () => {
  it('keeps source precision but shows at least 2 decimals', () => {
    expect(formatUnitPrice('0.712194', 'TRY')).toContain('0,712194')
    expect(formatUnitPrice('8', 'TRY')).toContain('8,00')
  })

  // An average cost drops division noise past 6 decimals (GLG-272).
  it('caps an average cost at the given digits', () => {
    expect(formatUnitPrice('5.046148571', 'TRY', 6)).toContain('5,046149')
    expect(formatUnitPrice('5.1', 'TRY', 6)).toContain('5,10')
  })

  // Below 0.0001 a fraction-digit cap drops real digits, not noise: PEPE-USD
  // at 0.00000874 read "$0,000009" (GLG-1024). The cap counts digits from
  // the first nonzero one there.
  it('keeps the digits of a price below 1', () => {
    expect(formatUnitPrice('0.00000874', 'USD', 6)).toContain('0,00000874')
    expect(formatUnitPrice('0.0000087412345678', 'USD', 6)).toContain('0,00000874123')
    expect(formatUnitPrice('0.1', 'USD', 6)).toContain('0,10')
  })
})

describe('privacy mode', () => {
  afterEach(() => setPrivacy(false))

  // Money is masked; quantities and percentages stay visible (GLG-564).
  it('masks money only while on', () => {
    setPrivacy(true)
    expect(formatMoney('1234.5', 'TRY')).toBe('•••')
    expect(signedMoney('-10', 'TRY')).toBe('•••')
    expect(formatUnitPrice('0.712194', 'TRY')).toBe('•••')
    expect(formatNumber('1234.5')).toContain('1.234,5')
    expect(signedPercent(0.059)).toContain('5,9')
    setPrivacy(false)
    expect(formatMoney('1234.5', 'TRY')).toContain('1.234,50')
  })
})
