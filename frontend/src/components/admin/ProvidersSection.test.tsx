import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ProvidersSection } from './ProvidersSection'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

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

describe('ProvidersSection', () => {
  afterEach(() => vi.restoreAllMocks())

  it('saves a key, shows its new last4 and translates a failed test', async () => {
    let providers = [
      { name: 'finnhub', configured: false, last4: '' },
      { name: 'tiingo', configured: true, last4: 'wxyz' },
    ]
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/admin/providers') return json(providers)
      if (url === '/api/admin/providers/finnhub' && method === 'PUT') {
        providers = [{ name: 'finnhub', configured: true, last4: '1234' }, providers[1]]
        return new Response(null, { status: 204 })
      }
      if (url === '/api/admin/providers/tiingo/test') {
        return json({ ok: false, error: 'invalid key' })
      }
      return json({ error: 'unexpected' }, 500)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    const finnhubInput = await screen.findByLabelText('Finnhub API anahtarı')
    const finnhub = within(finnhubInput.closest('li')!)
    expect(finnhub.getByText('Tanımlı değil')).toBeInTheDocument()

    fireEvent.change(finnhubInput, { target: { value: 'abcd1234' } })
    fireEvent.click(finnhub.getByRole('button', { name: 'Finnhub anahtarını kaydet' }))

    expect(await finnhub.findByText('Tanımlı ••••1234')).toBeInTheDocument()
    expect(finnhubInput).toHaveValue('')
    const put = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(put?.[1]?.body).toBe(JSON.stringify({ api_key: 'abcd1234' }))

    const tiingo = within(screen.getByLabelText('Tiingo API anahtarı').closest('li')!)
    fireEvent.click(tiingo.getByRole('button', { name: 'Tiingo anahtarını test et' }))
    expect(await tiingo.findByText('Sağlayıcı anahtarı reddetti.')).toBeInTheDocument()
  })

  // The test outcome rendered as a plain paragraph, so a screen
  // reader heard nothing after pressing Test et.
  it('announces the test result politely', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input) === '/api/admin/providers'
        ? json([{ name: 'tiingo', configured: true, last4: '5678' }])
        : json({ ok: false, error: 'unreachable' }),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Tiingo anahtarını test et' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Sağlayıcıya ulaşılamadı.')
  })

  // A disabled button loses focus in Chromium, so the running test
  // request marks the button pending instead and a second click is ignored.
  it('keeps the test button focused and pending while the test runs', async () => {
    let finish: (response: Response) => void = () => {}
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      if (String(input) === '/api/admin/providers') {
        return Promise.resolve(json([{ name: 'tiingo', configured: true, last4: 'wxyz' }]))
      }
      return new Promise<Response>((resolve) => (finish = resolve))
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    const button = await screen.findByRole('button', { name: 'Tiingo anahtarını test et' })
    button.focus()
    fireEvent.click(button)
    await waitFor(() => expect(button).toHaveAttribute('aria-disabled', 'true'))
    expect(button).toBeEnabled()
    expect(button).toHaveFocus()
    fireEvent.click(button)
    const tests = () => fetchMock.mock.calls.filter(([url]) => String(url).endsWith('/test'))
    expect(tests()).toHaveLength(1)

    finish(json({ ok: true }))
    await waitFor(() => expect(button).not.toHaveAttribute('aria-disabled'))
  })

  // isPending reaches the buttons a tick after the click, so a
  // double click's second click came before pending and sent the request twice.
  it('sends one request per double click on save and test', async () => {
    const sent: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      if (String(input) === '/api/admin/providers') {
        return json([{ name: 'tiingo', configured: true, last4: 'wxyz' }])
      }
      sent.push(`${init?.method ?? 'GET'} ${String(input)}`)
      return new Promise<Response>(() => {})
    })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    fireEvent.change(await screen.findByLabelText('Tiingo API anahtarı'), {
      target: { value: 'abcd1234' },
    })
    for (const name of ['Tiingo anahtarını kaydet', 'Tiingo anahtarını test et']) {
      const button = screen.getByRole('button', { name })
      fireEvent.click(button)
      fireEvent.click(button)
      await waitFor(() => expect(button).toHaveAttribute('aria-disabled', 'true'))
    }
    expect(sent).toEqual([
      'PUT /api/admin/providers/tiingo',
      'POST /api/admin/providers/tiingo/test',
    ])
  })

  // A save empties the input, which disables the focused Kaydet,
  // and a clear disables the Temizle its dialog hands focus back to. Chromium
  // then drops focus to <body>; jsdom does not blur on disable, so the test
  // checks focus moved to the row's key input and the button is disabled.
  it('keeps focus in the row after a keyboard save and a confirmed clear', async () => {
    let tiingo = { name: 'tiingo', configured: false, last4: '' }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/admin/providers') return json([tiingo])
      if (url === '/api/admin/providers/tiingo' && init?.method === 'PUT') {
        const key = JSON.parse(String(init.body)).api_key as string
        tiingo = { name: 'tiingo', configured: key !== '', last4: key.slice(-4) }
        return new Response(null, { status: 204 })
      }
      return json({ error: 'unexpected' }, 500)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    const keyInput = await screen.findByLabelText('Tiingo API anahtarı')
    const row = within(keyInput.closest('li')!)
    fireEvent.change(keyInput, { target: { value: 'abcdef5678' } })
    const save = row.getByRole('button', { name: 'Tiingo anahtarını kaydet' })
    save.focus()
    fireEvent.click(save)
    expect(await row.findByText('Tanımlı ••••5678')).toBeInTheDocument()
    expect(save).toBeDisabled()
    expect(keyInput).toHaveFocus()

    const clear = row.getByRole('button', { name: 'Tiingo anahtarını temizle' })
    clear.focus()
    fireEvent.click(clear)
    const dialog = screen.getByRole('dialog', { name: /Tiingo/ })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Tiingo anahtarını sil' }))
    expect(await row.findByText('Tanımlı değil')).toBeInTheDocument()
    expect(clear).toBeDisabled()
    expect(keyInput).toHaveFocus()
  })

  it('shows an unreadable stored key and still lets the admin replace it', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      if (String(input) === '/api/admin/providers') {
        return json([
          { name: 'finnhub', configured: true, last4: '', error: 'undecryptable' },
          { name: 'tiingo', configured: false, last4: '' },
        ])
      }
      return json({ error: 'stored key unreadable' }, 400)
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    const finnhub = within((await screen.findByLabelText('Finnhub API anahtarı')).closest('li')!)
    expect(finnhub.getByText('Kayıtlı anahtar okunamıyor, yeniden gir')).toBeInTheDocument()
    expect(finnhub.getByRole('button', { name: 'Finnhub anahtarını temizle' })).toBeEnabled()
    fireEvent.click(finnhub.getByRole('button', { name: 'Finnhub anahtarını test et' }))
    expect(
      await finnhub.findByText('Kayıtlı anahtar okunamıyor. Anahtarı yeniden gir.'),
    ).toBeInTheDocument()
  })

  // Below md the status sits under the name and description on
  // every row, whatever the description's length.
  it('puts the status under the name on every row below md', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
      json([
        { name: 'finnhub', configured: false, last4: '' },
        { name: 'tiingo', configured: false, last4: '' },
      ]),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProvidersSection />
      </QueryClientProvider>,
    )

    const statuses = await screen.findAllByText('Tanımlı değil')
    expect(statuses).toHaveLength(2)
    for (const status of statuses) expect(status.parentElement).toHaveClass('max-md:flex-col')
  })
})
