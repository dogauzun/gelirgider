import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { BrowserRouter, Link, MemoryRouter, Route, Routes, useNavigate } from 'react-router'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { ImportPage } from './ImportPage'

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
  window.matchMedia = (query) =>
    ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

const preview = {
  broker: 'midas',
  sha256: 'abc123',
  period_start: '2025-12-01',
  period_end: '2025-12-31',
  currency: 'USD',
  warnings: [],
  rows: [
    {
      key: 'cash-0',
      status: 'ok',
      type: 'deposit',
      date: '2025-12-12',
      amount: '100',
      fee: '0',
      currency: 'USD',
    },
  ],
  holdings: [],
  reconciliation: [],
}

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/api/portfolios')
      return new Response('[{"id":1,"name":"Emeklilik"},{"id":2,"name":"Birikim"}]')
    if (url === '/api/imports' && method === 'GET') return new Response('[]')
    if (url === '/api/imports/parse') return new Response(JSON.stringify(preview))
    throw new Error(`unexpected request: ${method} ${url}`)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

// A redirect the app makes itself, such as the one after a portfolio delete.
function Redirect() {
  const navigate = useNavigate()
  return (
    <button onClick={() => navigate('/portfolios/1/transactions', { replace: true })}>
      Redirect
    </button>
  )
}

// The page and links out of it, which stand in for the sidebar.
function routes() {
  return (
    <>
      <Link to="/portfolios/1/transactions">Sidebar</Link>
      <Link to="/portfolios/2/import">Other portfolio</Link>
      <Redirect />
      <Routes>
        <Route path="/portfolios/:id/import" element={<ImportPage />} />
        <Route path="/portfolios/:id/transactions" element={<p>işlemler sayfası</p>} />
      </Routes>
    </>
  )
}

function renderWith(router: ReactNode) {
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      {router}
    </QueryClientProvider>,
  )
}

// Opens a preview and unchecks its row, a change Vazgeç asks about.
async function editPreview() {
  const files = [new File(['%PDF-1.4'], 'ekstre.pdf', { type: 'application/pdf' })]
  fireEvent.change(document.querySelector('input[type="file"]')!, { target: { files } })
  await screen.findAllByRole('table')
  fireEvent.click(within(screen.getAllByRole('table')[0]).getAllByRole('checkbox')[1])
}

function leaveDialog() {
  return screen.findByRole('dialog', { name: 'İçe aktarmadan vazgeçilsin mi?' })
}

function beforeUnloadPrevented() {
  const event = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(event)
  return event.defaultPrevented
}

// A link out of a changed review asks first, as Vazgeç does.
it('asks before a link leaves a changed review', async () => {
  renderWith(<MemoryRouter initialEntries={['/portfolios/1/import']}>{routes()}</MemoryRouter>)
  await editPreview()

  fireEvent.click(screen.getByRole('link', { name: 'Sidebar' }))
  const ask = await leaveDialog()
  fireEvent.click(within(ask).getByRole('button', { name: 'Vazgeç' }))
  await waitFor(() => expect(ask).not.toHaveAttribute('open'))
  expect(screen.getAllByRole('table').length).toBeGreaterThan(0)
  expect(screen.queryByText('işlemler sayfası')).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('link', { name: 'Sidebar' }))
  fireEvent.click(await screen.findByRole('button', { name: 'İçe aktarmayı bırak' }))
  expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
})

// A reload or a tab close gets the browser's own prompt.
it('asks the browser to confirm a reload of a changed review only', async () => {
  renderWith(<MemoryRouter initialEntries={['/portfolios/1/import']}>{routes()}</MemoryRouter>)
  expect(beforeUnloadPrevented()).toBe(false)
  await editPreview()
  expect(beforeUnloadPrevented()).toBe(true)
})

// Chromium runs window listeners in the order they were added, capture or
// not, where jsdom runs capture ones first. Adds every popstate
// listener from here on as a non-capture one, so jsdom orders them the same.
function popstateInAddOrder() {
  const add = window.addEventListener.bind(window)
  const remove = window.removeEventListener.bind(window)
  const bubble = (type: string, options?: boolean | EventListenerOptions) =>
    type === 'popstate' ? false : options
  vi.spyOn(window, 'addEventListener').mockImplementation((type, listener, options) =>
    add(type, listener!, bubble(type, options)),
  )
  vi.spyOn(window, 'removeEventListener').mockImplementation((type, listener, options) =>
    remove(type, listener!, bubble(type, options)),
  )
}

// Back is a popstate the page sees only after the URL changed: it is undone
// and asked about, Vazgeç keeps the review, and it goes through once the
// user agrees.
it('asks before Back leaves a changed review', async () => {
  popstateInAddOrder()
  window.history.replaceState({ idx: 0 }, '', '/portfolios/1/transactions')
  window.history.pushState({ idx: 1 }, '', '/portfolios/1/import')
  renderWith(<BrowserRouter>{routes()}</BrowserRouter>)
  await editPreview()

  window.history.back()
  const ask = await leaveDialog()
  await waitFor(() => expect(window.location.pathname).toBe('/portfolios/1/import'))
  expect(screen.queryByText('işlemler sayfası')).not.toBeInTheDocument()
  fireEvent.click(within(ask).getByRole('button', { name: 'Vazgeç' }))
  await waitFor(() => expect(ask).not.toHaveAttribute('open'))
  const row = within(screen.getAllByRole('table')[0]).getAllByRole('checkbox')[1]
  expect(row).not.toBeChecked()

  window.history.back()
  await waitFor(() => expect(ask).toHaveAttribute('open'))
  await waitFor(() => expect(window.location.pathname).toBe('/portfolios/1/import'))
  fireEvent.click(within(ask).getByRole('button', { name: 'İçe aktarmayı bırak' }))
  expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
  expect(window.location.pathname).toBe('/portfolios/1/transactions')
})

// Agreeing before the undo of Back has landed still leaves.
it('leaves on Back when the user agrees before the undo lands', async () => {
  popstateInAddOrder()
  window.history.replaceState({ idx: 0 }, '', '/portfolios/1/transactions')
  window.history.pushState({ idx: 1 }, '', '/portfolios/1/import')
  renderWith(<BrowserRouter>{routes()}</BrowserRouter>)
  await editPreview()

  window.history.back()
  const ask = await leaveDialog()
  expect(window.location.pathname).toBe('/portfolios/1/transactions')
  fireEvent.click(within(ask).getByRole('button', { name: 'İçe aktarmayı bırak' }))
  expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
  expect(window.location.pathname).toBe('/portfolios/1/transactions')
})

// A confirmed leave that keeps the page mounted leaves the guard on for the
// next way out. These routes have no PerPortfolio, so another portfolio's
// import page keeps the page mounted here; in the app it remounts.
it('asks again on Back after a confirmed leave that kept the page', async () => {
  window.history.replaceState({ idx: 0 }, '', '/portfolios/1/import')
  renderWith(<BrowserRouter>{routes()}</BrowserRouter>)
  await editPreview()

  fireEvent.click(screen.getByRole('link', { name: 'Other portfolio' }))
  const ask = await leaveDialog()
  fireEvent.click(within(ask).getByRole('button', { name: 'İçe aktarmayı bırak' }))
  await waitFor(() => expect(window.location.pathname).toBe('/portfolios/2/import'))
  await waitFor(() => expect(ask).not.toHaveAttribute('open'))

  window.history.back()
  expect(await leaveDialog()).toHaveAttribute('open')
  await waitFor(() => expect(window.location.pathname).toBe('/portfolios/2/import'))
})

// A replace is the app's own redirect, never the user's way out.
it('lets a replace navigation through without asking', async () => {
  renderWith(<MemoryRouter initialEntries={['/portfolios/1/import']}>{routes()}</MemoryRouter>)
  await editPreview()

  fireEvent.click(screen.getByRole('button', { name: 'Redirect' }))
  expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})
