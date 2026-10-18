import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, useLocation } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { setPrivacy } from '../lib/privacy'
import { Sidebar } from './Sidebar'

// jsdom implements neither method on <dialog>.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

function portfolio(id: number, name: string, total_value: string, missing_rates: string[] = []) {
  return {
    id,
    name,
    created_at: '2026-01-01T00:00:00Z',
    total_value,
    cash: '0',
    unrealized_pl: null,
    currency: 'TRY',
    missing_rates,
  }
}

const twoPortfolios = [
  portfolio(1, 'Ana portföy', '1284650.40'),
  portfolio(2, 'Emeklilik', '842000'),
]

function mockApi({
  portfolios = twoPortfolios as unknown,
  status = 200,
  isAdmin = false,
}: { portfolios?: unknown; status?: number; isAdmin?: boolean } = {}) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/me') {
      return Response.json({
        id: 1,
        email: 'ada@example.com',
        reporting_currency: 'TRY',
        is_admin: isAdmin,
      })
    }
    if (url === '/api/portfolios') {
      return status === 200
        ? Response.json(portfolios)
        : Response.json({ error: 'boom' }, { status })
    }
    throw new Error(`unexpected request: ${url}`)
  })
}

function renderSidebar(path = '/') {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={[path]}>
        <Sidebar />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// Intl may put a no-break space before the unit.
const text = (element: HTMLElement) => element.textContent?.replace(/\s/g, ' ')

describe('Sidebar', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    setPrivacy(false)
  })

  it('lists each portfolio with its compact value and the combined total', async () => {
    mockApi()
    renderSidebar()

    const ana = await screen.findByRole('link', { name: /Ana portföy/ })
    expect(text(ana)).toContain('₺1,28 Mn')
    expect(within(ana).getByText('Ana portföy')).toHaveAttribute('title', 'Ana portföy')
    expect(text(screen.getByRole('link', { name: /Emeklilik/ }))).toContain('₺842 B')
    const all = screen.getByRole('link', { name: new RegExp(t('portfolio.all')) })
    expect(all).toHaveAttribute('href', '/portfolios/all')
    expect(text(all)).toContain('₺2,13 Mn')
  })

  // A value that left out holdings it could not convert says so.
  it('marks a value that left out currencies without a rate', async () => {
    mockApi({
      portfolios: [
        portfolio(1, 'Ana portföy', '0', ['USD']),
        portfolio(2, 'Emeklilik', '0', ['EUR', 'USD']),
        portfolio(3, 'Nakit', '500'),
      ],
    })
    renderSidebar()

    const mark = (currencies: string) => t('summary.missingRates', { currency: 'TRY', currencies })
    const ana = await screen.findByRole('link', { name: /Ana portföy/ })
    expect(within(ana).getByRole('img', { name: mark('USD') })).toBeInTheDocument()
    const nakit = screen.getByRole('link', { name: /Nakit/ })
    expect(within(nakit).queryByRole('img')).not.toBeInTheDocument()
    const all = screen.getByRole('link', { name: new RegExp(t('portfolio.all')) })
    expect(within(all).getByRole('img', { name: mark('EUR, USD') })).toBeInTheDocument()
  })

  it('links to the watchlist', async () => {
    mockApi()
    renderSidebar()

    expect(await screen.findByRole('link', { name: t('nav.watchlist') })).toHaveAttribute(
      'href',
      '/watchlist',
    )
  })

  it('marks the open portfolio and keeps the tab', async () => {
    mockApi()
    renderSidebar('/portfolios/1/transactions')

    expect(await screen.findByRole('link', { name: /Ana portföy/ })).toHaveAttribute(
      'aria-current',
      'page',
    )
    expect(screen.getByRole('link', { name: /Emeklilik/ })).toHaveAttribute(
      'href',
      '/portfolios/2/transactions',
    )
  })

  // Tüm portföyler has a Performans tab, and no other but Özet.
  it.each([
    ['/portfolios/1/performance', '/portfolios/2/performance', '/portfolios/all/performance'],
    ['/portfolios/all/performance', '/portfolios/2/performance', '/portfolios/all/performance'],
    ['/portfolios/1/dividends', '/portfolios/2/dividends', '/portfolios/all'],
  ])('from %s links the other portfolio to %s and the whole to %s', async (from, other, all) => {
    mockApi()
    renderSidebar(from)

    expect(await screen.findByRole('link', { name: /Emeklilik/ })).toHaveAttribute('href', other)
    expect(screen.getByRole('link', { name: new RegExp(t('portfolio.all')) })).toHaveAttribute(
      'href',
      all,
    )
  })

  // The instrument may not be held in the other portfolio.
  it('goes to the other portfolio summary from a position page', async () => {
    mockApi()
    renderSidebar('/portfolios/1/positions/5')

    expect(await screen.findByRole('link', { name: /Emeklilik/ })).toHaveAttribute(
      'href',
      '/portfolios/2',
    )
  })

  it('shows the admin link only to an admin', async () => {
    mockApi({ isAdmin: true })
    renderSidebar()
    expect(await screen.findByRole('link', { name: t('nav.admin') })).toHaveAttribute(
      'href',
      '/admin',
    )
  })

  it('hides the admin link from a user', async () => {
    mockApi()
    renderSidebar()
    await screen.findByRole('button', { name: t('account.menu') })
    expect(screen.queryByRole('link', { name: t('nav.admin') })).not.toBeInTheDocument()
  })

  it('masks values in privacy mode', async () => {
    setPrivacy(true)
    mockApi()
    renderSidebar()

    expect(await screen.findByRole('link', { name: /Ana portföy/ })).toHaveTextContent('•••')
    expect(screen.getByRole('link', { name: /Emeklilik/ })).toHaveTextContent('•••')
  })

  it('shows a negative total with its sign', async () => {
    mockApi({ portfolios: [portfolio(1, 'Ana portföy', '-35200')] })
    renderSidebar()

    expect(text(await screen.findByRole('link', { name: /Ana portföy/ }))).toContain('-₺35,2 B')
  })

  it('still navigates when the list fails to load', async () => {
    mockApi({ status: 500 })
    renderSidebar()

    await screen.findByRole('button', { name: t('account.menu') })
    // The loading rows go once the list has failed.
    await waitFor(() => expect(document.querySelector('.animate-pulse')).toBeNull())
    const all = screen.getByRole('link', { name: t('portfolio.all') })
    expect(all).toHaveAttribute('href', '/portfolios/all')
    expect(all).toHaveTextContent(new RegExp(`^${t('portfolio.all')}$`))
    expect(screen.queryByText(/₺/)).not.toBeInTheDocument()
  })

  it('offers to create the first portfolio', async () => {
    mockApi({ portfolios: [] })
    renderSidebar()

    fireEvent.click(await screen.findByRole('button', { name: t('portfolios.emptyCta') }))
    expect(screen.getByRole('dialog', { name: t('portfolios.new') })).toBeInTheDocument()
  })

  // The opener leaves with the empty list, so the new portfolio's
  // page has to be told to take focus, as from the header button.
  it('opens the first portfolio asking its page to take focus', async () => {
    const portfolios: unknown[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      if (String(input) === '/api/portfolios' && init?.method === 'POST') {
        portfolios.push(portfolio(7, 'Birikim', '0'))
        return Response.json({ id: 7, name: 'Birikim' }, { status: 201 })
      }
      if (String(input) === '/api/portfolios') return Response.json(portfolios)
      return Response.json({ id: 1, email: 'ada@example.com', reporting_currency: 'TRY' })
    })
    let location: { pathname: string; state: unknown } | undefined
    function Probe() {
      location = useLocation()
      return null
    }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <Sidebar />
          <Probe />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: t('portfolios.emptyCta') }))
    const dialog = screen.getByRole('dialog', { name: t('portfolios.new') })
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Birikim' } })
    fireEvent.click(within(dialog).getByRole('button', { name: t('portfolios.create') }))
    await waitFor(() => expect(location?.pathname).toBe('/portfolios/7'))
    expect(location?.state).toEqual({ focusAfterCreate: true })
  })

  it('shows the release version under the account row, nothing for dev', async () => {
    mockApi()
    vi.stubGlobal('__APP_VERSION__', 'v1.2.3')
    const { unmount } = renderSidebar()
    expect(await screen.findByText(t('nav.version', { version: 'v1.2.3' }))).toBeInTheDocument()
    unmount()

    vi.stubGlobal('__APP_VERSION__', 'dev')
    renderSidebar()
    await screen.findByRole('link', { name: /Ana portföy/ })
    expect(screen.queryByText(/Sürüm/)).not.toBeInTheDocument()
  })

  it('is a named navigation landmark', async () => {
    mockApi()
    renderSidebar()

    const nav = screen.getByRole('navigation', { name: t('nav.label') })
    expect(await within(nav).findByRole('link', { name: /Ana portföy/ })).toBeInTheDocument()
  })
})
