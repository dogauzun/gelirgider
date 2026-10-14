import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { ImportPage } from './ImportPage'

// GLG-1068: an action whose button leaves the page with it moves focus on,
// rather than dropping it to <body>. fireEvent.click does not focus what it
// clicks, so each test focuses the button first, as a click or Enter would.

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

afterEach(() => {
  vi.restoreAllMocks()
})

const xyz = { id: 777, symbol: 'XYZ', market: 'US', name: 'XYZ Holding', currency: 'USD' }

function statement(period_start: string, period_end: string, rows: unknown[] = []) {
  return {
    broker: 'midas',
    sha256: period_start,
    period_start,
    period_end,
    currency: 'USD',
    warnings: [],
    rows,
    holdings: [],
    reconciliation: [],
  }
}

// `lookup` answers the TEFAS fund lookup, when the test lets it.
function mockApi(parse: (name: string) => unknown, lookup?: Promise<Response>) {
  let id = 10
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/api/portfolios') return new Response('[{"id":1,"name":"Emeklilik"}]')
    if (url === '/api/imports' && method === 'GET') return new Response('[]')
    if (url.startsWith('/api/instruments?')) return new Response(JSON.stringify([xyz]))
    if (url === '/api/imports/funds' && lookup) return lookup
    if (url === '/api/imports/parse') {
      const file = (init?.body as FormData).get('file') as File
      return new Response(JSON.stringify(parse(file.name)))
    }
    if (url === '/api/imports' && method === 'POST') {
      id += 1
      return new Response(JSON.stringify({ id, transaction_count: 0 }), { status: 201 })
    }
    throw new Error(`unexpected request: ${method} ${url}`)
  })
}

function renderPage(names: string[]) {
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter initialEntries={['/portfolios/1/import']}>
        <Routes>
          <Route path="/portfolios/:id/import" element={<ImportPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  const files = names.map((name) => new File(['%PDF-1.4'], name, { type: 'application/pdf' }))
  fireEvent.change(document.querySelector('input[type="file"]')!, { target: { files } })
}

function press(button: HTMLElement) {
  button.focus()
  fireEvent.click(button)
}

it('moves focus to the heading of each batch step', async () => {
  mockApi((name) =>
    name === 'ocak.pdf'
      ? statement('2025-01-01', '2025-01-31')
      : statement('2025-02-01', '2025-02-28'),
  )
  renderPage(['subat.pdf', 'ocak.pdf'])

  // The first review, once the upfront parse is done.
  await waitFor(() => expect(screen.getByRole('heading', { name: 'ocak.pdf' })).toHaveFocus())

  // İçe aktar: the next file's review.
  press(screen.getByRole('button', { name: 'İçe aktar' }))
  await waitFor(() => expect(screen.getByRole('heading', { name: 'subat.pdf' })).toHaveFocus())

  // Bu ekstreyi atla on the last file: Bitti.
  press(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Bitti: 1 ekstre içe aktarıldı.' })).toHaveFocus(),
  )

  // Başka ekstre yükle: step 1.
  press(screen.getByRole('button', { name: 'Başka ekstre yükle' }))
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Ekstre yükle' })).toHaveFocus())
})

// Two rows to fix; the review opens on Sorunlu, where a fixed row leaves.
const rows = [
  {
    key: 'err-0',
    status: 'error',
    section: 'YATIRIM İŞLEMLERİ',
    message: 'quantity: invalid number',
    date: '2025-12-10',
    raw: ['10/12/25', 'Alış', 'VTI', '?'],
  },
  {
    key: 'trade-3',
    status: 'unknown_instrument',
    type: 'buy',
    date: '2025-12-20',
    symbol: 'XYZ',
    quantity: '1',
    price: '10',
    amount: '10',
    fee: '0',
    currency: 'USD',
  },
]

it('moves focus to the next row to fix when an edited row leaves Sorunlu', async () => {
  mockApi(() => statement('2025-12-01', '2025-12-31', rows))
  renderPage(['ekstre.pdf'])
  await screen.findByRole('radio', { name: 'Sorunlu 2', checked: true })

  press(screen.getByRole('button', { name: '10.12.2025 satırını düzenle' }))
  fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
  fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
  press(screen.getByRole('button', { name: 'Kaydet' }))

  expect(screen.getByRole('radio', { name: 'Sorunlu 1', checked: true })).toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: '20.12.2025 XYZ Alış 1 × $10,00 $10,00 satırını düzenle' }),
  ).toHaveFocus()
})

it('moves focus to the row left to fix when a picked row leaves Sorunlu', async () => {
  mockApi(() => statement('2025-12-01', '2025-12-31', rows))
  renderPage(['ekstre.pdf'])
  await screen.findByRole('radio', { name: 'Sorunlu 2', checked: true })

  const picker = screen.getByLabelText('XYZ için enstrüman seç')
  picker.focus()
  fireEvent.change(picker, { target: { value: 'xyz' } })
  fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

  expect(screen.getByRole('radio', { name: 'Sorunlu 1', checked: true })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '10.12.2025 satırını düzenle' })).toHaveFocus()
})

// A pick that lands after the user moved on, as a TEFAS lookup does, leaves
// focus where the user is.
it('leaves focus where the user went while a fund lookup ran', async () => {
  let answer: (response: Response) => void = () => {}
  const lookup = new Promise<Response>((resolve) => (answer = resolve))
  const fund = {
    key: 'fund-0',
    status: 'unknown_instrument',
    type: 'buy',
    date: '2025-12-05',
    symbol: 'GARANTİ PORTFÖY TL FONU',
    market: 'TEFAS',
    quantity: '10',
    price: '1.5',
    amount: '15',
    fee: '0',
    currency: 'TRY',
  }
  mockApi(() => statement('2025-12-01', '2025-12-31', [fund, ...rows]), lookup)
  renderPage(['ekstre.pdf'])
  await screen.findByRole('radio', { name: 'Sorunlu 3', checked: true })

  fireEvent.change(screen.getByLabelText('GARANTİ PORTFÖY TL FONU için enstrüman seç'), {
    target: { value: 'gtl' },
  })
  press(await screen.findByRole('button', { name: "GTL fonunu TEFAS'ta ara" }))
  const other = screen.getByLabelText('XYZ için enstrüman seç')
  other.focus()

  answer(
    new Response(
      JSON.stringify({ id: 888, symbol: 'GTL', market: 'TEFAS', name: 'GTL', currency: 'TRY' }),
    ),
  )
  await screen.findByRole('radio', { name: 'Sorunlu 2', checked: true })
  expect(other).toHaveFocus()
})
