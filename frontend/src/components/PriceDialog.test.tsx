import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { Position } from '../api/summary'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { flushToasts } from '../lib/toast'
import { PriceDialog } from './PriceDialog'
import { Toaster } from './ui/Toast'

beforeAll(() => {
  // jsdom implements neither showModal nor matchMedia.
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  window.matchMedia = (query) =>
    ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

afterEach(async () => {
  vi.restoreAllMocks()
  await flushToasts()
})

function position(fields: Partial<Position> = {}): Position {
  return {
    instrument_id: 7,
    symbol: 'THYAO',
    name: 'Türk Hava Yolları',
    market: 'BIST',
    currency: 'TRY',
    quantity: '10',
    avg_cost: '100.00',
    cost_basis: '1000.00',
    last_price: '125.00',
    has_price: true,
    price_source: 'bist',
    price_date: '2026-09-11',
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    market_value: '1250.00',
    unrealized_pl: '250.00',
    realized_pl: '0.00',
    market_value_reporting: '1250.00',
    unrealized_pl_reporting: '250.00',
    realized_pl_reporting: '0.00',
    total_pl_reporting: '250.00',
    closed: false,
    ...fields,
  }
}

// Answers the PUT; returns the fetch spy and a getter for the PUT calls.
function mockFetch() {
  const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (init?.method === 'PUT' && url.startsWith('/api/instruments/7/prices/')) {
      return new Response(
        JSON.stringify({ instrument_id: 7, date: todayIstanbul(), close: '1', source: 'manual' }),
        { status: 200 },
      )
    }
    // The invalidated queries refetch nothing here: none is mounted.
    throw new Error(`unexpected request: ${url}`)
  })
  const puts = () => fetchMock.mock.calls.filter(([, init]) => init?.method === 'PUT')
  return { puts }
}

function renderDialog(p: Position = position()) {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PriceDialog position={p} portfolioId={1} anchor={null} onClose={onClose} />
      <Toaster />
    </QueryClientProvider>,
  )
  return onClose
}

describe('PriceDialog', () => {
  it('is named by the symbol and starts from the price in effect and today', () => {
    mockFetch()
    renderDialog()
    expect(screen.getByRole('dialog', { name: 'THYAO için fiyat gir' })).toBeInTheDocument()
    expect(screen.getByLabelText('Fiyat')).toHaveValue('125,00')
    expect(screen.getByLabelText('Tarih')).toHaveValue(todayIstanbul())
  })

  it('saves on Enter, confirms with a toast and closes', async () => {
    const { puts } = mockFetch()
    const onClose = renderDialog()
    const input = screen.getByLabelText('Fiyat')
    fireEvent.change(input, { target: { value: '130.50' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(puts()[0][0]).toBe(`/api/instruments/7/prices/${todayIstanbul()}`)
    expect(puts()[0][1]?.body).toBe(JSON.stringify({ close: '130.50' }))
    expect(await screen.findByText(t('prices.saved'))).toBeInTheDocument()
  })

  it('closes without a request on a re-save of the manual price in effect (GLG-813)', async () => {
    const { puts } = mockFetch()
    const onClose = renderDialog(
      position({ price_source: 'manual', price_date: todayIstanbul(), last_price: '60.00' }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(onClose).toHaveBeenCalled()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(puts()).toHaveLength(0)
    expect(screen.queryByText(t('prices.saved'))).toBeNull()
  })

  it('saves its own tr prefill of a three-decimal price unchanged', async () => {
    const { puts } = mockFetch()
    renderDialog(position({ last_price: '12.345' }))
    expect(screen.getByLabelText('Fiyat')).toHaveValue('12,345')
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    await waitFor(() => expect(puts()).toHaveLength(1))
    expect(puts()[0][1]?.body).toBe(JSON.stringify({ close: '12.345' }))
  })

  it('announces an invalid price as an alert (GLG-437)', () => {
    const { puts } = mockFetch()
    renderDialog()
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent(
      "Fiyat 0'dan büyük bir sayı olmalı.",
    )
    expect(puts()).toHaveLength(0)
  })

  it('rejects an empty date without a request (GLG-478)', () => {
    const { puts } = mockFetch()
    renderDialog()
    fireEvent.change(screen.getByLabelText('Tarih'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent(
      'Tarih geçersiz.',
    )
    expect(puts()).toHaveLength(0)
  })

  it('closes on Vazgeç and on Escape', () => {
    mockFetch()
    const onClose = renderDialog()
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(screen.getByLabelText('Fiyat'), { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  // Chromium blurs a focused control the moment it is disabled, so
  // the running save marks Kaydet pending and the fields read-only instead; a
  // second click or Enter sends nothing.
  it('keeps Kaydet focused and pending while the save runs', async () => {
    let finish: (response: Response) => void = () => {}
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>((resolve) => (finish = resolve)))
    renderDialog()
    const input = screen.getByLabelText('Fiyat')
    fireEvent.change(input, { target: { value: '130' } })
    const save = screen.getByRole('button', { name: 'Kaydet' })
    save.focus()
    fireEvent.click(save)

    await waitFor(() => expect(save).toHaveAttribute('aria-disabled', 'true'))
    expect(save).toBeEnabled()
    expect(save).toHaveFocus()
    expect(input).toBeEnabled()
    fireEvent.click(save)
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    finish(new Response('{"error":"boom"}', { status: 500 }))
    await waitFor(() => expect(save).not.toHaveAttribute('aria-disabled'))
  })

  // isPending reaches the button a tick after the click, so a double
  // click's second click came before pending and sent the price twice.
  it('saves once on a double click of Kaydet', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>(() => {}))
    renderDialog()
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '130' } })
    const save = screen.getByRole('button', { name: 'Kaydet' })
    fireEvent.click(save)
    fireEvent.click(save)

    await waitFor(() => expect(save).toHaveAttribute('aria-disabled', 'true'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('shows the error of a failed save and stays open', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('{"error":"boom"}', { status: 500 }),
    )
    const onClose = renderDialog()
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '130' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
  })
})
