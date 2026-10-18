import { describe, expect, it } from 'vitest'
import { contributionRows } from './contribution'

function position(instrumentId: number, symbol: string, amount: string | null) {
  return { instrumentId, symbol, amount }
}

const brief = (rows: ReturnType<typeof contributionRows>['rows']) =>
  rows.map((row) => [row.symbol, row.amount, Number(row.fraction.toFixed(4))])

describe('contributionRows', () => {
  it('keeps the four largest gains then the four largest losses, best first', () => {
    const positions = [
      position(1, 'A', '10.00'),
      position(2, 'B', '-40.00'),
      position(3, 'C', '50.00'),
      position(4, 'D', '-5.00'),
      position(5, 'E', '30.00'),
      position(6, 'F', '-100.00'),
      position(7, 'G', '20.00'),
      position(8, 'H', '-20.00'),
      position(9, 'I', '5.00'),
      position(10, 'J', '-1.00'),
    ]
    const { rows, hidden } = contributionRows(positions, 'total', 4)
    expect(brief(rows)).toEqual([
      ['C', '50.00', 0.5],
      ['E', '30.00', 0.3],
      ['G', '20.00', 0.2],
      ['A', '10.00', 0.1],
      ['D', '-5.00', 0.05],
      ['H', '-20.00', 0.2],
      ['B', '-40.00', 0.4],
      ['F', '-100.00', 1],
    ])
    expect(hidden).toBe(2)
  })

  it('shows every position when the limit would leave none out', () => {
    const positions = [
      position(1, 'A', '3.00'),
      position(2, 'B', '-2.00'),
      position(3, 'C', '1.00'),
      position(4, 'D', '4.00'),
    ]
    const { rows, hidden } = contributionRows(positions, 'total', 4)
    expect(rows.map((row) => row.symbol)).toEqual(['D', 'A', 'C', 'B'])
    expect(hidden).toBe(0)
  })

  it('orders ties by symbol', () => {
    const positions = [
      position(1, 'ZOREN', '7.00'),
      position(2, 'AKBNK', '7.00'),
      position(3, 'MGROS', '7.00'),
    ]
    expect(contributionRows(positions, 'total', null).rows.map((row) => row.symbol)).toEqual([
      'AKBNK',
      'MGROS',
      'ZOREN',
    ])
  })

  it('leaves out a row without an amount', () => {
    const rows = [position(1, 'A', '3.00'), position(2, 'B', null), position(3, 'C', '-1.00')]
    expect(brief(contributionRows(rows, 'total', 4).rows)).toEqual([
      ['A', '3.00', 1],
      ['C', '-1.00', 0.3333],
    ])
    expect(contributionRows([position(1, 'A', null)], 'day', 4)).toEqual({ rows: [], hidden: 0 })
  })

  it('range mode keeps a closed position among the open ones and puts a null total last', () => {
    const rows = [
      position(1, 'OPEN', '10.00'),
      position(2, 'ZNULL', null),
      position(3, 'CLOSED', '40.00'),
      position(4, 'ANULL', null),
      position(5, 'LOSS', '-20.00'),
    ]
    const { rows: out, hidden } = contributionRows(rows, 'range', null)
    expect(brief(out)).toEqual([
      ['CLOSED', '40.00', 1],
      ['OPEN', '10.00', 0.25],
      ['LOSS', '-20.00', 0.5],
      ['ANULL', null, 0],
      ['ZNULL', null, 0],
    ])
    expect(hidden).toBe(0)
  })

  it('range mode keeps null totals outside the limit', () => {
    const rows = [
      ...Array.from({ length: 10 }, (_, i) => position(i + 1, `S${i}`, String(i + 1))),
      position(11, 'NONE', null),
    ]
    const { rows: out, hidden } = contributionRows(rows, 'range', 4)
    expect(out.map((row) => row.symbol)).toEqual([
      'S9',
      'S8',
      'S7',
      'S6',
      'S3',
      'S2',
      'S1',
      'S0',
      'NONE',
    ])
    expect(hidden).toBe(2)
  })

  it('gives every bar length 0 when everything is zero', () => {
    const positions = [position(1, 'A', '0.00'), position(2, 'B', '0')]
    expect(brief(contributionRows(positions, 'total', 4).rows)).toEqual([
      ['A', '0.00', 0],
      ['B', '0', 0],
    ])
  })

  it('gives a single position the full bar, a loss too', () => {
    expect(brief(contributionRows([position(1, 'A', '-12.34')], 'total', 4).rows)).toEqual([
      ['A', '-12.34', 1],
    ])
  })

  it('returns every position best first without a limit', () => {
    const positions = Array.from({ length: 10 }, (_, i) => position(i + 1, `S${i}`, String(i - 4)))
    const { rows, hidden } = contributionRows(positions, 'total', null)
    expect(rows.map((row) => row.amount)).toEqual([
      '5',
      '4',
      '3',
      '2',
      '1',
      '0',
      '-1',
      '-2',
      '-3',
      '-4',
    ])
    expect(hidden).toBe(0)
  })
})
