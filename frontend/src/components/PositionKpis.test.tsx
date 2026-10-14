import { render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
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
