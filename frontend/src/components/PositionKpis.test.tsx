import { render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { DividendForecastRow, DividendTimeline } from '../api/dividends'
import type { Position, Summary } from '../api/summary'
import { setLocale } from '../i18n/strings'
import { PositionKpis } from './PositionKpis'

const position = {
  instrument_id: 7,
  symbol: 'THYAO',
  currency: 'TRY',
  quantity: '10',
  avg_cost: '100.5',
  has_price: true,
  market_value_reporting: '1250.00',
  unrealized_pl_reporting: '245.00',
  realized_pl_reporting: '80.00',
  cost_basis: '1005.00',
  closed: false,
} as unknown as Position

const summary = { currency: 'TRY', cash: '0', positions: [position] } as unknown as Summary

// The read-only `all` scope has no dividends route (ADR 0014): null leaves
// the dividends cell out, and the average cost no longer spans the phone row.
it('leaves the dividends cell out when dividends is null', () => {
  render(<PositionKpis position={position} summary={summary} dividends={null} />)
  expect(screen.queryByText('Temettü')).toBeNull()
  expect(screen.getByText('Gerçekleşmiş')).toBeInTheDocument()
  const avgCost = screen.getByText('Ort. maliyet').parentElement!
  expect(avgCost).not.toHaveClass('col-span-2')
  expect(avgCost.closest('section')).toHaveClass('lg:grid-cols-5')
})

// En read "1 units" for a quantity of exactly 1, whatever its scale.
afterEach(() => setLocale('tr'))

it('reads the English singular for a quantity of 1', () => {
  setLocale('en')
  const cases: [string, RegExp][] = [
    ['1', /^1 unit\b(?!s)/],
    ['1.00', /^1 unit\b(?!s)/],
    ['2', /^2 units/],
    ['0.05', /^0\.05 units/],
  ]
  for (const [quantity, text] of cases) {
    const p = { ...position, quantity }
    const { unmount } = render(
      <PositionKpis position={p} summary={{ ...summary, positions: [p] }} dividends={null} />,
    )
    expect(screen.getByText(text)).toBeInTheDocument()
    unmount()
  }
})

it('keeps the Turkish adet for a quantity of 1', () => {
  const p = { ...position, quantity: '1' }
  render(<PositionKpis position={p} summary={{ ...summary, positions: [p] }} dividends={null} />)
  expect(screen.getByText(/^1 adet/)).toBeInTheDocument()
})

// A closed position has no fx_pl_reporting hint, so the range's currency
// figure comes from its attribution entry; a null figure reads MISSING.
it('shows the range split under the total of a closed USD position', () => {
  const p = {
    ...position,
    currency: 'USD',
    quantity: '0',
    closed: true,
    fx_pl_reporting: null,
  } as unknown as Position
  const attribution = {
    instrument_id: 7,
    symbol: 'THYAO',
    closed: true,
    price: '120.00',
    income: null,
    fx: '-35.00',
    total: null,
  }
  render(
    <PositionKpis
      position={p}
      summary={{ ...summary, positions: [p] }}
      dividends={null}
      attribution={attribution}
    />,
  )
  const row = (label: string) => screen.getByText(label, { selector: 'span' }).parentElement!
  expect(row('Kur farkı')).toHaveTextContent('₺35,00')
  expect(row('Kur farkı')).not.toHaveTextContent('—')
  expect(row('Fiyat')).toHaveTextContent('+₺120,00')
  expect(row('Gelir')).toHaveTextContent('—')
})

it('shows no range split without an attribution entry', () => {
  render(<PositionKpis position={position} summary={summary} dividends={null} />)
  expect(screen.queryByText('Seçilen aralık')).toBeNull()
})

const forecastRow = (net_reporting: string | null) =>
  ({ instrument_id: 7, net_reporting }) as unknown as DividendForecastRow

const timeline = {
  currency: 'TRY',
  months: [],
  years: [],
  by_instrument: [{ instrument_id: 7, symbol: 'THYAO', name: 'THY', count: 2, net: '80.00' }],
  missing_rates: [],
} as unknown as DividendTimeline

// The rows' net adds up exactly; the yield is that over the market value.
it('shows the expected annual dividend with its yield on the market value', () => {
  const p = { ...position, market_value_reporting: '2000.00' }
  render(
    <PositionKpis
      position={p}
      summary={{ ...summary, positions: [p] }}
      dividends={timeline}
      forecast={[forecastRow('20.10'), forecastRow('29.90'), forecastRow(null)]}
    />,
  )
  expect(screen.getByText('yılda ~₺50,00 bekleniyor · verim %2,5')).toBeInTheDocument()
})

it('leaves the yield out when the market value is null or zero', () => {
  for (const market_value_reporting of [null, '0.00']) {
    const p = { ...position, market_value_reporting } as unknown as Position
    const { unmount } = render(
      <PositionKpis
        position={p}
        summary={{ ...summary, positions: [p] }}
        dividends={timeline}
        forecast={[forecastRow('50.00')]}
      />,
    )
    expect(screen.getByText('yılda ~₺50,00 bekleniyor')).toBeInTheDocument()
    unmount()
  }
})

it('shows no forecast line without a forecast row', () => {
  render(<PositionKpis position={position} summary={summary} dividends={timeline} />)
  expect(screen.queryByText(/bekleniyor/)).toBeNull()
})
