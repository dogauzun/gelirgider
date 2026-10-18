import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import type { PortfolioScope, Position, Summary } from '../api/summary'
import { formatUnitPrice } from '../lib/format'
import { mockChartSize } from '../test/chartSize'
import { PositionDialog } from './PositionDialog'

beforeAll(() => {
  // jsdom implements neither showModal nor matchMedia.
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  window.matchMedia = (query) =>
    ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
})
afterEach(() => {
  restoreSize()
  vi.restoreAllMocks()
})

const THYAO = {
  instrument_id: 7,
  symbol: 'THYAO',
  name: 'Türk Hava Yolları',
  market: 'BIST',
  currency: 'TRY',
  quantity: '10',
  avg_cost: '100.5',
  cost_basis: '1005.00',
  last_price: '125.00',
  has_price: true,
  price_source: 'yahoo',
  price_date: '2026-09-26',
  prev_close: '120.00',
  prev_price_date: '2026-09-25',
  day_change: '50.00',
  day_change_reporting: '50.00',
  market_value: '1250.00',
  unrealized_pl: '245.00',
  realized_pl: '80.00',
  market_value_reporting: '1250.00',
  unrealized_pl_reporting: '245.00',
  realized_pl_reporting: '80.00',
  closed: false,
} as Position
const ASELS = { ...THYAO, instrument_id: 8, symbol: 'ASELS', name: 'Aselsan' }
const KCHOL = {
  ...THYAO,
  instrument_id: 9,
  symbol: 'KCHOL',
  name: 'Koç Holding',
  quantity: '0',
  market_value_reporting: null,
  unrealized_pl_reporting: null,
  closed: true,
} as Position
const positions = [THYAO, ASELS, KCHOL]

const summary = {
  currency: 'TRY',
  cash: '500.00',
  cash_by_currency: {},
  total_value: '3000.00',
  unrealized_pl: '490.00',
  realized_pl: '160.00',
  missing_rates: [],
  positions,
} as unknown as Summary

const tx = (id: number, date: string) => ({
  id,
  portfolio_id: 1,
  instrument_id: 7,
  instrument_symbol: 'THYAO',
  instrument_name: 'Türk Hava Yolları',
  type: 'buy',
  date,
  quantity: '1',
  price: '100.5',
  amount: '100.50',
  fee: '0',
  currency: 'TRY',
  note: null,
  created_at: `${date}T10:00:00Z`,
  import_id: null,
})

// Answers the dialog's requests for portfolio 1; anything else fails.
function mockApi() {
  const urls: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    urls.push(url)
    if (url === '/api/portfolios/1/dividends/timeline') {
      return Response.json({
        currency: 'TRY',
        months: [],
        years: [],
        by_instrument: [],
        missing_rates: [],
      })
    }
    if (/^\/api\/portfolios\/1\/instruments\/\d+\/history\?/.test(url)) {
      const point = (date: string) => ({
        date,
        close: '120.00',
        quantity: '10',
        avg_cost: '100.5',
        cost_basis: '1005.00',
        market_value: '1200.00',
      })
      return Response.json({
        currency: 'TRY',
        returns: { simple: '0.125', twr: '0.090909', mwr: '0.090909' },
        returns_from: '2026-09-25',
        points: [point('2026-09-25'), point('2026-09-26')],
      })
    }
    if (url.startsWith('/api/portfolios/1/transactions?limit=100&instrument_id=')) {
      // Seven, newest first, as the API pages them.
      return Response.json([1, 2, 3, 4, 5, 6, 7].map((d) => tx(8 - d, `2026-09-0${8 - d}`)))
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return urls
}

// The dialog with its current position in state, as the positions card
// will hold it.
function Harness({
  scope,
  start,
  onTrade,
}: {
  scope: PortfolioScope
  start: Position
  onTrade?: () => void
}) {
  const [current, setCurrent] = useState(start)
  return (
    <PositionDialog
      scope={scope}
      summary={summary}
      positions={positions}
      position={current}
      range={{ key: '1y', from: '', to: '' }}
      onChange={setCurrent}
      onTrade={onTrade}
      onEditPrice={() => {}}
      onClose={() => {}}
    />
  )
}

function renderDialog(scope: PortfolioScope, start: Position, onTrade?: () => void) {
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/portfolios/1?range=1y']}>
        <Harness scope={scope} start={start} onTrade={onTrade} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return screen.getByRole('dialog')
}

it('shows an open position: title, actions, KPIs with dividends, chart and the last five transactions', async () => {
  const urls = mockApi()
  const onTrade = vi.fn()
  const dialog = renderDialog(1, THYAO, onTrade)

  const title = within(dialog).getByRole('heading', { level: 2 })
  expect(within(title).getByRole('link', { name: 'THYAO' })).toHaveAttribute(
    'href',
    '/portfolios/1/positions/7?range=1y',
  )
  expect(within(dialog).getByText('Türk Hava Yolları')).toBeInTheDocument()
  expect(within(dialog).getByText('BIST')).toBeInTheDocument()
  expect(within(dialog).getByText(formatUnitPrice('125.00', 'TRY'))).toBeInTheDocument()
  expect(within(dialog).getByText('+%4,2 bugün')).toBeInTheDocument()
  expect(within(dialog).queryByText('Kapandı')).toBeNull()

  fireEvent.click(within(dialog).getByRole('button', { name: 'Sat' }))
  expect(onTrade).toHaveBeenCalledWith('sell', THYAO)
  expect(within(dialog).getByRole('button', { name: 'Al' })).toBeInTheDocument()
  expect(within(dialog).getByRole('button', { name: 'Fiyat gir' })).toBeInTheDocument()

  expect(within(dialog).getByText('Piyasa değeri')).toBeInTheDocument()
  expect(await within(dialog).findByText('Temettü')).toBeInTheDocument()

  await waitFor(() => expect(dialog.querySelector('.recharts-wrapper')).not.toBeNull())
  expect(urls.some((u) => u.startsWith('/api/portfolios/1/instruments/7/history?'))).toBe(true)
  const table = await within(dialog).findByRole('table')
  expect(within(table).getAllByRole('row')).toHaveLength(1 + 5)
})

it('offers only Al on a closed position and shows its closed state', () => {
  mockApi()
  const dialog = renderDialog(1, KCHOL)
  expect(within(dialog).getByText('Kapandı')).toBeInTheDocument()
  expect(within(dialog).getByRole('button', { name: 'Al' })).toBeInTheDocument()
  expect(within(dialog).queryByRole('button', { name: 'Sat' })).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'Fiyat gir' })).toBeNull()
  expect(within(dialog).queryByText('Piyasa değeri')).toBeNull()
})

it('on the all scope shows the summary row figures alone: no link, actions, dividends, chart or transactions', () => {
  const urls = mockApi()
  const dialog = renderDialog('all', THYAO)
  const title = within(dialog).getByRole('heading', { level: 2, name: 'THYAO' })
  expect(within(title).queryByRole('link')).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'Al' })).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'Sat' })).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'Fiyat gir' })).toBeNull()
  expect(within(dialog).getByText('Piyasa değeri')).toBeInTheDocument()
  expect(within(dialog).queryByText('Temettü')).toBeNull()
  expect(within(dialog).queryByText('Son işlemler')).toBeNull()
  expect(within(dialog).queryByRole('table')).toBeNull()
  expect(urls).toEqual([])
})

it('moves through the positions by the buttons and the arrow keys, disabled at the ends', () => {
  mockApi()
  const dialog = renderDialog('all', THYAO)
  const heading = () => within(dialog).getByRole('heading', { level: 2 }).textContent
  const previous = within(dialog).getByRole('button', { name: 'Önceki pozisyon' })
  const next = within(dialog).getByRole('button', { name: 'Sonraki pozisyon' })

  expect(previous).toBeDisabled()
  fireEvent.keyDown(dialog, { key: 'ArrowLeft' })
  expect(heading()).toBe('THYAO')

  fireEvent.click(next)
  expect(heading()).toBe('ASELS')
  expect(previous).toBeEnabled()

  fireEvent.keyDown(dialog, { key: 'ArrowRight' })
  expect(heading()).toBe('KCHOL')
  expect(next).toBeDisabled()
  fireEvent.keyDown(dialog, { key: 'ArrowRight' })
  expect(heading()).toBe('KCHOL')

  fireEvent.keyDown(dialog, { key: 'ArrowLeft' })
  expect(heading()).toBe('ASELS')
  fireEvent.click(previous)
  expect(heading()).toBe('THYAO')
})
