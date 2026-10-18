import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { WatchAddDialog } from './WatchAddDialog'

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

const xyz = {
  id: 7,
  symbol: 'XYZ',
  market: 'BIST',
  name: 'XYZ Holding',
  currency: 'TRY',
  sector_override: null,
}

function mockApi() {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.startsWith('/api/instruments?')) return Response.json([xyz])
    if (url === '/api/watchlist' && init?.method === 'POST') {
      bodies.push(JSON.parse(init.body as string))
      return Response.json({ id: 1 }, { status: 201 })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return bodies
}

it('keeps Ekle disabled until an instrument is picked, then posts it without a note key', async () => {
  const bodies = mockApi()
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <WatchAddDialog onClose={onClose} />
    </QueryClientProvider>,
  )

  const submit = screen.getByRole('button', { name: t('watchlist.addDialog.submit') })
  expect(submit).toBeDisabled()
  fireEvent.change(screen.getByLabelText(t('watchlist.instrument')), { target: { value: 'xyz' } })
  fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
  expect(submit).toBeEnabled()
  fireEvent.click(submit)

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([{ instrument_id: 7 }])
})
