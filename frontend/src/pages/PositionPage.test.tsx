import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { formatUnitPrice, MASK } from '../lib/format'
import { setPrivacy } from '../lib/privacy'
import { chartDrag, mockChartSize } from '../test/chartSize'
import { PositionPage } from './PositionPage'

beforeAll(() => {
  // The price editor sizes itself by viewport (useIsDesktop).
  window.matchMedia = (query) =>
    ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
  // jsdom does not implement showModal; the trade and price dialogs are <dialog>s.
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
})

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
})
afterEach(() => {
  restoreSize()
  localStorage.clear()
  setPrivacy(false)
  vi.restoreAllMocks()
  vi.useRealTimers()
})

// The runs of one chart series as drawn: one subpath of its line per run of
// days with a value, each a list of "x,y" points (a lone day is "M x,yZ").
const runs = (chart: Element, series: 'value' | 'cost') =>
  (chart.querySelector(`.series-${series} path[fill="none"]`)?.getAttribute('d') ?? '')
    .split('M')
    .filter(Boolean)
    .map((run) => run.replace(/Z$/, '').split('L'))

// THYAO: 10 held at 100.5, last 125 after 120 the day before; realized P/L
// 80, of which 30 is dividends (the backend counts a dividend's net into
// realized_pl). ASELS and the cash make the portfolio total 2500.
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
  day_change_base: '1200.00',
  market_value: '1250.00',
  unrealized_pl: '245.00',
  realized_pl: '80.00',
  market_value_reporting: '1250.00',
  unrealized_pl_reporting: '245.00',
  realized_pl_reporting: '80.00',
  total_pl_reporting: '325.00',
  fx_pl_reporting: '40.00',
  closed: false,
  splits: [{ date: '2026-09-26', ratio: '2' }],
}
const ASELS = {
  ...THYAO,
  instrument_id: 8,
  symbol: 'ASELS',
  name: 'Aselsan',
  market_value: '750.00',
  market_value_reporting: '750.00',
  splits: [],
}

type Pos = typeof THYAO

const summary = (positions: Pos[], cash = '500.00') => ({
  currency: 'TRY',
  cash,
  cash_by_currency: {},
  total_value: '2500.00',
  unrealized_pl: '245.00',
  realized_pl: '80.00',
  missing_rates: [],
  positions,
})

const timeline = (net: string | null, count = 2) => ({
  currency: 'TRY',
  months: [],
  years: [],
  by_instrument:
    net === null
      ? []
      : [{ instrument_id: 7, symbol: 'THYAO', name: 'x', count, net, last_date: '2026-09-01' }],
  missing_rates: [],
})

const tx = (id: number, type: string, date: string, fee = '0') => ({
  id,
  portfolio_id: 1,
  instrument_id: 7,
  instrument_symbol: 'THYAO',
  instrument_name: 'Türk Hava Yolları',
  type,
  date,
  quantity: '10',
  price: '100.5',
  amount: '1005.00',
  fee,
  currency: 'TRY',
  note: null,
  created_at: `${date}T10:00:00Z`,
  import_id: null,
})

const RETURNS = {
  returns: { simple: '0.125', twr: '0.090909', mwr: '0.090909' },
  returns_from: '2026-09-25',
  returns_to: '2026-09-26',
}

const point = (date: string, quantity: string, value: string) => ({
  date,
  close: '284.15',
  quantity,
  avg_cost: quantity === '0' ? '0' : '100.5',
  cost_basis: quantity === '0' ? '0.00' : '1005.00',
  market_value: value,
})

// Answers the page's requests; `urls` records them. Anything else fails.
function mockApi({
  positions = [THYAO, ASELS],
  cash = '500.00',
  dividends = timeline('30.00'),
  history = {
    currency: 'TRY',
    ...RETURNS,
    points: [
      point('2026-09-25', '10', '1200.00'),
      point('2026-09-26', '10', '1250.00'),
      // The zero-quantity day after the last held one (ADR 0016).
      point('2026-09-27', '0', '0.00'),
    ],
  } as unknown,
  transactions = [tx(2, 'sell', '2026-09-27', '1.50'), tx(1, 'buy', '2026-09-25')] as unknown[],
  attribution = undefined as unknown,
  trades = [] as unknown[],
}: {
  positions?: Pos[]
  cash?: string
  dividends?: unknown
  history?: unknown
  transactions?: unknown[]
  attribution?: unknown
  trades?: unknown[]
} = {}) {
  const urls: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    urls.push(url)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/summary') return Response.json(summary(positions, cash))
    if (url === '/api/portfolios/1/dividends/timeline') return Response.json(dividends)
    if (url.startsWith('/api/portfolios/1/instruments/7/history?')) return Response.json(history)
    if (url.startsWith('/api/portfolios/1/attribution?') && attribution) {
      return Response.json(attribution)
    }
    if (url === '/api/portfolios/1/transactions?limit=100&instrument_id=7') {
      return Response.json(transactions)
    }
    if (url === '/api/portfolios/1/trades?instrument_id=7') {
      return Response.json({ currency: 'TRY', trades, missing_rates: [] })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return urls
}

function renderPage(entry = '/portfolios/1/positions/7') {
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/portfolios/:id/positions/:instrumentId" element={<PositionPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// A KPI cell by its label.
const cell = (label: string) => screen.getByText(label, { selector: 'p' }).parentElement!
const cells = () => [...document.querySelectorAll('section > div > p:first-child')]
const chartCard = () => screen.getByRole('radiogroup', { name: 'Grafik' }).closest('section')!

it('leads with one title row: the Özet link, the symbol as the one h1, name, market, last price and the day change', async () => {
  mockApi()
  renderPage('/portfolios/1/positions/7?range=1y')

  const h1 = await screen.findByRole('heading', { level: 1, name: 'THYAO' })
  expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  const row = h1.parentElement!.parentElement!
  // The link back keeps the range.
  expect(within(row).getByRole('link', { name: 'Özet ›' })).toHaveAttribute(
    'href',
    '/portfolios/1?range=1y',
  )
  // The truncated instrument name shows in full on hover.
  expect(within(row).getByText('Türk Hava Yolları')).toHaveAttribute('title', 'Türk Hava Yolları')
  expect(within(row).getByText('BIST')).toBeInTheDocument()
  expect(within(row).getByText(formatUnitPrice('125.00', 'TRY'))).toBeInTheDocument()
  // 50 over the previous value 1200.
  expect(within(row).getByText('+%4,2 bugün')).toBeInTheDocument()
  expect(within(row).getByRole('button', { name: 'Al' })).toBeInTheDocument()
  expect(within(row).getByRole('button', { name: 'Sat' })).toBeInTheDocument()
  expect(within(row).getByRole('button', { name: 'Fiyat gir' })).toBeInTheDocument()
})

it('shows the return card between the KPIs and the chart card', async () => {
  mockApi()
  renderPage('/portfolios/1/positions/7?range=1y')

  const title = await screen.findByRole('heading', { name: 'Getiri' })
  expect(await screen.findByText('+%9,09')).toBeInTheDocument()
  expect(
    title.closest('section')!.compareDocumentPosition(chartCard()) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy()
})

it('opens the trade dialog prefilled from Al, Sat and the header add, and the price editor from Fiyat gir', async () => {
  mockApi()
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  const opened = () => within(screen.getByRole('dialog'))
  const close = () => fireEvent.click(opened().getByRole('button', { name: 'Vazgeç' }))

  fireEvent.click(screen.getByRole('button', { name: 'Al' }))
  expect(opened().getByRole('radio', { name: 'Alış' })).toHaveAttribute('aria-checked', 'true')
  expect(opened().getByLabelText('Enstrüman')).toHaveValue('THYAO - Türk Hava Yolları')
  expect(opened().getByLabelText('Adet')).toHaveValue('')
  close()

  fireEvent.click(screen.getByRole('button', { name: 'Sat' }))
  expect(opened().getByRole('radio', { name: 'Satış' })).toHaveAttribute('aria-checked', 'true')
  expect(opened().getByLabelText('Enstrüman')).toHaveValue('THYAO - Türk Hava Yolları')
  expect(opened().getByLabelText('Adet')).toHaveValue('10')
  close()

  // The header's İşlem ekle opens here, with this instrument.
  fireEvent.click(screen.getByRole('button', { name: 'İşlem ekle' }))
  expect(opened().getByRole('radio', { name: 'Alış' })).toHaveAttribute('aria-checked', 'true')
  expect(opened().getByLabelText('Enstrüman')).toHaveValue('THYAO - Türk Hava Yolları')
  close()

  fireEvent.click(screen.getByRole('button', { name: 'Fiyat gir' }))
  expect(screen.getByRole('dialog', { name: 'THYAO için fiyat gir' })).toBeInTheDocument()
})

it('shows six KPI cells: the total counts the dividends inside realized P/L once, the market value its weight', async () => {
  mockApi()
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  // Dividends come from the timeline query, after the summary.
  await waitFor(() => expect(within(cell('Temettü')).getByText('+₺30,00')).toBeInTheDocument())
  expect(cells().map((p) => p.textContent)).toEqual([
    'Toplam sonuç',
    'Piyasa değeri',
    'Gerçekleşmemiş',
    'Gerçekleşmiş',
    'Temettü',
    'Ort. maliyet',
  ])
  // 245 unrealized + 80 realized, which already holds the 30 of dividends.
  expect(within(cell('Toplam sonuç')).getByText('+₺325,00')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmiş')).getByText('+₺80,00')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmiş')).getByText('Temettü dahil')).toBeInTheDocument()
  expect(within(cell('Temettü')).getByText('2 ödeme · Gerçekleşmiş içinde')).toBeInTheDocument()
  // 1250 of THYAO 1250 + ASELS 750 + cash 500.
  expect(within(cell('Piyasa değeri')).getByText('₺1.250,00')).toBeInTheDocument()
  expect(within(cell('Piyasa değeri')).getByText('10 adet · Ağırlık %50,0')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmemiş')).getByText('+₺245,00')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmemiş')).getByText('+%24,4')).toBeInTheDocument()
  // Özet's phone row leaves the currency gain to this page.
  expect(
    within(cell('Gerçekleşmemiş')).getByText('Maliyetten bu yana kur +₺40,00'),
  ).toBeInTheDocument()
  expect(
    within(cell('Ort. maliyet')).getByText(formatUnitPrice('100.5', 'TRY', 6)),
  ).toBeInTheDocument()
})

// The range's split is read for the page's range and matched by instrument.
it('shows the range split of this position under the total', async () => {
  const urls = mockApi({
    attribution: {
      currency: 'TRY',
      from: '2026-01-01',
      to: '2026-03-31',
      total: {},
      positions: [
        { instrument_id: 3, price: '999.00', income: '0.00', fx: '0.00' },
        { instrument_id: 7, price: '120.00', income: '10.00', fx: '-35.00' },
      ],
      missing_rates: [],
    },
  })
  renderPage('/portfolios/1/positions/7?range=1y')
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })
  const total = cell('Toplam sonuç')
  expect(await within(total).findByText('+₺120,00')).toBeInTheDocument()
  expect(within(total).getByText('+₺10,00')).toBeInTheDocument()
  expect(within(total).getByText('-₺35,00')).toBeInTheDocument()
  expect(urls.some((u) => u.startsWith('/api/portfolios/1/attribution?'))).toBe(true)
})

// The server sums unrealized 1526.875 and realized 531.975 before
// rounding; the rounded parts would add up to a cent more.
it('shows the total the summary gives, not the sum of the rounded parts', async () => {
  mockApi({
    positions: [
      {
        ...THYAO,
        unrealized_pl_reporting: '1526.88',
        realized_pl_reporting: '531.98',
        total_pl_reporting: '2058.85',
      },
    ],
    dividends: timeline('0.05', 1),
  })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  expect(within(cell('Toplam sonuç')).getByText('+₺2.058,85')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmiş')).getByText('+₺531,98')).toBeInTheDocument()
})

// Realized_pl_reporting converts at today's rate, the
// timeline each dividend at its own date's, so their difference is not "from
// sales". A USD stock, no sells, one $10 net dividend at 30 TRY/USD, today 40.
it('shows realized P/L as the summary gives it, dividends included, never less the timeline', async () => {
  mockApi({
    positions: [
      {
        ...THYAO,
        currency: 'USD',
        realized_pl: '10.00',
        realized_pl_reporting: '400.00',
        total_pl_reporting: '645.00',
      },
    ],
    dividends: timeline('300.00', 1),
  })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  await waitFor(() => expect(within(cell('Temettü')).getByText('+₺300,00')).toBeInTheDocument())
  expect(within(cell('Gerçekleşmiş')).getByText('+₺400,00')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmiş')).getByText('Temettü dahil')).toBeInTheDocument()
  // 245 unrealized + 400 realized: the dividends once.
  expect(within(cell('Toplam sonuç')).getByText('+₺645,00')).toBeInTheDocument()
})

it('shows a closed position with three cells and no Sat or Fiyat gir', async () => {
  mockApi({
    positions: [
      {
        ...THYAO,
        quantity: '0',
        closed: true,
        has_price: false,
        last_price: null as unknown as string,
        day_change: null as unknown as string,
        market_value: '0.00',
        unrealized_pl: '0.00',
        market_value_reporting: '0.00',
        unrealized_pl_reporting: '0.00',
        total_pl_reporting: '80.00',
        fx_pl_reporting: null as unknown as string,
      },
    ],
    dividends: timeline(null),
  })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  await waitFor(() => expect(within(cell('Temettü')).getByText('₺0,00')).toBeInTheDocument())
  expect(cells().map((p) => p.textContent)).toEqual(['Toplam sonuç', 'Gerçekleşmiş', 'Temettü'])
  expect(within(cell('Toplam sonuç')).getByText('+₺80,00')).toBeInTheDocument()
  expect(within(cell('Gerçekleşmiş')).getByText('+₺80,00')).toBeInTheDocument()
  expect(screen.getByText('Kapandı')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Al' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Sat' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Fiyat gir' })).toBeNull()
})

it('masks every amount in privacy mode', async () => {
  setPrivacy(true)
  mockApi()
  renderPage()
  const h1 = await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  await waitFor(() => expect(within(cell('Temettü')).getByText(MASK)).toBeInTheDocument())
  for (const label of [
    'Toplam sonuç',
    'Piyasa değeri',
    'Gerçekleşmemiş',
    'Gerçekleşmiş',
    'Ort. maliyet',
  ]) {
    expect(within(cell(label)).getAllByText(MASK).length).toBeGreaterThan(0)
  }
  expect(document.body.textContent).not.toContain('325')
  expect(document.body.textContent).not.toContain('1.250')
  // The last price, and every amount in the transactions table.
  expect(within(h1.parentElement!.parentElement!).getByText(MASK)).toBeInTheDocument()
  expect(document.body.textContent).not.toContain('1.005')
})

it('has one chart card that switches between the price chart with markers and the value chart, and reads its range from the URL', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
  const urls = mockApi()
  renderPage('/portfolios/1/positions/7?range=1y')
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  // The range comes from the URL.
  await waitFor(() =>
    expect(urls).toContain('/api/portfolios/1/instruments/7/history?to=2026-09-28&from=2025-09-28'),
  )
  expect(urls.filter((u) => u.includes('/history?'))).toHaveLength(1)

  // Fiyat first: the held days and the closing day, so the closing sell keeps
  // its marker, with a marker per loaded buy and sell and per summary split;
  // the average cost carries over into the closing day instead of dropping
  // to the zero the API sends for it (ADR 0016). The chart arrives with its
  // lazy chunk; waitFor has setupTests.ts's longer timeout.
  const card = chartCard()
  expect(within(card).getByRole('radio', { name: 'Fiyat' })).toHaveAttribute('aria-checked', 'true')
  await waitFor(() =>
    expect(
      [...card.querySelectorAll('[data-marker]')].map((m) => m.getAttribute('data-marker')),
    ).toEqual(['sell', 'buy', 'split']),
  )
  const costLine = runs(card, 'cost')[0] ?? []
  expect(costLine).toHaveLength(3)
  expect(new Set(costLine.map((xy) => xy.split(',')[1])).size).toBe(1)

  // The price tooltip keeps a unit price's decimals, and privacy
  // masks it.
  const tooltip = () => card.querySelector('[role="tooltip"]')?.textContent ?? ''
  fireEvent.keyDown(within(card as HTMLElement).getByRole('application'), { key: 'ArrowRight' })
  expect(tooltip()).toContain('Fiyat₺284,15')
  expect(tooltip()).toContain('Ort. maliyet₺100,50')
  act(() => setPrivacy(true))
  fireEvent.keyDown(within(card as HTMLElement).getByRole('application'), { key: 'ArrowRight' })
  expect(tooltip()).toContain(MASK)
  expect(tooltip()).not.toContain('284')
  act(() => setPrivacy(false))

  // Değer ve maliyet: the held days only, no markers, whole amounts.
  fireEvent.click(within(card).getByRole('radio', { name: 'Değer ve maliyet' }))
  await waitFor(() => expect(runs(card, 'cost')[0]).toHaveLength(2))
  expect(card.querySelector('[data-marker]')).toBeNull()
  fireEvent.keyDown(within(card as HTMLElement).getByRole('application'), { key: 'ArrowRight' })
  expect(tooltip()).toContain('1.200')
  expect(tooltip()).not.toContain('1.200,00')
  // Both views read the one history query.
  expect(urls.filter((u) => u.includes('/history?'))).toHaveLength(1)

  fireEvent.click(within(card).getByRole('radio', { name: '3A' }))
  await waitFor(() =>
    expect(urls).toContain('/api/portfolios/1/instruments/7/history?to=2026-09-28&from=2026-06-28'),
  )
})

// Getiri (%): the held days' twr as a percent line from 0%, or why there is none.
const days = (twr: (string | null)[]) => ({
  currency: 'TRY',
  ...RETURNS,
  points: [
    { ...point('2026-09-25', '10', '1200.00'), twr: twr[0] },
    { ...point('2026-09-26', '10', '1250.00'), twr: twr[1] },
    point('2026-09-27', '0', '0.00'),
  ],
})
it('draws the time-weighted return of the held days in percent', async () => {
  mockApi({ history: days(['0', '0.0416']) })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  const card = chartCard()
  fireEvent.click(within(card).getByRole('radio', { name: 'Getiri (%)' }))
  // From 0% on the first held day to 4,2% on the second: the stroked path
  // is closed along the zero baseline, so the line is its first two points.
  await waitFor(() => expect(runs(card, 'value')[0]).toBeDefined())
  const [start, end] = runs(card, 'value')[0].map((xy) => xy.split(',').map(Number))
  expect(end[0]).toBeGreaterThan(start[0])
  expect(end[1]).toBeLessThan(start[1])
  expect(card.querySelector('[data-marker]')).toBeNull()
  fireEvent.keyDown(within(card).getByRole('application'), { key: 'ArrowRight' })
  fireEvent.keyDown(within(card).getByRole('application'), { key: 'ArrowRight' })
  const tooltip = card.querySelector('[role="tooltip"]')?.textContent ?? ''
  expect(tooltip).toContain('Pozisyon (zaman ağırlıklı)')
  expect(tooltip).toContain('+%4,2')
  expect(within(card).queryByText('Bu aralık için tanımsız')).toBeNull()
})

it('says why the return chart is empty when no held day has a twr', async () => {
  mockApi({ history: days([null, null]) })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })
  fireEvent.click(within(chartCard()).getByRole('radio', { name: 'Getiri (%)' }))
  expect(await within(chartCard()).findByText('Bu aralık için tanımsız')).toBeInTheDocument()
  expect(chartCard().querySelector('.recharts-wrapper')).toBeNull()
})

it('requests the stored benchmark, shows its pill and draws its dotted line on the return view', async () => {
  localStorage.setItem('benchmark', 'XU100')
  const withBenchmark = {
    ...days(['0', '0.0416']),
    benchmark: {
      code: 'XU100',
      returns: { simple: '0.1', twr: '0.1', mwr: '0.1' },
      estimated_from: null,
    },
  }
  withBenchmark.points[0] = { ...withBenchmark.points[0], benchmark_twr: '0' } as never
  withBenchmark.points[1] = { ...withBenchmark.points[1], benchmark_twr: '0.1' } as never
  const urls = mockApi({ history: withBenchmark })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  await waitFor(() =>
    expect(urls.some((u) => u.includes('/history?') && u.includes('benchmark=XU100'))).toBe(true),
  )
  expect(await screen.findByText(/BIST 100 \+%10,00/)).toBeInTheDocument()
  const card = chartCard()
  fireEvent.click(within(card).getByRole('radio', { name: 'Getiri (%)' }))
  await waitFor(() => expect(card.querySelector('.series-benchmark')).not.toBeNull())
})

it('sets the range from a drag on the chart, and the clear button returns to the range before it', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
  const urls = mockApi()
  renderPage('/portfolios/1/positions/7?range=1y')
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  // At 600 px, x 60 is the first of the price chart's three days and 580 the
  // last (LineChart.test.tsx).
  const card = chartCard()
  const chart = await waitFor(() => {
    const el = card.querySelector('.recharts-wrapper')
    if (!el) throw new Error('no chart yet')
    return el
  })
  await chartDrag(chart, 60, 580)
  await waitFor(() =>
    expect(urls).toContain('/api/portfolios/1/instruments/7/history?to=2026-09-27&from=2026-09-25'),
  )
  expect(within(card).getByRole('radio', { name: 'Özel' })).toHaveAttribute('aria-checked', 'true')

  fireEvent.click(within(card).getByRole('button', { name: 'Özel aralığı temizle' }))
  expect(within(card).getByRole('radio', { name: '1Y' })).toHaveAttribute('aria-checked', 'true')
})

it("lists the instrument's transactions in the compact table", async () => {
  mockApi()
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  const table = await screen.findByRole('table')
  expect(
    within(table)
      .getAllByRole('columnheader')
      .map((th) => th.textContent),
  ).toEqual(['Tarih', 'Tür', 'Adet × fiyat', 'Kesinti', 'Tutar'])
  const rows = within(table).getAllByRole('row').slice(1)
  expect(rows).toHaveLength(2)
  expect(rows[0]).toHaveClass('h-11')
  expect(within(rows[0]).getByText('Satış')).toBeInTheDocument()
  expect(within(rows[0]).getByText('₺1,50')).toBeInTheDocument()
  expect(within(rows[0]).getByText('+₺1.005,00')).toBeInTheDocument()
  expect(within(rows[1]).getByText('−₺1.005,00')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Tüm işlemler' })).toHaveAttribute(
    'href',
    '/portfolios/1/transactions?instrument=7&symbol=THYAO',
  )
})

const sale = {
  portfolio_id: 1,
  date: '2026-09-27',
  instrument_id: 7,
  symbol: 'THYAO',
  name: 'Türk Hava Yolları',
  market: 'BIST',
  currency: 'TRY',
  quantity: '10',
  proceeds: '1250.00',
  cost: '1005.00',
  fee: '1.50',
  realized: '245.00',
  realized_pct: '0.2438',
  opened_at: '2026-09-25',
  days: 2,
  realized_reporting: '245.00',
}

it("lists the instrument's sales between the chart and the transactions, without the instrument column", async () => {
  mockApi({ trades: [sale] })
  renderPage()

  const heading = await screen.findByRole('heading', { name: 'Satışlar' })
  const table = screen.getByRole('table', { name: 'Satışlar' })
  expect(screen.queryByRole('columnheader', { name: 'Enstrüman' })).toBeNull()
  const rows = within(table).getAllByRole('row').slice(1)
  expect(rows).toHaveLength(1)
  expect(rows[0]).toHaveTextContent('+₺245,00')
  const card = heading.closest('section')!
  const follows = (a: Element, b: Element) =>
    a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING
  expect(follows(chartCard(), card)).toBeTruthy()
  expect(
    follows(card, screen.getByRole('heading', { name: 'İşlemler' }).closest('section')!),
  ).toBeTruthy()
})

it('shows no sales card for a holding never sold', async () => {
  mockApi()
  renderPage()

  await screen.findByRole('heading', { name: 'İşlemler' })
  await waitFor(() => expect(screen.queryByRole('heading', { name: 'Satışlar' })).toBeNull())
  expect(screen.queryByText('Henüz satış yok.')).toBeNull()
})

it('draws nothing on the days nothing is held: before the first held day and in a closed gap', async () => {
  const p = (date: string, held: boolean) => ({
    date,
    close: '125.00',
    quantity: held ? '10' : '0',
    avg_cost: held ? '100.5' : '0',
    cost_basis: held ? '1005.00' : '0.00',
    market_value: held ? '1250.00' : '0.00',
  })
  // Closed before the range, reopened on 09-21, closed on 09-22, reopened on
  // 09-24.
  mockApi({
    transactions: [],
    history: {
      currency: 'TRY',
      ...RETURNS,
      points: [
        p('2026-09-20', false),
        p('2026-09-21', true),
        p('2026-09-22', false),
        p('2026-09-23', false),
        p('2026-09-24', true),
        p('2026-09-25', true),
      ],
    },
  })
  renderPage()
  await screen.findByRole('heading', { level: 1, name: 'THYAO' })

  const lines = () => [...runs(chartCard(), 'cost'), ...runs(chartCard(), 'value')]
  // The lazy chart chunk, as above.
  await waitFor(() => expect(lines().length).toBeGreaterThan(0))

  // Price: from 09-21 in one line through the gap; the average cost carries
  // into the closing day 09-22, then breaks until the reopen.
  expect(lines().map((l) => l.length)).toEqual([2, 2, 5])

  // Value vs cost: from 09-21, split around 09-22 and 09-23, never at zero:
  // cost and value are two segments each, and each line stays at one height.
  fireEvent.click(screen.getByRole('radio', { name: 'Değer ve maliyet' }))
  await waitFor(() => expect(lines().map((l) => l.length)).toEqual([1, 2, 1, 2]))
  const heights = (ls: string[][]) => new Set(ls.flat().map((xy) => xy.split(',')[1])).size
  expect(heights(lines().slice(0, 2))).toBe(1)
  expect(heights(lines().slice(2))).toBe(1)
})

it('has exactly one h1 while loading and for an instrument not held (GLG-677)', async () => {
  let resolveSummary: (r: Response) => void = () => {}
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/summary') {
      return new Promise<Response>((resolve) => (resolveSummary = resolve))
    }
    if (url === '/api/portfolios/1/dividends/timeline') return Response.json(timeline(null))
    if (url.startsWith('/api/portfolios/1/instruments/16/history?')) {
      return Response.json({ currency: 'TRY', ...RETURNS, points: [] })
    }
    if (url === '/api/portfolios/1/transactions?limit=100&instrument_id=16') {
      return Response.json([])
    }
    if (url === '/api/portfolios/1/trades?instrument_id=16') {
      return Response.json({ currency: 'TRY', trades: [], missing_rates: [] })
    }
    throw new Error(`unexpected request: ${url}`)
  })

  renderPage('/portfolios/1/positions/16')

  // Loading: the header's portfolio name is the h1.
  expect(await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })).toBeInTheDocument()
  expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)

  resolveSummary(Response.json({ ...summary([]), total_value: '0.00' }))

  // Not held: the not-found message is the h1, and the only one, with a way
  // back to the portfolio's summary.
  expect(
    await screen.findByRole('heading', { level: 1, name: 'Pozisyon bulunamadı.' }),
  ).toBeInTheDocument()
  expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  expect(screen.getByRole('link', { name: 'Özete dön' })).toHaveAttribute('href', '/portfolios/1')
  expect(document.title).toBe('Sayfa bulunamadı · Emeklilik · gelirgider')
})

// The history query's failure is announced once, with a Retry.
it('announces a failed history load once, with a Retry', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/summary') return Response.json(summary([THYAO]))
    if (url === '/api/portfolios/1/dividends/timeline') return Response.json(timeline(null))
    if (url.startsWith('/api/portfolios/1/instruments/7/history?')) {
      return Response.json({ error: 'internal error' }, { status: 500 })
    }
    if (url.startsWith('/api/portfolios/1/transactions?')) return Response.json([])
    if (url.startsWith('/api/portfolios/1/trades?')) {
      return Response.json({ currency: 'TRY', trades: [], missing_rates: [] })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  renderPage()

  expect(await screen.findByRole('alert')).toHaveTextContent('Grafik yüklenemedi')
  expect(screen.getAllByRole('alert')).toHaveLength(1)
  expect(screen.getAllByRole('button', { name: 'Tekrar dene' })).toHaveLength(1)
})

// Chromium blurs a focused button the moment it is disabled, so the
// load-more button is pending while the next page loads; a second click asks
// for nothing.
it('keeps the load-more button focused and pending while the next page loads', async () => {
  mockApi()
  const api = vi.mocked(globalThis.fetch).getMockImplementation()!
  const pages: string[] = []
  vi.mocked(globalThis.fetch).mockImplementation((input, init) => {
    const url = String(input)
    if (!url.startsWith('/api/portfolios/1/transactions?')) return api(input, init)
    pages.push(url)
    if (url.includes('before=')) return new Promise<Response>(() => {})
    return Promise.resolve(
      Response.json([tx(2, 'sell', '2026-09-27'), tx(1, 'buy', '2026-09-25')], {
        headers: { 'X-Next-Cursor': '2026-09-25,1' },
      }),
    )
  })
  renderPage()

  const more = await screen.findByRole('button', { name: 'Daha fazla yükle' })
  more.focus()
  fireEvent.click(more)
  await waitFor(() => expect(more).toHaveAttribute('aria-disabled', 'true'))
  expect(more).toBeEnabled()
  expect(more).toHaveFocus()
  fireEvent.click(more)
  expect(pages).toHaveLength(2)
})

const addDays = (date: string, days: number) =>
  new Date(Date.parse(date) + days * 86_400_000).toISOString().slice(0, 10)

// `count` held days from 2026-01-01 whose twr alternates 0, 0.01, 0, ...
const riskHistory = (count: number) => ({
  currency: 'TRY',
  returns: { simple: '0.01', twr: '0.01', mwr: '0.01' },
  returns_from: '2026-01-01',
  returns_to: addDays('2026-01-01', count - 1),
  points: Array.from({ length: count }, (_, i) => ({
    ...point(addDays('2026-01-01', i), '10', '1200.00'),
    twr: i % 2 === 0 ? '0.000000' : '0.010000',
  })),
})

it('shows the risk figures of the history under the return card', async () => {
  mockApi({ history: riskHistory(40) })
  renderPage('/portfolios/1/positions/7?range=1y')

  const title = await screen.findByRole('heading', { name: t('risk.title') })
  expect(within(title.closest('section')!).getByText(t('risk.volatility'))).toBeInTheDocument()
  expect(await screen.findByText(t('risk.days', { count: 39 }))).toBeInTheDocument()
})

it('says the risk figures need more data on a short history', async () => {
  mockApi({ history: riskHistory(5) })
  renderPage('/portfolios/1/positions/7?range=1y')

  expect(await screen.findByText(t('risk.tooShort'))).toBeInTheDocument()
})
