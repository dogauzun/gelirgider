import { daysBetween } from './date'

// annualize turns a period return r over `days` calendar days into a yearly
// rate, (1 + r)^(365 / days) - 1 (ADR 0011). MWR is a period IRR, so the same
// formula holds for all three methods. A total loss (r = -1) stays -1; a loss
// beyond the money put in (r < -1) has no real annual rate and gives NaN.
export function annualize(r: number, days: number): number {
  return (1 + r) ** (365 / days) - 1
}

// annualizedReturn is the yearly rate of a period return over the days between
// two market dates: null for a missing return or date, or under 364 days. A
// span of 364 to 366 days counts as one year (a calendar year, 2025-01-01 to
// 2025-12-31, is 364 days apart, 2024 is 365; the 1Y preset is 365, or 366 over
// a leap day), so its annualized return is its period return.
export function annualizedReturn(
  ret: number | null,
  from?: string | null,
  to?: string | null,
): number | null {
  if (ret === null || !from || !to) return null
  const gap = daysBetween(from, to)
  return gap >= 364 ? annualize(ret, gap <= 366 ? 365 : gap) : null
}

// excessReturn is return p over benchmark return b, (1 + p) / (1 + b) - 1:
// the real return when the benchmark is TÜFE (ADR 0012). null when the
// benchmark lost everything or more (1 + b <= 0), where the ratio means nothing.
export function excessReturn(p: number, b: number): number | null {
  // eslint-disable-next-line no-restricted-syntax -- the one place for it
  return 1 + b > 0 ? (1 + p) / (1 + b) - 1 : null
}
