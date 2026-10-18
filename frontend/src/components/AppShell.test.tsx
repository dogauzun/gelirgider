import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate, useParams } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { AppShell } from './AppShell'

// jsdom implements neither method on <dialog>.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

function Bomb(): never {
  throw new Error('boom')
}

// A page with state of its own, like the chart range on Özet.
function Counter() {
  const { id } = useParams()
  const [count, setCount] = useState(0)
  return (
    <main id="main">
      <p>{`portfolio ${id}`}</p>
      <button type="button" onClick={() => setCount(count + 1)}>{`count ${count}`}</button>
    </main>
  )
}

function portfolio(id: number, name: string) {
  return {
    id,
    name,
    created_at: '2026-01-01T00:00:00Z',
    total_value: '0',
    cash: '0',
    unrealized_pl: null,
    currency: 'TRY',
    missing_rates: [],
  }
}

function mockApi(portfolios: unknown[] = []) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/me') {
      return Response.json({ id: 1, email: 'a@b.co', reporting_currency: 'TRY', is_admin: false })
    }
    if (url === '/api/portfolios') return Response.json(portfolios)
    throw new Error(`unexpected request: ${url}`)
  })
}

// Any page: says where the router is.
function Where() {
  return <main id="main">{`at ${useLocation().pathname}`}</main>
}

function renderShell(path: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          {/* Shaped like App.tsx: the bar reads the matched route. */}
          <Route element={<AppShell />}>
            {['/account', '/watchlist', '/portfolios/:id', '/portfolios/:id/transactions'].map(
              (path) => (
                <Route key={path} path={path} element={<Where />} />
              ),
            )}
            <Route path="*" element={<Where />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const twoPortfolios = [portfolio(1, 'Ana portföy'), portfolio(2, 'Emeklilik')]

function openDrawer() {
  fireEvent.click(screen.getByRole('button', { name: t('nav.openMenu') }))
  return screen.getByRole('dialog')
}

describe('AppShell', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  // The shell sits inside the app's outer boundary, so a page error
  // must not take the navigation down with it.
  it('keeps the navigation when a page fails and clears the error on navigation', async () => {
    // React logs the error to the console; keep test output clean.
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockApi()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route element={<AppShell />}>
              <Route path="/portfolios/1" element={<Bomb />} />
              <Route path="/portfolios/all" element={<main id="main">all portfolios</main>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(t('error.render'))
    expect(document.querySelectorAll('main')).toHaveLength(1)
    const nav = screen.getByRole('navigation', { name: t('nav.label') })

    fireEvent.click(within(nav).getByRole('link', { name: t('portfolio.all') }))

    expect(await screen.findByText('all portfolios')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  // The page boundary resets on a new path without remounting the page: the
  // shell itself does not remount a page whose params change. App's portfolio
  // routes key each page by the portfolio id on top of this.
  it('does not remount the page itself when only the route params change', async () => {
    mockApi([portfolio(1, 'One'), portfolio(2, 'Two')])

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1']}>
          <Routes>
            <Route element={<AppShell />}>
              <Route path="/portfolios/:id" element={<Counter />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'count 0' }))
    fireEvent.click(screen.getByRole('button', { name: 'count 1' }))
    const nav = screen.getByRole('navigation', { name: t('nav.label') })
    fireEvent.click(await within(nav).findByRole('link', { name: /Two/ }))

    expect(await screen.findByText('portfolio 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'count 2' })).toBeInTheDocument()
  })

  // Below lg the sidebar opens as a drawer from the top bar.
  it('opens the drawer from the menu button', () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1')

    const button = screen.getByRole('button', { name: t('nav.openMenu') })
    expect(button).toHaveAttribute('aria-expanded', 'false')
    const dialog = openDrawer()

    expect(within(dialog).getByRole('navigation', { name: t('nav.label') })).toBeInTheDocument()
    expect(button).toHaveAttribute('aria-expanded', 'true')
  })

  it('closes the drawer when a link is followed', async () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1')

    const dialog = openDrawer()
    fireEvent.click(await within(dialog).findByRole('link', { name: /Emeklilik/ }))

    expect(await screen.findByText('at /portfolios/2')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    // Rendered only while open, so focus goes back by hand, to the opener.
    expect(document.activeElement).toBe(screen.getByRole('button', { name: t('nav.openMenu') }))
  })

  it('closes the drawer on cancel', () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1')

    fireEvent(openDrawer(), new Event('cancel'))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: t('nav.openMenu') }))
  })

  // React passes a cancel up through every onCancel in the tree, so Esc in
  // the new-portfolio dialog would reach the drawer around it.
  it('keeps the drawer open when a dialog inside it is cancelled', () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1')

    const drawer = openDrawer()
    fireEvent.click(within(drawer).getByRole('button', { name: t('portfolios.new') }))
    const inner = screen.getByRole('dialog', { name: t('portfolios.new') })
    fireEvent(inner, new Event('cancel'))

    expect(screen.queryByRole('dialog', { name: t('portfolios.new') })).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: t('nav.label') })).toBe(drawer)
  })

  // A new page starts at the top; back, forward and a change of
  // search params or state on the same page leave the scroll alone.
  it('scrolls to the top only on a link to another page', async () => {
    mockApi(twoPortfolios)
    const scrollTo = vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    function Driver() {
      const navigate = useNavigate()
      const here = '/portfolios/1/transactions'
      return (
        <>
          <button onClick={() => navigate(`${here}?type=buy`)}>filter</button>
          <button onClick={() => navigate(here, { replace: true, state: null })}>state</button>
          <button onClick={() => navigate(-1)}>back</button>
        </>
      )
    }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Driver />
          <Routes>
            <Route element={<AppShell />}>
              <Route path="*" element={<Where />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'filter' }))
    fireEvent.click(screen.getByRole('button', { name: 'state' }))
    expect(scrollTo).not.toHaveBeenCalled()

    const nav = screen.getByRole('navigation', { name: t('nav.label') })
    fireEvent.click(await within(nav).findByRole('link', { name: /Emeklilik/ }))
    expect(await screen.findByText('at /portfolios/2/transactions')).toBeInTheDocument()
    expect(scrollTo).toHaveBeenCalledExactlyOnceWith(0, 0)

    fireEvent.click(screen.getByRole('button', { name: 'back' }))
    expect(await screen.findByText('at /portfolios/1/transactions')).toBeInTheDocument()
    expect(scrollTo).toHaveBeenCalledOnce()
  })

  // The bar names the page without a heading of its own: the page owns the h1.
  it('names the open portfolio in the top bar', async () => {
    mockApi(twoPortfolios)
    const bar = () => screen.getByRole('banner')

    let view = renderShell('/portfolios/1/transactions')
    expect(await within(bar()).findByText('Ana portföy')).toBeInTheDocument()
    expect(within(bar()).queryByRole('heading')).not.toBeInTheDocument()
    view.unmount()

    view = renderShell('/portfolios/all')
    expect(within(bar()).getByText(t('portfolio.all'))).toBeInTheDocument()
    view.unmount()

    // An id the loaded list lacks is the not-found page; until the
    // list is in, the app's name stands in.
    view = renderShell('/portfolios/999')
    expect(within(bar()).getByText(t('app.name'))).toBeInTheDocument()
    expect(await within(bar()).findByText(t('notFound.title'))).toBeInTheDocument()
    view.unmount()

    // The other pages by their own title, not the app's name.
    view = renderShell('/account')
    expect(within(bar()).getByText(t('account.heading'))).toBeInTheDocument()
    expect(within(bar()).queryByText(t('app.name'))).not.toBeInTheDocument()
    view.unmount()

    renderShell('/watchlist')
    expect(within(bar()).getByText(t('watchlist.title'))).toBeInTheDocument()
  })

  // The bar names the page the router rendered, and the router
  // ignores case and a trailing slash.
  it('titles the bar the way the router matches', () => {
    mockApi(twoPortfolios)

    for (const path of ['/account/', '/Account']) {
      const view = renderShell(path)
      expect(within(screen.getByRole('banner')).getByText(t('account.heading'))).toBeInTheDocument()
      view.unmount()
    }
  })

  // An unknown sub-path of a portfolio is the not-found page.
  it('titles an unknown portfolio sub-path as not found', async () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1/foo')

    await screen.findByRole('link', { name: /Ana portföy/ })
    expect(within(screen.getByRole('banner')).getByText(t('notFound.title'))).toBeInTheDocument()
  })

  // Jsdom has no layout, so the structure stands in for it. A long
  // portfolio name may only truncate the title; anything else that shrinks
  // spills out of the bar and scrolls the page sideways.
  it('lets only the title shrink in the top bar', async () => {
    mockApi(twoPortfolios)
    renderShell('/portfolios/1')

    const bar = screen.getByRole('banner')
    await within(bar).findByRole('button', { name: t('account.menu') })
    const title = await within(bar).findByText('Ana portföy')
    expect(title).toHaveClass('min-w-0', 'truncate')
    const fixed = [...bar.children].filter((child) => child !== title)
    expect(fixed).toHaveLength(4)
    fixed.forEach((child) => expect(child).toHaveClass('shrink-0'))
  })
})
