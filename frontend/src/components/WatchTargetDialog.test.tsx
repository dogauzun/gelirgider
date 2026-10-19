import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { WatchItem } from '../api/watchlist'
import { t } from '../i18n/strings'
import { WatchTargetDialog } from './WatchTargetDialog'

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

const watched: WatchItem = {
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
  target_price: '130',
  target_side: 'above',
  target_reached: false,
  note: '',
  created_at: '2026-10-01T00:00:00Z',
}

// Answers the PUT with `reply` and returns the bodies it got.
function mockApi(reply: Response = Response.json(watched)) {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/watchlist/3' && init?.method === 'PUT') {
      bodies.push(JSON.parse(init.body as string))
      return reply.clone()
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return bodies
}

function renderDialog(item = watched) {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WatchTargetDialog item={item} onClose={onClose} />
    </QueryClientProvider>,
  )
  return onClose
}

const targetField = () =>
  screen.getByLabelText(t('watchlist.targetDialog.field', { currency: 'TRY' }))

it('puts the stored target in the field and saves the typed one', async () => {
  const bodies = mockApi()
  const onClose = renderDialog()

  expect(targetField()).toHaveValue('130')
  fireEvent.change(targetField(), { target: { value: '150' } })
  fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.save') }))

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([{ target_price: '150', note: '' }])
})

it('clears the target with Hedefi kaldır, without checking the field', async () => {
  const bodies = mockApi()
  const onClose = renderDialog()

  fireEvent.change(targetField(), { target: { value: '' } })
  fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.clear') }))

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([{ target_price: null, note: '' }])
})

it('saves an empty target as no target, with the changed note', async () => {
  const bodies = mockApi()
  const onClose = renderDialog({ ...watched, target_price: null, target_side: null, note: 'a' })

  fireEvent.change(
    screen.getByLabelText(`${t('transactions.note')} · ${t('transactions.optional')}`),
    { target: { value: 'b' } },
  )
  fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.save') }))

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([{ target_price: null, note: 'b' }])
})

it('rejects a target that is not a positive decimal and sends nothing', async () => {
  const bodies = mockApi()
  renderDialog()

  for (const value of ['0', '12abc']) {
    fireEvent.change(targetField(), { target: { value } })
    fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.save') }))
    expect(await screen.findByText(t('error.field.target_price'))).toBeInTheDocument()
  }
  fireEvent.change(targetField(), { target: { value: '1.000' } })
  fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.save') }))
  expect(await screen.findByText(t('decimal.error.ambiguousThousands'))).toBeInTheDocument()
  expect(bodies).toEqual([])
})

it("shows the server's message when the instrument has no close", async () => {
  mockApi(Response.json({ error: 'target_price: no close to compare with' }, { status: 400 }))
  const onClose = renderDialog()

  fireEvent.click(screen.getByRole('button', { name: t('watchlist.targetDialog.save') }))

  expect(await screen.findByRole('alert')).toHaveTextContent(t('error.watchNoClose'))
  expect(onClose).not.toHaveBeenCalled()
})
