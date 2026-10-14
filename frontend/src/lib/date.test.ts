import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n/strings'
import { daysBetween, formatMarketDate, isStale, monthsBetween, todayIstanbul } from './date'

// Run west of UTC, where a naive `new Intl.DateTimeFormat('tr-TR').format(new
// Date(iso))` renders 13.09.2026 for a 14.09.2026 market date.
beforeAll(() => vi.stubEnv('TZ', 'America/New_York'))
afterAll(() => vi.unstubAllEnvs())
// The other tests assert Turkish output, so leave the default locale behind.
afterEach(() => setLocale('tr'))

describe('formatMarketDate', () => {
  it('keeps the calendar day in a zone behind UTC', () => {
    expect(new Intl.DateTimeFormat('tr-TR').format(new Date('2026-09-14'))).toBe('13.09.2026')
    expect(formatMarketDate('2026-09-14')).toBe('14.09.2026')
    expect(formatMarketDate('2026-01-01')).toBe('01.01.2026')
  })

  it('honours format options', () => {
    expect(formatMarketDate('2026-09-14', { day: '2-digit', month: 'short' })).toBe('14 Eyl')
  })

  it('follows the active locale', () => {
    setLocale('en')
    expect(formatMarketDate('2026-09-14')).toBe('9/14/2026')
  })

  // Building a DateTimeFormat per call made the transactions page take
  // seconds to render a thousand rows.
  it('reuses one formatter for the same locale and options', () => {
    const spy = vi.spyOn(Intl, 'DateTimeFormat')
    formatMarketDate('2026-09-14', { weekday: 'long' })
    formatMarketDate('2026-09-15', { weekday: 'long' })
    expect(spy).toHaveBeenCalledTimes(1)
    setLocale('en')
    expect(formatMarketDate('2026-09-14', { weekday: 'long' })).toBe('Monday')
    expect(spy).toHaveBeenCalledTimes(2)
    spy.mockRestore()
  })
})

describe('isStale', () => {
  it('flags a price past its market limit in calendar days', () => {
    expect(isStale('CRYPTO', '2026-03-27', '2026-03-29')).toBe(false)
    expect(isStale('CRYPTO', '2026-03-26', '2026-03-29')).toBe(true)
    expect(isStale('BIST', '2026-03-22', '2026-03-29')).toBe(false)
    expect(isStale('BIST', '2026-03-21', '2026-03-29')).toBe(true)
    expect(isStale('BIST', null, '2026-03-29')).toBe(false)
  })
})

describe('daysBetween', () => {
  it('counts calendar days, negative when reversed', () => {
    expect(daysBetween('2026-03-29', '2026-03-29')).toBe(0)
    // Across a month end and the 29 March DST change in Europe.
    expect(daysBetween('2026-03-27', '2026-04-03')).toBe(7)
    expect(daysBetween('2026-04-03', '2026-03-27')).toBe(-7)
  })
})

describe('monthsBetween', () => {
  it('counts a month only once its day-of-month is reached', () => {
    expect(monthsBetween('2026-09-01', '2026-09-29')).toBe(0)
    expect(monthsBetween('2026-08-30', '2026-09-29')).toBe(0)
    expect(monthsBetween('2026-08-29', '2026-09-29')).toBe(1)
    expect(monthsBetween('2025-03-15', '2026-05-14')).toBe(13)
    expect(monthsBetween('2024-07-15', '2026-09-29')).toBe(26)
  })
})

// The positions table calls it once per row per render.
describe('todayIstanbul', () => {
  it('formats the Istanbul day without building a formatter per call', () => {
    const spy = vi.spyOn(Intl, 'DateTimeFormat')
    expect(todayIstanbul()).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    todayIstanbul()
    expect(spy).not.toHaveBeenCalled()
    spy.mockRestore()
  })
})
