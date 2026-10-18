import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { shouldRetryQuery } from '../api'
import { attentionRulesQueryKey } from '../api/auth'
import { Toaster } from '../components/ui/Toast'
import { t } from '../i18n/strings'
import { chooseOption } from '../test/select'
import { formatMarketDate, formatShortMarketDate, todayIstanbul } from '../lib/date'
import { resolveRange } from '../lib/ranges'
import { flushToasts } from '../lib/toast'
import { SummaryPage } from './SummaryPage'

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname + location.search}</output>
}

// jsdom has no matchMedia. The positions card renders the table or the phone
// list by viewport, so tests get the table unless they set `desktop`.
let desktop = true
beforeAll(() => {
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
  // jsdom does not implement showModal; the trade buttons open a <dialog>.
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
})

// The return split card's endpoint, answered with four zeros wherever a test
// does not look at the card.
const attribution = () =>
  Response.json({
    currency: 'TRY',
    from: '2026-01-01',
    to: '2026-03-31',
    total: { price: '0.00', income: '0.00', fx: '0.00', cash_fx: '0.00' },
    positions: [],
    missing_rates: [],
  })

// A KPI sub-line by its whole text: each money figure in it is its own
// nowrap span, and the hero's ▲/▼ arrow is left out.
const subLine = (text: string) => (_: string, el: Element | null) =>
  el?.tagName === 'P' && el.textContent?.replace(/^[▲▼] /, '') === text

// The return card's headline: the return, the change and the
// benchmark pill, as "+%30 · +₺100,00 · Dolar +%5 · fark +%23,8".
async function headline(): Promise<string> {
  const el = await screen.findByTestId('return-headline')
  return [...el.children].map((c) => c.textContent).join(' · ')
}

// A KPI cell's hint: opened from the cell's label, read, closed.
// The popup is a dialog named by the label.
async function hint(label: string): Promise<string> {
  // The positions table's sort buttons share some names; a hint sits in a <p>.
  const trigger = screen.getAllByRole('button', { name: label }).find((b) => b.closest('p'))!
  fireEvent.click(trigger)
  const text = (await screen.findByRole('dialog', { name: label })).textContent ?? ''
  fireEvent.click(trigger)
  await vi.waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  return text
}

describe('SummaryPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
    desktop = true
  })

  it('renders both position symbols and the total value', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00', USD: '20.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '13750.00',
            positions_value: '13250.00',
            cost_basis: '10500.00',
            unrealized_pl: '2750.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
              {
                instrument_id: 8,
                symbol: 'GARAN',
                name: 'Garanti BBVA',
                currency: 'TRY',
                quantity: '5',
                avg_cost: '50.00',
                cost_basis: '250.00',
                last_price: null,
                has_price: false,
                market_value: '0.00',
                unrealized_pl: '0.00',
                realized_pl: '0.00',
                market_value_reporting: '0.00',
                unrealized_pl_reporting: '0.00',
                realized_pl_reporting: '0.00',
              },
              {
                instrument_id: 9,
                symbol: 'AAPL',
                name: 'Apple Inc.',
                currency: 'USD',
                quantity: '2',
                avg_cost: '150.00',
                cost_basis: '300.00',
                last_price: '190.00',
                has_price: true,
                market_value: '380.00',
                unrealized_pl: '80.00',
                realized_pl: '0.00',
                market_value_reporting: '12000.00',
                unrealized_pl_reporting: '2500.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      // Mocked so the history query never fails and retries into the next test.
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // THYAO renders a second time in the Allocation panel, so table
    // assertions are scoped to the table.
    const tableEl = await within(container).findByRole('table')
    const table = within(tableEl)
    // Wider than the card at tablet widths, so it must scroll sideways.
    expect(tableEl.parentElement).toHaveClass('overflow-x-auto')
    expect(table.getByText('THYAO')).toBeInTheDocument()
    expect(table.getByText('GARAN')).toBeInTheDocument()
    // The KPI strip's total, in the reporting currency (TRY) with Intl
    // currency formatting.
    expect(screen.getByText('₺13.750,00')).toBeInTheDocument()
    // Two columns even on a phone, with the total spanning both there.
    const totalCell = screen.getByText('₺13.750,00').closest('p')!.parentElement!
    expect(totalCell).toHaveClass('col-span-2', 'lg:col-span-1')
    expect(totalCell.parentElement).toHaveClass('grid-cols-2')
    expect(totalCell.parentElement).not.toHaveClass('grid-cols-1')
    // The return against the cost the API sums (cost_basis), as the hint of
    // the unrealized cell.
    expect(await hint('Gerçekleşmemiş K/Z')).toBe('+%26,2 maliyete göre · Maliyet ₺10.500,00')
    // The USD position's unit price stays in dollars.
    expect(table.getByText('$190,00')).toBeInTheDocument()
    // cash_by_currency has two non-zero currencies, so the cash cell's hint
    // carries both.
    expect(await hint('Nakit')).toBe('₺500,00 · $20,00')
    // AAPL's market_value_reporting (12000.00 TRY) is the value column, its
    // own dollars on hover.
    expect(table.getByText('₺12.000,00')).toHaveAttribute('title', '$380,00')
    // P/L in the reporting currency with the return over the cost basis.
    expect(table.getByText('+₺2.500,00 · +%26,7')).toBeInTheDocument()
    // GARAN has no price yet, so its P/L is unknown, not a flat zero.
    const garan = within(table.getByText('GARAN').closest('tr')!)
    expect(garan.getAllByRole('cell').at(-2)).toHaveTextContent('—')
    // The footer sums the reporting values of all three positions.
    expect(table.getByText('Toplam · 3 pozisyon, TRY olarak')).toBeInTheDocument()
    expect(table.getByText('₺13.250,00')).toBeInTheDocument()
  })

  it("shows the API's cost and positions value, not totals less cash (GLG-976)", async () => {
    // Cash 8.995 and a 1.005 position, each rounded on its own: total minus
    // cash minus gain is 10.00 - 9.00 - 0.00 = 1.00, a cent off the row.
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '9.00',
            cash_by_currency: { TRY: '9.00' },
            cash_by_currency_reporting: { TRY: '9.00' },
            total_value: '10.00',
            positions_value: '1.01',
            cost_basis: '1.01',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'TTE',
                name: 'Test Fund',
                currency: 'TRY',
                quantity: '1',
                avg_cost: '1.005',
                cost_basis: '1.01',
                last_price: '1.005',
                has_price: true,
                market_value: '1.01',
                unrealized_pl: '0.00',
                realized_pl: '0.00',
                market_value_reporting: '1.01',
                unrealized_pl_reporting: '0.00',
                realized_pl_reporting: '0.00',
                splits: [],
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    expect(await hint('Gerçekleşmemiş K/Z')).toContain('Maliyet ₺1,01')
    // The row and the footer both read 1,01.
    const footer = table.getByText('Toplam · 1 pozisyon, TRY olarak').closest('tr')!
    expect(within(footer).getByText('₺1,01')).toBeInTheDocument()
  })

  it('builds only the positions layout that matches the viewport (GLG-460)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '1250.00',
            positions_value: '1250.00',
            cost_basis: '1000.00',
            unrealized_pl: '250.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const renderCard = async () => {
      const { container, unmount } = render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      const heading = await within(container).findByRole('heading', { name: 'Pozisyonlar' })
      const card = within(heading.closest('section')!)
      return { card, unmount }
    }

    const wide = await renderCard()
    expect(wide.card.getByRole('table')).toBeInTheDocument()
    expect(wide.card.queryByRole('list')).not.toBeInTheDocument()
    expect(wide.card.getAllByText('Toplam · 1 pozisyon, TRY olarak')).toHaveLength(1)
    wide.unmount()

    desktop = false
    const phone = await renderCard()
    expect(phone.card.queryByRole('table')).not.toBeInTheDocument()
    expect(within(phone.card.getByRole('list')).getByText('THYAO')).toBeInTheDocument()
    expect(phone.card.getAllByText('Toplam · 1 pozisyon, TRY olarak')).toHaveLength(1)

    // The compact row names the instrument; a tap opens the position dialog,
    // so the symbol is no link of its own; no Al/Sat on the row.
    const item = within(phone.card.getByRole('listitem'))
    expect(item.queryByRole('link')).not.toBeInTheDocument()
    expect(item.getByText('Türk Hava Yolları')).toBeInTheDocument()
    expect(item.queryByRole('button', { name: 'Al' })).not.toBeInTheDocument()
    expect(item.queryByRole('button', { name: 'Sat' })).not.toBeInTheDocument()
    // No price line or price editor on the phone row any more.
    expect(item.queryByText('₺125,00')).not.toBeInTheDocument()
    expect(item.queryByRole('button', { name: 'THYAO işlemleri' })).not.toBeInTheDocument()
  })

  it("leaves a foreign position's currency gain to the position page (GLG-537, GLG-933)", async () => {
    const position = (id: number, symbol: string, currency: string, fx: string | null) => ({
      instrument_id: id,
      symbol,
      name: `${symbol} Inc.`,
      currency,
      quantity: '10',
      avg_cost: '100.00',
      cost_basis: '1000.00',
      last_price: '120.00',
      has_price: true,
      price_source: 'yahoo',
      price_date: '2026-09-11',
      market_value: '1200.00',
      unrealized_pl: '200.00',
      realized_pl: '0.00',
      market_value_reporting: '1200.00',
      unrealized_pl_reporting: '200.00',
      realized_pl_reporting: '0.00',
      fx_pl_reporting: fx,
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '2400.00',
            positions_value: '2400.00',
            cost_basis: '2000.00',
            unrealized_pl: '400.00',
            realized_pl: '0.00',
            // Null so the KPI card's own currency line stays out of the way.
            fx_pl: null,
            missing_rates: [],
            positions: [position(1, 'AAPL', 'USD', '4000.00'), position(2, 'THYAO', 'TRY', null)],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const renderCard = async () => {
      const { container, unmount } = render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      const heading = await within(container).findByRole('heading', { name: 'Pozisyonlar' })
      return { card: within(heading.closest('section')!), unmount }
    }

    // The one-line rows leave AAPL's currency gain to the position page.
    const wide = await renderCard()
    expect(wide.card.getByText('AAPL')).toBeInTheDocument()
    expect(wide.card.queryByText(/Kur farkı/)).not.toBeInTheDocument()
    expect(wide.card.queryByText('+₺4.000,00')).not.toBeInTheDocument()
    wide.unmount()

    desktop = false
    const phone = await renderCard()
    // The compact phone row leaves it to the position page.
    expect(within(phone.card.getByRole('list')).getByText('AAPL')).toBeInTheDocument()
    expect(phone.card.queryByText(/Kur farkı/)).not.toBeInTheDocument()
  })

  it("shows each position's daily change and their total in the KPI strip (GLG-552)", async () => {
    const position = (
      id: number,
      symbol: string,
      currency: string,
      change: string | null,
      reporting: string | null,
    ) => ({
      instrument_id: id,
      symbol,
      name: `${symbol} Inc.`,
      currency,
      quantity: '10',
      avg_cost: '1000.00',
      cost_basis: '10000.00',
      last_price: '1012.00',
      has_price: true,
      price_source: 'yahoo',
      price_date: '2026-09-11',
      prev_close: change === null ? null : '1000.00',
      prev_price_date: change === null ? null : '2026-09-10',
      day_change: change,
      day_change_reporting: reporting,
      market_value: '10120.00',
      unrealized_pl: '120.00',
      realized_pl: '0.00',
      market_value_reporting: '10120.00',
      unrealized_pl_reporting: '120.00',
      realized_pl_reporting: '0.00',
    })
    // THYAO moved and has a rate; AAPL moved but has no rate, so it stays out
    // of the total; GARAN has no previous close. The total is the server's
    // day_change.
    let dayChange: string | null = '120.00'
    let positions = [
      position(1, 'THYAO', 'TRY', '120.00', '120.00'),
      position(2, 'AAPL', 'USD', '120.00', null),
      position(3, 'GARAN', 'TRY', null, null),
    ]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '30360.00',
            positions_value: '30360.00',
            cost_basis: '30360.00',
            unrealized_pl: null,
            realized_pl: '0.00',
            fx_pl: null,
            day_change: dayChange,
            missing_rates: [],
            positions,
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })
    const renderPage = async () => {
      const view = render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      await within(view.container).findByRole('heading', { name: 'Pozisyonlar' })
      return view
    }

    const page = await renderPage()
    const rows = page.getAllByRole('row')
    const thyao = rows.find((row) => within(row).queryByText('THYAO'))!
    const garan = rows.find((row) => within(row).queryByText('GARAN'))!
    // 120 over the 10.000 it was worth at the previous close. The row shows
    // the percent alone, keeping it one line; the amount and the
    // dates are in a toggletip, which a tap or focus shows.
    const title = `+₺120,00 · Önceki kapanış ${formatShortMarketDate('2026-09-10')} → ${formatShortMarketDate('2026-09-11')}`
    const button = within(thyao).getByRole('button', { description: title })
    expect(button).toHaveTextContent('+%1,2')
    expect(within(thyao).getByText(title)).not.toBeVisible()
    fireEvent.click(button)
    expect(within(thyao).getByText(title)).toBeVisible()
    fireEvent.keyDown(button, { key: 'Escape' })
    expect(within(thyao).getByText(title)).not.toBeVisible()
    expect(within(garan).queryByText(/^Önceki kapanış/)).not.toBeInTheDocument()
    // The total is the KPI strip's Günlük cell, dated in its hint.
    const dayCell = page.getByText('Günlük', { selector: 'p button' }).closest('div')!
    expect(within(dayCell).getByText('+₺120,00').closest('p')).toHaveClass('text-gain')
    expect(await hint('Günlük')).toBe(`${formatShortMarketDate('2026-09-11')} fiyatlarıyla`)
    page.unmount()

    positions = positions.map((p) => ({ ...p, day_change: null, day_change_reporting: null }))
    dayChange = null
    const flat = await renderPage()
    const flatCell = flat.getByText('Günlük', { selector: 'p' }).parentElement!
    expect(within(flatCell).getByText('—')).toBeInTheDocument()
    expect(within(flatCell).queryByRole('button')).toBeNull()
  })

  it('opens the position page when a symbol is clicked (GLG-549)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '1250.00',
            positions_value: '1250.00',
            cost_basis: '1000.00',
            unrealized_pl: '250.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
            <Route path="/portfolios/:id/positions/:instrumentId" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    fireEvent.click(table.getByRole('link', { name: 'THYAO' }))
    expect(within(container).getByTestId('location')).toHaveTextContent('/portfolios/1/positions/7')
  })

  it('renders the all scope read-only: no price editor, no symbol link, copy for AI (GLG-545, GLG-608)', async () => {
    const urls: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      urls.push(url)
      if (url === '/api/portfolios') {
        return Response.json([
          {
            id: 1,
            name: 'Emeklilik',
            created_at: '2026-01-01T00:00:00Z',
            total_value: '1250.00',
            cash: '0.00',
            unrealized_pl: '250.00',
            currency: 'TRY',
            missing_rates: [],
          },
        ])
      }
      // The Portföyler row's own requests are not under test here.
      if (/^\/api\/portfolios\/1\//.test(url)) {
        return Response.json({ error: 'boom' }, { status: 500 })
      }
      if (url === '/api/portfolios/all/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '1250.00',
            positions_value: '1250.00',
            cost_basis: '1000.00',
            unrealized_pl: '250.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                price_source: 'manual',
                price_date: '2026-01-02',
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/all/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/all/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/all']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const positions = await within(container).findByRole('heading', { name: 'Pozisyonlar' })
    const table = within(within(positions.closest('section')!).getByRole('table'))
    expect(table.getByText('THYAO')).toBeInTheDocument()
    expect(table.queryByRole('link')).toBeNull()
    // No row menu, so no trading and no price entry.
    expect(table.queryByRole('button', { name: 'THYAO işlemleri' })).toBeNull()
    expect(table.getByText('₺125,00')).toBeInTheDocument()
    expect(screen.queryByText(/Fiyat gir/)).toBeNull()
    expect(urls).toContain('/api/portfolios/all/summary')
    expect(urls.some((u) => u.startsWith('/api/portfolios/all/history?'))).toBe(true)

    await chooseOption(screen.getByLabelText('Karşılaştır'), 'Dolar')
    await vi.waitFor(() =>
      expect(
        urls.some(
          (u) => u.startsWith('/api/portfolios/all/history?') && u.includes('benchmark=USD'),
        ),
      ).toBe(true),
    )
    localStorage.removeItem('benchmark')

    // The copy button sits in the card header, outside the table.
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    fireEvent.click(screen.getByRole('button', { name: 'AI için kopyala' }))
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('| THYAO |'))
    expect(writeText.mock.calls[0][0]).toContain('# Tüm portföyler')
  })

  it('offers an import link under the one primary add on an empty portfolio (GLG-781)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return Response.json([{ id: 3, name: 'Boş', created_at: '2026-01-01T00:00:00Z' }])
      }
      if (url === '/api/portfolios/3/summary') {
        return Response.json({
          currency: 'TRY',
          cash: '0.00',
          cash_by_currency: {},
          cash_by_currency_reporting: {},
          total_value: '0.00',
          positions_value: '0.00',
          cost_basis: '0.00',
          unrealized_pl: '0.00',
          realized_pl: '0.00',
          missing_rates: [],
          positions: [],
        })
      }
      if (url.startsWith('/api/portfolios/3/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/3/history?')) {
        return Response.json({ currency: 'TRY', points: [] })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/3']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const link = await screen.findByRole('link', { name: t('summary.emptyImport') })
    expect(link).toHaveAttribute('href', '/portfolios/3/import')
    // The card's button is the one primary action; the header's add steps back.
    expect(screen.getByRole('button', { name: t('summary.emptyLink') })).toHaveClass('bg-accent')
    expect(screen.getByRole('button', { name: t('transactions.addTitle') })).not.toHaveClass(
      'bg-accent',
    )
  })

  it('opens a sell prefilled with the instrument and held quantity from a row (GLG-558)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '1250.00',
            positions_value: '1250.00',
            cost_basis: '1000.00',
            unrealized_pl: '250.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '12.5',
                avg_cost: '80.00',
                cost_basis: '1000.00',
                last_price: '100.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    // In the row menu, so the row stays one line.
    fireEvent.click(table.getByRole('button', { name: 'THYAO işlemleri' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Sat' }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByRole('radio', { name: 'Satış' })).toHaveAttribute('aria-checked', 'true')
    expect(dialog.getByLabelText('Enstrüman')).toHaveValue('THYAO - Türk Hava Yolları')
    expect(dialog.getByLabelText('Adet')).toHaveValue('12,5')
  })

  it('warns about currencies missing from the totals', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '500.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: ['USD'],
            positions: [
              {
                instrument_id: 9,
                symbol: 'AAPL',
                name: 'Apple Inc.',
                currency: 'USD',
                quantity: '2',
                avg_cost: '150.00',
                cost_basis: '300.00',
                last_price: '190.00',
                has_price: true,
                market_value: '380.00',
                unrealized_pl: '80.00',
                realized_pl: '0.00',
                market_value_reporting: null,
                unrealized_pl_reporting: null,
                realized_pl_reporting: null,
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // On a portfolio the attention card says it, with the positions left
    // out; missing_rates names the currencies left OUT of the
    // totals.
    const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
    expect(
      card.getByText('USD kur eksikliği nedeniyle toplam dışında · 1 pozisyon'),
    ).toBeInTheDocument()
    // The notice is for the `all` scope, which has no attention card yet.
    expect(screen.queryByText(/^Kur bulunamadığı için şu/)).toBeNull()
    // AAPL's market_value_reporting is null (no USD rate); the value column
    // falls back to '-' instead of formatting null as money.
    const table = within(screen.getAllByRole('table').at(-1) as HTMLElement)
    expect(table.getAllByText('—').length).toBeGreaterThan(0)
  })

  it('shows - for a position with no reporting value and leaves it out of the total (GLG-164)', async () => {
    const position = (id: number, symbol: string, reporting: string | null) => ({
      instrument_id: id,
      symbol,
      name: symbol,
      currency: 'USD',
      quantity: '10',
      avg_cost: '100.00',
      cost_basis: '1000.00',
      last_price: '120.00',
      has_price: true,
      price_source: 'yahoo',
      price_date: '2026-09-11',
      market_value: '1200.00',
      unrealized_pl: '200.00',
      realized_pl: '0.00',
      market_value_reporting: reporting,
      unrealized_pl_reporting: reporting === null ? null : '400.00',
      realized_pl_reporting: reporting === null ? null : '0.00',
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '2000.00',
            positions_value: '2000.00',
            cost_basis: '1600.00',
            unrealized_pl: '400.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [position(1, 'AAPL', '2000.00'), position(2, 'MSFT', null)],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    // MSFT has no rate, so its weight, value and P/L read '-' ...
    const msft = within(table.getAllByText('MSFT')[0].closest('tr')!)
    expect(msft.getAllByText('—')).toHaveLength(3)
    expect(table.getAllByText('—')).toHaveLength(3)
    // ... and the footer has the server's total of AAPL alone instead of
    // counting the gap as zero: once in AAPL's row and once as the total.
    expect(table.getAllByText('₺2.000,00')).toHaveLength(2)
    expect(table.getByText('+₺400,00 · +%20')).toBeInTheDocument()
    expect(table.getByText('+₺400,00 · +%25')).toBeInTheDocument()
    expect(table.getByText('Toplam · 2 pozisyon, TRY olarak')).toBeInTheDocument()
  })

  it('does not show the empty state when cash exists only in a currency missing an FX rate', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            // No positions, and the reporting-currency total is zero
            // because the only cash held (USD) has no FX rate -- data.cash
            // alone would wrongly say this portfolio is empty.
            cash: '0.00',
            cash_by_currency: { USD: '100.00' },
            cash_by_currency_reporting: {},
            total_value: '0.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: ['USD'],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // The attention card only renders in the non-empty branch (see
    // SummaryPage.tsx), so finding its missing-rate line proves isEmpty came
    // out false despite data.cash (the reporting-currency total) being "0.00".
    const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
    expect(card.getByText('USD kur eksikliği nedeniyle toplam dışında')).toBeInTheDocument()
  })

  it('shows the untotaled cash balance and no zero total when the only currency has no rate (GLG-113)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            // The ticket's repro: a fresh install with no fx_rates rows and a
            // single USD deposit. Both reporting-currency totals come back
            // 0.00 because USD was excluded from them.
            cash: '0.00',
            cash_by_currency: { USD: '1000.00' },
            cash_by_currency_reporting: {},
            total_value: '0.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: ['USD'],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    // Scoped to this render's container: earlier tests leave their DOM mounted
    // and they render both '-' and money amounts of their own.
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // Total value and cash both read '-' instead of a wrong ₺0,00.
    for (const label of ['Toplam değer', 'Nakit']) {
      const cell = (
        await within(container).findAllByText(label, { selector: 'p, p button' })
      )[0].closest('p')!.parentElement!
      expect(within(cell).getByText('—')).toBeInTheDocument()
    }
    // The cash cell's hint has the balance even though there is only one
    // currency, so the actual balance is on the page.
    expect(await hint('Nakit')).toBe('$1.000,00')
  })

  it('badges a position changed by a split with its date and ratio (GLG-276)', async () => {
    const position = {
      currency: 'TRY',
      quantity: '10',
      avg_cost: '100.00',
      cost_basis: '1000.00',
      last_price: '100.00',
      has_price: true,
      market_value: '1000.00',
      unrealized_pl: '0.00',
      realized_pl: '0.00',
      market_value_reporting: '1000.00',
      unrealized_pl_reporting: '0.00',
      realized_pl_reporting: '0.00',
    }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '2000.00',
            positions_value: '2000.00',
            cost_basis: '2000.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                ...position,
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                splits: [{ date: '2026-05-12', ratio: '1.5' }],
              },
              { ...position, instrument_id: 8, symbol: 'GARAN', name: 'Garanti BBVA', splits: [] },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // THYAO's table row has one; GARAN, with no splits, has no badge at all.
    const badges = await screen.findAllByRole('img', { name: /^Bölünme:/ })
    expect(badges).toHaveLength(1)
    for (const badge of badges) {
      expect(badge).toHaveAccessibleName('Bölünme: 12.05.2026 · ×1,5')
      expect(badge).toHaveAttribute('title', 'Bölünme: 12.05.2026 · ×1,5')
      expect(badge).toHaveTextContent('Bölünme')
    }
  })

  it('names the holdings with no price yet under the chart (GLG-254)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '100.00',
            cash_by_currency: { TRY: '100.00' },
            cash_by_currency_reporting: { TRY: '100.00' },
            total_value: '100.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(
          JSON.stringify({ currency: 'TRY', points: [], missing_prices: ['AAPL'] }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText(/Fiyat bilinmiyor.*AAPL/)).toBeInTheDocument()
  })

  it('shows the chosen return method in the chart header and remembers it (GLG-392)', async () => {
    localStorage.removeItem('returnMethod')
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '1100.00',
            cash_by_currency: { TRY: '1100.00' },
            cash_by_currency_reporting: { TRY: '1100.00' },
            total_value: '1100.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            points: [
              { date: '2026-01-01', total_value: '1000.00', cash: '1000.00', cost_basis: '0' },
              { date: '2026-02-01', total_value: '1100.00', cash: '1100.00', cost_basis: '0' },
            ],
            missing_rates: [],
            missing_prices: [],
            net_flow: '0.00',
            returns: { simple: '0.100000', twr: '0.200000', mwr: '0.300000' },
          }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await headline()).toBe('+%30 · +₺100,00')
    expect(screen.getByRole('heading', { name: 'Getiri · son 3 ay' })).toBeInTheDocument()
    await chooseOption(screen.getByLabelText('Getiri yöntemi'), 'Basit')
    expect(await headline()).toBe('+%10 · +₺100,00')
    expect(localStorage.getItem('returnMethod')).toBe('simple')
    localStorage.removeItem('returnMethod')
  })

  it('shows a placeholder when the chosen return method is null for the range (GLG-410)', async () => {
    localStorage.removeItem('returnMethod')
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Eksi', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '-500.00',
            cash_by_currency: { TRY: '-500.00' },
            cash_by_currency_reporting: { TRY: '-500.00' },
            total_value: '-500.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            points: [
              { date: '2026-09-09', total_value: '0.00', cash: '0.00', cost_basis: '0' },
              { date: '2026-09-10', total_value: '-500.00', cash: '-500.00', cost_basis: '0' },
            ],
            missing_rates: [],
            missing_prices: [],
            net_flow: '-500.00',
            returns: { simple: null, twr: null, mwr: null },
          }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await headline()).toBe('— · ₺0,00')
    expect(within(screen.getByTestId('return-headline')).getByText('—')).toHaveAttribute(
      'title',
      'Bu aralık için tanımsız',
    )
  })

  // A range of a year or more adds the annualized returns under the period
  // figure, the benchmark's after the portfolio's. A calendar year
  // and the 1Y preset are one year: annualized equals
  // the period return.
  it.each([
    ['two-year', '2024-01-03', '2026-01-01', 'yıllık +%10 · BIST 100 yıllık +%20'],
    ['six-month', '2025-07-01', '2026-01-01', null],
    ['2024 calendar-year', '2024-01-01', '2024-12-31', 'yıllık +%21 · BIST 100 yıllık +%44'],
    ['2025 calendar-year', '2025-01-01', '2025-12-31', 'yıllık +%21 · BIST 100 yıllık +%44'],
    ['one-year', '2025-10-01', '2026-10-01', 'yıllık +%21 · BIST 100 yıllık +%44'],
  ])('annualizes returns on a %s range from a year on (GLG-607)', async (_, from, to, line) => {
    localStorage.setItem('benchmark', 'XU100')
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2024-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '1210.00',
            cash_by_currency: { TRY: '1210.00' },
            cash_by_currency_reporting: { TRY: '1210.00' },
            total_value: '1210.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            points: [
              {
                date: from,
                total_value: '1000.00',
                cash: '1000.00',
                cost_basis: '0',
                benchmark_value: '100',
              },
              {
                date: to,
                total_value: '1210.00',
                cash: '1210.00',
                cost_basis: '0',
                benchmark_value: '144',
              },
            ],
            missing_rates: [],
            missing_prices: [],
            net_flow: '0.00',
            returns: { simple: '0.210000', twr: '0.210000', mwr: '0.210000' },
            benchmark: {
              code: 'XU100',
              returns: { simple: '0.440000', twr: '0.440000', mwr: '0.440000' },
              estimated_from: null,
            },
          }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await headline()).toMatch(/^\+%21 · \+₺210,00 · /)
    if (line) {
      const sub = screen.getByText((_, el) => el?.tagName === 'P' && el.textContent === line)
      expect(within(sub).getByText(line.split(' · ')[0])).toHaveClass('text-gain')
    } else {
      expect(screen.queryByText(/yıllık/)).not.toBeInTheDocument()
    }
    localStorage.removeItem('benchmark')
  })

  describe('benchmark comparison (GLG-496)', () => {
    // Serves an empty portfolio in `currency` whose history carries a
    // benchmark when one is requested; returns every requested URL.
    function mockBenchmarkApi(
      currency: string,
      benchmarkMwr: string | null,
      benchmarkValues = benchmarkMwr !== null,
      positions: unknown[] = [],
    ) {
      const urls: string[] = []
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        urls.push(url)
        if (url === '/api/portfolios') {
          return new Response(
            JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
            { status: 200 },
          )
        }
        if (url === '/api/portfolios/1/summary') {
          return new Response(
            JSON.stringify({
              currency,
              cash: '1100.00',
              cash_by_currency: { [currency]: '1100.00' },
              cash_by_currency_reporting: { [currency]: '1100.00' },
              total_value: '1100.00',
              positions_value: '0.00',
              cost_basis: '0.00',
              unrealized_pl: '0.00',
              realized_pl: '0.00',
              missing_rates: [],
              positions,
            }),
            { status: 200 },
          )
        }
        if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
        if (url.startsWith('/api/portfolios/1/history?')) {
          const code = new URL(url, 'http://x').searchParams.get('benchmark')
          const withValues = code !== null && benchmarkValues
          return new Response(
            JSON.stringify({
              currency,
              points: [
                { date: '2026-01-01', total_value: '1000.00', cash: '1000.00', cost_basis: '0' },
                { date: '2026-02-01', total_value: '1100.00', cash: '1100.00', cost_basis: '0' },
              ].map((p, i) =>
                withValues ? { ...p, benchmark_value: i === 0 ? '1000.00' : '1050.00' } : p,
              ),
              missing_rates: [],
              missing_prices: [],
              net_flow: '0.00',
              returns: { simple: '0.100000', twr: '0.100000', mwr: '0.100000' },
              benchmark: code && {
                code,
                returns: { simple: benchmarkMwr, twr: benchmarkMwr, mwr: benchmarkMwr },
                estimated_from: null,
              },
            }),
            { status: 200 },
          )
        }
        throw new Error(`unexpected request: ${url}`)
      })
      return urls
    }

    function renderPage() {
      return render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    }

    afterEach(() => {
      localStorage.removeItem('benchmark')
      localStorage.removeItem('returnMethod')
    })

    it('requests the chosen benchmark, shows its return and the excess, and remembers it', async () => {
      const urls = mockBenchmarkApi('TRY', '0.050000')
      const { unmount } = renderPage()

      await chooseOption(await screen.findByLabelText('Karşılaştır'), 'Dolar')
      // (1 + 0.10) / (1 + 0.05) - 1 = 4.76%
      expect(await screen.findByText('Dolar +%5 · fark +%4,8')).toHaveClass('text-gain')
      expect(urls.some((u) => u.includes('benchmark=USD'))).toBe(true)
      expect(localStorage.getItem('benchmark')).toBe('USD')

      unmount()
      renderPage()
      expect(await screen.findByText('Dolar +%5 · fark +%4,8')).toHaveClass('text-gain')
      expect(screen.getByLabelText('Karşılaştır')).toHaveTextContent('Dolar')

      // The none option's value is '', which the page reads as no benchmark.
      await chooseOption(screen.getByLabelText('Karşılaştır'), 'Karşılaştırma yok')
      expect(screen.getByLabelText('Karşılaştır')).toHaveTextContent('Karşılaştırma yok')
      expect(localStorage.getItem('benchmark')).toBeNull()
      await vi.waitFor(() =>
        expect(urls.filter((u) => u.includes('/history?')).at(-1)).not.toContain('benchmark='),
      )
    })

    it('keeps the full-width header dropdowns out of the wrapping row (GLG-504)', async () => {
      mockBenchmarkApi('TRY', '0.050000')
      renderPage()

      // A w-full item placed straight in a flex-wrap row takes a whole line.
      for (const label of ['Karşılaştır', 'Getiri yöntemi']) {
        const select = await screen.findByLabelText(label)
        expect(select).toHaveClass('w-full')
        expect(select.parentElement).toHaveClass('inline-block')
        expect(select.parentElement).not.toHaveClass('flex-wrap')
      }
    })

    it('hides the USD option when the reporting currency is USD', async () => {
      localStorage.setItem('benchmark', 'USD')
      const urls = mockBenchmarkApi('USD', '0.050000')
      renderPage()

      const select = await screen.findByLabelText('Karşılaştır')
      expect(select).toHaveTextContent('Karşılaştırma yok')
      fireEvent.click(select)
      const options = within(await screen.findByRole('listbox'))
        .getAllByRole('option')
        .map((o) => o.textContent)
      expect(options).toEqual([
        'Karşılaştırma yok',
        'Euro',
        'BIST 100',
        'Enflasyon (TÜFE)',
        'Altın (gram)',
      ])
      await screen.findByRole('heading', { name: 'Getiri · son 3 ay' })
      // Once the currency is known, the hidden stored choice is not requested.
      expect(urls.filter((u) => u.includes('/history?')).at(-1)).not.toContain('benchmark=')
    })

    // A TEFAS fund position; quantity 0 makes it a closed one.
    const fund = (instrument_id: number, symbol: string, quantity = '10') => ({
      instrument_id,
      symbol,
      name: symbol,
      market: 'TEFAS',
      currency: 'TRY',
      quantity,
      avg_cost: '10.00',
      cost_basis: '100.00',
      last_price: '11.00',
      has_price: true,
      market_value: '110.00',
      unrealized_pl: '10.00',
      realized_pl: '0.00',
      market_value_reporting: '110.00',
      unrealized_pl_reporting: '10.00',
      realized_pl_reporting: '0.00',
      closed: quantity === '0',
    })

    it('offers open positions by symbol and keeps a stored one the scope holds (GLG-702)', async () => {
      localStorage.setItem('benchmark', 'instrument:7')
      const urls = mockBenchmarkApi('TRY', '0.050000', true, [
        fund(7, 'GJH'),
        fund(3, 'AFT'),
        fund(5, 'ZZZ', '0'),
      ])
      renderPage()

      // The position reads as its symbol; the excess stays "fark".
      expect(await screen.findByText('GJH +%5 · fark +%4,8')).toBeInTheDocument()
      const select = screen.getByLabelText('Karşılaştır')
      expect(select).toHaveTextContent('GJH')
      fireEvent.click(select)
      const listbox = await screen.findByRole('listbox')
      const group = within(listbox).getByRole('group', { name: 'Pozisyonlar' })
      expect(
        within(group)
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['AFT', 'GJH'])
      expect(urls.some((u) => u.includes('benchmark=instrument%3A7'))).toBe(true)
      fireEvent.keyDown(listbox, { key: 'Escape' })
      await vi.waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
      // The other position's option carries its own code.
      await chooseOption(select, 'AFT')
      await vi.waitFor(() =>
        expect(urls.some((u) => u.includes('benchmark=instrument%3A3'))).toBe(true),
      )
    })

    it('treats a stored position the scope does not hold as none (GLG-702)', async () => {
      localStorage.setItem('benchmark', 'instrument:7')
      const urls = mockBenchmarkApi('TRY', '0.050000', true, [fund(3, 'AFT')])
      renderPage()

      await screen.findByRole('heading', { name: 'Getiri · son 3 ay' })
      expect(screen.getByLabelText('Karşılaştır')).toHaveTextContent('Karşılaştırma yok')
      expect(
        urls.filter((u) => u.includes('/history?')).every((u) => !u.includes('benchmark=')),
      ).toBe(true)
    })

    it('says the benchmark has no data in place of the pill (GLG-934)', async () => {
      localStorage.setItem('benchmark', 'XU100')
      mockBenchmarkApi('TRY', null)
      renderPage()

      expect(await headline()).toBe('+%10 · +₺100,00 · BIST 100 · bu aralık için veri yok')
    })

    it('shows a dash with the undefined hint when the benchmark has values but no return', async () => {
      localStorage.setItem('benchmark', 'USD')
      localStorage.setItem('returnMethod', 'twr')
      mockBenchmarkApi('TRY', null, true)
      renderPage()

      const figure = await screen.findByText('Dolar —')
      expect(figure.parentElement).toHaveAttribute('title', 'Bu aralık için tanımsız')
    })
  })

  it('requests history from Jan 1 of the Istanbul year on YTD, keeping 3M the default (GLG-554)', async () => {
    // 22:30 UTC on Dec 31 is already Jan 1 in Istanbul (UTC+3).
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-12-31T22:30:00Z'))
    const urls: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      urls.push(url)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '500.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await vi.waitFor(() =>
      expect(urls).toContain('/api/portfolios/1/history?to=2027-01-01&from=2026-10-01'),
    )
    expect(await screen.findByRole('radio', { name: '3A' })).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(screen.getByRole('radio', { name: 'YBİ' }))
    await vi.waitFor(() =>
      expect(urls).toContain('/api/portfolios/1/history?to=2027-01-01&from=2027-01-01'),
    )
  })

  it('shows the translated error instead of an empty chart when the history query fails, with a Retry that refetches it (GLG-771)', async () => {
    let historyCalls = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '500.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      // The backend should no longer 400 on its own default `from`,
      // but the chart must still surface any other history-query failure
      // (e.g. a network error, or a future regression) instead of quietly
      // rendering nothing. The retry then answers.
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        historyCalls++
        if (historyCalls === 1) {
          return new Response(JSON.stringify({ error: 'internal error' }), { status: 500 })
        }
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Grafik yüklenemedi')
    expect(alert).toHaveTextContent('Sunucu hatası. Lütfen daha sonra tekrar dene.')

    fireEvent.click(within(alert).getByRole('button', { name: 'Tekrar dene' }))
    expect(await screen.findByText('Grafik için yeterli veri yok.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(historyCalls).toBe(2)
  })

  it('keeps the previous range on the chart, dimmed and busy, while a new range loads (GLG-771)', async () => {
    let answer1y: (r: Response) => void = () => {}
    const oneYear = resolveRange({ key: '1y', from: '', to: '' }, todayIstanbul())
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
      }
      if (url === '/api/portfolios/1/summary') {
        return Response.json({
          currency: 'TRY',
          cash: '500.00',
          cash_by_currency: { TRY: '500.00' },
          cash_by_currency_reporting: { TRY: '500.00' },
          total_value: '500.00',
          positions_value: '0.00',
          cost_basis: '0.00',
          unrealized_pl: '0.00',
          realized_pl: '0.00',
          missing_rates: [],
          positions: [],
        })
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        const empty = Response.json({ currency: 'TRY', points: [] })
        // 3M is the default; the 1Y answer is held until the test sends it.
        if (!url.includes(`from=${oneYear.from}`)) return empty
        return new Promise<Response>((resolve) => {
          answer1y = resolve
        })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const chart = await screen.findByText('Grafik için yeterli veri yok.')
    expect(chart.closest('[aria-busy]')).toHaveAttribute('aria-busy', 'false')

    fireEvent.click(screen.getByRole('radio', { name: '1Y' }))
    // Still the 3M chart, not a blank card, but marked as loading.
    await vi.waitFor(() =>
      expect(
        screen.getByText('Grafik için yeterli veri yok.').closest('[aria-busy]'),
      ).toHaveAttribute('aria-busy', 'true'),
    )

    answer1y(Response.json({ currency: 'TRY', points: [] }))
    await vi.waitFor(() =>
      expect(
        screen.getByText('Grafik için yeterli veri yok.').closest('[aria-busy]'),
      ).toHaveAttribute('aria-busy', 'false'),
    )
  })

  it('does not retry a 404 and shows the translated not-found message (GLG-94)', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      if (url === '/api/portfolios/999/summary') {
        return new Response(JSON.stringify({ error: 'portfolio not found' }), { status: 404 })
      }
      if (url.startsWith('/api/portfolios/999/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/999/history?')) {
        return new Response(JSON.stringify({ error: 'portfolio not found' }), { status: 404 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    // Same retry policy the app's QueryClient uses in main.tsx, so this
    // proves a 404 is never retried instead of just asserting the message.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: shouldRetryQuery } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/999']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('Portföy bulunamadı.')).toBeInTheDocument()
    expect(document.title).toBe('Sayfa bulunamadı · gelirgider')
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === '/api/portfolios/999/summary'),
    ).toHaveLength(1)
  })

  it('saves a manually entered price from the row menu on Enter', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '10500.00',
            positions_value: '10000.00',
            cost_basis: '9750.00',
            unrealized_pl: '250.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      // A saved price refreshes the expected dividends too; the dialog waits
      // for that refetch, so it has to answer rather than retry forever.
      if (url === '/api/portfolios/1/dividends/expected') {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      if (init?.method === 'PUT' && url === `/api/instruments/7/prices/${todayIstanbul()}`) {
        return new Response(
          JSON.stringify({
            instrument_id: 7,
            date: todayIstanbul(),
            close: '130.50',
            source: 'manual',
          }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })
    const historyCalls = () =>
      fetchMock.mock.calls.filter(([input]) =>
        String(input).startsWith('/api/portfolios/1/history?'),
      ).length

    const queryClient = new QueryClient()
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // Fiyat gir in the row's menu opens the price dialog.
    const table = within(await within(container).findByRole('table'))
    await vi.waitFor(() => expect(historyCalls()).toBe(1))
    const menu = table.getByRole('button', { name: 'THYAO işlemleri' })
    menu.focus()
    fireEvent.click(menu)
    fireEvent.click(screen.getByRole('menuitem', { name: 'Fiyat gir' }))
    const dialog = within(screen.getByRole('dialog', { name: 'THYAO için fiyat gir' }))

    const priceInput = dialog.getByLabelText('Fiyat')
    expect(priceInput).toHaveValue('125,00')
    fireEvent.change(priceInput, { target: { value: '130.50' } })
    fireEvent.keyDown(priceInput, { key: 'Enter' })

    const putCall = await vi.waitFor(() => {
      const call = fetchMock.mock.calls.find(([, callInit]) => callInit?.method === 'PUT')
      if (!call) throw new Error('PUT not called yet')
      return call
    })

    expect(putCall[0]).toBe(`/api/instruments/7/prices/${todayIstanbul()}`)
    expect(putCall[1]?.body).toBe(JSON.stringify({ close: '130.50' }))

    // The value chart must refetch its history after the price is saved.
    await vi.waitFor(() => expect(historyCalls()).toBe(2))
    // The dialog closes and focus is back on the row's menu.
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(menu)
  })

  // A price saved from the Dikkat card's Fiyat gir removed that
  // button once the summaries refetched, and focus fell to <body>. On Tüm
  // portföyler the price leaves every portfolio's line, the dialog's own
  // first and the others after the dialog has closed.
  it('moves focus to the previous Fiyat gir, else the Dikkat heading, as prices are saved from the card', async () => {
    const priced = new Set<number>()
    const position = (instrument_id: number, symbol: string) => ({
      instrument_id,
      symbol,
      name: symbol,
      currency: 'TRY',
      quantity: '10',
      avg_cost: '40.00',
      cost_basis: '400.00',
      last_price: priced.has(instrument_id) ? '41.50' : null,
      has_price: priced.has(instrument_id),
      market_value: priced.has(instrument_id) ? '415.00' : '0.00',
      unrealized_pl: '0.00',
      realized_pl: '0.00',
      market_value_reporting: priced.has(instrument_id) ? '415.00' : '0.00',
      unrealized_pl_reporting: '0.00',
      realized_pl_reporting: '0.00',
    })
    const summary = (positions: object[]) => ({
      currency: 'TRY',
      cash: '0.00',
      cash_by_currency: {},
      cash_by_currency_reporting: {},
      total_value: '0.00',
      positions_value: '0.00',
      cost_basis: '0.00',
      unrealized_pl: '0.00',
      realized_pl: '0.00',
      missing_rates: [],
      positions,
    })
    const listed = (id: number, name: string) => ({
      id,
      name,
      created_at: '2026-01-01T00:00:00Z',
      total_value: '0.00',
      cash: '0.00',
      unrealized_pl: null,
      currency: 'TRY',
      missing_rates: [],
    })
    // Ana holds SISE and THYAO, Yan holds THYAO; none has a price yet.
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/portfolios') return Response.json([listed(1, 'Ana'), listed(2, 'Yan')])
      if (url === '/api/portfolios/1/summary')
        return Response.json(summary([position(4, 'SISE'), position(6, 'THYAO')]))
      if (url === '/api/portfolios/2/summary') {
        // Yan refetches after the price dialog has closed, as over a network.
        if (priced.size > 0) await new Promise((resolve) => setTimeout(resolve, 50))
        return Response.json(summary([position(6, 'THYAO')]))
      }
      if (url === '/api/portfolios/all/summary')
        return Response.json(summary([position(4, 'SISE'), position(6, 'THYAO')]))
      if (/^\/api\/portfolios\/(all|1|2)\/attribution\?/.test(url)) return attribution()
      if (/^\/api\/portfolios\/(all|1|2)\/history\?/.test(url))
        return Response.json({
          currency: 'TRY',
          points: [],
          missing_rates: [],
          missing_prices: [],
          net_flow: '0.00',
          returns: { simple: null, twr: null, mwr: null },
          benchmark: null,
        })
      if (/^\/api\/portfolios\/(1|2)\/dividends\/expected$/.test(url)) return Response.json([])
      const put = url.match(/^\/api\/instruments\/(\d+)\/prices\//)
      if (init?.method === 'PUT' && put) {
        priced.add(Number(put[1]))
        return Response.json({})
      }
      throw new Error(`unexpected request: ${url}`)
    })
    function renderAt(path: string) {
      return render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    }
    async function enterPrice(name: string) {
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      const button = await card.findByRole('button', { name })
      button.focus()
      fireEvent.click(button)
      const price = within(screen.getByRole('dialog')).getByLabelText('Fiyat')
      fireEvent.change(price, { target: { value: '41,5' } })
      fireEvent.keyDown(price, { key: 'Enter' })
      await vi.waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
      await vi.waitFor(() => expect(card.queryByRole('button', { name: /THYAO/ })).toBeNull())
      return card
    }

    // Tüm portföyler: THYAO leaves Ana's line and Yan's after it, so focus
    // goes back to Ana's SISE.
    const all = renderAt('/portfolios/all')
    let card = await enterPrice('Ana: THYAO için fiyat gir')
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(
        card.getByRole('button', { name: 'Ana: SISE için fiyat gir' }),
      ),
    )
    all.unmount()

    // One portfolio's Özet: the last missing price leaves only the heading.
    renderAt('/portfolios/1')
    card = await enterPrice('SISE için fiyat gir')
    await vi.waitFor(() =>
      expect(card.queryByRole('button', { name: 'SISE için fiyat gir' })).toBeNull(),
    )
    expect(document.activeElement).toBe(card.getByRole('heading', { name: 'Dikkat' }))
  })

  it('shows - instead of a zero P/L when every contributing currency is excluded (GLG-129)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            // The ticket's repro: no fx_rates rows and a profitable USD
            // position, so the backend nulls both P/L totals instead of
            // reporting the 0.00 it summed from nothing.
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '0.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: null,
            realized_pl: null,
            missing_rates: ['USD'],
            positions: [
              {
                instrument_id: 9,
                symbol: 'AAPL',
                name: 'Apple Inc',
                currency: 'USD',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '0.00',
                market_value_reporting: null,
                unrealized_pl_reporting: null,
                realized_pl_reporting: null,
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    // Scoped to this render's container: earlier tests leave their DOM mounted.
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // The position's own dollar price is still on screen...
    const table = within(await within(container).findByRole('table'))
    expect(table.getByText('$125,00')).toBeInTheDocument()
    // ...while everything in the reporting currency reads '-'. In the table
    // that is the row's weight, value and P/L, and both footer sums.
    expect(table.getAllByText('—')).toHaveLength(5)
    // Nine in the page: those five, and the total value, daily and both P/L
    // cells of the KPI strip. The cash cell is not one of them -- there is
    // no cash at all, so its ₺0,00 is a real zero.
    expect(within(container).getAllByText('—')).toHaveLength(9)
  })

  describe('manual price removal (GLG-134)', () => {
    // The ticket's repro: a wrong-dated manual row (2026-09-12, 60) shadowing
    // the fetched close (2026-09-11, 58). After the DELETE the refetched
    // summary is the fetched price again.
    const summary = (manual: boolean) => ({
      currency: 'TRY',
      cash: '0.00',
      cash_by_currency: {},
      cash_by_currency_reporting: {},
      total_value: '600.00',
      positions_value: '600.00',
      cost_basis: '500.00',
      unrealized_pl: '100.00',
      realized_pl: '0.00',
      missing_rates: [],
      positions: [
        {
          instrument_id: 8,
          symbol: 'GARAN',
          name: 'Garanti BBVA',
          market: 'BIST',
          currency: 'TRY',
          quantity: '10',
          avg_cost: '50.00',
          cost_basis: '500.00',
          last_price: manual ? '60.00' : '58.00',
          has_price: true,
          price_source: manual ? 'manual' : 'bist',
          price_date: manual ? '2026-09-12' : todayIstanbul(),
          market_value: '600.00',
          unrealized_pl: '100.00',
          realized_pl: '0.00',
          market_value_reporting: '600.00',
          unrealized_pl_reporting: '100.00',
          realized_pl_reporting: '0.00',
        },
      ],
    })

    // `remove` answers the DELETE; the summary is the fetched price after a
    // successful one.
    const mockFetch = (remove: () => Promise<Response>) => {
      let deleted = false
      return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
        const url = String(input)
        if (url === '/api/portfolios') {
          return new Response(
            JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
            { status: 200 },
          )
        }
        if (url === '/api/portfolios/1/summary') {
          return new Response(JSON.stringify(summary(!deleted)), { status: 200 })
        }
        if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
        if (url.startsWith('/api/portfolios/1/history?')) {
          return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
        }
        if (init?.method === 'DELETE' && url === '/api/instruments/8/prices/2026-09-12') {
          const response = await remove()
          deleted = response.ok
          return response
        }
        throw new Error(`unexpected request: ${url}`)
      })
    }

    const renderPage = () =>
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
          <Toaster />
        </QueryClientProvider>,
      )

    // Opens the row's menu as a keyboard user would, with focus on it, and
    // picks an item.
    const choose = (menu: HTMLElement, name: string) => {
      menu.focus()
      fireEvent.click(menu)
      fireEvent.click(screen.getByRole('menuitem', { name }))
    }

    afterEach(() => flushToasts())

    it('tags a manual price and removes it from the row menu', async () => {
      const fetchMock = mockFetch(async () => new Response(null, { status: 204 }))
      const { container } = renderPage()

      // The pill's toggletip carries the date the manual row has.
      const pill = await within(container).findByRole('button', {
        description: t('prices.manualTitle', { date: formatMarketDate('2026-09-12') }),
      })
      expect(pill).toHaveTextContent('manuel')
      const menu = within(container).getByRole('button', { name: 'GARAN işlemleri' })
      choose(menu, 'Elle fiyatı kaldır')

      const deleteCall = await vi.waitFor(() => {
        const call = fetchMock.mock.calls.find(([, callInit]) => callInit?.method === 'DELETE')
        if (!call) throw new Error('DELETE not called yet')
        return call
      })
      expect(deleteCall[0]).toBe('/api/instruments/8/prices/2026-09-12')

      // The summary refetches, so the fetched close takes over and the tag goes.
      expect((await within(container).findAllByText('₺58,00')).length).toBeGreaterThan(0)
      expect(within(container).queryByText('manuel')).not.toBeInTheDocument()
      // Focus stays on the row's menu.
      expect(document.activeElement).toBe(menu)
    })

    // A failed removal must say so, and focus must not be lost.
    it('reports a failed removal in a toast and keeps focus on the row menu', async () => {
      mockFetch(
        async () => new Response(JSON.stringify({ error: 'internal error' }), { status: 500 }),
      )
      const { container } = renderPage()
      const menu = await within(container).findByRole('button', { name: 'GARAN işlemleri' })
      choose(menu, 'Elle fiyatı kaldır')

      expect(await screen.findByText(t('error.internal'))).toBeInTheDocument()
      expect(within(container).getByText('manuel')).toBeInTheDocument()
      expect(document.activeElement).toBe(menu)
    })

    // Closing the price dialog unmounts the focused input; focus goes
    // back to the row's menu, not to <body>.
    it("returns focus to the row's menu when the price dialog closes", async () => {
      mockFetch(async () => new Response(null, { status: 204 }))
      const { container } = renderPage()
      const menu = await within(container).findByRole('button', { name: 'GARAN işlemleri' })

      choose(menu, 'Fiyat gir')
      const input = within(screen.getByRole('dialog')).getByLabelText('Fiyat')
      input.focus()
      fireEvent.keyDown(input, { key: 'Escape' })
      expect(screen.queryByRole('dialog')).toBeNull()
      expect(document.activeElement).toBe(menu)

      choose(menu, 'Fiyat gir')
      fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Vazgeç' }))
      expect(screen.queryByRole('dialog')).toBeNull()
      expect(document.activeElement).toBe(menu)
    })
  })

  it('does not tag a fetched price as manual (GLG-134)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.00',
            cash_by_currency: {},
            cash_by_currency_reporting: {},
            total_value: '580.00',
            positions_value: '580.00',
            cost_basis: '500.00',
            unrealized_pl: '80.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 8,
                symbol: 'GARAN',
                name: 'Garanti BBVA',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '50.00',
                cost_basis: '500.00',
                last_price: '58.00',
                has_price: true,
                price_source: 'bist',
                price_date: '2026-09-11',
                market_value: '580.00',
                unrealized_pl: '80.00',
                realized_pl: '0.00',
                market_value_reporting: '580.00',
                unrealized_pl_reporting: '80.00',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const queryClient = new QueryClient()
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect((await within(container).findAllByText('₺58,00')).length).toBeGreaterThan(0)
    expect(within(container).queryByText(/^manuel/)).not.toBeInTheDocument()
    fireEvent.click(within(container).getByRole('button', { name: 'GARAN işlemleri' }))
    expect(screen.queryByRole('menuitem', { name: 'Elle fiyatı kaldır' })).toBeNull()
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
  })

  // The chart's own hint, independent of the summary banner --
  // summary.missing_rates is empty here (today's rate exists) while the range
  // still has days that excluded USD, which is exactly the cliff case.
  it('warns under the chart only when the range excludes a currency', async () => {
    let historyMissingRates: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '500.00',
            positions_value: '0.00',
            cost_basis: '0.00',
            unrealized_pl: '0.00',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(
          JSON.stringify({ currency: 'TRY', points: [], missing_rates: historyMissingRates }),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const hint =
      'Kur bulunamadığı için bazı günlerde şu para birimlerindeki tutarlar TRY toplamlarına dahil edilmedi: USD'

    const renderPage = () =>
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )

    // ₺500,00 lands in both the cash and total-value tiles; any of them is
    // enough to know the page has loaded.
    const covered = renderPage()
    expect((await within(covered.container).findAllByText('₺500,00')).length).toBeGreaterThan(0)
    expect(within(covered.container).queryByText(hint)).not.toBeInTheDocument()
    covered.unmount()

    historyMissingRates = ['USD']
    const uncovered = renderPage()
    expect(await within(uncovered.container).findByText(hint)).toBeInTheDocument()
  })
  // The hero states the return in percent, which is unrealized over
  // what the positions cost (cost_basis).
  it('states the unrealized return against cost in a hint and drops it without a figure', async () => {
    let unrealized: string | null = '57277.75'
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '74399.58',
            cash_by_currency: { TRY: '74399.58' },
            cash_by_currency_reporting: { TRY: '74399.58' },
            total_value: '437860.80',
            positions_value: '363461.22',
            cost_basis: '306183.47',
            unrealized_pl: unrealized,
            realized_pl: '0.00',
            missing_rates: [],
            positions: [],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const renderPage = () =>
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )

    // 57.277,75 over the cost 306.183,47, in the unrealized cell's hint.
    const withGain = renderPage()
    expect(await within(withGain.container).findByText('+₺57.277,75')).toBeInTheDocument()
    expect(await hint('Gerçekleşmemiş K/Z')).toBe('+%18,7 maliyete göre · Maliyet ₺306.183,47')
    withGain.unmount()

    // Null unrealized P/L: no hint at all, and the cell reads '-'.
    unrealized = null
    const withoutGain = renderPage()
    expect(await within(withoutGain.container).findByText('₺437.860,80')).toBeInTheDocument()
    const cell = within(withoutGain.container).getByText('Gerçekleşmemiş K/Z', {
      selector: 'p',
    }).parentElement!
    expect(within(cell).getByText('—')).toBeInTheDocument()
  })

  // A closed position (quantity 0, e.g. an instrument sold out or one
  // that only paid a dividend) is listed by the API too, flagged closed. It
  // belongs behind the Kapalı switch, not in the open rows or the
  // allocation; the realized P/L total is the header card's alone.
  it('keeps closed positions behind the Kapalı switch', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            total_value: '1750.00',
            positions_value: '1250.00',
            cost_basis: '1000.00',
            unrealized_pl: '250.00',
            realized_pl: '174.39',
            missing_rates: [],
            positions: [
              {
                instrument_id: 7,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '10',
                avg_cost: '100.00',
                cost_basis: '1000.00',
                last_price: '125.00',
                has_price: true,
                price_source: 'bist',
                price_date: '2026-09-19',
                market_value: '1250.00',
                unrealized_pl: '250.00',
                realized_pl: '100.00',
                market_value_reporting: '1250.00',
                unrealized_pl_reporting: '250.00',
                realized_pl_reporting: '100.00',
              },
              {
                instrument_id: 9,
                symbol: 'QQQ',
                name: 'Invesco QQQ Trust',
                currency: 'TRY',
                quantity: '0',
                avg_cost: '0.00',
                cost_basis: '0.00',
                last_price: null,
                has_price: false,
                price_source: null,
                price_date: null,
                market_value: '0.00',
                unrealized_pl: '0.00',
                realized_pl: '74.39',
                market_value_reporting: '0.00',
                unrealized_pl_reporting: '0.00',
                realized_pl_reporting: '74.39',
                closed: true,
              },
              {
                // Long in one portfolio, oversold by as much in
                // another. Quantity 0, but open.
                instrument_id: 11,
                symbol: 'SISE',
                name: 'Şişecam',
                currency: 'TRY',
                quantity: '0',
                avg_cost: '0',
                cost_basis: '2000.00',
                last_price: null,
                has_price: false,
                price_source: null,
                price_date: null,
                market_value: '2000.00',
                unrealized_pl: '0.00',
                realized_pl: '0.00',
                market_value_reporting: '2000.00',
                unrealized_pl_reporting: '0.00',
                realized_pl_reporting: '0.00',
                closed: false,
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    // Only the open positions are counted, SISE too at quantity 0, and the
    // closed one is hidden until Kapalı is chosen.
    expect(table.getByText('Toplam · 2 pozisyon, TRY olarak')).toBeInTheDocument()
    expect(table.getByText('SISE')).toBeInTheDocument()
    expect(within(container).queryByText('QQQ')).not.toBeInTheDocument()
    // Realized P/L left the table: the header card has the total.
    expect(within(container).getAllByText('+₺174,39')).toHaveLength(1)

    fireEvent.click(within(container).getByRole('radio', { name: 'Kapalı' }))
    const closed = within(within(container).getByRole('table'))
    expect(closed.getByText('QQQ')).toBeInTheDocument()
    expect(closed.getByText('+₺74,39')).toBeInTheDocument()
  })

  // The API rounds each row to 2 decimals and the totals from the
  // unrounded amounts, so the rows (QA 2026-09-23: 106,070.72 + 10,500.00 -
  // 58,167.43 = 58,403.29) can sum a cent off the header card (58,403.30).
  // With every row converted, the footer shows the server's totals instead.
  it('shows the server totals in the table footer rather than the rounded row sum', async () => {
    const position = (id: number, value: string, gain: string) => ({
      instrument_id: id,
      symbol: `SYM${id}`,
      name: `SYM${id}`,
      currency: 'TRY',
      quantity: '1',
      avg_cost: '1.00',
      cost_basis: '1.00',
      last_price: value,
      has_price: true,
      price_source: 'bist',
      price_date: '2026-09-22',
      market_value: value,
      unrealized_pl: gain,
      realized_pl: '0.00',
      market_value_reporting: value,
      unrealized_pl_reporting: gain,
      realized_pl_reporting: '0.00',
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '500.00',
            cash_by_currency: { TRY: '500.00' },
            cash_by_currency_reporting: { TRY: '500.00' },
            // Rows sum to 350,000.01; unrounded they come to 350,000.03.
            total_value: '350500.03',
            positions_value: '350000.03',
            cost_basis: '291596.73',
            unrealized_pl: '58403.30',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              position(1, '200000.00', '106070.72'),
              position(2, '50000.00', '10500.00'),
              position(3, '100000.01', '-58167.43'),
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await within(container).findByRole('table'))
    expect(table.getByText(/^\+₺58\.403,30/)).toBeInTheDocument()
    expect(table.queryByText(/^\+₺58\.403,29/)).not.toBeInTheDocument()
    expect(table.getByText('₺350.000,03')).toBeInTheDocument()
  })

  it('subtracts cash and unrealized P/L from the total exactly, not as floats (GLG-943)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)

      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/summary') {
        return new Response(
          JSON.stringify({
            currency: 'TRY',
            cash: '0.01',
            cash_by_currency: { TRY: '0.01' },
            cash_by_currency_reporting: { TRY: '0.01' },
            total_value: '987654321098765.43',
            positions_value: '987654321098765.42',
            cost_basis: '987654321098765.41',
            unrealized_pl: '0.01',
            realized_pl: '0.00',
            missing_rates: [],
            positions: [
              {
                instrument_id: 1,
                symbol: 'THYAO',
                name: 'Türk Hava Yolları',
                currency: 'TRY',
                quantity: '1',
                avg_cost: '987654321098765.41',
                cost_basis: '987654321098765.41',
                last_price: '987654321098765.42',
                has_price: true,
                market_value: '987654321098765.42',
                unrealized_pl: '0.01',
                realized_pl: '0.00',
                market_value_reporting: '987654321098765.42',
                unrealized_pl_reporting: '0.01',
                realized_pl_reporting: '0.00',
              },
            ],
          }),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
      if (url.startsWith('/api/portfolios/1/history?')) {
        return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // As floats both come out at ₺987.654.321.098.765,40.
    const footer = within(await within(container).findByRole('table'))
      .getAllByRole('row')
      .at(-1)!
    expect(within(footer).getByText('₺987.654.321.098.765,42')).toBeInTheDocument()
    expect(await hint('Gerçekleşmemiş K/Z')).toContain('Maliyet ₺987.654.321.098.765,41')
  })

  describe('statistics card (GLG-596)', () => {
    function renderStats(reporting: Record<string, string> | null) {
      // 09:00 UTC on Sep 29 2026; 2024-07-15 is 2 years 2 months before it.
      vi.useFakeTimers({ toFake: ['Date'] })
      vi.setSystemTime(new Date('2026-09-29T09:00:00Z'))
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/api/portfolios') {
          return new Response(
            JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
            { status: 200 },
          )
        }
        if (url === '/api/portfolios/1/summary') {
          return new Response(
            JSON.stringify({
              currency: 'TRY',
              cash: '500.00',
              cash_by_currency: { TRY: '500.00' },
              cash_by_currency_reporting: { TRY: '500.00' },
              total_value: '500.00',
              positions_value: '0.00',
              cost_basis: '0.00',
              unrealized_pl: '0.00',
              realized_pl: '0.00',
              missing_rates: reporting ? [] : ['USD'],
              positions: [],
              stats: { first_transaction_date: '2024-07-15', by_currency: {}, reporting },
            }),
            { status: 200 },
          )
        }
        if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
        if (url.startsWith('/api/portfolios/1/history?')) {
          return new Response(JSON.stringify({ currency: 'TRY', points: [] }), { status: 200 })
        }
        throw new Error(`unexpected request: ${url}`)
      })
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    }

    // The nets are the server's, each rounded once: the rounded parts would
    // read ₺1.206,88 and ₺2,06.
    it('shows net contributions, net dividends, interest, fees and the first trade', async () => {
      renderStats({
        deposits: '1207.25',
        withdrawals: '0.37',
        net_contribution: '1206.89',
        fees: '7.89',
        dividends_gross: '2.43',
        dividends_tax: '0.37',
        dividends_net: '2.07',
        interest: '12.34',
      })
      const card = within(
        (await screen.findByRole('heading', { name: 'İstatistikler' })).closest(
          'section',
        ) as HTMLElement,
      )
      expect(card.getByText('₺1.206,89')).toBeInTheDocument()
      expect(card.getByText(subLine('yatırılan ₺1.207,25 · çekilen ₺0,37'))).toBeInTheDocument()
      expect(card.getByText('₺2,07')).toBeInTheDocument()
      expect(card.getByText(subLine('brüt ₺2,43 · stopaj ₺0,37'))).toBeInTheDocument()
      expect(card.getByText('₺12,34')).toBeInTheDocument()
      expect(card.getByText('₺7,89')).toBeInTheDocument()
      expect(card.getByText('15.07.2024')).toBeInTheDocument()
      expect(card.getByText('2 yıl 2 ay')).toBeInTheDocument()
    })

    it('keeps the cents of a net figure above 2^53 units (GLG-635)', async () => {
      renderStats({
        deposits: '100000000000000.03',
        withdrawals: '0.02',
        net_contribution: '100000000000000.01',
        fees: '0.00',
        dividends_gross: '0.00',
        dividends_tax: '0.00',
        dividends_net: '0.00',
        interest: '0.00',
      })
      const card = within(
        (await screen.findByRole('heading', { name: 'İstatistikler' })).closest(
          'section',
        ) as HTMLElement,
      )
      expect(card.getByText('₺100.000.000.000.000,01')).toBeInTheDocument()
    })

    it('shows - for the money cells without reporting totals', async () => {
      renderStats(null)
      const card = within(
        (await screen.findByRole('heading', { name: 'İstatistikler' })).closest(
          'section',
        ) as HTMLElement,
      )
      expect(card.getAllByText('—')).toHaveLength(4)
      expect(card.getByText('15.07.2024')).toBeInTheDocument()
    })
  })

  describe('layout A (GLG-934)', () => {
    const position = (
      instrument_id: number,
      symbol: string,
      value: string,
      pl: string,
      day: string | null,
    ) => ({
      instrument_id,
      symbol,
      name: symbol,
      market: 'BIST',
      currency: 'TRY',
      quantity: '10',
      avg_cost: '10.00',
      cost_basis: '100.00',
      last_price: '11.00',
      prev_close: '10.00',
      has_price: true,
      price_date: '2026-10-02',
      price_source: 'fetched',
      market_value: value,
      unrealized_pl: pl,
      realized_pl: '0.00',
      market_value_reporting: value,
      unrealized_pl_reporting: pl,
      realized_pl_reporting: '0.00',
      day_change_reporting: day,
    })
    const tupras = {
      instrument_id: 7,
      symbol: 'TUPRS',
      market: 'BIST',
      date: '2026-10-12',
      quantity: '10',
      per_share: '48',
      gross: '480.00',
      stopaj: '72.00',
      net: '408.00',
      currency: 'TRY',
    }

    // The body GET /api/me/attention-rules answers with.
    let attentionRules: object = {}
    afterEach(() => {
      attentionRules = {}
    })

    // Serves portfolio 1 with `summary` and records every request with its
    // method and body.
    // `created` holds the create call's answer back until it resolves.
    function mockApi(summary: object, expected: object[] = [], created?: Promise<void>) {
      const requests: { url: string; method: string; body: string }[] = []
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
        const url = String(input)
        requests.push({ url, method: init?.method ?? 'GET', body: String(init?.body ?? '') })
        if (url === '/api/me/attention-rules') return Response.json(attentionRules)
        if (url === '/api/portfolios') {
          return new Response(
            JSON.stringify([{ id: 1, name: 'Ana', created_at: '2026-01-01T00:00:00Z' }]),
            { status: 200 },
          )
        }
        if (url === '/api/portfolios/1/summary') {
          return new Response(JSON.stringify(summary), { status: 200 })
        }
        if (url === '/api/portfolios/1/dividends/expected') {
          return new Response(JSON.stringify(expected), { status: 200 })
        }
        if (url === '/api/portfolios/1/dividends/expected/dismiss') {
          return new Response(null, { status: 204 })
        }
        if (url === '/api/portfolios/1/transactions' && init?.method === 'POST') {
          await created
          return new Response(JSON.stringify({ id: 99 }), { status: 201 })
        }
        if (url.startsWith('/api/portfolios/1/attribution?')) return attribution()
        if (url.startsWith('/api/portfolios/1/history?')) {
          return new Response(
            JSON.stringify({
              currency: 'TRY',
              points: [
                { date: '2026-07-01', total_value: '900.00', cash: '500.00', cost_basis: '400' },
                { date: '2026-10-01', total_value: '1000.00', cash: '500.00', cost_basis: '400' },
              ],
              missing_rates: [],
              missing_prices: [],
              net_flow: '0.00',
              returns: { simple: '0.111111', twr: '0.111111', mwr: '0.111111' },
              benchmark: null,
            }),
            { status: 200 },
          )
        }
        throw new Error(`unexpected request: ${url}`)
      })
      return requests
    }

    const twoPositions = {
      currency: 'TRY',
      cash: '500.00',
      cash_by_currency: { TRY: '500.00' },
      cash_by_currency_reporting: { TRY: '500.00' },
      total_value: '1000.00',
      positions_value: '500.00',
      cost_basis: '400.00',
      unrealized_pl: '123.45',
      realized_pl: '10.00',
      fx_pl: null,
      day_change: '9.00',
      missing_rates: [],
      positions: [
        position(1, 'AKBNK', '300.00', '100.00', '12.00'),
        position(2, 'SISE', '200.00', '23.45', '-3.00'),
      ],
      stats: {
        first_transaction_date: '2026-01-05',
        by_currency: {},
        reporting: {
          deposits: '1000.00',
          withdrawals: '0.00',
          net_contribution: '1000.00',
          fees: '0.00',
          dividends_gross: '0.00',
          dividends_tax: '0.00',
          dividends_net: '0.00',
          interest: '0.00',
        },
      },
    }

    function renderPage() {
      return render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1']}>
            <Routes>
              <Route path="/portfolios/:id" element={<SummaryPage />} />
            </Routes>
            <Toaster />
          </MemoryRouter>
        </QueryClientProvider>,
      )
    }

    afterEach(() => flushToasts())

    it('orders return, attention, KPI strip, statistics, positions, then allocation beside Katkı', async () => {
      mockApi(twoPositions)
      renderPage()
      const sections = [
        await screen.findByRole('heading', { name: /^Getiri ·/ }),
        await screen.findByRole('heading', { name: 'Getiri dağılımı' }),
        await screen.findByRole('heading', { name: 'Dikkat' }),
        screen.getByText('Toplam değer', { selector: 'p' }),
        screen.getByRole('heading', { name: 'İstatistikler' }),
        screen.getByRole('heading', { name: 'Pozisyonlar' }),
        screen.getByRole('heading', { name: 'Dağılım' }),
        screen.getByRole('heading', { name: 'Katkı' }),
      ]
      const [ret, split, attention, , stats, , allocation, contribution] = sections
      for (let i = 1; i < sections.length; i++) {
        expect(
          sections[i - 1].compareDocumentPosition(sections[i]) & Node.DOCUMENT_POSITION_FOLLOWING,
        ).toBeTruthy()
      }
      // Return and attention share a row, about 2:1, from lg, with the split
      // under the return in its column.
      const column = ret.closest('section')!.parentElement!
      expect(column).toContainElement(split)
      const top = column.parentElement!
      expect(top).toHaveClass('lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]')
      expect(top).toContainElement(attention)
      expect(column).not.toContainElement(attention)
      // İstatistikler a full-width strip of five under the KPI strip, in view
      // without scrolling.
      expect(stats.closest('section')).toHaveClass('lg:grid-cols-5')
      // Dağılım and Katkı side by side from lg, stretched to one height.
      const bottom = allocation.closest('section')!.parentElement!
      expect(bottom).toHaveClass('lg:grid-cols-2')
      expect(bottom).not.toHaveClass('lg:items-start')
      expect(bottom).toContainElement(contribution)
      // The Katkı rows link to the position page.
      const katki = within(contribution.closest('section')!)
      expect(katki.getAllByRole('listitem').map((li) => li.textContent)).toEqual([
        'AKBNK+₺100,00',
        'SISE+₺23,45',
      ])
      expect(katki.getByRole('link', { name: 'AKBNK' })).toHaveAttribute(
        'href',
        '/portfolios/1/positions/1',
      )
    })

    it('shows five KPI cells and the unrealized P/L once outside the positions table', async () => {
      mockApi(twoPositions)
      renderPage()
      const total = await screen.findByText('Toplam değer', { selector: 'p' })
      const strip = total.closest('section')!
      expect(
        within(strip)
          .getAllByText((_, el) => el?.tagName === 'P' && el.className.includes('uppercase'))
          .map((el) => el.textContent),
      ).toEqual(['Toplam değer', 'Günlük', 'Nakit', 'Gerçekleşmemiş K/Z', 'Gerçekleşmiş K/Z'])
      expect(strip).toHaveClass('grid-cols-2', 'lg:grid-cols-[1.3fr_1fr_1fr_1fr_1fr]')
      // The day total is the sum of the rows' reporting day changes.
      expect(within(strip).getByText('+₺9,00')).toBeInTheDocument()
      const positions = screen.getByRole('heading', { name: 'Pozisyonlar' }).closest('section')!
      const outside = screen
        .getAllByText('+₺123,45', { exact: false })
        .filter((el) => !positions.contains(el))
      expect(outside).toHaveLength(1)
      expect(strip).toContainElement(outside[0])
      // What the old hero's sub-lines said is a hint on the cell it explains.
      fireEvent.click(within(strip).getByRole('button', { name: 'Gerçekleşmemiş K/Z' }))
      expect(await screen.findByText('+%30,9 maliyete göre · Maliyet ₺400,00')).toBeInTheDocument()
    })

    it('feeds the expected dividends to the attention card and books one on Ekle', async () => {
      const requests = mockApi(twoPositions, [tupras])
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      fireEvent.click(
        await card.findByRole('button', { name: 'TUPRS 12.10.2026 temettüsünü ekle' }),
      )
      await vi.waitFor(() =>
        expect(
          requests.find((r) => r.url === '/api/portfolios/1/transactions' && r.method === 'POST'),
        ).toBeDefined(),
      )
      const post = JSON.parse(
        requests.find((r) => r.url === '/api/portfolios/1/transactions')!.body,
      )
      expect(post).toMatchObject({ type: 'dividend', instrument_id: 7, date: '2026-10-12' })
      expect(Number(post.amount)).toBe(480)
      expect(Number(post.fee)).toBe(72)
      expect(await screen.findByText('1 temettü eklendi')).toBeInTheDocument()
    })

    it('books a dividend once when Ekle is clicked again while the first is in flight', async () => {
      let resolve = () => {}
      const created = new Promise<void>((r) => (resolve = r))
      const requests = mockApi(twoPositions, [tupras], created)
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      const add = await card.findByRole('button', { name: 'TUPRS 12.10.2026 temettüsünü ekle' })
      fireEvent.click(add)
      await vi.waitFor(() => expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1))
      fireEvent.click(add)
      fireEvent.click(card.getByRole('button', { name: 'TUPRS 12.10.2026 temettüsünü yoksay' }))
      // Both stay focusable but are announced as unavailable meanwhile.
      expect(add).toHaveAttribute('aria-disabled', 'true')
      resolve()
      expect(await screen.findByText('1 temettü eklendi')).toBeInTheDocument()
      expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1)
    })

    // isPending reaches the buttons a tick after the click, so a
    // double click's second click came before pending and booked it twice.
    it('books a dividend once on a double click of Ekle', async () => {
      let resolve = () => {}
      const created = new Promise<void>((r) => (resolve = r))
      const requests = mockApi(twoPositions, [tupras], created)
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      const add = await card.findByRole('button', { name: 'TUPRS 12.10.2026 temettüsünü ekle' })
      fireEvent.click(add)
      fireEvent.click(add)
      await vi.waitFor(() => expect(add).toHaveAttribute('aria-disabled', 'true'))
      resolve()
      await vi.waitFor(() => expect(add).not.toHaveAttribute('aria-disabled'))
      expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1)
    })

    it('dismisses an expected dividend on Yoksay', async () => {
      const requests = mockApi(twoPositions, [tupras])
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      fireEvent.click(
        await card.findByRole('button', { name: 'TUPRS 12.10.2026 temettüsünü yoksay' }),
      )
      await vi.waitFor(() =>
        expect(
          requests.find((r) => r.url === '/api/portfolios/1/dividends/expected/dismiss'),
        ).toBeDefined(),
      )
      expect(await screen.findByText('Temettü yoksayıldı')).toBeInTheDocument()
    })

    it('renders a cash-only portfolio without a Bugün group and with an empty positions table', async () => {
      mockApi({
        ...twoPositions,
        total_value: '500.00',
        positions_value: '0.00',
        cost_basis: '0.00',
        unrealized_pl: '0.00',
        realized_pl: '0.00',
        positions: [],
      })
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      expect(card.queryByText('Bugün')).toBeNull()
      // All cash: cash is above 40% of the total.
      expect(card.getByText('Yoğunlaşma')).toBeInTheDocument()
      expect(await screen.findByText('Açık pozisyon yok.')).toBeInTheDocument()
    })

    // AKBNK is 30% of the total: above the default 25%, below an override of 0.5.
    const concentrated = (card: ReturnType<typeof within>) =>
      within(card.getByText('Yoğunlaşma').nextElementSibling as HTMLElement)
        .getAllByRole('listitem')
        .map((li) => li.textContent)

    it('hides a concentration item that the owner raised the position share above', async () => {
      attentionRules = { position_share: { value: 0.5 }, no_hedge: { enabled: false } }
      mockApi(twoPositions)
      renderPage()
      const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
      // Cash, 50% of the total, stays above its own default of 40%; as TRY cash it
      // is the TRY item.
      await vi.waitFor(() =>
        expect(concentrated(card)).toEqual([expect.stringContaining('TL nakitte')]),
      )
    })

    describe('Tüm portföyler (GLG-935)', () => {
      const listed = (id: number, name: string, total_value: string) => ({
        id,
        name,
        created_at: '2026-01-01T00:00:00Z',
        total_value,
        cash: '0.00',
        unrealized_pl: null,
        currency: 'TRY',
        missing_rates: [],
      })
      const history = {
        currency: 'TRY',
        points: [
          { date: '2026-07-01', total_value: '900.00', cash: '500.00', cost_basis: '400' },
          { date: '2026-10-01', total_value: '1000.00', cash: '500.00', cost_basis: '400' },
        ],
        missing_rates: [],
        missing_prices: [],
        net_flow: '0.00',
        returns: { simple: '0.111111', twr: '0.111111', mwr: '0.111111' },
        benchmark: null,
      }

      // Two portfolios: Ana holds AKBNK and SISE with a TUPRS dividend to
      // confirm, Yan holds THYAO; `all` is their sum.
      function mockAll(all: object = twoPositions, portfolios = [listed(1, 'Ana', '1000.00')]) {
        const requests: { url: string; method: string }[] = []
        vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
          const url = String(input)
          const method = init?.method ?? 'GET'
          requests.push({ url, method })
          if (url === '/api/me/attention-rules') return Response.json(attentionRules)
          if (url === '/api/portfolios' && method === 'POST') {
            portfolios.push(listed(2, 'Birikim', '0.00'))
            return Response.json({ id: 2, name: 'Birikim' }, { status: 201 })
          }
          if (url === '/api/portfolios') return Response.json(portfolios)
          if (url === '/api/portfolios/all/summary') return Response.json(all)
          if (url === '/api/portfolios/1/summary') return Response.json(twoPositions)
          if (url === '/api/portfolios/2/summary')
            return Response.json({
              ...twoPositions,
              positions: [position(3, 'THYAO', '100.00', '1.00', '50.00')],
            })
          if (url === '/api/portfolios/1/dividends/expected') return Response.json([tupras])
          if (url === '/api/portfolios/2/dividends/expected') return Response.json([])
          if (url === '/api/portfolios/1/transactions' && method === 'POST')
            return Response.json({ id: 99 }, { status: 201 })
          if (/^\/api\/portfolios\/(all|1|2)\/attribution\?/.test(url)) return attribution()
          if (/^\/api\/portfolios\/(all|1|2)\/history\?/.test(url)) return Response.json(history)
          throw new Error(`unexpected request: ${url}`)
        })
        return requests
      }

      function renderAll(client = new QueryClient()) {
        return render(
          <QueryClientProvider client={client}>
            <MemoryRouter initialEntries={['/portfolios/all']}>
              <Routes>
                <Route path="/portfolios/:id" element={<SummaryPage />} />
              </Routes>
              <LocationProbe />
              <Toaster />
            </MemoryRouter>
          </QueryClientProvider>,
        )
      }

      it('has Özet and Performans as its tabs and offers a new portfolio in the header', async () => {
        mockAll()
        renderAll()
        expect(await screen.findByRole('button', { name: 'Yeni portföy' })).toBeInTheDocument()
        // Performans brought the tab row back.
        const tabs = screen.getByRole('navigation', { name: 'Portföy sekmeleri' })
        expect(
          within(tabs)
            .getAllByRole('link')
            .map((l) => l.textContent),
        ).toEqual(['Özet', 'Performans'])
      })

      it('names the portfolio of each attention item and books its dividend there', async () => {
        const requests = mockAll(twoPositions, [
          listed(1, 'Ana', '1000.00'),
          listed(2, 'Yan', '100.00'),
        ])
        renderAll()
        const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
        // The action's name leads with the portfolio.
        const add = await card.findByRole('button', {
          name: 'Ana: TUPRS 12.10.2026 temettüsünü ekle',
        })
        // The name is its own element, titled with it.
        const named = (li: HTMLElement) => [
          li.querySelector('[title]')?.getAttribute('title'),
          li.textContent,
        ]
        expect(named(add.closest('li')!)).toEqual(['Ana', expect.stringMatching(/^AnaTUPRS/)])
        // The largest movers over every portfolio, two at most, each named.
        await vi.waitFor(() => expect(card.getByText('Bugün')).toBeInTheDocument())
        const movers = within(card.getByText('Bugün').nextElementSibling as HTMLElement)
          .getAllByRole('listitem')
          .map(named)
        expect(movers).toEqual([
          ['Yan', expect.stringMatching(/^YanTHYAO/)],
          ['Ana', expect.stringMatching(/^AnaAKBNK/)],
        ])
        fireEvent.click(add)
        await vi.waitFor(() =>
          expect(
            requests.find((r) => r.url === '/api/portfolios/1/transactions' && r.method === 'POST'),
          ).toBeDefined(),
        )
        expect(await screen.findByText('1 temettü eklendi')).toBeInTheDocument()
      })

      it('hides a concentration item that the owner raised the position share above', async () => {
        attentionRules = { position_share: { value: 0.5 }, no_hedge: { enabled: false } }
        mockAll()
        const client = new QueryClient()
        renderAll(client)
        // The override is in the cache the account page invalidates.
        await vi.waitFor(() =>
          expect(client.getQueryData(attentionRulesQueryKey)).toEqual(attentionRules),
        )
        const card = within(await screen.findByRole('region', { name: 'Dikkat' }))
        await vi.waitFor(() =>
          expect(concentrated(card)).toEqual([expect.stringContaining('TL nakitte')]),
        )
      })

      it('shows the statistics above the positions, and Katkı beside Dağılım with plain-text symbols', async () => {
        mockAll()
        renderAll()
        const katki = (await screen.findByRole('heading', { name: 'Katkı' })).closest('section')!
        expect(katki.parentElement).toContainElement(
          screen.getByRole('heading', { name: 'Dağılım' }),
        )
        expect(within(katki).getAllByRole('listitem')).toHaveLength(2)
        expect(within(katki).queryByRole('link')).toBeNull()
        expect(
          screen
            .getByRole('heading', { name: 'İstatistikler' })
            .compareDocumentPosition(screen.getByRole('heading', { name: 'Pozisyonlar' })) &
            Node.DOCUMENT_POSITION_FOLLOWING,
        ).toBeTruthy()
      })

      it('lists every portfolio below the KPI strip', async () => {
        mockAll(twoPositions, [listed(1, 'Ana', '1000.00'), listed(2, 'Yan', '100.00')])
        renderAll()
        const heading = await screen.findByRole('heading', { name: /^Portföyler/ })
        const table = within(heading.closest('section')!)
        expect(table.getByRole('link', { name: 'Ana' })).toHaveAttribute('href', '/portfolios/1')
        expect(table.getByRole('link', { name: 'Yan' })).toHaveAttribute('href', '/portfolios/2')
        expect(
          screen.getByText('Toplam değer', { selector: 'p' }).compareDocumentPosition(heading) &
            Node.DOCUMENT_POSITION_FOLLOWING,
        ).toBeTruthy()
        expect(
          heading.compareDocumentPosition(screen.getByRole('heading', { name: 'Pozisyonlar' })) &
            Node.DOCUMENT_POSITION_FOLLOWING,
        ).toBeTruthy()
      })

      // Reporting in EUR with no rate, cash in TRY and USD. The
      // figures that leave the excluded currencies out carry the mark the
      // sidebar total carries.
      it('marks the total, cash and unrealized P/L when currencies are left out', async () => {
        mockAll({
          ...twoPositions,
          currency: 'EUR',
          cash: '0.00',
          cash_by_currency: { TRY: '500.00', USD: '20.00' },
          cash_by_currency_reporting: {},
          unrealized_pl: null,
          missing_rates: ['TRY', 'USD'],
        })
        renderAll()
        const strip = (await screen.findByText('Toplam değer', { selector: 'p' })).closest(
          'section',
        )!
        const label =
          'Kur bulunamadığı için şu para birimlerindeki tutarlar EUR toplamlarına dahil edilmedi: TRY, USD'
        for (const cell of ['Toplam değer', 'Nakit', 'Gerçekleşmemiş K/Z']) {
          const kpi = within(strip).getByText(cell).closest('div')!
          expect(within(kpi).getByLabelText(label)).toBeInTheDocument()
        }
        // The marks and the attention card say it; no notice repeats it.
        expect(screen.queryByText(label)).toBeNull()
      })

      it('shows no mark when every currency converts', async () => {
        mockAll()
        renderAll()
        await screen.findByText('Toplam değer', { selector: 'p' })
        expect(screen.queryByText('*')).toBeNull()
      })

      it('offers to create the first portfolio when there is none', async () => {
        mockAll(twoPositions, [])
        renderAll()
        expect(await screen.findByText('Henüz portföyün yok')).toBeInTheDocument()
        expect(screen.queryByText('Toplam değer', { selector: 'p' })).toBeNull()
        fireEvent.click(screen.getByRole('button', { name: 'İlk portföyünü oluştur' }))
        expect(
          screen.getByText('En fazla 100 karakter. Aynı adda ikinci portföy açılamaz.'),
        ).toBeInTheDocument()
      })

      // The opener leaves with Tüm portföyler, so the new
      // portfolio's page takes focus instead of <body>. The real
      // hook refetches the list before the create resolves, which unmounts
      // the empty state and its dialog first; the POST answers only once
      // that has happened, so the create still has to navigate and toast.
      it.each([
        ['the header', [listed(1, 'Ana', '1000.00')], 'Yeni portföy'],
        ['the empty state', [], 'İlk portföyünü oluştur'],
      ])(
        "focuses the new portfolio's add button after creating it from %s",
        async (_, start, opener) => {
          const client = new QueryClient()
          mockAll(twoPositions, start)
          const fetchMock = vi.mocked(globalThis.fetch)
          const answer = fetchMock.getMockImplementation()!
          fetchMock.mockImplementation(async (input, init) => {
            const response = await answer(input, init)
            if (init?.method === 'POST') {
              await client.refetchQueries({ queryKey: ['portfolios'] })
              await vi.waitFor(() => expect(screen.queryByText('Henüz portföyün yok')).toBeNull())
            }
            return response
          })
          renderAll(client)
          fireEvent.click(await screen.findByRole('button', { name: opener }))
          const dialog = screen.getByRole('dialog')
          fireEvent.change(within(dialog).getByRole('textbox', { name: 'Ad' }), {
            target: { value: 'Birikim' },
          })
          fireEvent.click(within(dialog).getByRole('button', { name: 'Oluştur' }))
          await vi.waitFor(() =>
            expect(screen.getByTestId('location')).toHaveTextContent('/portfolios/2'),
          )
          await vi.waitFor(() =>
            expect(document.activeElement).toBe(screen.getByRole('button', { name: 'İşlem ekle' })),
          )
          expect(await screen.findByText('Portföy oluşturuldu')).toBeInTheDocument()
        },
      )
    })
  })
})
