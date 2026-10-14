import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { summaryQueryKey } from '../api/summary'
import { formatMarketDate, todayIstanbul } from '../lib/date'
import { Toaster } from '../components/ui/Toast'
import { chooseOption } from '../test/select'
import { clearToasts, flushToasts, toast } from '../lib/toast'
import { TransactionsPage } from './TransactionsPage'

// Passed through unchanged; every row formats its date with it, so its call
// count tells whether the rows were rendered again.
vi.mock('../lib/date', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/date')>()
  return { ...actual, formatMarketDate: vi.fn(actual.formatMarketDate) }
})

// jsdom implements neither method on <dialog>; both flip the open state, and
// close() hands focus back to what had it before showModal(), as browsers do.
let focusBeforeDialog: Element | null = null
let desktop = true
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    focusBeforeDialog = document.activeElement
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
    if (focusBeforeDialog instanceof HTMLElement) focusBeforeDialog.focus()
  }
  // jsdom has no matchMedia. The page renders the table or the phone list by
  // viewport, so tests get the table unless they set `desktop`.
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

// The sub-header lists the user's portfolios, so every mock below
// answers this too.
function portfoliosResponse() {
  return new Response(
    JSON.stringify([
      { id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' },
      { id: 2, name: 'Uzun vade', created_at: '2026-01-02T00:00:00Z' },
    ]),
    { status: 200 },
  )
}

// Reads back what the page put in the URL, since MemoryRouter has no address
// bar to look at.
function SearchProbe() {
  return <output data-testid="search">{useLocation().search}</output>
}

function openAddDialog() {
  fireEvent.click(screen.getByRole('button', { name: 'İşlem ekle' }))
}

describe('TransactionsPage', () => {
  afterEach(async () => {
    // A delete waits behind its undo toast; send it while this
    // test's fetch mock still answers.
    await flushToasts()
    cleanup()
    vi.restoreAllMocks()
    desktop = true
  })

  it('opens the dialog from the page button and submits a buy with its live cash total', async () => {
    let createdBody: Record<string, unknown> | null = null

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      if (url.startsWith('/api/instruments?q=') && method === 'GET') {
        return new Response(
          JSON.stringify([
            { id: 7, symbol: 'THYAO', market: 'BIST', name: 'Türk Hava Yolları', currency: 'TRY' },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/transactions' && method === 'POST') {
        createdBody = JSON.parse(init?.body as string) as Record<string, unknown>
        return new Response(
          JSON.stringify({
            id: 1,
            portfolio_id: 1,
            instrument_id: 7,
            type: 'buy',
            date: createdBody.date,
            quantity: '10',
            price: '100',
            amount: '1000',
            fee: '0',
            currency: 'TRY',
            note: null,
            created_at: '2026-01-01T00:00:00Z',
          }),
          { status: 201 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    // Seeds the summary query so its cache entry exists to be marked
    // invalidated below -- invalidateQueries has nothing to mark otherwise.
    queryClient.setQueryData(summaryQueryKey(1), {
      currency: 'TRY',
      cash: '0',
      cash_by_currency: {},
      total_value: '0',
      unrealized_pl: null,
      realized_pl: null,
      missing_rates: [],
      positions: [],
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Henüz işlem yok.')

    // The form is behind the dialog now: nothing of it is on the page until
    // the button is clicked.
    expect(screen.queryByRole('heading', { name: 'İşlem ekle' })).toBeNull()
    openAddDialog()
    expect(screen.getByRole('heading', { name: 'İşlem ekle' })).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Sembol veya isim ara'), {
      target: { value: 'THY' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /THYAO/ }))

    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '200' } })
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '71,10' } })
    fireEvent.change(screen.getByLabelText('Komisyon'), { target: { value: '14,22' } })

    // 200 x 71,10 + 14,22 leaves the cash account.
    expect(screen.getByText('Nakitten düşülecek')).toBeInTheDocument()
    expect(screen.getByText('−₺14.234,22')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    await vi.waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      type: 'buy',
      instrument_id: 7,
      quantity: '200',
      price: '71.10',
      fee: '14.22',
    })
    expect(createdBody).not.toHaveProperty('currency')

    // A plain submit closes the dialog; Cmd/Ctrl+Enter is what keeps it open.
    await vi.waitFor(() => expect(screen.queryByRole('heading', { name: 'İşlem ekle' })).toBeNull())

    // Creating a transaction must also invalidate the summary query,
    // so switching to the summary tab doesn't paint stale totals from cache.
    expect(queryClient.getQueryState(summaryQueryKey(1))?.isInvalidated).toBe(true)
  })

  it('submits a deposit with the selected currency', async () => {
    let createdBody: Record<string, unknown> | null = null

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      if (url === '/api/portfolios/1/transactions' && method === 'POST') {
        createdBody = JSON.parse(init?.body as string) as Record<string, unknown>
        return new Response(
          JSON.stringify({
            id: 2,
            portfolio_id: 1,
            instrument_id: null,
            instrument_symbol: null,
            instrument_name: null,
            type: 'deposit',
            date: createdBody.date,
            quantity: null,
            price: null,
            amount: '500',
            fee: '0',
            currency: 'USD',
            note: null,
            created_at: '2026-01-01T00:00:00Z',
          }),
          { status: 201 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Henüz işlem yok.')

    openAddDialog()
    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
    await chooseOption(screen.getByLabelText('Para birimi'), 'Amerikan Doları (USD)')

    // Cmd/Ctrl+Enter submits and keeps the dialog open for the next row.
    fireEvent.keyDown(screen.getByLabelText('Tutar'), { key: 'Enter', ctrlKey: true })

    await vi.waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      type: 'deposit',
      amount: '500',
      currency: 'USD',
    })
    expect(createdBody).not.toHaveProperty('instrument_id')

    await vi.waitFor(() => expect(screen.getByLabelText('Tutar')).toHaveValue(''))
    expect(screen.getByRole('heading', { name: 'İşlem ekle' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Yatırma' })).toHaveAttribute('aria-checked', 'true')
  })

  // A dividend takes its currency from the instrument, so the form
  // must not offer a choice the server would reject.
  it('offers no currency select for a dividend and sends no currency', async () => {
    let createdBody: Record<string, unknown> | null = null

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(JSON.stringify([]), { status: 200 })
      }
      if (url.startsWith('/api/instruments?q=') && method === 'GET') {
        return new Response(
          JSON.stringify([
            { id: 7, symbol: 'THYAO', market: 'BIST', name: 'Türk Hava Yolları', currency: 'TRY' },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/transactions' && method === 'POST') {
        createdBody = JSON.parse(init?.body as string) as Record<string, unknown>
        return new Response(
          JSON.stringify({
            id: 3,
            portfolio_id: 1,
            instrument_id: 7,
            instrument_symbol: 'THYAO',
            instrument_name: 'Türk Hava Yolları',
            type: 'dividend',
            date: createdBody.date,
            quantity: null,
            price: null,
            amount: '25',
            fee: '0',
            currency: 'TRY',
            note: null,
            created_at: '2026-01-01T00:00:00Z',
          }),
          { status: 201 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('Henüz işlem yok.')

    openAddDialog()
    fireEvent.click(screen.getByRole('radio', { name: 'Temettü' }))
    expect(screen.queryByRole('combobox', { name: 'Para birimi' })).toBeNull()

    fireEvent.change(screen.getByPlaceholderText('Sembol veya isim ara'), {
      target: { value: 'THY' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /THYAO/ }))

    // The instrument decides the currency, so the field only reports it.
    expect(screen.queryByRole('combobox', { name: 'Para birimi' })).toBeNull()
    expect(screen.getByLabelText('Para birimi')).toHaveValue('TRY · enstrümandan')

    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    await vi.waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({ type: 'dividend', instrument_id: 7, amount: '25' })
    expect(createdBody).not.toHaveProperty('currency')
  })

  it('opens the dialog prefilled when a row is edited and closes it on cancel', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'THYAO',
              instrument_name: 'Türk Hava Yolları',
              type: 'buy',
              date: '2026-01-02',
              quantity: '10',
              price: '100',
              amount: '1000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-02T00:00:00Z',
            },
            {
              id: 2,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-01',
              quantity: null,
              price: null,
              amount: '500',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-01T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const editButtons = await screen.findAllByRole('button', { name: /işlemini düzenle$/ })

    // Each row's icon buttons name the row they act on.
    expect(editButtons[0]).toHaveAccessibleName(
      'THYAO 02.01.2026 Alış 10 × ₺100,00 −₺1.000,00 işlemini düzenle',
    )
    expect(
      screen.getAllByRole('button', {
        name: 'THYAO 02.01.2026 Alış 10 × ₺100,00 −₺1.000,00 işlemini sil',
      }),
    ).toHaveLength(1)

    // Wider than the card at tablet widths, so it must scroll sideways,
    // and its row actions stay visible on devices without hover.
    const card = screen.getByRole('table').parentElement!
    expect(card).toHaveClass('overflow-x-auto')
    // Positioned, so the absolute sr-only header label is clipped by
    // the card instead of widening the page at 768 px.
    expect(card).toHaveClass('relative')
    expect(editButtons[0].parentElement).not.toHaveClass('opacity-0')
    // Focusing a row's button scrolls the card to the right; closing
    // the dialog brings the date column back. jsdom has no layout, so the
    // scroll position is a plain property here.
    Object.defineProperty(card, 'scrollLeft', { value: 127, writable: true })

    fireEvent.click(editButtons[0])

    expect(screen.getByRole('heading', { name: 'İşlemi düzenle' })).toBeInTheDocument()
    expect(screen.getByLabelText('Adet')).toHaveValue('10')
    expect(screen.getByLabelText('Fiyat')).toHaveValue('100')
    expect(editButtons[0].closest('tr')).toHaveClass('bg-paper')
    expect(editButtons[1].closest('tr')).not.toHaveClass('bg-paper')

    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))

    expect(screen.queryByRole('heading', { name: 'İşlemi düzenle' })).toBeNull()
    expect(editButtons[0].closest('tr')).not.toHaveClass('bg-paper')
    expect(card.scrollLeft).toBe(0)

    // A click on any cell of a desktop row edits that row, as a tap
    // does on the phone list.
    fireEvent.click(screen.getByText('01 Oca'))

    expect(screen.getByRole('heading', { name: 'İşlemi düzenle' })).toBeInTheDocument()
    expect(screen.getByLabelText('Tutar')).toHaveValue('500')
    expect(editButtons[1].closest('tr')).toHaveClass('bg-paper')
  })

  // A recurring buy is re-entered by cloning the row, dated today.
  it('clones a row into the add dialog dated today and saves it as a new row', async () => {
    let createdBody: Record<string, unknown> | null = null
    let updated = false

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'THYAO',
              instrument_name: 'Türk Hava Yolları',
              type: 'buy',
              date: '2026-01-02',
              quantity: '10',
              price: '100',
              amount: '1000',
              fee: '1.5',
              currency: 'TRY',
              note: 'aylık',
              import_id: 3,
              created_at: '2026-01-02T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios/1/transactions' && method === 'POST') {
        createdBody = JSON.parse(init?.body as string) as Record<string, unknown>
        return new Response(JSON.stringify({ ...createdBody, id: 2 }), { status: 201 })
      }
      if (url.startsWith('/api/transactions/') && method === 'PUT') {
        updated = true
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'THYAO 02.01.2026 Alış 10 × ₺100,00 −₺1.000,00 işlemini kopyala',
      }),
    )

    const form = screen.getByRole('heading', { name: 'İşlem ekle' }).closest('form')!
    expect(within(form).getByPlaceholderText('Sembol veya isim ara')).toHaveValue(
      'THYAO - Türk Hava Yolları',
    )
    expect(within(form).getByLabelText('Adet')).toHaveValue('10')
    expect(within(form).getByLabelText('Tarih')).toHaveValue(todayIstanbul())

    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    await vi.waitFor(() => expect(createdBody).not.toBeNull())
    expect(updated).toBe(false)
    expect(createdBody).toEqual({
      type: 'buy',
      date: todayIstanbul(),
      instrument_id: 7,
      quantity: '10',
      price: '100',
      fee: '1.5',
      note: 'aylık',
    })
  })

  // CSS used to hide one of the two layouts, but both were built,
  // doubling the DOM on a long list.
  it('renders only the layout that matches the viewport', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') return portfoliosResponse()
      if (url.startsWith('/api/portfolios/1/transactions?')) {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'THYAO',
              instrument_name: 'Türk Hava Yolları',
              type: 'buy',
              date: '2026-01-02',
              quantity: '10',
              price: '100',
              amount: '1000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-02T00:00:00Z',
            },
            {
              id: 2,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-01',
              quantity: null,
              price: null,
              amount: '5000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-01T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      return new Response('[]', { status: 200 })
    })

    function renderPage() {
      render(
        <QueryClientProvider client={new QueryClient()}>
          <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
            <Routes>
              <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
    }

    renderPage()
    await screen.findByRole('table')
    expect(screen.queryAllByRole('button', { name: /^İşlemi düzenle:/ })).toHaveLength(0)
    expect(screen.getAllByRole('button', { name: /işlemini sil$/ })).toHaveLength(2)

    cleanup()
    desktop = false
    renderPage()
    // The phone list's row button is named by a hidden prefix, which
    // leaves the row's own content in the accessible name. jsdom applies no
    // CSS, so the spans concatenate with no space between them here.
    expect(
      await screen.findByRole('button', { name: /^İşlemi düzenle:.*THYAO/ }),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^İşlemi düzenle:Para yatırma/ })).toBeInTheDocument()
    // The row date names its month ("02 Oca"), not tr's numeric 02/01.
    expect(screen.getByText(/^02 Oca · /)).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.getAllByRole('button', { name: /işlemini sil$/ })).toHaveLength(2)
  })

  // The dialog is unmounted, not close()d, so without a restore
  // focus fell to <body> instead of the button that opened it.
  it('returns focus to the add button when the dialog is dismissed with Escape', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') return portfoliosResponse()
      return new Response('[]', { status: 200 })
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const add = await screen.findByRole('button', { name: 'İşlem ekle' })
    add.focus()
    fireEvent.click(add)
    const dialog = screen.getByRole('dialog')
    // What a browser's showModal() does and the stub does not.
    within(dialog).getAllByRole('button')[0].focus()

    // Escape on a modal <dialog> fires `cancel`; jsdom does not map the key.
    fireEvent(dialog, new Event('cancel', { cancelable: true }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(add)
  })

  // Opened by another page's "İşlem ekle" through navigation state,
  // the button that had focus is gone by the time the dialog closes.
  it('focuses the add button when a dialog opened by navigation is dismissed', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/portfolios') return portfoliosResponse()
      return new Response('[]', { status: 200 })
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter
          initialEntries={[{ pathname: '/portfolios/1/transactions', state: { add: true } }]}
        >
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const add = await screen.findByRole('button', { name: 'İşlem ekle' })
    const dialog = screen.getByRole('dialog')
    within(dialog).getAllByRole('button')[0].focus()

    fireEvent(dialog, new Event('cancel', { cancelable: true }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(document.activeElement).toBe(add)
  })

  // The list is paged, so the page asks for one page at a time and
  // follows the X-Next-Cursor header when the user clicks "Daha fazla yükle".
  it('loads the next page from the cursor when asked for more', async () => {
    const requested: string[] = []
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        requested.push(url)
        const before = new URL(url, 'http://localhost').searchParams.get('before')
        if (before === null) {
          // A full page, so the server hands out a cursor to continue from.
          return new Response(
            JSON.stringify([
              {
                id: 2,
                portfolio_id: 1,
                instrument_id: null,
                instrument_symbol: null,
                instrument_name: null,
                type: 'deposit',
                date: '2026-01-02',
                quantity: null,
                price: null,
                amount: '100',
                fee: '0',
                currency: 'TRY',
                note: null,
                created_at: '2026-01-02T00:00:00Z',
              },
              {
                id: 1,
                portfolio_id: 1,
                instrument_id: null,
                instrument_symbol: null,
                instrument_name: null,
                type: 'deposit',
                date: '2026-01-02',
                quantity: null,
                price: null,
                amount: '100',
                fee: '0',
                currency: 'TRY',
                note: null,
                created_at: '2026-01-02T00:00:00Z',
              },
            ]),
            { status: 200, headers: { 'X-Next-Cursor': '2026-01-02,1' } },
          )
        }
        if (before === '2026-01-02,1') {
          await held
          // Last page: no cursor, so no button after this.
          return new Response(
            JSON.stringify([
              {
                id: 3,
                portfolio_id: 1,
                instrument_id: null,
                instrument_symbol: null,
                instrument_name: null,
                type: 'deposit',
                date: '2026-01-01',
                quantity: null,
                price: null,
                amount: '100',
                fee: '0',
                currency: 'TRY',
                note: null,
                created_at: '2026-01-01T00:00:00Z',
              },
            ]),
            { status: 200 },
          )
        }
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findAllByRole('button', { name: /işlemini düzenle$/ })).toHaveLength(2)
    expect(requested[0]).toContain('limit=')
    // More pages exist, so the count says it is partial.
    expect(screen.getByText('2+ işlem')).toBeInTheDocument()

    // Chromium blurs a focused button the moment it is disabled, so
    // the button is pending while the page loads; a second click asks for
    // nothing.
    const more = screen.getByRole('button', { name: 'Daha fazla yükle' })
    more.focus()
    fireEvent.click(more)
    await vi.waitFor(() => expect(more).toHaveAttribute('aria-disabled', 'true'))
    expect(more).toBeEnabled()
    expect(more).toHaveFocus()
    fireEvent.click(more)
    expect(requested).toHaveLength(2)
    release()

    // The second page is appended, not swapped in, and the button goes away
    // once the server stops sending a cursor.
    await vi.waitFor(() =>
      expect(screen.getAllByRole('button', { name: /işlemini düzenle$/ })).toHaveLength(3),
    )
    expect(screen.queryByRole('button', { name: 'Daha fazla yükle' })).toBeNull()
    expect(screen.getByText('3 işlem')).toBeInTheDocument()
  })

  // The amount carries the cash-flow sign (ADR 0005) and the rows are
  // broken by month, both presentation only -- the API amount stays unsigned.
  it('signs amounts by cash flow and starts a group row at each new month', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'THYAO',
              instrument_name: 'Türk Hava Yolları',
              type: 'buy',
              date: '2026-02-10',
              quantity: '200',
              price: '71.10',
              amount: '14220',
              fee: '14.22',
              currency: 'TRY',
              note: null,
              created_at: '2026-02-10T00:00:00Z',
            },
            {
              id: 2,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-05',
              quantity: null,
              price: null,
              amount: '250000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-05T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = await screen.findByRole('table')
    const rows = within(table).getAllByRole('row')

    // Header, then a group row before the rows of each month.
    expect(rows[1]).toHaveTextContent('Şubat 2026')
    expect(rows[2]).toHaveTextContent('−₺14.220,00')
    expect(rows[3]).toHaveTextContent('Ocak 2026')
    expect(rows[4]).toHaveTextContent('+₺250.000,00')

    expect(screen.getByText('2 işlem')).toBeInTheDocument()
    // The expected dividends panel moved to the Temettü tab.
    expect(screen.queryByText('Onay bekleyen temettüler')).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.map(([input]) => String(input))).not.toContain(
      '/api/portfolios/1/dividends/expected',
    )
  })

  // Window.confirm is gone; the delete goes through the shared
  // ConfirmDialog, which names what it deletes on the button.
  it('deletes a row only after the confirm dialog is accepted', async () => {
    let deleted = false

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify(
            deleted
              ? []
              : [
                  {
                    id: 1,
                    portfolio_id: 1,
                    instrument_id: null,
                    instrument_symbol: null,
                    instrument_name: null,
                    type: 'deposit',
                    date: '2026-01-05',
                    quantity: null,
                    price: null,
                    amount: '500',
                    fee: '0',
                    currency: 'TRY',
                    note: null,
                    created_at: '2026-01-05T00:00:00Z',
                  },
                ],
          ),
          { status: 200 },
        )
      }
      if (url === '/api/transactions/1' && method === 'DELETE') {
        deleted = true
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click((await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0])

    // The delete button sits inside a clickable row, but asks only
    // for the confirm, never opens the edit dialog.
    expect(screen.queryByRole('heading', { name: 'İşlemi düzenle' })).toBeNull()

    // Dismissing the dialog leaves the row alone.
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    await vi.waitFor(() => expect(screen.queryByRole('button', { name: 'İşlemi sil' })).toBeNull())
    expect(deleted).toBe(false)

    fireEvent.click(screen.getAllByRole('button', { name: /işlemini sil$/ })[0])
    fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))

    // The row goes at once; the request waits for the undo toast.
    await screen.findByText('Henüz işlem yok.')
    expect(deleted).toBe(false)
    void flushToasts()
    await vi.waitFor(() => expect(deleted).toBe(true))
  })

  // The delete is sent when its toast goes, which may be after the
  // page was left, so the failure is a toast (in the Toaster's live region)
  // rather than a notice on the page.
  it('reports a delete that fails after the page was left', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-05',
              quantity: null,
              price: null,
              amount: '500',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-05T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/transactions/1' && method === 'DELETE') {
        return new Response(JSON.stringify({ error: 'internal error' }), { status: 500 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    // The Toaster lives in the app shell, outside the page, so it stays when
    // the user leaves İşlemler before the delete is sent.
    const queryClient = new QueryClient()
    const page = () => (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
    render(<Toaster />)
    const { unmount } = render(page())

    fireEvent.click((await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0])
    fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))
    await screen.findByText('Henüz işlem yok.')

    unmount()
    // Newer toasts push the undo toast out, which sends the delete. Not
    // flushToasts: it also clears the error toast, as logout must.
    act(() => ['a', 'b', 'c'].forEach((message) => toast(message)))

    expect(
      await screen.findByText('İşlem silinemedi. Sunucu hatası. Lütfen daha sonra tekrar dene.'),
    ).toBeInTheDocument()
    // The row the failed delete hid is back when the page is opened again.
    render(page())
    expect((await screen.findAllByRole('button', { name: /işlemini sil$/ })).length).toBe(1)
  })

  // The deleted row takes its focused delete button with it, so
  // focus moves to the next row, and with no row left to the add button.
  it('moves focus to the next row, then to the add button, after a delete', async () => {
    const deposit = (id: number, date: string) => ({
      id,
      portfolio_id: 1,
      instrument_id: null,
      instrument_symbol: null,
      instrument_name: null,
      type: 'deposit',
      date,
      quantity: null,
      price: null,
      amount: '500',
      fee: '0',
      currency: 'TRY',
      note: null,
      created_at: `${date}T00:00:00Z`,
    })
    let rows = [deposit(2, '2026-01-06'), deposit(1, '2026-01-05')]

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(JSON.stringify(rows), { status: 200 })
      }
      if (url.startsWith('/api/transactions/') && method === 'DELETE') {
        const id = Number(url.split('/').pop())
        rows = rows.filter((row) => row.id !== id)
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // A keyboard user: the delete button has focus when it is pressed.
    function deleteRow(name: string) {
      const button = screen.getAllByRole('button', { name })[0]
      button.focus()
      fireEvent.click(button)
      fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))
    }

    await screen.findAllByRole('button', { name: '06.01.2026 Para yatırma +₺500,00 işlemini sil' })
    deleteRow('06.01.2026 Para yatırma +₺500,00 işlemini sil')
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getAllByRole('button', { name: '05.01.2026 Para yatırma +₺500,00 işlemini sil' })[0],
      ),
    )

    deleteRow('05.01.2026 Para yatırma +₺500,00 işlemini sil')
    await vi.waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'İşlem ekle' })),
    )
  })

  // The delete waits behind the toast, so undo brings the row back
  // and no request is sent. Focus moves to undo, then to the row.
  it('restores a deleted row on undo without deleting it', async () => {
    let deleted = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-05',
              quantity: null,
              price: null,
              amount: '500',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-05T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/transactions/1' && method === 'DELETE') {
        deleted = true
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
        <Toaster />
      </QueryClientProvider>,
    )

    // A keyboard user: the delete button has focus when it is pressed.
    const button = (await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0]
    button.focus()
    fireEvent.click(button)
    fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))
    await screen.findByText('Henüz işlem yok.')

    // The undo is one key away, and after it focus is back on the
    // restored row's delete button, not on <body>.
    const undo = screen.getByRole('button', { name: 'Geri al' })
    await vi.waitFor(() => expect(document.activeElement).toBe(undo))
    fireEvent.click(undo)
    expect(screen.queryByText('Henüz işlem yok.')).toBeNull()
    expect(document.activeElement).toBe(screen.getAllByRole('button', { name: /işlemini sil$/ })[0])
    void flushToasts()
    expect(deleted).toBe(false)
  })

  // The pending delete outlives the page, so a row deleted before
  // leaving stays hidden when the page is opened again under its toast.
  it('keeps a pending delete hidden when the page is opened again', async () => {
    let deleted = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify(
            deleted
              ? []
              : [
                  {
                    id: 1,
                    portfolio_id: 1,
                    instrument_id: null,
                    instrument_symbol: null,
                    instrument_name: null,
                    type: 'deposit',
                    date: '2026-01-05',
                    quantity: null,
                    price: null,
                    amount: '500',
                    fee: '0',
                    currency: 'TRY',
                    note: null,
                    created_at: '2026-01-05T00:00:00Z',
                  },
                ],
          ),
          { status: 200 },
        )
      }
      if (url === '/api/transactions/1' && method === 'DELETE') {
        deleted = true
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    const page = () => (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
    const { unmount } = render(page())

    fireEvent.click((await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0])
    fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))
    await screen.findByText('Henüz işlem yok.')

    unmount()
    render(page())
    await screen.findByText('Henüz işlem yok.')
    expect(screen.queryAllByRole('button', { name: /işlemini sil$/ })).toHaveLength(0)
    expect(deleted).toBe(false)

    // Once sent, the row is gone for good: coming back fetches the list.
    await flushToasts()
    expect(deleted).toBe(true)
    cleanup()
    render(page())
    expect(screen.queryAllByRole('button', { name: /işlemini sil$/ })).toHaveLength(0)
    await screen.findByText('Henüz işlem yok.')
  })

  // A session that ends under the undo toast drops the delete
  // unsent, so the row comes back rather than staying hidden.
  it('shows a pending delete again when its toast is dropped unsent', async () => {
    let deleted = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-01-05',
              quantity: null,
              price: null,
              amount: '500',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-05T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/transactions/1' && method === 'DELETE') {
        deleted = true
        return new Response(null, { status: 204 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const page = () => (
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
    const { unmount } = render(page())

    fireEvent.click((await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0])
    fireEvent.click(screen.getByRole('button', { name: 'İşlemi sil' }))
    await screen.findByText('Henüz işlem yok.')

    unmount()
    act(() => clearToasts())
    render(page())
    expect(await screen.findAllByRole('button', { name: /işlemini sil$/ })).toHaveLength(1)
    await flushToasts()
    expect(deleted).toBe(false)
  })

  // The dialog hides the list, so it has to say which row goes.
  it('names the row in the delete confirm dialog', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'TUPRS',
              instrument_name: 'Tüpraş',
              type: 'buy',
              date: '2026-01-05',
              quantity: '20',
              price: '200',
              amount: '4000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-05T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.click((await screen.findAllByRole('button', { name: /işlemini sil$/ }))[0])

    expect(
      screen.getByText('Silinecek işlem: Alış · 05.01.2026 · TUPRS · −₺4.000,00'),
    ).toBeInTheDocument()
  })

  // Paging is server-side, so the filter has to reach the server.
  // A transaction that came from a statement import says so, and
  // one entered by hand does not.
  it('marks only the imported rows with the imported pill', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-02-10',
              quantity: null,
              price: null,
              amount: '1000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-02-10T00:00:00Z',
              import_id: 4,
            },
            {
              id: 2,
              portfolio_id: 1,
              instrument_id: null,
              instrument_symbol: null,
              instrument_name: null,
              type: 'deposit',
              date: '2026-02-09',
              quantity: null,
              price: null,
              amount: '2000',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-02-09T00:00:00Z',
              import_id: null,
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const table = within(await screen.findByRole('table'))
    const rows = table.getAllByRole('row')
    // Header, month group row, then the two transactions.
    expect(rows[2]).toHaveTextContent('içe aktarıldı')
    expect(rows[3]).not.toHaveTextContent('içe aktarıldı')
  })

  it('sends the selected type to the server and keeps it in the URL', async () => {
    const requested: string[] = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        requested.push(url)
        const types = new URL(url, 'http://localhost').searchParams.get('type')
        // Only the unfiltered list has a row; the buy filter empties it.
        const rows =
          types === null
            ? [
                {
                  id: 1,
                  portfolio_id: 1,
                  instrument_id: null,
                  instrument_symbol: null,
                  instrument_name: null,
                  type: 'deposit',
                  date: '2026-01-02',
                  quantity: null,
                  price: null,
                  amount: '100',
                  fee: '0',
                  currency: 'TRY',
                  note: null,
                  created_at: '2026-01-02T00:00:00Z',
                },
              ]
            : []
        return new Response(JSON.stringify(rows), { status: 200 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route
              path="/portfolios/:id/transactions"
              element={
                <>
                  <TransactionsPage />
                  <SearchProbe />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('button', { name: /işlemini düzenle$/ })
    expect(requested[0]).not.toContain('type=')

    const filters = screen.getByRole('radiogroup', { name: 'Türe göre filtrele' })
    fireEvent.click(within(filters).getByRole('radio', { name: 'Alış' }))

    await vi.waitFor(() => expect(requested.at(-1)).toContain('type=buy'))
    expect(within(filters).getByRole('radio', { name: 'Alış' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    // In the URL, so a reload or the back button lands on the same filter.
    expect(screen.getByTestId('search').textContent).toBe('?type=buy')
    // An empty filtered list says so, and keeps the control on screen so the
    // filter can be cleared again.
    await screen.findByText('Bu türde işlem yok.')
    expect(screen.getByRole('radio', { name: 'Tümü' })).toBeTruthy()

    fireEvent.click(screen.getByRole('radio', { name: 'Nakit' }))
    await vi.waitFor(() =>
      expect(requested.at(-1)).toContain(`type=${encodeURIComponent('deposit,withdrawal,fee')}`),
    )

    // The arrow keys move and select, wrapping from the last option
    // to the first.
    const cash = screen.getByRole('radio', { name: 'Nakit' })
    expect(cash).toHaveAttribute('tabindex', '0')
    cash.focus()
    fireEvent.keyDown(cash, { key: 'ArrowRight' })
    const all = screen.getByRole('radio', { name: 'Tümü' })
    await vi.waitFor(() => expect(all).toHaveAttribute('aria-checked', 'true'))
    expect(all).toHaveFocus()
    expect(cash).toHaveAttribute('tabindex', '-1')
    expect(screen.getByTestId('search').textContent).toBe('')
  })

  it('filters by the picked instrument on the server and clears it again', async () => {
    const requested: string[] = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        requested.push(url)
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: 7,
              instrument_symbol: 'THYAO',
              instrument_name: 'Türk Hava Yolları',
              type: 'buy',
              date: '2026-01-02',
              quantity: '1',
              price: '100',
              amount: '100',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-01-02T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url.startsWith('/api/instruments?q=') && method === 'GET') {
        return new Response(
          JSON.stringify([
            { id: 7, symbol: 'THYAO', market: 'BIST', name: 'Türk Hava Yolları', currency: 'TRY' },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route
              path="/portfolios/:id/transactions"
              element={
                <>
                  <TransactionsPage />
                  <SearchProbe />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByRole('button', { name: /işlemini düzenle$/ })
    expect(requested[0]).not.toContain('instrument_id=')

    fireEvent.change(screen.getByRole('combobox', { name: 'Enstrümana göre filtrele' }), {
      target: { value: 'THY' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /THYAO/ }))

    await vi.waitFor(() => expect(requested.at(-1)).toContain('instrument_id=7'))
    expect(screen.getByTestId('search').textContent).toBe('?instrument=7&symbol=THYAO')

    const chip = await screen.findByRole('button', { name: 'Enstrüman filtresini kaldır: THYAO' })
    // The search unmounts; focus lands on the chip that replaces it.
    expect(document.activeElement).toBe(chip)
    fireEvent.click(chip)
    await vi.waitFor(() => expect(requested.at(-1)).not.toContain('instrument_id='))
    expect(screen.getByTestId('search').textContent).toBe('')
    // The chip unmounts; focus lands on the search that replaces it.
    expect(document.activeElement).toBe(
      screen.getByRole('combobox', { name: 'Enstrümana göre filtrele' }),
    )
  })

  it('does not render the rows again while the instrument filter is typed in', async () => {
    const deposit = (id: number, date: string) => ({
      id,
      portfolio_id: 1,
      instrument_id: null,
      instrument_symbol: null,
      instrument_name: null,
      type: 'deposit',
      date,
      quantity: null,
      price: null,
      amount: '500',
      fee: '0',
      currency: 'TRY',
      note: null,
      created_at: `${date}T00:00:00Z`,
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response(JSON.stringify([deposit(2, '2026-01-06'), deposit(1, '2026-01-05')]), {
          status: 200,
        })
      }
      if (url.startsWith('/api/instruments?q=') && method === 'GET') {
        return new Response('[]', { status: 200 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findAllByRole('button', { name: /işlemini düzenle$/ })
    const combobox = screen.getByRole('combobox', { name: 'Enstrümana göre filtrele' })
    vi.mocked(formatMarketDate).mockClear()
    fireEvent.change(combobox, { target: { value: 'T' } })
    fireEvent.change(combobox, { target: { value: 'TH' } })
    fireEvent.change(combobox, { target: { value: 'THY' } })

    expect(combobox).toHaveProperty('value', 'THY')
    expect(formatMarketDate).not.toHaveBeenCalled()
  })

  it('sends the date range and type to the list and the export, and clears the range again', async () => {
    const requested: string[] = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        requested.push(url)
        return new Response(
          JSON.stringify([
            {
              id: 1,
              portfolio_id: 1,
              instrument_id: null,
              type: 'deposit',
              date: '2026-02-02',
              quantity: null,
              price: null,
              amount: '100',
              fee: '0',
              currency: 'TRY',
              note: null,
              created_at: '2026-02-02T00:00:00Z',
            },
          ]),
          { status: 200 },
        )
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/portfolios/1/transactions']}>
          <Routes>
            <Route
              path="/portfolios/:id/transactions"
              element={
                <>
                  <TransactionsPage />
                  <SearchProbe />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const exportLink = () => screen.getByRole('link', { name: 'CSV indir' }).getAttribute('href')
    fireEvent.click(await screen.findByRole('button', { name: 'Tarih: Tümü' }))
    const from = await screen.findByLabelText('Başlangıç')
    const to = screen.getByLabelText('Bitiş')
    expect(requested[0]).not.toContain('from=')
    expect(screen.queryByRole('button', { name: 'Tarih filtresini kaldır' })).toBeNull()

    fireEvent.change(from, { target: { value: '2026-01-01' } })
    fireEvent.change(to, { target: { value: '2026-03-31' } })
    await vi.waitFor(() => expect(requested.at(-1)).toContain('from=2026-01-01&to=2026-03-31'))
    expect(screen.getByTestId('search').textContent).toBe('?from=2026-01-01&to=2026-03-31')
    expect(exportLink()).toBe('/api/portfolios/1/transactions.csv?from=2026-01-01&to=2026-03-31')

    // An end before the start is an error, never a request.
    const count = requested.length
    fireEvent.change(to, { target: { value: '2025-12-31' } })
    expect(screen.getByRole('alert').textContent).toBe('Bitiş tarihi başlangıçtan önce olamaz.')
    expect(requested).toHaveLength(count)
    expect(exportLink()).toBe('/api/portfolios/1/transactions.csv?from=2026-01-01&to=2026-03-31')

    const clear = screen.getByRole('button', { name: 'Tarih filtresini kaldır' })
    clear.focus()
    fireEvent.click(clear)
    await vi.waitFor(() => expect(requested.at(-1)).not.toContain('from='))
    // The clear button unmounts; focus lands on the start date.
    expect(screen.queryByRole('button', { name: 'Tarih filtresini kaldır' })).toBeNull()
    expect(document.activeElement).toBe(from)
    expect(requested.at(-1)).not.toContain('to=')
    expect(screen.getByTestId('search').textContent).toBe('')
    expect(exportLink()).toBe('/api/portfolios/1/transactions.csv')

    // The type filter goes along too.
    const filters = screen.getByRole('radiogroup', { name: 'Türe göre filtrele' })
    fireEvent.click(within(filters).getByRole('radio', { name: 'Alış' }))
    expect(exportLink()).toBe('/api/portfolios/1/transactions.csv?type=buy')
  })

  it('names the picked instrument when its filtered list is empty', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        return new Response('[]', { status: 200 })
      }
      if (url === '/api/portfolios' && method === 'GET') {
        return portfoliosResponse()
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter
          initialEntries={['/portfolios/1/transactions?type=dividend&instrument=245&symbol=GARAN']}
        >
          <Routes>
            <Route path="/portfolios/:id/transactions" element={<TransactionsPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    // No row carries the symbol, so it comes from the URL, not the id.
    await screen.findByText('GARAN için bu türde işlem yok.')
    expect(screen.getByRole('button', { name: 'Enstrüman filtresini kaldır: GARAN' })).toBeTruthy()
  })

  // Core pages spec, İşlemler: the rows, the imports and the mock
  // the tests below share.
  const buyRow = {
    id: 1,
    portfolio_id: 1,
    instrument_id: 7,
    instrument_symbol: 'THYAO',
    instrument_name: 'Türk Hava Yolları',
    type: 'buy',
    date: '2026-06-18',
    quantity: '200',
    price: '71.10',
    amount: '14234.22',
    fee: '14.22',
    currency: 'TRY',
    note: 'aylık alım',
    created_at: '2026-06-18T00:00:00Z',
    import_id: 4,
  }
  const depositRow = {
    id: 2,
    portfolio_id: 1,
    instrument_id: null,
    instrument_symbol: null,
    instrument_name: null,
    type: 'deposit',
    date: '2026-06-12',
    quantity: null,
    price: null,
    amount: '3000',
    fee: '0',
    currency: 'TRY',
    note: null,
    created_at: '2026-06-12T00:00:00Z',
    import_id: null,
  }
  const imports = [
    {
      id: 4,
      broker: 'midas',
      portfolio_id: 1,
      period_start: '2026-06-01',
      period_end: '2026-06-30',
      currency: 'TRY',
      transaction_count: 3,
      created_at: '2026-07-01T00:00:00Z',
    },
    // Another portfolio's import is not offered here.
    {
      id: 5,
      broker: 'garanti',
      portfolio_id: 2,
      period_start: '2026-05-01',
      period_end: '2026-05-31',
      currency: 'TRY',
      transaction_count: 9,
      created_at: '2026-06-01T00:00:00Z',
    },
  ]

  function mockList(requested: string[] = [], rows: unknown[] = [buyRow, depositRow]) {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.startsWith('/api/portfolios/1/transactions?') && method === 'GET') {
        requested.push(url)
        return new Response(JSON.stringify(rows), { status: 200 })
      }
      if (url === '/api/imports' && method === 'GET') {
        return new Response(JSON.stringify(imports), { status: 200 })
      }
      if (url === '/api/portfolios' && method === 'GET') return portfoliosResponse()
      throw new Error(`unexpected request: ${method} ${url}`)
    })
  }

  function renderAt(entry: string | { pathname: string; search: string; state: unknown }) {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route
              path="/portfolios/:id/transactions"
              element={
                <>
                  <TransactionsPage />
                  <SearchProbe />
                </>
              }
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  }

  it('keeps the filters, the count and the export in one toolbar row, in order', async () => {
    mockList()
    renderAt('/portfolios/1/transactions')

    const csv = await screen.findByRole('link', { name: 'CSV indir' })
    const toolbar = csv.parentElement as HTMLElement
    expect(toolbar).toHaveClass('flex')
    const parts = [
      screen.getByRole('combobox', { name: 'Enstrümana göre filtrele' }),
      screen.getByRole('radiogroup', { name: 'Türe göre filtrele' }),
      screen.getByRole('button', { name: 'Tarih: Tümü' }),
      screen.getByRole('combobox', { name: 'Kaynak' }),
      screen.getByText('2 işlem'),
      csv,
    ]
    for (const [i, part] of parts.entries()) {
      expect(toolbar.contains(part)).toBe(true)
      if (i > 0) {
        expect(parts[i - 1].compareDocumentPosition(part) & Node.DOCUMENT_POSITION_FOLLOWING).toBe(
          Node.DOCUMENT_POSITION_FOLLOWING,
        )
      }
    }
  })

  it('shows the spec columns, shared cells, the source and always-visible actions', async () => {
    mockList()
    renderAt('/portfolios/1/transactions')

    const table = await screen.findByRole('table')
    await screen.findByText('Midas · Haz 2026')
    const [header, group, buy, deposit] = within(table).getAllByRole('row')
    expect(
      within(header)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual([
      'Tarih',
      'Tür',
      'Enstrüman',
      'Adet × fiyat',
      'Kesinti',
      'Tutar',
      'Kaynak',
      'Eylemler',
    ])
    expect(group).toHaveTextContent('Haziran 2026')

    const buyCells = within(buy).getAllByRole('cell')
    expect(buyCells.map((td) => td.textContent)).toEqual([
      '18 Haz',
      'Alış',
      'THYAOTürk Hava Yollarıaylık alım',
      '200 × ₺71,10',
      '₺14,22',
      '−₺14.234,22',
      'Midas · Haz 2026',
      '',
    ])
    // The note follows the name in grey, the full text on hover.
    const note = within(buyCells[2]).getByText('aylık alım')
    expect(note).toHaveAttribute('title', 'aylık alım')
    expect(note).toHaveClass('text-muted')

    // No commission and no withholding: Kesinti is empty, not a dash; a
    // row entered by hand says so.
    const depositCells = within(deposit).getAllByRole('cell')
    expect(depositCells[3].textContent).toBe('')
    expect(depositCells[4].textContent).toBe('')
    expect(depositCells[6].textContent).toBe('Elle')

    // The three actions are in every row and never wait for a hover.
    for (const row of [buy, deposit]) {
      const actions = within(row).getAllByRole('button')
      expect(actions).toHaveLength(3)
      expect(actions[0].parentElement?.className).not.toMatch(/opacity-0/)
    }
  })

  it('filters by one import or by the rows entered by hand, in the URL, list and export', async () => {
    const requested: string[] = []
    mockList(requested)
    renderAt('/portfolios/1/transactions')

    const source = await screen.findByRole('combobox', { name: 'Kaynak' })
    fireEvent.click(source)
    const listbox = await screen.findByRole('listbox')
    expect(
      within(listbox)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Tümü', 'Elle', 'Midas · Haz 2026'])
    fireEvent.keyDown(listbox, { key: 'Escape' })

    await chooseOption(source, 'Midas · Haz 2026')
    await vi.waitFor(() => expect(requested.at(-1)).toContain('import_id=4'))
    expect(screen.getByTestId('search').textContent).toBe('?import=4')
    expect(screen.getByRole('link', { name: 'CSV indir' })).toHaveAttribute(
      'href',
      '/api/portfolios/1/transactions.csv?import_id=4',
    )

    await chooseOption(source, 'Elle')
    await vi.waitFor(() => expect(requested.at(-1)).toContain('import_id=none'))
    expect(screen.getByTestId('search').textContent).toBe('?import=none')

    await chooseOption(source, 'Tümü')
    await vi.waitFor(() => expect(requested.at(-1)).not.toContain('import_id'))
    expect(screen.getByTestId('search').textContent).toBe('')
  })

  it('names a new import in a banner that keeps or clears its filter', async () => {
    const requested: string[] = []
    mockList(requested)
    const arrival = {
      pathname: '/portfolios/1/transactions',
      search: '?import=4',
      state: { imported: 4 },
    }
    renderAt(arrival)

    const banner = (await screen.findByText(/içe aktarıldı · 3 işlem/)).closest(
      '[role="status"]',
    ) as HTMLElement
    expect(banner.textContent).toContain('Midas · Haz 2026 içe aktarıldı · 3 işlem')
    // The banner names the import as Kaynak does, short month.
    expect(screen.getByRole('combobox', { name: 'Kaynak' }).textContent).toContain(
      'Midas · Haz 2026',
    )
    expect(requested[0]).toContain('import_id=4')
    fireEvent.click(within(banner).getByRole('button', { name: 'Sadece bunları göster' }))
    expect(screen.queryByText(/içe aktarıldı · 3 işlem/)).toBeNull()
    expect(screen.getByTestId('search').textContent).toBe('?import=4')

    cleanup()
    renderAt(arrival)
    const again = (await screen.findByText(/içe aktarıldı · 3 işlem/)).closest(
      '[role="status"]',
    ) as HTMLElement
    fireEvent.click(within(again).getByRole('button', { name: 'Bildirimi kapat' }))
    expect(screen.queryByText(/içe aktarıldı · 3 işlem/)).toBeNull()
    expect(screen.getByTestId('search').textContent).toBe('')
  })

  // The banner's ✕ clears the filter to Tümü; once the user has picked
  // another source the banner no longer names the filter, so it goes and
  // cannot undo that choice.
  it('drops the import banner once Kaynak names another source', async () => {
    mockList()
    renderAt({
      pathname: '/portfolios/1/transactions',
      search: '?import=4',
      state: { imported: 4 },
    })

    await screen.findByText(/içe aktarıldı · 3 işlem/)
    await chooseOption(await screen.findByRole('combobox', { name: 'Kaynak' }), 'Elle')
    expect(screen.getByTestId('search').textContent).toBe('?import=none')
    expect(screen.queryByRole('button', { name: 'Bildirimi kapat' })).toBeNull()
    expect(screen.queryByText(/içe aktarıldı · 3 işlem/)).toBeNull()
  })

  // jsdom has no layout: `desktop = false` makes the page render what it
  // renders below md, the Filtrele button with Kaynak inside the closed sheet.
  it('moves focus to the Filtrele button when the banner closes on a phone', async () => {
    desktop = false
    mockList()
    renderAt({
      pathname: '/portfolios/1/transactions',
      search: '?import=4',
      state: { imported: 4 },
    })

    await screen.findByRole('button', { name: 'Filtrele (1)' })
    const banner = (await screen.findByText(/içe aktarıldı · 3 işlem/)).closest(
      '[role="status"]',
    ) as HTMLElement
    fireEvent.click(within(banner).getByRole('button', { name: 'Bildirimi kapat' }))
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Filtrele' }))
  })

  // The İçe aktar button that committed the import leaves with ImportPage,
  // which dropped focus to <body>: the banner takes it once the
  // imports load. A plain visit or a reloaded ?import= URL leaves focus alone.
  it('moves focus into the banner on arrival from an import commit', async () => {
    mockList()
    renderAt({
      pathname: '/portfolios/1/transactions',
      search: '?import=4',
      state: { imported: 4 },
    })

    const showOnly = await screen.findByRole('button', { name: 'Sadece bunları göster' })
    await vi.waitFor(() => expect(document.activeElement).toBe(showOnly))
  })

  it('leaves focus alone on a plain visit or a reloaded ?import= URL', async () => {
    mockList()
    renderAt('/portfolios/1/transactions')
    await screen.findByRole('combobox', { name: 'Kaynak' })
    await screen.findAllByText('Midas · Haz 2026')
    expect(document.activeElement).toBe(document.body)

    cleanup()
    renderAt('/portfolios/1/transactions?import=4')
    await vi.waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Kaynak' }).textContent).toContain(
        'Midas · Haz 2026',
      ),
    )
    expect(screen.queryByText(/içe aktarıldı/)).toBeNull()
    expect(document.activeElement).toBe(document.body)
  })

  // A nowrap pill wider than its
  // column spilled over the shrink-0 amount on a phone. jsdom has no layout,
  // so this pins the classes that keep them apart: the pill column has no
  // min-w-0, so one pill is its floor, and the row wraps the amount, pushed
  // right, under the pills when both do not fit.
  it('lets the phone row wrap the amount under the pills instead of overlapping them', async () => {
    desktop = false
    mockList([], [{ ...depositRow, amount: '100000' }])
    renderAt('/portfolios/1/transactions')

    const amount = await screen.findByText('+₺100.000,00')
    const amountColumn = amount.parentElement as HTMLElement
    const row = amountColumn.parentElement as HTMLElement
    const pillColumn = screen.getByText('Para yatırma').parentElement?.parentElement as HTMLElement
    expect(pillColumn.parentElement).toBe(row)
    expect(pillColumn).toHaveClass('flex-1')
    expect(pillColumn).not.toHaveClass('min-w-0')
    expect(row).toHaveClass('flex', 'flex-wrap')
    expect(amountColumn).toHaveClass('ml-auto', 'shrink-0')
  })

  // The details line got only the row button's width,
  // the li minus the two 44px icon buttons, so "0,004 × ₺2.100.000,00" was
  // cut at 360px. jsdom has no layout, so this pins the line as a line of
  // the row button, which spans the whole li, with the icon buttons laid
  // over the button's first line and that line keeping their room free.
  it('runs the phone row details line under the clone and delete buttons', async () => {
    desktop = false
    mockList([], [buyRow])
    renderAt('/portfolios/1/transactions')

    const details = await screen.findByText(/200 × ₺71,10/)
    const row = screen.getByRole('button', { name: /^İşlemi düzenle:/ })
    expect(details.parentElement).toBe(row)
    expect(details).toHaveClass('truncate')
    expect(row).toHaveClass('w-full')
    expect(details.previousElementSibling).toHaveClass('pr-26')
    const li = row.parentElement as HTMLElement
    const icons = li.querySelector('[data-delete-tx]')?.parentElement
    expect(icons?.parentElement).toBe(li)
    expect(icons).toHaveClass('absolute')
  })

  it('shows an empty portfolio as a card with one action', async () => {
    mockList([], [])
    renderAt('/portfolios/1/transactions')

    const text = await screen.findByText('Henüz işlem yok.')
    const card = text.closest('section') as HTMLElement
    expect(card).toHaveClass('card')
    expect(within(card).getAllByRole('link')).toHaveLength(1)
    expect(within(card).getByRole('link', { name: 'Ekstre içe aktar' })).toHaveAttribute(
      'href',
      '/portfolios/1/import',
    )
    expect(within(card).queryByRole('button')).toBeNull()
  })

  it('puts the filters behind one Filtrele button with the active count on a phone', async () => {
    desktop = false
    mockList()
    renderAt('/portfolios/1/transactions?type=buy&import=none')

    const open = await screen.findByRole('button', { name: 'Filtrele (2)' })
    expect(screen.queryByRole('radiogroup')).toBeNull()
    fireEvent.click(open)
    const sheet = screen.getByRole('dialog', { name: 'Filtrele' })
    expect(within(sheet).getByRole('radio', { name: 'Alış' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    expect(within(sheet).getByRole('button', { name: 'Tarih: Tümü' })).toBeTruthy()
    fireEvent.click(within(sheet).getByRole('radio', { name: 'Tümü' }))
    expect(screen.getByTestId('search').textContent).toBe('?import=none')
    fireEvent.click(within(sheet).getByRole('button', { name: 'Bitti' }))
    expect(screen.queryByRole('dialog', { name: 'Filtrele' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Filtrele (1)' })).toBeTruthy()
  })
})
