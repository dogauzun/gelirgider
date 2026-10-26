import { getLocale } from '../i18n/strings'

// formatMarketDate renders a market date -- an "YYYY-MM-DD" Europe/Istanbul
// calendar date, not an instant -- in the active locale (dd.MM.yyyy for tr).
//
// `new Date("2026-09-14")` is UTC midnight, so formatting it in the browser's
// own zone moves the day back by one for every user west of UTC. Formatting
// in UTC keeps the calendar day the user actually entered.
//
// Building a DateTimeFormat costs far more than formatting with one, and the
// transactions page formats several dates per row on every render,
// so formatters are kept per locale and options. The locale is part of the
// key, so a runtime locale switch still takes effect.
const formatters = new Map<string, Intl.DateTimeFormat>()

// The bare numeric form: options that set no date or time field (none, or only
// a timeZone). tr pads day and month ("09.12.2025") only when asked: newer ICU
// (Node 26) drops the pad by default. en stays M/d/yyyy.
const fieldOptions = [
  'weekday',
  'era',
  'year',
  'month',
  'day',
  'dayPeriod',
  'hour',
  'minute',
  'second',
  'fractionalSecondDigits',
  'timeZoneName',
  'dateStyle',
  'timeStyle',
] as const
const paddedTurkish: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
}

export function formatMarketDate(iso: string, options?: Intl.DateTimeFormatOptions): string {
  const key = JSON.stringify([getLocale(), options])
  let formatter = formatters.get(key)
  if (!formatter) {
    const bare =
      getLocale() === 'tr' && !fieldOptions.some((field) => options?.[field] !== undefined)
    // eslint-disable-next-line no-restricted-syntax -- the per-locale cache itself
    formatter = new Intl.DateTimeFormat(getLocale(), {
      timeZone: 'UTC',
      ...(bare && paddedTurkish),
      ...options,
    })
    formatters.set(key, formatter)
  }
  return formatter.format(new Date(iso))
}

// yearUnlessThisYear is the one "add the year only when it is not this year"
// rule: no options when iso falls in the current year of timeZone, else the
// numeric year. Both years are read in timeZone, so an instant near New Year
// is compared in the zone it is shown in.
function yearUnlessThisYear(iso: string, timeZone: string): Intl.DateTimeFormatOptions {
  const year = (at: string) => formatMarketDate(at, { timeZone, year: 'numeric' })
  return year(iso) === year(new Date().toISOString()) ? {} : { year: 'numeric' }
}

// formatShortMarketDate renders a market date as day and short month ("12 Eyl")
// when it falls in the current Europe/Istanbul year, and adds the year
// otherwise ("01 Oca 1990"), so a years-old date never reads as recent.
// A named month keeps it unambiguous across locales, where a
// numeric day-month pattern is dd/MM in tr and MM/dd in en.
// Takes no second parameter: Recharts passes the tick index to a
// tickFormatter.
export function formatShortMarketDate(iso: string): string {
  return formatMarketDate(iso, {
    day: '2-digit',
    month: 'short',
    ...yearUnlessThisYear(iso, 'Europe/Istanbul'),
  })
}

// formatShortInstant is formatShortMarketDate for a UTC instant, shown in
// timeZone ("4 Eki 14:05" with options for the time). Always pass a real
// zone: formatMarketDate falls back to the browser zone for undefined.
export function formatShortInstant(
  iso: string,
  timeZone: string,
  options: Intl.DateTimeFormatOptions = {},
): string {
  return formatMarketDate(iso, {
    timeZone,
    day: '2-digit',
    month: 'short',
    ...options,
    ...yearUnlessThisYear(iso, timeZone),
  })
}

// isStale reports whether a fetched price dated priceDate is too old for its
// market on the Europe/Istanbul day `today`: more than 2 calendar
// days for CRYPTO, which trades daily, and more than 7 for every other market.
// No holiday calendar, so a long bayram can flag BIST briefly.
export function isStale(market: string, priceDate: string | null, today: string): boolean {
  if (priceDate === null) return false
  return daysBetween(priceDate, today) > (market === 'CRYPTO' ? 2 : 7)
}

// daysBetween counts the calendar days from one market date to another, both
// "YYYY-MM-DD": negative when `to` comes first. Both parse as UTC midnight, so
// no DST shift moves the gap and the count is always whole.
export function daysBetween(from: string, to: string): number {
  // eslint-disable-next-line no-restricted-syntax -- the one day count
  return (Date.parse(to) - Date.parse(from)) / 86_400_000
}

// monthsBetween counts the whole calendar months from one market date to a
// later one, both "YYYY-MM-DD": 2025-03-15 to 2026-05-14 is 13, since the
// 15th of May has not come yet. A month counts once its day-of-month is
// reached, so 01-31 to 02-28 is still 0.
export function monthsBetween(from: string, to: string): number {
  const [fy, fm, fd] = from.split('-').map(Number)
  const [ty, tm, td] = to.split('-').map(Number)
  return (ty - fy) * 12 + (tm - fm) - (td < fd ? 1 : 0)
}

const istanbulDay = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Istanbul' })

// todayIstanbul is today's date as "YYYY-MM-DD" in Europe/Istanbul: the
// market day, and so the date input default. The positions table calls it
// once per row per render, so the formatter is built once.
export function todayIstanbul(): string {
  return istanbulDay.format(new Date())
}
