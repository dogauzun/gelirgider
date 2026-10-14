import { expect, it } from 'vitest'
import { setLocale } from '../i18n/strings'
import { sortRows } from './sortRows'

it('sorts symbols in the app locale, figures exactly and gaps last', () => {
  setLocale('tr')
  const rows = [
    { symbol: 'İA', n: '2.5', r: 0.5 },
    { symbol: 'IZ', n: null, r: null },
    { symbol: 'ZZ', n: '10', r: 0.25 },
    { symbol: 'AA', n: '-1', r: 1 },
  ]
  const symbols = (r: typeof rows) => r.map((x) => x.symbol)
  // tr puts I before İ; a locale-less compare puts İA before IZ.
  expect(symbols(sortRows(rows, (x) => x.symbol, { key: 'symbol', dir: 'asc' }))).toEqual([
    'AA',
    'IZ',
    'İA',
    'ZZ',
  ])
  // A missing figure is last in either direction.
  expect(symbols(sortRows(rows, (x) => x.n, { key: 'net', dir: 'desc' }))).toEqual([
    'ZZ',
    'İA',
    'AA',
    'IZ',
  ])
  expect(symbols(sortRows(rows, (x) => x.r, { key: 'pct', dir: 'asc' }))).toEqual([
    'ZZ',
    'İA',
    'AA',
    'IZ',
  ])
})
