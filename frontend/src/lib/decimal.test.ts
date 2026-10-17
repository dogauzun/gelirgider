import { afterEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n/strings'
import {
  addDecimal,
  mulDecimal,
  compareDecimal,
  formatDecimalInput,
  formatQuantity,
  unitsToDecimal,
  isAmbiguousThousands,
  moneyByCurrency,
  parseDecimalInput,
  subDecimal,
} from './decimal'

afterEach(() => setLocale('tr'))

describe('parseDecimalInput', () => {
  it('accepts comma and dot decimals, rejects the rest', () => {
    expect(parseDecimalInput('1,5')).toBe('1.5')
    expect(parseDecimalInput('1.5')).toBe('1.5')
    expect(parseDecimalInput(' 300 ')).toBe('300')
    expect(parseDecimalInput('1,5,5')).toBeNull()
    expect(parseDecimalInput('abc')).toBeNull()
    expect(parseDecimalInput('')).toBeNull()
  })

  it('strips a thousands separator when both . and , are present, keeping the last as decimal', () => {
    expect(parseDecimalInput('100.000,50')).toBe('100000.50')
    expect(parseDecimalInput('100,000.50')).toBe('100000.50')
    expect(parseDecimalInput('1.000.000,50')).toBe('1000000.50')
    expect(parseDecimalInput('1,000,000.50')).toBe('1000000.50')
    expect(parseDecimalInput('1.2.3,4')).toBeNull()
    expect(parseDecimalInput('1,2,3.4')).toBeNull()
    expect(parseDecimalInput('1000.000,5')).toBeNull()
  })

  it('rejects a first thousands group that starts with 0 (GLG-977)', () => {
    expect(parseDecimalInput('0.234,5')).toBeNull()
    expect(parseDecimalInput('012.345,6')).toBeNull()
    expect(parseDecimalInput('0,234.5')).toBeNull()
    expect(parseDecimalInput('1.234,5')).toBe('1234.5')
    expect(parseDecimalInput('1,234.56')).toBe('1234.56')
    expect(parseDecimalInput('0,5')).toBe('0.5')
    expect(parseDecimalInput('0.5')).toBe('0.5')
  })

  it('in tr, rejects a dot-only thousands shape as ambiguous but keeps dot decimals', () => {
    setLocale('tr')
    expect(parseDecimalInput('10.000')).toBeNull()
    expect(isAmbiguousThousands('10.000')).toBe(true)
    expect(isAmbiguousThousands('1.000.000')).toBe(true)
    expect(parseDecimalInput('1.5')).toBe('1.5')
    expect(parseDecimalInput('12.50')).toBe('12.50')
    expect(parseDecimalInput('0.0003')).toBe('0.0003')
    expect(isAmbiguousThousands('0.0003')).toBe(false)
    expect(parseDecimalInput('1,000')).toBe('1.000')
  })

  it('in en, rejects a comma-only thousands shape as ambiguous but keeps other decimals', () => {
    setLocale('en')
    expect(parseDecimalInput('1,000')).toBeNull()
    expect(isAmbiguousThousands('1,000')).toBe(true)
    expect(isAmbiguousThousands('1,000,000')).toBe(true)
    expect(parseDecimalInput('1,5')).toBe('1.5')
    expect(parseDecimalInput('0,0003')).toBe('0.0003')
    expect(isAmbiguousThousands('0,0003')).toBe(false)
    expect(parseDecimalInput('10.000')).toBe('10.000')
    expect(isAmbiguousThousands('10.000')).toBe(false)
  })

  it('rejects a leading minus', () => {
    expect(parseDecimalInput('-1,5')).toBeNull()
    expect(parseDecimalInput('-100.000,50')).toBeNull()
  })
})

describe('formatDecimalInput', () => {
  it('writes the locale decimal separator, no grouping, and parses back unchanged', () => {
    setLocale('tr')
    expect(formatDecimalInput('22.5')).toBe('22,5')
    expect(formatDecimalInput('150')).toBe('150')
    expect(formatDecimalInput('0.00')).toBe('0,00')
    // A tr "10.000" would be read as ambiguous thousands; "10,000" is not.
    expect(formatDecimalInput('10.000')).toBe('10,000')
    expect(formatDecimalInput('1000.123456')).toBe('1000,123456')
    for (const value of ['22.5', '0.00', '10.000', '1000.123456']) {
      expect(parseDecimalInput(formatDecimalInput(value))).toBe(value)
    }

    setLocale('en')
    expect(formatDecimalInput('22.5')).toBe('22.5')
    expect(parseDecimalInput(formatDecimalInput('10.000'))).toBe('10.000')
  })
})

describe('formatQuantity', () => {
  it('keeps fund-unit precision instead of rounding to 3 decimals', () => {
    expect(formatQuantity('1000.123456')).toBe('1.000,123456')
    expect(formatQuantity('0.12345678')).toBe('0,12345678')
    // A Midas fractional share keeps all 9 decimals.
    expect(formatQuantity('0.006218342')).toBe('0,006218342')
    expect(formatQuantity('10')).toBe('10')
  })
})

describe('unitsToDecimal', () => {
  it('reads 1e-9 units back exactly, past what a float holds', () => {
    // Number() of this sum would print 100000000000000.02.
    expect(unitsToDecimal(100_000_000_000_000_010_000_000n)).toBe('100000000000000.01')
    expect(unitsToDecimal(-1_500_000_000n)).toBe('-1.5')
    expect(unitsToDecimal(0n)).toBe('0')
  })
})

describe('moneyByCurrency', () => {
  it('adds each currency exactly across rows, in first-seen order', () => {
    setLocale('en')
    expect(
      moneyByCurrency([
        { currency: 'TRY', amount: '0.1' },
        { currency: 'USD', amount: '12.34' },
        { currency: 'TRY', amount: '0.2' },
      ]),
    ).toEqual(['₺0.30', '$12.34'])
    expect(moneyByCurrency([])).toEqual([])
  })
})

describe('addDecimal, subDecimal, compareDecimal', () => {
  it('work exactly on strings of mixed scale and sign', () => {
    expect(addDecimal('1.5', '0.25')).toBe('1.75')
    expect(addDecimal('0.1', '0.2')).toBe('0.3')
    expect(addDecimal('100000000000000.01', '-0.000000001')).toBe('100000000000000.009999999')
    expect(subDecimal('10', '10.00')).toBe('0')
    expect(subDecimal('0.30', '1.123456')).toBe('-0.823456')
    expect(subDecimal('-2', '-0.5')).toBe('-1.5')
    expect(compareDecimal('2.50', '2.5')).toBe(0)
    expect(compareDecimal('10', '9.999999999')).toBe(1)
    expect(compareDecimal('-0.1', '0.05')).toBe(-1)
  })
})

describe('mulDecimal', () => {
  it('multiplies exactly at any scale and sign', () => {
    expect(mulDecimal('0.1', '3')).toBe('0.3')
    expect(mulDecimal('1234567.89', '1234.5678')).toBe('1524157763.907942')
    expect(mulDecimal('123.456789', '0.000123')).toBe('0.015185185047')
    expect(mulDecimal('2.50', '4')).toBe('10')
    expect(mulDecimal('0.000000001', '0.000000001')).toBe('0.000000000000000001')
    expect(mulDecimal('-1.5', '2')).toBe('-3')
    expect(mulDecimal('-1.5', '0')).toBe('0')
  })
})
