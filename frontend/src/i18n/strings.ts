// Minimal i18n: one flat key -> string map per locale and no library. `tr` is
// the default and defines the key set; `en` is typed against it. Each map is
// composed from one file per area (`tr/<area>.ts`, `en/<area>.ts`); a new key
// goes into the area file of its prefix, in both locales. The active locale is
// a `createStore` mirrored in localStorage, and `useLocale()` lets the root
// component re-render the tree on switch while every `t()` call site stays a
// plain function call.
import { createStore, readStored, writeStored } from '../lib/createStore'
import { trAccount } from './tr/account'
import { trAdmin } from './tr/admin'
import { trAuth } from './tr/auth'
import { trCharts } from './tr/charts'
import { trCommon } from './tr/common'
import { trDeposits } from './tr/deposits'
import { trDividends } from './tr/dividends'
import { trErrors } from './tr/errors'
import { trImport } from './tr/import'
import { trPortfolios } from './tr/portfolios'
import { trPosition } from './tr/position'
import { trSummary } from './tr/summary'
import { trTransactions } from './tr/transactions'
import { enAccount } from './en/account'
import { enAdmin } from './en/admin'
import { enAuth } from './en/auth'
import { enCharts } from './en/charts'
import { enCommon } from './en/common'
import { enDeposits } from './en/deposits'
import { enDividends } from './en/dividends'
import { enErrors } from './en/errors'
import { enImport } from './en/import'
import { enPortfolios } from './en/portfolios'
import { enPosition } from './en/position'
import { enSummary } from './en/summary'
import { enTransactions } from './en/transactions'

export const tr = {
  ...trAccount,
  ...trAdmin,
  ...trAuth,
  ...trCharts,
  ...trCommon,
  ...trDeposits,
  ...trDividends,
  ...trErrors,
  ...trImport,
  ...trPortfolios,
  ...trPosition,
  ...trSummary,
  ...trTransactions,
} as const

export type StringKey = keyof typeof tr

// Typed per area against the Turkish key set (see types.ts), so `tsc -b` names the
// area file that misses a key.
type Strings = Record<StringKey, string> & Partial<Record<`${StringKey}.one`, string>>

const en: Strings = {
  ...enAccount,
  ...enAdmin,
  ...enAuth,
  ...enCharts,
  ...enCommon,
  ...enDeposits,
  ...enDividends,
  ...enErrors,
  ...enImport,
  ...enPortfolios,
  ...enPosition,
  ...enSummary,
  ...enTransactions,
}

export type Locale = 'tr' | 'en'
export const locales: readonly Locale[] = ['tr', 'en']

const maps: Record<Locale, Strings> = { tr, en }

// shortcut: the locale lives in the browser only (localStorage), not on the
// user row, because it affects rendering alone. Multi-device sync, if ever
// wanted: a `locale` column next to `reporting_currency` and `PUT /api/me/locale`.
// Nothing stored, or storage disabled: the default.
function storedLocale(): Locale {
  return readStored('locale') === 'en' ? 'en' : 'tr'
}

const store = createStore(storedLocale, (next) => {
  document.documentElement.lang = next
  writeStored('locale', next)
})
document.documentElement.lang = store.get()

export const getLocale = store.get
export const setLocale = store.set
// Re-renders the caller on every locale switch. The root component
// reads it so the whole tree picks up the new strings while the `t()` call
// sites stay plain function calls.
export const useLocale = store.use

export function t(key: StringKey, vars: Record<string, string | number> = {}): string {
  const map = maps[store.get()]
  const text = (String(vars.count) === '1' && map[`${key}.one`]) || map[key]
  return text.replace(/\{(\w+)\}/g, (_match, name: string) => String(vars[name] ?? ''))
}

// A market's display name: its market.<CODE> string when there is one
// (CRYPTO, GOLD, DEPOSIT), else the code itself (BIST, US, TEFAS read the same in
// every locale).
export function marketLabel(market: string): string {
  const key = `market.${market}`
  return key in tr ? t(key as StringKey) : market
}
