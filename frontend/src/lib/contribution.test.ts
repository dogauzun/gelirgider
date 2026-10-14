import { describe, expect, it } from 'vitest'
import type { Position } from '../api/summary'
import { contributionRows } from './contribution'

function position(
  instrumentId: number,
  symbol: string,
  pl: string | null,
  day: string | null = null,
): Position {
  return {
    instrument_id: instrumentId,
    symbol,
    name: symbol,
    market: 'BIST',
    currency: 'TRY',
    quantity: '1',
    avg_cost: '0',
    cost_basis: '0',
    last_price: null,
    has_price: true,
    price_source: null,
    price_date: null,
    prev_close: null,
    prev_price_date: null,
    day_change: day,
    day_change_reporting: day,
    market_value: '0',
    unrealized_pl: pl ?? '0',
    realized_pl: '0',
    market_value_reporting: '0',
    unrealized_pl_reporting: pl,
    realized_pl_reporting: '0',
    total_pl_reporting: pl,
    closed: false,
  }
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

  it('leaves out positions without the chosen value', () => {
    const positions = [
      position(1, 'A', '3.00', null),
      position(2, 'B', null, '1.50'),
      position(3, 'C', '-1.00', '-0.50'),
    ]
    expect(brief(contributionRows(positions, 'total', 4).rows)).toEqual([
      ['A', '3.00', 1],
      ['C', '-1.00', 0.3333],
    ])
    expect(brief(contributionRows(positions, 'day', 4).rows)).toEqual([
      ['B', '1.50', 1],
      ['C', '-0.50', 0.3333],
    ])
    expect(contributionRows([position(1, 'A', null)], 'total', 4)).toEqual({ rows: [], hidden: 0 })
  })

  it('leaves out a position without a price, whose P/L the API sends as 0.00', () => {
    const unpriced = { ...position(9, 'QAN', '0.00', null), has_price: false }
    const positions = [
      ...Array.from({ length: 10 }, (_, i) => position(i + 1, `S${i}`, String(i + 1))),
      unpriced,
    ]
    expect(contributionRows(positions, 'total', null).rows.map((row) => row.symbol)).not.toContain(
      'QAN',
    )
    expect(contributionRows(positions, 'total', 4).hidden).toBe(2)
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
