import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Portfolio } from '../api/portfolios'
import { setPrivacy } from '../lib/privacy'
import { PortfoliosTable } from './PortfoliosTable'

function portfolio(id: number, name: string, total_value: string): Portfolio {
  return {
    id,
    name,
    created_at: '2026-01-01T00:00:00Z',
    total_value,
    cash: '0.00',
    unrealized_pl: null,
    currency: 'TRY',
    missing_rates: [],
  }
}

// One open position per portfolio, carrying the portfolio's day change.
function summary(day: string | null) {
  return {
    currency: 'TRY',
    day_change: day,
    positions: [
      { instrument_id: 1, closed: false, day_change_reporting: day, price_date: '2026-10-02' },
      { instrument_id: 2, closed: true, day_change_reporting: null, price_date: null },
    ],
  }
}

function history(returns: string, values: string[]) {
  return {
    currency: 'TRY',
    points: values.map((total_value, i) => ({
      date: `2026-0${i + 1}-01`,
      total_value,
      cash: '0',
      cost_basis: '0',
    })),
    missing_rates: [],
    missing_prices: [],
    net_flow: '0.00',
    returns: { simple: returns, twr: returns, mwr: returns },
    benchmark: null,
  }
}

function LocationProbe() {
  const { pathname, search } = useLocation()
  return <output data-testid="location">{pathname + search}</output>
}

// Portfolio 3's history fails; the others answer.
function mockApi() {
  const urls: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    urls.push(url)
    if (url === '/api/portfolios/1/summary') return Response.json(summary('9.00'))
    if (url === '/api/portfolios/2/summary') return Response.json(summary('-4.50'))
    if (url === '/api/portfolios/3/summary') return Response.json(summary(null))
    if (url.startsWith('/api/portfolios/1/history?'))
      return Response.json(history('0.081000', ['100', '110', '120']))
    if (url.startsWith('/api/portfolios/2/history?'))
      return Response.json(history('-0.032000', ['100', '90']))
    if (url.startsWith('/api/portfolios/3/history?'))
      return Response.json({ error: 'boom' }, { status: 500 })
    throw new Error(`unexpected request: ${url}`)
  })
  return urls
}

const three = [
  portfolio(1, 'Ana portföy', '600.00'),
  portfolio(2, 'Emeklilik', '300.00'),
  portfolio(3, 'Kripto', '100.00'),
]

function renderTable(portfolios: Portfolio[] = three) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/portfolios/all?range=1y']}>
        <Routes>
          <Route
            path="/portfolios/:id"
            element={
              <PortfoliosTable
                portfolios={portfolios}
                range={{ key: '1y', from: '', to: '' }}
                method="twr"
              />
            }
          />
        </Routes>
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const rowOf = (name: string) => screen.getByRole('link', { name }).closest('tr')!

describe('PortfoliosTable', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    setPrivacy(false)
  })

  // The table scrolls sideways inside the card, never the page.
  it('scrolls the table sideways inside the card', () => {
    mockApi()
    renderTable()
    expect(screen.getByRole('table').parentElement).toHaveClass('relative', 'overflow-x-auto')
  })

  it('shows each portfolio with value, share, return, sparkline and day change', async () => {
    const urls = mockApi()
    renderTable()
    expect(screen.getAllByRole('columnheader').map((th) => th.textContent?.trim())).toEqual([
      'Portföy',
      'Değer',
      'Pay',
      'Getiri · 1Y',
      'Eğilim',
      'Günlük',
    ])
    // The sparkline column is named for screen readers.
    expect(screen.getByRole('columnheader', { name: 'Eğilim' })).toBeInTheDocument()
    const ana = within(rowOf('Ana portföy'))
    expect(ana.getByText('₺600,00')).toBeInTheDocument()
    expect(ana.getByText('%60,0')).toBeInTheDocument()
    expect(await ana.findByText('+%8,1')).toBeInTheDocument()
    expect(await ana.findByText('+₺9,00')).toBeInTheDocument()
    expect(rowOf('Ana portföy').querySelector('svg path')).not.toBeNull()
    const emeklilik = within(rowOf('Emeklilik'))
    expect(await emeklilik.findByText('-%3,2')).toBeInTheDocument()
    expect(await emeklilik.findByText('-₺4,50')).toBeInTheDocument()
    // The return is the chosen method's over the chosen range.
    expect(urls.some((u) => /^\/api\/portfolios\/1\/history\?.*from=/.test(u))).toBe(true)
  })

  it('opens the portfolio from its name or its row, keeping the range', async () => {
    mockApi()
    renderTable()
    expect(screen.getByRole('link', { name: 'Emeklilik' })).toHaveAttribute(
      'href',
      '/portfolios/2?range=1y',
    )
    fireEvent.click(within(rowOf('Kripto')).getByText('₺100,00'))
    expect(screen.getByTestId('location')).toHaveTextContent('/portfolios/3?range=1y')
  })

  it('shows a dash for the return of a row whose history fails, and keeps the rest', async () => {
    mockApi()
    renderTable()
    const kripto = within(rowOf('Kripto'))
    // Its summary answers but has no day change: a dash there too.
    await vi.waitFor(() => expect(kripto.getAllByText('—')).toHaveLength(2))
    expect(kripto.getByText('₺100,00')).toBeInTheDocument()
    expect(kripto.getByText('%10,0')).toBeInTheDocument()
    expect(rowOf('Kripto').querySelector('svg')).toBeNull()
  })

  it('gives shares that sum to 100% within rounding, a negative total none', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      Response.json({ error: 'boom' }, { status: 500 }),
    )
    renderTable([
      portfolio(1, 'Ana portföy', '1.00'),
      portfolio(2, 'Emeklilik', '1.00'),
      portfolio(3, 'Kripto', '1.00'),
      portfolio(4, 'Borç', '-5.00'),
    ])
    const shares = ['Ana portföy', 'Emeklilik', 'Kripto'].map((name) =>
      Number(
        within(rowOf(name)).getByText(/^%/).textContent!.replaceAll('%', '').replace(',', '.'),
      ),
    )
    const sum = shares.reduce((a, b) => a + b, 0)
    expect(sum).toBeGreaterThanOrEqual(99.8)
    expect(sum).toBeLessThanOrEqual(100.2)
    expect(within(rowOf('Borç')).queryByText(/^%/)).toBeNull()
  })

  it('masks the amounts in privacy mode', async () => {
    mockApi()
    setPrivacy(true)
    renderTable()
    const ana = within(rowOf('Ana portföy'))
    expect(await ana.findByText('+%8,1')).toBeInTheDocument()
    await vi.waitFor(() => expect(ana.getAllByText('•••')).toHaveLength(2))
    expect(ana.queryByText('₺600,00')).toBeNull()
  })
})
