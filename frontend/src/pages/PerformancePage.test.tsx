import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, expect, it, vi } from 'vitest'
import type { CashGap, PeriodBucket } from '../api/history'
import { t } from '../i18n/strings'
import { signedPercent } from '../lib/format'
import { mockChartSize } from '../test/chartSize'
import { chooseOption } from '../test/select'
import { PerformancePage } from './PerformancePage'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

const same = (v: string | null) => ({ simple: v, twr: v, mwr: v })
const bucket = (
  period: string,
  returns: PeriodBucket['returns'],
  benchmark = false,
  [from, to] = ['2026-01-01', '2026-01-28'],
) => ({
  period,
  from,
  to,
  partial: false,
  returns,
  benchmark: benchmark ? same('0.050000') : null,
})

const summary = {
  currency: 'TRY',
  cash: '0.00',
  cash_by_currency: {},
  cash_by_currency_reporting: {},
  total_value: '0.00',
  positions_value: '0.00',
  cost_basis: '0.00',
  unrealized_pl: null,
  realized_pl: null,
  fx_pl: null,
  missing_rates: [],
  positions: [],
}

const noYears = { currency: 'TRY', years: [], missing_rates: [], inflation_estimated_from: null }

// Portfolio 1 with one month; `returns` is that month's and its year's,
// `span` both buckets' from..to.
function mockApi(
  returns: PeriodBucket['returns'] = { simple: '0.100000', twr: '0.020000', mwr: '0.030000' },
  span?: [string, string],
  gaps: CashGap[] = [],
  historyTwr: (string | null)[] = ['0.000000', '0.250000', '0.000000', '0.300000'],
) {
  const urls: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    urls.push(url)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url.endsWith('/summary')) return Response.json(summary)
    if (url.includes('/years')) return Response.json(noYears)
    if (url.endsWith('/cash-gaps')) return Response.json(gaps)
    if (url.endsWith('/cashflows'))
      return Response.json({ currency: 'TRY', months: [], totals: {}, missing_rates: [] })
    if (url.endsWith('/trades'))
      return Response.json({ currency: 'TRY', trades: [], missing_rates: [] })
    if (url.includes('/returns/periods')) {
      const withBenchmark = url.includes('benchmark=')
      return Response.json({
        currency: 'TRY',
        months: [bucket('2026-01', returns, withBenchmark, span)],
        years: [bucket('2026', returns, withBenchmark, span)],
      })
    }
    // The all-time history: by default a 20% fall and a recovery.
    if (url.includes('/history')) {
      return Response.json({
        currency: 'TRY',
        points: historyTwr.map((twr, i) => ({
          date: `2026-01-0${i + 1}`,
          total_value: '0.00',
          cash: '0.00',
          cost_basis: '0.00',
          twr,
        })),
        missing_rates: [],
        missing_prices: [],
        net_flow: '0.00',
        returns: same('0.300000'),
        benchmark: null,
      })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return urls
}

function renderPage(path = '/portfolios/1/performance') {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/portfolios/:id/performance" element={<PerformancePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const pct = (v: number) => signedPercent(v, { minimumFractionDigits: 1 })
const janCell = async () => {
  const table = await screen.findByRole('table', { name: t('performance.periods.title') })
  return within(within(table).getAllByRole('row')[1]).getAllByRole('cell')[0]
}

it('shows the stored method and changes the numbers with it', async () => {
  localStorage.setItem('returnMethod', 'simple')
  mockApi()
  renderPage()

  expect(await janCell()).toHaveTextContent(pct(0.1))
  await chooseOption(
    screen.getByRole('combobox', { name: t('chart.return.method') }),
    t('chart.return.twr'),
  )
  expect(await janCell()).toHaveTextContent(pct(0.02))
  expect(localStorage.getItem('returnMethod')).toBe('twr')
  expect(screen.getByRole('link', { name: t('performance.title') })).toHaveAttribute(
    'aria-current',
    'page',
  )
})

it('offers the excess view only with a benchmark', async () => {
  localStorage.setItem('returnMethod', 'simple')
  const urls = mockApi()
  renderPage()

  await janCell()
  expect(screen.queryByRole('radiogroup', { name: t('performance.periods.view') })).toBeNull()

  await chooseOption(
    screen.getByRole('combobox', { name: t('chart.benchmark.label') }),
    t('chart.benchmark.USD'),
  )
  expect(localStorage.getItem('benchmark')).toBe('USD')
  expect(urls).toContain('/api/portfolios/1/returns/periods?benchmark=USD')
  const views = await screen.findByRole('radiogroup', { name: t('performance.periods.view') })
  expect(await janCell()).toHaveTextContent(pct(0.1))

  fireEvent.click(within(views).getByRole('radio', { name: t('performance.periods.excess') }))
  expect(await janCell()).toHaveTextContent(pct(1.1 / 1.05 - 1))
})

// The bars are component state, so the card opens on the heatmap.
it('switches the card between the heatmap and the bars, opening on the heatmap', async () => {
  const restoreSize = mockChartSize()
  try {
    localStorage.setItem('benchmark', 'USD')
    mockApi()
    renderPage()

    await janCell()
    const shapes = screen.getByRole('radiogroup', { name: t('chart.returnBars.view') })
    const heatmap = within(shapes).getByRole('radio', { name: t('chart.returnBars.view.heatmap') })
    expect(heatmap).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(within(shapes).getByRole('radio', { name: t('chart.returnBars.view.bars') }))
    expect(
      await screen.findByRole('application', { name: /aylık getiri: Portföy, Dolar$/ }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
    // The bars draw the benchmark beside the portfolio: no excess view.
    expect(screen.queryByRole('radiogroup', { name: t('performance.periods.view') })).toBeNull()

    fireEvent.click(heatmap)
    expect(await janCell()).toBeInTheDocument()
    // The drawdown card below has its own chart.
    expect(screen.queryByRole('application', { name: /aylık getiri/ })).toBeNull()
    expect(Object.keys(localStorage)).toEqual(['benchmark'])
  } finally {
    restoreSize()
  }
})

it('explains a return that cannot be computed', async () => {
  mockApi(same(null))
  renderPage()
  expect(await screen.findByText(t('performance.cashGaps.noValue'))).toBeInTheDocument()
  expect(screen.queryByRole('table')).toBeNull()
})

// The cash gaps show in the three cards, and the dialog opens from either.
it('shows the cash gaps in the three cards', async () => {
  mockApi(
    same(null),
    undefined,
    [{ portfolio_id: 1, date: '2026-01-02', currency: 'TRY', amount: '500' }],
    [null, null, null, null],
  )
  renderPage()
  await waitFor(() =>
    expect(screen.getAllByRole('button', { name: t('performance.cashGaps.add') })).toHaveLength(3),
  )
  expect(screen.queryByRole('table')).toBeNull()
})

// On the day of the first transaction every bucket is that one day and its
// returns are null: too little data, as the drawdown card says,
// not a missing deposit.
it('says there is too little data on the first day', async () => {
  mockApi(same(null), ['2026-01-05', '2026-01-05'])
  renderPage()
  expect(await screen.findByText(t('chart.empty'))).toBeInTheDocument()
  expect(screen.queryByText(t('performance.cashGaps.noValue'))).toBeNull()
})

it('shows the drawdown of the all-time history below the periodic returns', async () => {
  localStorage.setItem('benchmark', 'USD')
  const urls = mockApi()
  renderPage()

  const figures = await screen.findByTestId('drawdown-figures')
  expect(figures).toHaveTextContent('En büyük-%20')
  // All time, without the page's benchmark; the risk card below asks with it.
  expect(urls.filter((u) => u.includes('/history'))).toEqual([
    expect.stringMatching(/^\/api\/portfolios\/1\/history\?to=[\d-]+$/),
    expect.stringMatching(/^\/api\/portfolios\/1\/history\?to=[\d-]+&benchmark=USD$/),
  ])
  const periods = screen.getByRole('heading', { name: t('performance.periods.title') })
  const drawdown = screen.getByRole('heading', { name: t('drawdown.title') })
  expect(periods.compareDocumentPosition(drawdown) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('shows the risk card below the drawdown', async () => {
  mockApi()
  renderPage()

  const risk = await screen.findByRole('heading', { name: t('risk.title') })
  const drawdown = screen.getByRole('heading', { name: t('drawdown.title') })
  expect(drawdown.compareDocumentPosition(risk) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('lists the realized trades of the portfolio below the drawdown', async () => {
  const urls = mockApi()
  renderPage()

  expect(await screen.findByText(t('trades.empty'))).toBeInTheDocument()
  expect(urls).toContain('/api/portfolios/1/trades')
  const drawdown = screen.getByRole('heading', { name: t('drawdown.title') })
  const trades = screen.getByRole('heading', { name: t('trades.title') })
  expect(drawdown.compareDocumentPosition(trades) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('reads the cash flow months of the portfolio below the trades', async () => {
  const urls = mockApi()
  renderPage()

  expect(await screen.findByRole('heading', { name: t('cashflow.title') })).toBeInTheDocument()
  await waitFor(() => expect(urls).toContain('/api/portfolios/1/cashflows'))
  const trades = screen.getByRole('heading', { name: t('trades.title') })
  const cashflow = screen.getByRole('heading', { name: t('cashflow.title') })
  expect(trades.compareDocumentPosition(cashflow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('reads the yearly statement of the portfolio right below the periodic returns', async () => {
  const urls = mockApi()
  renderPage()

  const years = await screen.findByRole('heading', { name: t('years.title') })
  await waitFor(() => expect(urls).toContain('/api/portfolios/1/years'))
  const periods = screen.getByRole('heading', { name: t('performance.periods.title') })
  expect(periods.compareDocumentPosition(years) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

// A scope with no transactions has no buckets at all: nothing to compute,
// rather than a return that cannot be computed, and nothing to switch
// between, even with a benchmark.
it('says there are no transactions yet for an empty scope', async () => {
  localStorage.setItem('benchmark', 'USD')
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/portfolios') return Response.json([])
    if (url.endsWith('/summary')) return Response.json(summary)
    if (url.includes('/years')) return Response.json(noYears)
    if (url.includes('/returns/periods')) {
      return Response.json({ currency: 'TRY', months: [], years: [] })
    }
    if (url.endsWith('/cashflows'))
      return Response.json({ currency: 'TRY', months: [], totals: {}, missing_rates: [] })
    if (url.endsWith('/trades'))
      return Response.json({ currency: 'TRY', trades: [], missing_rates: [] })
    throw new Error(`unexpected request: ${url}`)
  })
  renderPage('/portfolios/all/performance')
  // The cash flow card below says the same, so look in the returns card.
  const card = (
    await screen.findByRole('heading', { name: t('performance.periods.title') })
  ).closest('section')!
  expect(await within(card).findByText(t('transactions.empty'))).toBeInTheDocument()
  expect(screen.queryByText(t('performance.cashGaps.noValue'))).toBeNull()
  expect(screen.queryByRole('table')).toBeNull()
  expect(within(card).queryByRole('radiogroup')).toBeNull()
})

it('serves Tüm portföyler with Özet and Performans as its tabs', async () => {
  const urls = mockApi()
  renderPage('/portfolios/all/performance')

  await janCell()
  expect(urls).toContain('/api/portfolios/all/returns/periods')
  const tabs = screen.getByRole('navigation', { name: t('portfolio.tabs') })
  expect(
    within(tabs)
      .getAllByRole('link')
      .map((l) => l.getAttribute('href')),
  ).toEqual(['/portfolios/all', '/portfolios/all/performance'])
})
