import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { WatchItem } from '../api/watchlist'
import { t } from '../i18n/strings'
import { WatchlistPage } from './WatchlistPage'

// jsdom implements neither method on <dialog>; both only flip the open state.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const item = (fields: Partial<WatchItem>): WatchItem => ({
  id: 3,
  instrument_id: 10,
  symbol: 'THYAO',
  name: 'Türk Hava Yolları',
  market: 'BIST',
  currency: 'TRY',
  last_price: '123.45',
  price_date: '2026-10-16',
  price_source: 'yahoo',
  prev_close: '120',
  prev_price_date: '2026-10-15',
  day_change: '3.45',
  target_price: null,
  target_side: null,
  target_reached: false,
  note: '',
  created_at: '2026-10-01T00:00:00Z',
  ...fields,
})

// Answers the list with `items` and records the requests that change it; a
// DELETE drops the item from the list.
function mockApi(items: WatchItem[]) {
  const sent: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/watchlist' && !init?.method) return Response.json(items)
    if (url.startsWith('/api/watchlist/') && init?.method === 'DELETE') {
      sent.push(`DELETE ${url}`)
      items.splice(
        items.findIndex((row) => url === `/api/watchlist/${row.id}`),
        1,
      )
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return sent
}

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <WatchlistPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const row = async (symbol: string) => (await screen.findByText(symbol)).closest('tr') as HTMLElement

it('shows the last price, the day change and a reached target with its pill', async () => {
  mockApi([item({ target_price: '120', target_side: 'above', target_reached: true })])
  renderPage()

  const cells = within(await row('THYAO'))
  expect(cells.getByText('BIST')).toBeInTheDocument()
  expect(cells.getByText('₺123,45')).toBeInTheDocument()
  expect(cells.getByText('+₺3,45')).toBeInTheDocument()
  expect(cells.getByText('+%2,88')).toBeInTheDocument()
  expect(cells.getByText('₺120,00')).toBeInTheDocument()
  expect(cells.getByText(t('watchlist.above'))).toBeInTheDocument()
  expect(cells.getByText(t('watchlist.reached'))).toBeInTheDocument()
})

it('shows a dash for a price, a change and a target it does not have', async () => {
  mockApi([item({ last_price: null, price_date: null, prev_close: null, day_change: null })])
  renderPage()

  const cells = within(await row('THYAO'))
  expect(cells.getAllByText('—')).toHaveLength(3)
  expect(cells.queryByText(t('watchlist.reached'))).toBeNull()
})

it('removes an item after a confirm', async () => {
  const sent = mockApi([item({ id: 3 })])
  renderPage()

  fireEvent.click(
    await screen.findByRole('button', { name: t('watchlist.remove', { symbol: 'THYAO' }) }),
  )
  const confirm = await screen.findByRole('dialog', {
    name: t('watchlist.removeConfirm', { symbol: 'THYAO' }),
  })
  fireEvent.click(within(confirm).getByRole('button', { name: t('watchlist.removeButton') }))

  await waitFor(() => expect(sent).toEqual(['DELETE /api/watchlist/3']))
})

it('offers to add the first instrument when the list is empty', async () => {
  mockApi([])
  renderPage()

  expect(await screen.findByText(t('watchlist.empty'))).toBeInTheDocument()
  fireEvent.click(screen.getAllByRole('button', { name: t('watchlist.add') })[1])
  expect(screen.getByRole('dialog', { name: t('watchlist.addDialog.title') })).toBeInTheDocument()
})

it('opens the target dialog from the edit button', async () => {
  mockApi([item({})])
  renderPage()

  fireEvent.click(
    await screen.findByRole('button', { name: t('watchlist.editTarget', { symbol: 'THYAO' }) }),
  )
  expect(
    screen.getByRole('dialog', { name: t('watchlist.targetDialog.title', { symbol: 'THYAO' }) }),
  ).toBeInTheDocument()
})
