import { describe, expect, it } from 'vitest'
import type { Position } from '../api/summary'
import { allocationRows } from './allocation'

function position(
  instrumentId: number,
  symbol: string,
  marketValueReporting: string | null,
  market = 'BIST',
  currency = 'TRY',
  assetClass: string | null = null,
): Position {
  return {
    instrument_id: instrumentId,
    symbol,
    name: symbol,
    market,
    asset_class: assetClass,
    currency,
    quantity: '1',
    avg_cost: '0',
    cost_basis: '0',
    last_price: null,
    has_price: true,
    price_source: null,
    price_date: null,
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    market_value: marketValueReporting ?? '0',
    unrealized_pl: '0',
    realized_pl: '0',
    market_value_reporting: marketValueReporting,
    unrealized_pl_reporting: '0',
    realized_pl_reporting: '0',
    total_pl_reporting: '0',
    closed: false,
  }
}

const brief = (rows: ReturnType<typeof allocationRows>['rows']) =>
  rows.map((row) => [row.label, row.amount, Number(row.share.toFixed(4))])

describe('allocationRows', () => {
  it('gives one row per position, largest first, and cash last', () => {
    const { rows, total, other } = allocationRows({
      positions: [position(1, 'GARAN', '100.00'), position(2, 'THYAO', '300.00')],
      cash: '100.00',
      cashByCurrencyReporting: { TRY: '100.00' },
      view: 'symbol',
    })

    // Cash is last even though it ties with GARAN.
    expect(brief(rows)).toEqual([
      ['THYAO', '300', 0.6],
      ['GARAN', '100', 0.2],
      ['Nakit', '100', 0.2],
    ])
    expect(rows.map((row) => row.color)).toEqual([
      'var(--color-cat-1)',
      'var(--color-cat-2)',
      'var(--color-cash)',
    ])
    expect(total).toBe(500)
    expect(other).toEqual([])
  })

  it('uses the reporting value and leaves out a position without one', () => {
    const { rows } = allocationRows({
      positions: [
        position(1, 'AAPL', '900.00', 'US', 'USD'),
        position(2, 'THYAO', '100.00'),
        position(3, 'MISSING', null),
        position(4, 'ZERO', '0'),
      ],
      cash: '0.00',
      cashByCurrencyReporting: {},
      view: 'symbol',
    })

    expect(brief(rows)).toEqual([
      ['AAPL', '900', 0.9],
      ['THYAO', '100', 0.1],
    ])
  })

  it('gives no rows when nothing has a reporting value', () => {
    expect(
      allocationRows({
        positions: [position(1, 'MISSING', null)],
        cash: '0.00',
        cashByCurrencyReporting: {},
        view: 'symbol',
      }).rows,
    ).toEqual([])
  })

  it('gives one row with share 1 when there is only cash', () => {
    const { rows } = allocationRows({
      positions: [],
      cash: '250.50',
      cashByCurrencyReporting: { TRY: '250.50' },
      view: 'symbol',
    })

    expect(brief(rows)).toEqual([['Nakit', '250.5', 1]])
  })

  it('groups by market and by currency, splitting cash by currency in the currency view', () => {
    const input = {
      positions: [
        position(1, 'THYAO', '300.00'),
        position(2, 'GARAN', '100.00'),
        position(3, 'AAPL', '200.00', 'US', 'USD'),
        position(4, 'BTC', '100.00', 'CRYPTO', 'USD'),
      ],
      cash: '300.00',
      cashByCurrencyReporting: { TRY: '200.00', USD: '100.00' },
    }

    expect(brief(allocationRows({ ...input, view: 'market' }).rows)).toEqual([
      ['BIST', '400', 0.4],
      ['US', '200', 0.2],
      ['Kripto', '100', 0.1],
      ['Nakit', '300', 0.3],
    ])
    // No "Nakit" row: TRY cash joins the TRY positions, USD cash the USD ones.
    expect(brief(allocationRows({ ...input, view: 'currency' }).rows)).toEqual([
      ['TRY', '600', 0.6],
      ['USD', '400', 0.4],
    ])
  })

  it('groups by asset class derived from the market and puts an unmapped market in its own row', () => {
    const { rows } = allocationRows({
      positions: [
        position(1, 'AFT', '200.00', 'TEFAS'),
        position(2, 'GRAM', '100.00', 'GOLD'),
        position(3, 'THYAO', '300.00'),
        position(4, 'MEV', '150.00', 'DEPOSIT'),
        position(5, 'AAPL', '50.00', 'US', 'USD'),
        position(6, 'WHAT', '100.00', 'MARS'),
      ],
      cash: '100.00',
      cashByCurrencyReporting: { TRY: '100.00' },
      view: 'assetClass',
    })

    expect(brief(rows)).toEqual([
      ['Hisse senedi', '350', 0.35],
      ['Fon', '200', 0.2],
      ['Mevduat', '150', 0.15],
      ['Emtia', '100', 0.1],
      ['Bilinmiyor', '100', 0.1],
      ['Nakit', '100', 0.1],
    ])
  })

  it('groups by the stored asset class over the market, and falls back to the market without one', () => {
    const { rows } = allocationRows({
      positions: [
        position(1, 'AFT', '200.00', 'BIST', 'TRY', 'fund'),
        position(2, 'THYAO', '300.00'),
        position(3, 'GLDTR', '100.00', 'BIST', 'TRY', 'commodity'),
        position(4, 'VTI', '100.00', 'US', 'USD', null),
      ],
      cash: '100.00',
      cashByCurrencyReporting: { TRY: '100.00' },
      view: 'assetClass',
    })

    expect(brief(rows)).toEqual([
      ['Hisse senedi', '400', 0.5],
      ['Fon', '200', 0.25],
      ['Emtia', '100', 0.125],
      ['Nakit', '100', 0.125],
    ])
    expect(rows.reduce((sum, row) => sum + row.share, 0)).toBeCloseTo(1, 10)
  })

  it('folds rows past the fifth into "Diğer", names its members and sums exactly', () => {
    const { rows, other } = allocationRows({
      positions: ['A', 'B', 'C', 'D', 'E', 'F', 'G'].map((symbol, i) =>
        position(i + 1, symbol, ['70', '60', '50', '40', '30', '0.1', '0.2'][i]),
      ),
      cash: '20.00',
      cashByCurrencyReporting: { TRY: '20.00' },
      view: 'symbol',
    })

    expect(rows.map((row) => [row.label, row.amount])).toEqual([
      ['A', '70'],
      ['B', '60'],
      ['C', '50'],
      ['D', '40'],
      ['E', '30'],
      ['Diğer (2)', '0.3'],
      ['Nakit', '20'],
    ])
    expect(other).toEqual(['G', 'F'])
    expect(new Set(rows.map((row) => row.color)).size).toBe(rows.length)
    expect(rows.reduce((sum, row) => sum + row.share, 0)).toBeCloseTo(1, 10)
  })
})
