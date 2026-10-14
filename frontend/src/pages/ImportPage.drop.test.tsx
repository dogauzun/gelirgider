import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeAll, expect, it, vi } from 'vitest'
import { ImportPage } from './ImportPage'

beforeAll(() => {
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

// GLG-1052: the drop zone is gone while a preview is under review, and a
// file dropped anywhere on the page would open in the tab and lose it.
it('keeps a file dropped during the review from opening in the tab', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/api/portfolios') return new Response('[{"id":1,"name":"Emeklilik"}]')
    if (url === '/api/imports' && method === 'GET') return new Response('[]')
    if (url === '/api/imports/parse') return new Response(JSON.stringify(preview))
    throw new Error(`unexpected request: ${method} ${url}`)
  })
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
  const file = new File(['%PDF-1.4'], 'ekstre.pdf', { type: 'application/pdf' })
  fireEvent.change(document.querySelector('input[type="file"]')!, { target: { files: [file] } })
  await screen.findByRole('button', { name: 'İçe aktar' })
  expect(document.querySelector('input[type="file"]')).toBeNull()

  const stray = new Event('drop', { bubbles: true, cancelable: true })
  Object.defineProperty(stray, 'dataTransfer', { value: { types: ['Files'] } })
  document.body.dispatchEvent(stray)
  expect(stray.defaultPrevented).toBe(true)
})

function renderPage() {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/api/portfolios') return new Response('[{"id":1,"name":"Emeklilik"}]')
    if (url === '/api/imports' && method === 'GET') return new Response('[]')
    if (url === '/api/imports/parse') return new Response(JSON.stringify(preview))
    throw new Error(`unexpected request: ${method} ${url}`)
  })
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
}

function dropFiles(files: File[]) {
  const zone = screen.getByRole('button', { name: /Dosyaları buraya bırak/ })
  const items = files.map((file) => ({
    kind: 'file',
    getAsFile: () => file,
    webkitGetAsEntry: () => ({ isDirectory: false }),
  }))
  fireEvent.drop(zone, { dataTransfer: { files, items, types: ['Files'] } })
}

const pdf = (name: string) => new File(['%PDF-1.4'], name, { type: 'application/pdf' })
const note = () => new File(['hello'], 'note.txt', { type: 'text/plain' })

// GLG-1064: the good file opens the preview at once and unmounts the drop
// zone, so the refused file is named where the review is.
it('names a refused file in the review of the one good file dropped with it', async () => {
  renderPage()
  dropFiles([pdf('ekstre.pdf'), note()])
  await screen.findByRole('button', { name: 'İçe aktar' })
  expect(screen.getByText('Seçilen aracı kurum bu dosya türünü almıyor: note.txt')).toBeVisible()
})

it('lists a refused file in the queue of a batch with its reason', async () => {
  renderPage()
  dropFiles([pdf('aralik.pdf'), pdf('ocak.pdf'), note()])
  await screen.findByRole('button', { name: 'İçe aktar' })
  fireEvent.click(screen.getByRole('button', { name: 'Tüm dosyaları göster' }))
  const entry = screen.getByText('note.txt').closest('li')!
  expect(entry).toBeVisible()
  expect(entry).toHaveTextContent('Seçilen aracı kurum bu dosya türünü almıyor')
  expect(entry).toHaveTextContent('Aktarılmadı')
})
