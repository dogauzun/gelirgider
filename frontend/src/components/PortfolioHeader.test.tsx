import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Link, MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { SummaryPage } from '../pages/SummaryPage'
import { clearToasts } from '../lib/toast'
import { PortfolioHeader } from './PortfolioHeader'
import { Toaster } from './ui/Toast'

// jsdom implements neither method on <dialog>; both flip the open state, and
// close() hands focus back to what had it before showModal(), as browsers do.
let focusBeforeDialog: Element | null = null
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    focusBeforeDialog = document.activeElement
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
    if (focusBeforeDialog instanceof HTMLElement) focusBeforeDialog.focus()
  }
})

// Portfolios 1 and 2; DELETE removes 1. Every summary is empty and every
// history fails, as the header is under test, not the page body. `change`
// gives portfolio 2 a new value, as booking a dividend would.
function mockDeleteApi() {
  let deleted = false
  let value = 0
  const card = (id: number, name: string) => ({
    id,
    name,
    created_at: '2026-01-01T00:00:00Z',
    total_value: `${id === 2 ? value : 0}.00`,
    cash: '0.00',
    unrealized_pl: null,
    currency: 'TRY',
    missing_rates: [],
  })
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if ((init?.method ?? 'GET') === 'DELETE') {
      deleted = true
      return new Response(null, { status: 204 })
    }
    if (url === '/api/portfolios') {
      const list = [card(2, 'Uzun vade')]
      if (!deleted) list.unshift(card(1, 'Emeklilik'))
      return new Response(JSON.stringify(list), { status: 200 })
    }
    if (url.includes('/history?')) return Response.json({ error: 'boom' }, { status: 500 })
    if (url.endsWith('/summary')) {
      return Response.json({
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
        stats: { first_transaction_date: null, by_currency: {}, reporting: null },
      })
    }
    return new Response(JSON.stringify([]), { status: 200 })
  })
  return { change: () => (value += 1) }
}

function LocationProbe() {
  const { pathname, search } = useLocation()
  return <output data-testid="location">{pathname + search}</output>
}

describe('PortfolioHeader', () => {
  afterEach(() => {
    cleanup()
    clearToasts()
    vi.restoreAllMocks()
  })

  // GLG-587: the position page is a drill-down of Özet, so Özet is its tab.
  it('marks the summary tab active on a position page', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([
            { id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' },
            { id: 2, name: 'Uzun vade', created_at: '2026-01-02T00:00:00Z' },
          ]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/positions/7']}>
          <PortfolioHeader portfolioId={1} ownHeading />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Emeklilik')
    expect(screen.getByRole('link', { name: t('summary.title') })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(screen.getByRole('link', { name: t('transactions.title') })).not.toHaveAttribute(
      'aria-current',
    )
  })

  // GLG-988: the Özet tab keeps the chart range from a position page, as the
  // sidebar does; İşlemler's own from / to filter is not a chart range.
  it.each([
    ['/portfolios/1/positions/7?range=1y', '/portfolios/1?range=1y'],
    ['/portfolios/1/transactions?from=2026-01-01&to=2026-02-01', '/portfolios/1'],
  ])('links the summary tab from %s to %s', async (entry, href) => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={[entry]}>
          <PortfolioHeader portfolioId={1} ownHeading />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Emeklilik')
    expect(screen.getByRole('link', { name: t('summary.title') })).toHaveAttribute('href', href)
  })

  it('announces a failed portfolio list load (GLG-469)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 }),
    )

    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByRole('alert')).toHaveTextContent(t('portfolios.error'))
  })

  // GLG-568: the pending-dividend count lives in the panel's summary; on the
  // tab it read as a transaction count. The header no longer asks for it.
  it('puts no count on the transactions tab', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // GLG-890: the truncated name shows in full on hover.
    expect(await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })).toHaveAttribute(
      'title',
      'Emeklilik',
    )
    expect(screen.getByRole('link', { name: 'İşlemler' })).toHaveTextContent(/^İşlemler$/)
    // GLG-605: the Temettü tab sits between İşlemler and İçe aktar; GLG-955:
    // Performans between Özet and İşlemler.
    expect(screen.getAllByRole('link').map((link) => link.textContent)).toEqual([
      t('summary.title'),
      t('performance.title'),
      t('transactions.title'),
      t('dividends.page.title'),
      t('import.title'),
    ])
    expect(screen.getByRole('link', { name: t('dividends.page.title') })).toHaveAttribute(
      'href',
      '/portfolios/1/dividends',
    )
    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual(['/api/portfolios'])
  })

  it('sticks the tab row under the top bar on phone (GLG-571)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <PortfolioHeader portfolioId={1}>
            <div>body</div>
          </PortfolioHeader>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    const nav = screen.getByRole('navigation')
    expect(nav).toHaveClass('max-sm:sticky', 'max-sm:top-12', 'max-sm:z-10', 'max-sm:bg-paper')
    // A sticky box only sticks within its parent: it shares one with the page
    // body, so it stays stuck while the body scrolls past.
    expect(nav.parentElement).toBe(screen.getByText('body').parentElement)
  })

  it('scrolls the tab row to the active tab (GLG-1060)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )
    // jsdom has no layout: a 375 px row whose active tab ends at 420 px.
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: Element,
    ) {
      return this.getAttribute('aria-current') === 'page'
        ? new DOMRect(340, 0, 80, 40)
        : new DOMRect(0, 0, 375, 40)
    })
    const scrollLeft = vi.spyOn(Element.prototype, 'scrollLeft', 'set')

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/import']}>
          <PortfolioHeader portfolioId={1}>
            <div>body</div>
          </PortfolioHeader>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    expect(scrollLeft).toHaveBeenCalledWith(45)
  })

  it('names the page with an h1 inside the main landmark, beside a named app nav (GLG-668)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <nav aria-label={t('nav.label')} />
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const heading = await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    const main = screen.getByRole('main')
    expect(main).toContainElement(heading)
    // Visible now that the sidebar switches portfolios (GLG-875).
    expect(heading).not.toHaveClass('sr-only')
    const names = screen.getAllByRole('navigation').map((nav) => nav.getAttribute('aria-label'))
    expect(names).toEqual([t('nav.label'), t('portfolio.tabs')])
  })

  // GLG-940: below lg the top bar already names the page, so the h1 is there
  // for screen readers only; from lg it shows. Tüm portföyler too.
  it.each([
    [1, 'Emeklilik'],
    ['all', t('portfolio.all')],
  ] as const)('hides the h1 below lg only (GLG-940): %s', async (portfolioId, name) => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={[`/portfolios/${portfolioId}`]}>
          <PortfolioHeader portfolioId={portfolioId} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // The all scope's heading reads the same, fully hidden, while the list
    // loads; wait for the loaded one.
    const heading = await screen.findByRole('heading', { level: 1, name })
    await waitFor(() => expect(heading).toHaveClass('max-lg:sr-only'))
    expect(heading).not.toHaveClass('sr-only')
  })

  // GLG-995: below lg the hidden title leaves its block empty, so the header
  // row spaces it from the actions only from lg; on a phone an empty Özet's
  // stacked actions start where the header starts. The rename input shows
  // below lg, so it gets the gap back.
  it('spaces the hidden title from the actions from lg only (GLG-995)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} add="quiet" />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const heading = await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    const row = heading.parentElement!.parentElement!
    expect(row).toHaveClass('flex-col', 'lg:gap-3')
    expect(row).not.toHaveClass('gap-3')

    fireEvent.click(screen.getByRole('button', { name: t('portfolio.menu') }))
    fireEvent.click(screen.getByRole('menuitem', { name: t('portfolios.rename') }))
    expect(row).toHaveClass('gap-3')
  })

  // GLG-940: the rename input takes the title row's width, up to a maximum,
  // instead of shrinking to its own 208 px.
  it('widens the rename input to the title row (GLG-940)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    fireEvent.click(screen.getByRole('button', { name: t('portfolio.menu') }))
    fireEvent.click(screen.getByRole('menuitem', { name: t('portfolios.rename') }))
    const input = screen.getByLabelText(t('portfolios.name'))
    expect(input).toHaveClass('w-full', 'max-w-xl')
    expect(input.parentElement).toHaveClass('flex-1')
  })

  it('keeps an h1 while the portfolio list loads (GLG-690)', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise<Response>(() => {}))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(
      screen.getByRole('heading', { level: 1, name: t('portfolio.label') }),
    ).toBeInTheDocument()
  })

  it('leaves the h1 to a page with its own (GLG-668, GLG-677)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/positions/7']}>
          <PortfolioHeader portfolioId={1} ownHeading />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Emeklilik')
    expect(screen.queryByRole('heading', { level: 1 })).not.toBeInTheDocument()
  })

  it('hands focus back to the menu button after a rename or a cancelled delete (GLG-441)', async () => {
    const patches: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if ((init?.method ?? 'GET') === 'PATCH') {
        patches.push(init?.body as string)
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      return new Response(JSON.stringify([]), { status: 200 })
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <PortfolioHeader portfolioId={1} />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    const menu = screen.getByRole('button', { name: t('portfolio.menu') })
    // fireEvent.click does not move focus; a keyboard user's does.
    const choose = (name: string) => {
      fireEvent.click(menu)
      const item = screen.getByRole('menuitem', { name })
      item.focus()
      fireEvent.click(item)
    }
    const rename = (key: string, value: string) => {
      choose(t('portfolios.rename'))
      const input = screen.getByLabelText(t('portfolios.name'))
      fireEvent.change(input, { target: { value } })
      // Default cancelled, so Enter does not go on to click the menu button
      // that now has focus and reopen the menu (GLG-474).
      expect(fireEvent.keyDown(input, { key })).toBe(false)
    }

    rename('Enter', 'Kenar')
    expect(document.activeElement).toBe(menu)
    await waitFor(() => expect(patches).toEqual([JSON.stringify({ name: 'Kenar' })]))

    // Escape reverts: focus comes back and nothing more is sent.
    rename('Escape', 'Taslak')
    expect(document.activeElement).toBe(menu)

    choose(t('portfolios.delete'))
    fireEvent.click(await screen.findByRole('button', { name: t('confirm.cancel') }))
    expect(document.activeElement).toBe(menu)
    expect(patches).toHaveLength(1)
  })

  // GLG-857: the header unmounts on the way home, taking focus with it; the
  // home page puts it on the first remaining card instead of <body>. GLG-863:
  // the status region says the portfolio was deleted.
  it('focuses the next portfolio on Tüm portföyler and announces the delete from its own page', async () => {
    mockDeleteApi()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
            <Route path="/portfolios/1" element={<PortfolioHeader portfolioId={1} />} />
          </Routes>
        </MemoryRouter>
        <Toaster />
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    fireEvent.click(screen.getByRole('button', { name: t('portfolio.menu') }))
    const item = screen.getByRole('menuitem', { name: t('portfolios.delete') })
    item.focus()
    fireEvent.click(item)
    fireEvent.click(await screen.findByRole('button', { name: t('portfolios.delete') }))

    const remaining = await screen.findByRole('link', { name: 'Uzun vade' })
    await waitFor(() => expect(document.activeElement).toBe(remaining))
    expect(screen.getByRole('status')).toHaveTextContent('Portföy silindi')
  })

  // The delete lands on Tüm portföyler through the same /portfolios/:id
  // route element, so the page and its header stay mounted. A later list
  // refetch must not send it there again: that dropped ?range= and moved
  // focus once more.
  it('redirects once after a delete, not again when the list refetches', async () => {
    const api = mockDeleteApi()
    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route path="/portfolios/:id" element={<SummaryPage />} />
          </Routes>
          <Link to="/portfolios/all?range=1y">1Y</Link>
          <LocationProbe />
        </MemoryRouter>
        <Toaster />
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: 'Emeklilik' })
    fireEvent.click(screen.getByRole('button', { name: t('portfolio.menu') }))
    fireEvent.click(screen.getByRole('menuitem', { name: t('portfolios.delete') }))
    fireEvent.click(await screen.findByRole('button', { name: t('portfolios.delete') }))
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('link', { name: 'Uzun vade' })),
    )

    // The user picks a range and moves on; then the list refetches.
    fireEvent.click(screen.getByRole('link', { name: '1Y' }))
    expect(screen.getByTestId('location')).toHaveTextContent('/portfolios/all?range=1y')
    const button = screen.getByRole('button', { name: t('portfolios.new') })
    button.focus()
    api.change()
    await queryClient.refetchQueries({ queryKey: ['portfolios'], exact: true })
    await new Promise((resolve) => setTimeout(resolve, 50))

    expect(screen.getByTestId('location')).toHaveTextContent('/portfolios/all?range=1y')
    expect(document.activeElement).toBe(button)
  })

  it('shows a read-only header for all portfolios (GLG-540)', async () => {
    const urls: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      urls.push(url)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([
            { id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' },
            { id: 2, name: 'Uzun vade', created_at: '2026-01-02T00:00:00Z' },
          ]),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/all']}>
          <PortfolioHeader portfolioId="all" />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('heading', { level: 1, name: t('portfolio.all') })
    // Özet and Performans only (GLG-955).
    expect(screen.getAllByRole('link').map((link) => link.getAttribute('href'))).toEqual([
      '/portfolios/all',
      '/portfolios/all/performance',
    ])
    expect(
      screen.queryByRole('button', { name: t('transactions.addTitle') }),
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: t('portfolio.menu') })).not.toBeInTheDocument()
    expect(urls).toEqual(['/api/portfolios'])

    // A new portfolio instead, opened in the create dialog.
    fireEvent.click(screen.getByRole('button', { name: t('portfolios.new') }))
    expect(screen.getByText(t('portfolios.nameHint'))).toBeInTheDocument()
  })

  it('offers no actions for a portfolio not in the list (GLG-338)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') {
        return new Response(
          JSON.stringify([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }]),
          { status: 200 },
        )
      }
      throw new Error(`unexpected request: ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/3']}>
          <PortfolioHeader portfolioId={3}>
            <p>page body</p>
          </PortfolioHeader>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Portföy bulunamadı.' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Portföylere dön' })).toHaveAttribute('href', '/')
    expect(screen.queryByRole('button', { name: 'İşlem ekle' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Özet' })).not.toBeInTheDocument()
    expect(screen.queryByText('page body')).not.toBeInTheDocument()
  })
})
