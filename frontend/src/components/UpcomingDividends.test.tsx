import { cleanup, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import type { DividendForecast } from '../api/dividends'
import { t } from '../i18n/strings'
import { UpcomingDividends } from './UpcomingDividends'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const months = ['2026-10', '2026-11', '2026-12'].map((period, i) => ({
  period,
  net: ['127.50', '0.00', '90.00'][i],
}))

const row = (
  period: string,
  date: string | null,
  symbol: string,
): DividendForecast['rows'][number] => ({
  instrument_id: 7,
  symbol,
  market: 'BIST',
  period,
  date,
  planned: date !== null,
  quantity: '100',
  per_share: '1.5',
  gross: '150.00',
  stopaj: '22.50',
  net: '127.50',
  currency: 'TRY',
  net_reporting: '127.50',
})

function renderCard(rows: DividendForecast['rows']) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (String(input) === '/api/portfolios/1/dividends/forecast')
      return Response.json({ currency: 'TRY', months, rows, missing_rates: [] })
    throw new Error(`unexpected request: ${input}`)
  })
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <UpcomingDividends portfolioId={1} />
    </QueryClientProvider>,
  )
}

it('groups planned and projected rows by period, skipping a month without rows', async () => {
  renderCard([row('2026-10', '2026-10-05', 'THYAO'), row('2026-12', null, 'AKBNK')])

  expect(
    await screen.findByRole('heading', { name: t('dividends.upcoming.title') }),
  ).toBeInTheDocument()
  const headings = screen.getAllByRole('heading', { level: 3 })
  expect(headings.map((h) => h.textContent)).toEqual(['Ekim 2026', 'Aralık 2026'])
  expect(headings[0].parentElement).toHaveTextContent(`${t('dividends.upcoming.monthNet')} ₺127,50`)
  expect(headings[1].parentElement).toHaveTextContent(`${t('dividends.upcoming.monthNet')} ₺90,00`)

  const [planned, projected] = screen.getAllByRole('listitem')
  expect(planned).toHaveTextContent('THYAO')
  expect(planned).toHaveTextContent('05.10.2026')
  expect(planned).toHaveTextContent('₺150,00')
  expect(planned).toHaveTextContent('₺22,50')
  expect(within(planned).getByText(t('dividends.upcoming.planned'))).toBeInTheDocument()
  expect(projected).toHaveTextContent('AKBNK')
  expect(projected).not.toHaveTextContent(/\d{2}\.\d{2}\.\d{4}/)
  expect(within(projected).getByText(t('dividends.upcoming.estimated'))).toBeInTheDocument()
  expect(within(projected).queryByText(t('dividends.upcoming.planned'))).not.toBeInTheDocument()
})

it('renders nothing when the forecast has no rows', async () => {
  const { container } = renderCard([])

  await vi.waitFor(() => expect(globalThis.fetch).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})
