import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { setPrivacy } from '../lib/privacy'
import { ReturnSplitCard } from './ReturnSplitCard'

afterEach(() => {
  setPrivacy(false)
  vi.restoreAllMocks()
})

const response = (total: Record<string, string | null>, missing: string[] = []) => ({
  currency: 'TRY',
  from: '2026-01-01',
  to: '2026-03-31',
  total,
  positions: [],
  missing_rates: missing,
})

function renderCard(body: unknown) {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async () => Response.json(body))
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <ReturnSplitCard scope={1} range={{ key: 'custom', from: '2026-01-01', to: '2026-03-31' }} />
    </QueryClientProvider>,
  )
  return fetch
}

const row = (name: string) => screen.getByText(name).closest('li') as HTMLElement
const bar = (li: HTMLElement) => li.querySelector('[data-bar]') as HTMLElement

describe('ReturnSplitCard', () => {
  it('draws the four buckets as signed bars with their amounts', async () => {
    const fetch = renderCard(
      response({ price: '100.00', income: '10.00', fx: '-50.00', cash_fx: '0.00' }),
    )

    expect(screen.getByRole('heading', { name: 'Getiri dağılımı' })).toBeInTheDocument()
    await screen.findByText('Fiyat')
    expect(String(fetch.mock.calls[0][0])).toBe(
      '/api/portfolios/1/attribution?to=2026-03-31&from=2026-01-01',
    )
    expect(within(row('Fiyat')).getByText('+₺100,00')).toHaveClass('text-gain')
    expect(bar(row('Fiyat'))).toHaveClass('left-1/2', 'bg-gain')
    expect(bar(row('Fiyat')).style.width).toBe('50%')
    expect(within(row('Gelir')).getByText('Net temettü ve faiz')).toBeInTheDocument()
    expect(bar(row('Gelir')).style.width).toBe('5%')
    expect(within(row('Kur farkı')).getByText('-₺50,00')).toHaveClass('text-loss')
    expect(bar(row('Kur farkı'))).toHaveClass('right-1/2', 'bg-loss')
    expect(bar(row('Kur farkı')).style.width).toBe('25%')
    expect(within(row('Nakit kur farkı')).getByText('₺0,00')).toBeInTheDocument()
    expect(bar(row('Nakit kur farkı')).style.width).toBe('0%')
  })

  it('shows MISSING and the missing-rates mark for a null bucket', async () => {
    renderCard(response({ price: '100.00', income: '0.00', fx: null, cash_fx: null }, ['USD']))

    const fx = await screen.findByText('Kur farkı')
    const li = fx.closest('li') as HTMLElement
    expect(li).toHaveTextContent('—')
    expect(within(li).getByRole('img', { name: /USD/ })).toBeInTheDocument()
    expect(bar(li).style.width).toBe('0%')
    expect(within(row('Fiyat')).queryByRole('img')).toBeNull()
  })

  it('masks every amount in privacy mode and keeps the bars', async () => {
    setPrivacy(true)
    renderCard(response({ price: '100.00', income: '10.00', fx: '-50.00', cash_fx: '0.00' }))

    await screen.findByText('Fiyat')
    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(4)
    for (const li of items) expect(li.textContent).not.toMatch(/\d/)
    expect(bar(row('Fiyat')).style.width).toBe('50%')
    expect(bar(row('Kur farkı')).style.width).toBe('25%')
  })

  it('shows the four rows at zero for a range with no holdings', async () => {
    renderCard(response({ price: '0.00', income: '0.00', fx: '0.00', cash_fx: '0.00' }))

    await screen.findByText('Fiyat')
    expect(screen.getAllByText('₺0,00')).toHaveLength(4)
  })
})
