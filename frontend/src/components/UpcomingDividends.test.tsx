import { cleanup, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpectedDividend } from '../api/dividends'
import { t } from '../i18n/strings'
import { UpcomingDividends } from './UpcomingDividends'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const dividend = (
  instrument_id: number,
  symbol: string,
  date: string,
  gross: string,
  stopaj: string,
  net: string,
  currency = 'TRY',
): ExpectedDividend => ({
  instrument_id,
  symbol,
  market: currency === 'TRY' ? 'BIST' : 'US',
  date,
  quantity: '100',
  per_share: '1.5',
  gross,
  stopaj,
  net,
  currency,
})

function renderCard(rows: ExpectedDividend[]) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (String(input) === '/api/portfolios/1/dividends/upcoming') return Response.json(rows)
    throw new Error(`unexpected request: ${input}`)
  })
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <UpcomingDividends portfolioId={1} />
    </QueryClientProvider>,
  )
}

it('lists the dividends by month with each month’s net total', async () => {
  renderCard([
    dividend(7, 'THYAO', '2026-10-05', '150.00', '22.50', '127.50'),
    dividend(8, 'AKBNK', '2026-10-20', '100.00', '15.00', '85.00'),
    dividend(9, 'AAPL', '2026-10-21', '20.00', '3.00', '17.00', 'USD'),
    dividend(10, 'GARAN', '2026-11-03', '200.00', '30.00', '170.00'),
  ])

  expect(
    await screen.findByRole('heading', { name: t('dividends.upcoming.title') }),
  ).toBeInTheDocument()
  const months = screen.getAllByRole('heading', { level: 3 })
  expect(months.map((h) => h.textContent)).toEqual(['Ekim 2026', 'Kasım 2026'])

  // ₺127,50 + ₺85,00 added exactly, and the dollars kept apart.
  const october = months[0].parentElement!
  expect(october).toHaveTextContent(`${t('dividends.upcoming.monthNet')} ₺212,50 + $17,00`)
  expect(months[1].parentElement).toHaveTextContent(`${t('dividends.upcoming.monthNet')} ₺170,00`)

  const rows = screen.getAllByRole('listitem')
  expect(rows).toHaveLength(4)
  expect(rows[0]).toHaveTextContent('05.10.2026')
  expect(rows[0]).toHaveTextContent('THYAO')
  expect(rows[0]).toHaveTextContent('₺150,00')
  expect(rows[0]).toHaveTextContent('₺22,50')
  expect(rows[0]).toHaveTextContent('₺127,50')
  for (const row of rows) {
    expect(within(row).getByText(t('dividends.upcoming.planned'))).toBeInTheDocument()
  }
})

it('renders nothing when no dividend is upcoming', async () => {
  const { container } = renderCard([])

  await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})
