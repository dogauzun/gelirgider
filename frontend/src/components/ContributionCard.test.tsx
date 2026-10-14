import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it } from 'vitest'
import type { Position } from '../api/summary'
import { setPrivacy } from '../lib/privacy'
import { ContributionCard } from './ContributionCard'

afterEach(() => setPrivacy(false))

function position(instrumentId: number, symbol: string, pl: string | null, day: string | null) {
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
  } satisfies Position
}

function renderCard(positions: Position[], portfolioId: number | 'all' = 7) {
  render(
    <MemoryRouter initialEntries={['/portfolios/7?range=1y']}>
      <ContributionCard positions={positions} currency="TRY" portfolioId={portfolioId} />
    </MemoryRouter>,
  )
  return screen.getByRole('heading', { name: /^Katkı/ }).closest('section') as HTMLElement
}

const items = (card: HTMLElement) =>
  within(card)
    .queryAllByRole('listitem')
    .map((li) => li.textContent)

const bar = (item: HTMLElement) => item.querySelector('[data-bar]') as HTMLElement

describe('ContributionCard', () => {
  it('lists each position with its signed amount, gains right of the zero line, losses left', () => {
    const card = renderCard([
      position(1, 'AKBNK', '-50.00', null),
      position(2, 'THYAO', '100.00', null),
    ])
    expect(items(card)).toEqual(['THYAO+₺100,00', 'AKBNK-₺50,00'])
    const [gain, loss] = within(card).getAllByRole('listitem')
    expect(bar(gain)).toHaveClass('left-1/2', 'bg-gain')
    expect(bar(gain).style.width).toBe('50%')
    expect(bar(loss)).toHaveClass('right-1/2', 'bg-loss')
    expect(bar(loss).style.width).toBe('25%')
    expect(within(gain).getByText('+₺100,00')).toHaveClass('text-gain')
    expect(within(loss).getByText('-₺50,00')).toHaveClass('text-loss')
  })

  it('switches between the total P/L and today', () => {
    const card = renderCard([
      position(1, 'AKBNK', '-50.00', '3.00'),
      position(2, 'THYAO', '100.00', null),
    ])
    const today = within(card).getByRole('radio', { name: 'Bugün' })
    expect(today).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(today)
    expect(today).toHaveAttribute('aria-checked', 'true')
    expect(items(card)).toEqual(['AKBNK+₺3,00'])
  })

  it('shows the best four and the worst four and expands to all', () => {
    const positions = Array.from({ length: 10 }, (_, i) =>
      position(i + 1, `S${i}`, `${(i - 4) * 10}.00`, null),
    )
    const card = renderCard(positions)
    expect(within(card).getAllByRole('listitem')).toHaveLength(8)
    expect(items(card)).not.toContain('S4₺0,00')
    fireEvent.click(within(card).getByRole('button', { name: 'Tümünü göster' }))
    expect(within(card).getAllByRole('listitem')).toHaveLength(10)
    expect(within(card).queryByRole('button', { name: 'Tümünü göster' })).not.toBeInTheDocument()
  })

  it('has no expand button with eight positions or fewer', () => {
    const card = renderCard(
      Array.from({ length: 8 }, (_, i) => position(i + 1, `S${i}`, `${i}.00`, null)),
    )
    expect(within(card).getAllByRole('listitem')).toHaveLength(8)
    expect(within(card).queryByRole('button', { name: 'Tümünü göster' })).not.toBeInTheDocument()
  })

  it('links a symbol to its position page with the range, plain text on all portfolios', () => {
    const card = renderCard([position(5, 'THYAO', '1.00', null)])
    expect(within(card).getByRole('link', { name: 'THYAO' })).toHaveAttribute(
      'href',
      '/portfolios/7/positions/5?range=1y',
    )
  })

  it('shows the symbols as plain text on all portfolios', () => {
    const card = renderCard([position(5, 'THYAO', '1.00', null)], 'all')
    expect(within(card).queryByRole('link')).not.toBeInTheDocument()
    expect(items(card)).toEqual(['THYAO+₺1,00'])
  })

  it('says so in one line when no position has the chosen value', () => {
    const card = renderCard([position(1, 'AKBNK', null, null)])
    expect(within(card).queryByRole('list')).not.toBeInTheDocument()
    expect(within(card).getByText('Gösterilecek kâr veya zarar yok')).toBeInTheDocument()
  })

  it('masks the amounts in privacy mode and keeps the bar lengths', () => {
    setPrivacy(true)
    const card = renderCard([
      position(1, 'AKBNK', '-50.00', null),
      position(2, 'THYAO', '100.00', null),
    ])
    expect(items(card)).toEqual(['THYAO•••', 'AKBNK•••'])
    const [gain, loss] = within(card).getAllByRole('listitem')
    expect(bar(gain).style.width).toBe('50%')
    expect(bar(loss).style.width).toBe('25%')
  })
})
