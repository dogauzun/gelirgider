import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chooseOption } from '../../test/select'
import { InstrumentsSection } from './InstrumentsSection'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

describe('InstrumentsSection', () => {
  afterEach(() => vi.restoreAllMocks())

  it('sends the CRYPTO currency choice, shows the saved instrument and a 400 inline', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        json({ id: 7, symbol: 'SOL', market: 'CRYPTO', name: 'Solana', currency: 'USD' }),
      )
      .mockResolvedValueOnce(json({ error: 'market and currency must be a supported pair' }, 400))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentsSection />
      </QueryClientProvider>,
    )

    const currency = screen.getByLabelText('Para birimi')
    expect(currency).toBeDisabled()
    expect(currency).toHaveTextContent('TRY')

    await chooseOption(screen.getByLabelText('Pazar'), 'Kripto')
    expect(currency).toBeEnabled()
    await chooseOption(currency, 'USD')
    expect(currency).toHaveTextContent('USD')
    fireEvent.change(screen.getByLabelText('Sembol'), { target: { value: 'sol' } })
    fireEvent.change(screen.getByLabelText('Ad'), { target: { value: 'Solana' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Kaydedildi: SOL · Solana · Kripto · USD',
    )
    expect(fetchMock.mock.calls[0][0]).toBe('/api/admin/instruments')
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      market: 'CRYPTO',
      symbol: 'sol',
      name: 'Solana',
      currency: 'USD',
    })
    expect(screen.getByLabelText('Sembol')).toHaveValue('')
    expect(screen.getByLabelText('Ad')).toHaveValue('')

    fireEvent.change(screen.getByLabelText('Sembol'), { target: { value: 'X' } })
    fireEvent.change(screen.getByLabelText('Ad'), { target: { value: 'X' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Bu pazar ve para birimi birlikte kullanılamaz.',
    )
  })

  // GLG-1268: isPending reaches the button a tick after the click, so a double
  // click's second click came before pending and posted the instrument twice.
  it('posts once on a double click', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>(() => {}))
    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentsSection />
      </QueryClientProvider>,
    )
    fireEvent.change(screen.getByLabelText('Sembol'), { target: { value: 'THYAO' } })
    fireEvent.change(screen.getByLabelText('Ad'), { target: { value: 'Türk Hava Yolları' } })
    const submit = screen.getByRole('button', { name: 'Ekle' })
    fireEvent.click(submit)
    fireEvent.click(submit)

    await waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
