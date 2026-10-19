import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { signedMoney, signedPercent } from '../lib/format'
import { TradesCard } from './TradesCard'

afterEach(() => {
  vi.restoreAllMocks()
})

const trade = (over: object) => ({
  portfolio_id: 1,
  date: '2025-03-03',
  instrument_id: 7,
  symbol: 'THYAO',
  name: 'Türk Hava Yolları',
  market: 'BIST',
  currency: 'TRY',
  quantity: '10',
  proceeds: '478.00',
  cost: '400.00',
  fee: '2.00',
  realized: '78.00',
  realized_pct: '0.1950',
  opened_at: '2025-03-02',
  days: 1,
  realized_reporting: '78.00',
  ...over,
})

// Newest first, as the server answers.
const sales = [
  trade({ date: '2025-03-05', realized: '-60.00', realized_pct: '-0.1000', days: 2 }),
  trade({}),
]

// `/trades` answers `trades`; the portfolio list a mocked one.
function renderCard(trades: object[], scope: PortfolioScope = 1, instrumentId?: number) {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2025-01-01T00:00:00Z' }])
    }
    if (url.includes('/trades')) {
      return Response.json({ currency: 'TRY', trades, missing_rates: [] })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <TradesCard scope={scope} instrumentId={instrumentId} />
    </QueryClientProvider>,
  )
  return fetch
}

describe('TradesCard', () => {
  it('lists the sells newest first with the realized money and percent', async () => {
    const fetch = renderCard(sales)

    const table = await screen.findByRole('table', { name: t('trades.title') })
    const rows = within(table).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent(`${signedMoney('-60.00', 'TRY')}${signedPercent(-0.1)}`)
    expect(rows[1]).toHaveTextContent(`${signedMoney('78.00', 'TRY')}${signedPercent(0.195)}`)
    expect(rows[0]).toHaveTextContent('THYAO')
    // No portfolio column on one portfolio.
    expect(screen.queryByRole('columnheader', { name: t('trades.portfolio') })).toBeNull()
    expect(String(fetch.mock.calls.find((c) => String(c[0]).includes('/trades'))?.[0])).toBe(
      '/api/portfolios/1/trades',
    )
    expect(screen.queryByText(/kuru yok/)).toBeNull()
  })

  it('says there are no sales yet', async () => {
    renderCard([])

    expect(await screen.findByText(t('trades.empty'))).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('prints the short sale label for a sell without days and counts the missing rates', async () => {
    renderCard([
      trade({ days: null, opened_at: null, realized_pct: null, realized_reporting: null }),
    ])

    expect(await screen.findByText(t('trades.short'))).toBeInTheDocument()
    expect(screen.getByText(t('trades.missingRate', { count: 1 }))).toBeInTheDocument()
  })

  it('names the portfolio on all', async () => {
    renderCard(sales, 'all')

    expect(await screen.findByRole('columnheader', { name: t('trades.portfolio') })).toBeVisible()
    expect(await screen.findAllByRole('cell', { name: 'Emeklilik' })).toHaveLength(2)
  })

  it('asks for one instrument and leaves its column out', async () => {
    const fetch = renderCard(sales, 1, 7)

    await screen.findByRole('table')
    expect(screen.queryByRole('columnheader', { name: t('trades.instrument') })).toBeNull()
    expect(fetch.mock.calls.map((c) => String(c[0]))).toContain(
      '/api/portfolios/1/trades?instrument_id=7',
    )
  })

  it('shows a load error with a retry', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input) === '/api/portfolios'
        ? Response.json([])
        : Response.json({ error: 'internal' }, { status: 500 }),
    )
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <TradesCard scope={1} />
      </QueryClientProvider>,
    )

    expect(await screen.findByText(t('trades.error'))).toBeInTheDocument()
  })
})
