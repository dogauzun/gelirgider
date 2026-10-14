import { expect, it } from 'vitest'
import { nextSort } from './table'

it('flips the active column and starts text A to Z, figures largest first', () => {
  expect(nextSort({ key: 'net', dir: 'desc' }, 'net')).toEqual({ key: 'net', dir: 'asc' })
  expect(nextSort({ key: 'net', dir: 'asc' }, 'net')).toEqual({ key: 'net', dir: 'desc' })
  expect(nextSort({ key: 'net', dir: 'desc' }, 'symbol')).toEqual({ key: 'symbol', dir: 'asc' })
  expect(nextSort({ key: 'symbol', dir: 'asc' }, 'count')).toEqual({ key: 'count', dir: 'desc' })
})
