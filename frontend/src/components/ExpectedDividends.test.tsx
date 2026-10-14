import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ExpectedDividends } from './ExpectedDividends'

// jsdom has no showModal() on <dialog>; this only flips the open state. Nor
// matchMedia: the panel renders the table from md up and the phone list below
// (GLG-1166), so tests get the table unless they clear `desktop`.
let desktop = true
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

beforeEach(() => {
  desktop = true
})

function row(instrumentId: number, symbol: string, gross: string, stopaj: string) {
  return {
    instrument_id: instrumentId,
    symbol,
    market: 'BIST',
    date: '2026-05-04',
    quantity: '100',
    per_share: '1.5',
    gross,
    stopaj,
    net: '127.50',
    currency: 'TRY',
  }
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      {/* The page header's "İşlem ekle", where focus lands once the panel empties. */}
      <button data-add-transaction>İşlem ekle</button>
      <ExpectedDividends portfolioId={1} />
    </QueryClientProvider>,
  )
}

describe('ExpectedDividends', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders nothing when there are no expected dividends', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('[]', { status: 200 }))

    renderPanel()

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(screen.queryByText('Onay bekleyen temettüler')).toBeNull()
  })

  // GLG-939: the one thing on Temettü that asks for an action is open, not a
  // collapsed panel; the header says how many and how much (GLG-568).
  it('is open with the count and net total in its header', async () => {
    const expected = [
      row(7, 'THYAO', '150.00', '22.50'),
      row(9, 'TUPRS', '80.00', '12.00'),
      { ...row(11, 'AAPL', '0.30', '0.06'), market: 'US', net: '0.24', currency: 'USD' },
    ]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(expected), { status: 200 }),
    )

    renderPanel()
    const title = await screen.findByRole('heading', { name: 'Onay bekleyen temettüler' })
    const card = title.closest('section')!
    expect(card.querySelector('details')).toBeNull()
    // Currencies are not added together: there is no FX rate here.
    expect(card).toHaveTextContent('3 · ₺255,00 + $0,24 net')
    expect(screen.getByRole('button', { name: 'THYAO 04.05.2026 temettüsünü ekle' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Tümünü ekle' })).toBeVisible()
  })

  // GLG-1166: one layout per viewport, not both with one hidden by CSS.
  it('renders the phone list instead of the table below md', async () => {
    desktop = false
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify([row(7, 'THYAO', '150.00', '22.50')]), { status: 200 }),
    )

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    expect(screen.queryByRole('table')).toBeNull()
    expect(
      within(screen.getByRole('listitem')).getByRole('button', {
        name: 'THYAO 04.05.2026 temettüsünü ekle',
      }),
    ).toBeVisible()
  })

  // GLG-631: the sum runs past 2^53 units, where a float loses the cent.
  it('shows an exact net total for large amounts', async () => {
    const expected = [
      { ...row(7, 'THYAO', '0', '0'), net: '100000000000000.00' },
      { ...row(9, 'TUPRS', '0', '0'), net: '0.01' },
    ]
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(expected), { status: 200 }),
    )

    renderPanel()
    const title = await screen.findByText('Onay bekleyen temettüler')
    expect(title.closest('section')).toHaveTextContent('2 · ₺100.000.000.000.000,01 net')
  })

  it('posts one dividend transaction per row on add all', async () => {
    const posted: unknown[] = []
    let expected = [row(7, 'THYAO', '150.00', '22.50'), row(9, 'TUPRS', '80.00', '12.00')]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/portfolios/1/dividends/expected') {
        return new Response(JSON.stringify(expected), { status: 200 })
      }
      if (url === '/api/portfolios/1/transactions' && init?.method === 'POST') {
        posted.push(JSON.parse(init.body as string))
        return new Response('{}', { status: 201 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')

    // GLG-351: each row's buttons name the dividend they act on.
    expect(
      screen.getByRole('button', { name: 'THYAO 04.05.2026 temettüsünü ekle' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'TUPRS 04.05.2026 temettüsünü yoksay' }),
    ).toBeInTheDocument()

    expected = []
    const addAll = screen.getByRole('button', { name: 'Tümünü ekle' })
    addAll.focus()
    fireEvent.click(addAll)

    await waitFor(() => expect(screen.queryByText('Onay bekleyen temettüler')).toBeNull())
    // GLG-459: the emptied panel takes the focused button with it.
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'İşlem ekle' })),
    )
    expect(posted).toEqual([
      {
        type: 'dividend',
        date: '2026-05-04',
        instrument_id: 7,
        amount: '150.00',
        fee: '22.50',
        note: '',
      },
      {
        type: 'dividend',
        date: '2026-05-04',
        instrument_id: 9,
        amount: '80.00',
        fee: '12.00',
        note: '',
      },
    ])
  })

  // GLG-1260: isPending reaches the button a tick after the click, so a double
  // click's second click came before pending and booked every row twice. One
  // act holds back the render between the clicks, as that tick does.
  it('posts one request per double click of Tümünü ekle', async () => {
    let resolve = () => {}
    const created = new Promise<void>((r) => (resolve = r))
    const posts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      if (init?.method === 'POST') {
        posts.push(String(input))
        await created
        return new Response('{}', { status: 201 })
      }
      return new Response(JSON.stringify([row(7, 'THYAO', '150.00', '22.50')]), { status: 200 })
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    const addAll = screen.getByRole('button', { name: 'Tümünü ekle' })
    act(() => {
      fireEvent.click(addAll)
      fireEvent.click(addAll)
    })
    await waitFor(() => expect(addAll).toHaveAttribute('aria-disabled', 'true'))
    resolve()
    await waitFor(() => expect(addAll).not.toHaveAttribute('aria-disabled'))
    expect(posts).toEqual(['/api/portfolios/1/transactions'])
  })

  it('names the failed row and the rows left when add all stops', async () => {
    const expected = [
      row(7, 'THYAO', '150.00', '22.50'),
      row(9, 'TUPRS', '80.00', '12.00'),
      row(11, 'ASELS', '40.00', '6.00'),
    ]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/portfolios/1/dividends/expected') {
        return new Response(JSON.stringify(expected), { status: 200 })
      }
      if (url === '/api/portfolios/1/transactions' && init?.method === 'POST') {
        const body = JSON.parse(init.body as string) as { instrument_id: number }
        if (body.instrument_id === 9) {
          return new Response('{"error":"boom"}', { status: 400 })
        }
        return new Response('{}', { status: 201 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    fireEvent.click(screen.getByRole('button', { name: 'Tümünü ekle' }))

    expect(
      await screen.findByText(/^TUPRS 04\.05\.2026 eklenemedi, 2 temettü eklenmeden kaldı\./),
    ).toBeTruthy()
  })

  // GLG-442: the booked suggestion takes its row and its focused add button
  // with it, so focus moves to the next suggestion's add button.
  it('moves focus to the next suggestion after one is added', async () => {
    let expected = [row(7, 'THYAO', '150.00', '22.50'), row(9, 'TUPRS', '80.00', '12.00')]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/portfolios/1/dividends/expected') {
        return new Response(JSON.stringify(expected), { status: 200 })
      }
      if (url === '/api/portfolios/1/transactions' && init?.method === 'POST') {
        expected = expected.slice(1)
        return new Response('{}', { status: 201 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    // A keyboard user: the add button has focus when it is pressed.
    const add = screen.getByRole('button', { name: 'THYAO 04.05.2026 temettüsünü ekle' })
    add.focus()
    fireEvent.click(add)
    // showModal() moves focus into the dialog in a browser.
    const save = within(screen.getByRole('dialog')).getByRole('button', { name: 'Ekle' })
    save.focus()
    fireEvent.click(save)

    await waitFor(() => expect(screen.queryByText('THYAO')).toBeNull())
    expect(document.activeElement).toBe(
      screen.getByRole('button', { name: 'TUPRS 04.05.2026 temettüsünü ekle' }),
    )
  })

  // GLG-994: Chromium blurs a focused button the moment it is disabled, so a
  // running dismiss marks the buttons pending instead; a second click sends
  // nothing.
  it('keeps Yoksay focused and pending while the dismiss runs', async () => {
    const posts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation((input, init) => {
      const url = String(input)
      if (init?.method === 'POST') {
        posts.push(url)
        return new Promise<Response>(() => {})
      }
      return Promise.resolve(
        new Response(JSON.stringify([row(7, 'THYAO', '150.00', '22.50')]), { status: 200 }),
      )
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    const dismiss = screen.getByRole('button', {
      name: 'THYAO 04.05.2026 temettüsünü yoksay',
    })
    dismiss.focus()
    fireEvent.click(dismiss)

    await waitFor(() => expect(dismiss).toHaveAttribute('aria-disabled', 'true'))
    expect(dismiss).toBeEnabled()
    expect(dismiss).toHaveFocus()
    fireEvent.click(dismiss)
    expect(posts).toHaveLength(1)
  })

  // GLG-459: "Yoksay" takes its row and its focused button with it too.
  it('moves focus to the next suggestion after one is dismissed', async () => {
    let expected = [row(7, 'THYAO', '150.00', '22.50'), row(9, 'TUPRS', '80.00', '12.00')]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/portfolios/1/dividends/expected') {
        return new Response(JSON.stringify(expected), { status: 200 })
      }
      if (url === '/api/portfolios/1/dividends/expected/dismiss' && init?.method === 'POST') {
        expected = expected.slice(1)
        return new Response(null, { status: 204 })
      }
      throw new Error(`unexpected request: ${url}`)
    })

    renderPanel()
    await screen.findByText('Onay bekleyen temettüler')
    const dismiss = screen.getByRole('button', {
      name: 'THYAO 04.05.2026 temettüsünü yoksay',
    })
    dismiss.focus()
    fireEvent.click(dismiss)

    await waitFor(() => expect(screen.queryByText('THYAO')).toBeNull())
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByRole('button', { name: 'TUPRS 04.05.2026 temettüsünü ekle' }),
      ),
    )
  })
})
