import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { mockChartSize } from '../test/chartSize'
import { DividendsPage } from './DividendsPage'

// jsdom has no showModal() on <dialog>; this only flips the open state. Nor
// matchMedia: the expected dividends card picks its table by viewport.
beforeAll(() => {
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
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
})
afterEach(() => {
  restoreSize()
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
})

const bucket = (period: string, net: string, tax: string, interest = '0.00') => ({
  period,
  gross: net,
  tax,
  net,
  interest,
})

const timeline = {
  currency: 'TRY',
  // 2024-06 is older than the chart's 24 months and the last 12 months.
  months: [bucket('2024-06', '50.00', '7.50'), bucket('2026-05', '100.00', '15.00', '5.00')],
  years: [bucket('2024', '50.00', '7.50'), bucket('2026', '100.00', '15.00', '5.00')],
  by_instrument: [
    {
      instrument_id: 7,
      symbol: 'THYAO',
      name: 'Türk Hava Yolları',
      count: 2,
      net: '150.00',
      last_date: '2026-05-04',
    },
    {
      instrument_id: 8,
      symbol: 'AKBNK',
      name: 'Akbank',
      count: 1,
      net: '50.00',
      last_date: '2024-06-03',
    },
  ],
  missing_rates: [],
}

const forecast = {
  currency: 'TRY',
  months: Array.from({ length: 12 }, (_, i) => ({
    period: `${2026 + Math.floor((9 + i) / 12)}-${String(((9 + i) % 12) + 1).padStart(2, '0')}`,
    net: i === 0 ? '100.00' : '0.00',
  })),
  rows: [],
  missing_rates: [] as string[],
}

const expectedRow = {
  instrument_id: 7,
  symbol: 'THYAO',
  market: 'BIST',
  date: '2026-09-01',
  quantity: '100',
  per_share: '1.5',
  gross: '150.00',
  stopaj: '22.50',
  net: '127.50',
  currency: 'TRY',
}

function mockApi({
  data = timeline,
  expected = [expectedRow],
  upcoming = [] as (typeof expectedRow)[],
  // The open positions' reporting cost the yield divides by.
  costBasis = '1000.00',
  positionsValue = '2000.00',
  forecastData = forecast,
}: {
  data?: typeof timeline
  expected?: (typeof expectedRow)[]
  upcoming?: (typeof expectedRow)[]
  costBasis?: string
  positionsValue?: string
  forecastData?: typeof forecast
} = {}) {
  const calls = { timeline: 0, posts: [] as string[] }
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios/1/transactions' && init?.method === 'POST') {
      calls.posts.push(init.body as string)
      return Response.json({ id: 9 }, { status: 201 })
    }
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/dividends/timeline') {
      calls.timeline++
      return Response.json(data)
    }
    if (url === '/api/portfolios/1/dividends/forecast') return Response.json(forecastData)
    if (url === '/api/portfolios/1/dividends/expected') return Response.json(expected)
    if (url === '/api/portfolios/1/dividends/upcoming') return Response.json(upcoming)
    if (url === '/api/portfolios/1/summary')
      return Response.json({
        currency: 'TRY',
        cost_basis: costBasis,
        positions_value: positionsValue,
        positions: [],
      })
    throw new Error(`unexpected request: ${url}`)
  })
  return calls
}

function renderPage(search = '') {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[`/portfolios/1/dividends${search}`]}>
        <Routes>
          <Route path="/portfolios/:id/dividends" element={<DividendsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// A KPI cell's figure: the paragraph after its label.
const kpi = async (key: Parameters<typeof t>[0]) =>
  (await screen.findByText(t(key))).nextElementSibling?.textContent

it('leads with the dividends to confirm, open, and books them from there', async () => {
  const calls = mockApi()
  const { container } = renderPage()

  const title = await screen.findByRole('heading', { name: t('dividends.expected.title') })
  await screen.findByText(t('dividends.page.last12'))
  // The first card on the page, above the KPI strip, and open.
  expect(container.querySelector('main section')).toBe(title.closest('section'))
  const vars = { symbol: 'THYAO', date: '01.09.2026' }
  expect(screen.getByRole('button', { name: t('dividends.expected.addLabel', vars) })).toBeVisible()
  expect(
    screen.getByRole('button', { name: t('dividends.expected.dismissLabel', vars) }),
  ).toBeVisible()

  // Booking the expected dividend refetches the timeline, so the figures
  // below take the new row in.
  expect(calls.timeline).toBe(1)
  fireEvent.click(screen.getByRole('button', { name: t('dividends.expected.addAll') }))
  await waitFor(() => expect(calls.timeline).toBe(2))
  expect(calls.posts).toHaveLength(1)
})

it('links an instrument to its position page with the current range', async () => {
  mockApi()
  renderPage('?range=1y&x=1')

  expect(await screen.findByRole('link', { name: 'THYAO' })).toHaveAttribute(
    'href',
    '/portfolios/1/positions/7?range=1y',
  )
})

it('shows the upcoming dividends after the confirm card, even with nothing received', async () => {
  mockApi({
    data: { ...timeline, months: [], years: [], by_instrument: [] },
    upcoming: [{ ...expectedRow, date: '2026-10-05' }],
  })
  renderPage()

  const upcoming = await screen.findByRole('heading', { name: t('dividends.upcoming.title') })
  expect(screen.getByText(t('dividends.page.empty'))).toBeInTheDocument()
  expect(within(upcoming.closest('section')!).getByText('THYAO')).toBeInTheDocument()
  const confirm = screen.getByRole('heading', { name: t('dividends.expected.title') })
  expect(confirm.compareDocumentPosition(upcoming) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('has no confirm card when nothing is expected', async () => {
  mockApi({ expected: [] })
  renderPage()

  await screen.findByText(t('dividends.page.last12'))
  await waitFor(() =>
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/portfolios/1/dividends/expected',
      expect.anything(),
    ),
  )
  expect(screen.queryByText(t('dividends.expected.title'))).toBeNull()
})

it('shows the KPI cells with the yield on the open positions’ cost', async () => {
  mockApi()
  renderPage()

  expect(await kpi('dividends.page.last12')).toBe('₺100,00')
  // ₺100 over the open positions' ₺1.000 cost.
  await waitFor(async () => expect(await kpi('dividends.page.yieldOnCost')).toBe('%10,0'))
  expect(screen.getByText(t('dividends.page.yieldOnCostHint'))).toBeInTheDocument()
  expect(await kpi('dividends.page.thisYear')).toBe('₺100,00')
  expect(await kpi('dividends.page.totalNet')).toBe('₺150,00')
  expect(await kpi('dividends.page.totalTax')).toBe('₺22,50')
  expect(await kpi('dividends.page.interest')).toBe('₺5,00')
})

it('shows no yield when the open positions cost nothing', async () => {
  // Every position closed: there is no cost to divide by.
  mockApi({ costBasis: '0.00' })
  renderPage()

  expect(await kpi('dividends.page.last12')).toBe('₺100,00')
  await waitFor(() =>
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/portfolios/1/summary', expect.anything()),
  )
  expect(await kpi('dividends.page.yieldOnCost')).toBe('—')
})

it('draws a bar per month or year', async () => {
  mockApi()
  const { container } = renderPage()

  // Monthly: the last 24 months, zero-filled; yearly: 2024 through 2026.
  const bars = () => container.querySelectorAll('.series-bars .recharts-bar-rectangle')
  // The chart arrives with its lazy chunk, which can take longer
  // than vi.waitFor's fixed 1s in a parallel run; waitFor has setupTests.ts's
  // longer timeout.
  await waitFor(() => expect(bars()).toHaveLength(24))
  fireEvent.click(screen.getByRole('radio', { name: t('dividends.page.yearly') }))
  expect(bars()).toHaveLength(3)
  expect(localStorage.getItem('dividendsGroup')).toBe('year')
})

it('shows the twelve-month forecast net and yield, and charts it', async () => {
  mockApi({ forecastData: { ...forecast, missing_rates: ['EUR'] } })
  const { container } = renderPage()

  expect(await kpi('dividends.page.forecast12')).toBe('₺100,00')
  await waitFor(async () => expect(await kpi('dividends.page.forwardYield')).toBe('%5,0'))
  // The forecast's currency joins the timeline's (none here) in the Notice.
  expect(await screen.findByText(/EUR/)).toBeVisible()

  fireEvent.click(screen.getByRole('radio', { name: t('dividends.page.forecast') }))
  const bars = () => container.querySelectorAll('.series-bars .recharts-bar-rectangle')
  await waitFor(() => expect(bars()).toHaveLength(12))
  expect(screen.getByText(t('dividends.page.forecastHint'))).toBeVisible()
  expect(localStorage.getItem('dividendsGroup')).toBe('forecast')
})

it('lists payments, share and the last payment with its year per instrument', async () => {
  mockApi()
  renderPage()

  const table = (await screen.findByRole('link', { name: 'THYAO' })).closest('table')!
  expect(
    within(table)
      .getAllByRole('columnheader')
      .map((th) => th.textContent),
  ).toEqual(['Enstrüman', 'Ödeme sayısı', 'Pay', 'Net↓', 'Son ödeme'])
  const cells = (symbol: string) =>
    Array.from(within(table).getByRole('link', { name: symbol }).closest('tr')!.cells).map(
      (td) => td.textContent,
    )
  // The year is there even for this year's date; the share is of ₺200 net.
  // Under the symbol: the name, or on a phone the payment count, whose
  // column is left out there with Pay (CSS, not visible to jsdom).
  expect(cells('THYAO')).toEqual([
    'THYAOTürk Hava Yolları2 ödeme',
    '2',
    '%75,0',
    '₺150,00',
    '04 May 2026',
  ])
  expect(cells('AKBNK')).toEqual(['AKBNKAkbank1 ödeme', '1', '%25,0', '₺50,00', '03 Haz 2024'])
  expect(screen.getByRole('link', { name: 'THYAO' })).toHaveAttribute(
    'href',
    '/portfolios/1/positions/7',
  )

  // The table starts sorted by net, largest first, as the API sends it, and
  // says so; a click on net flips it.
  const symbols = () => Array.from(table.querySelectorAll('tbody a')).map((a) => a.textContent)
  const netHeader = within(table).getByRole('button', { name: t('dividends.page.net') })
  expect(symbols()).toEqual(['THYAO', 'AKBNK'])
  expect(netHeader.closest('th')).toHaveAttribute('aria-sort', 'descending')
  fireEvent.click(netHeader)
  expect(symbols()).toEqual(['AKBNK', 'THYAO'])
  expect(netHeader.closest('th')).toHaveAttribute('aria-sort', 'ascending')
  fireEvent.click(within(table).getByRole('button', { name: t('transactions.instrument') }))
  expect(symbols()).toEqual(['AKBNK', 'THYAO'])
  expect(netHeader.closest('th')).toHaveAttribute('aria-sort', 'none')
  fireEvent.click(within(table).getByRole('button', { name: t('dividends.page.lastPayment') }))
  expect(symbols()).toEqual(['THYAO', 'AKBNK'])
})

it('shows an empty state card with an action to add a dividend', async () => {
  mockApi({
    data: { ...timeline, months: [], years: [], by_instrument: [] },
    expected: [],
  })
  renderPage()

  const empty = await screen.findByText(t('dividends.page.empty'))
  expect(empty.closest('section')).toHaveClass('card')
  fireEvent.click(screen.getByRole('button', { name: t('dividends.page.emptyAdd') }))
  expect(await screen.findByRole('dialog')).toBeInTheDocument()
})
