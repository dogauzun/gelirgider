import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi, type MockInstance } from 'vitest'
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

    // The sector form's search input keeps its own empty live region, so match by text.
    expect(await screen.findByText('Kaydedildi: SOL · Solana · Kripto · USD')).toBeVisible()
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

  // isPending reaches the button a tick after the click, so a double
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

  describe('sector override', () => {
    // Searches answer with one instrument; a PATCH echoes it with the sent override.
    async function pickThyao(override: string | null) {
      const thyao = {
        id: 7,
        symbol: 'THYAO',
        market: 'BIST',
        name: 'Türk Hava Yolları',
        currency: 'TRY',
        sector_override: override,
      }
      const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation((_url, init) => {
        if (init?.method === 'PATCH') {
          const body = JSON.parse(String(init.body))
          return Promise.resolve(json({ ...thyao, sector_override: body.sector_override }))
        }
        return Promise.resolve(json([thyao]))
      })
      render(
        <QueryClientProvider client={new QueryClient()}>
          <InstrumentsSection />
        </QueryClientProvider>,
      )
      expect(screen.getByRole('button', { name: 'Sektörü kaydet' })).toBeDisabled()
      const search = screen.getByRole('combobox', { name: 'Enstrüman' })
      fireEvent.focus(search)
      fireEvent.change(search, { target: { value: 'THY' } })
      fireEvent.click(await screen.findByRole('option', { name: /THYAO/ }))
      return fetchMock
    }

    function patchBody(fetchMock: MockInstance<typeof fetch>) {
      const call = fetchMock.mock.calls.find(([, init]) => init?.method === 'PATCH')
      expect(call?.[0]).toBe('/api/admin/instruments/7')
      return JSON.parse(String(call?.[1]?.body))
    }

    it('saves the chosen sector key', async () => {
      const fetchMock = await pickThyao(null)

      const sector = screen.getByLabelText('Sektör')
      expect(sector).toHaveTextContent('Sağlayıcı değeri')
      await chooseOption(sector, 'Enerji')
      fireEvent.click(screen.getByRole('button', { name: 'Sektörü kaydet' }))

      expect(await screen.findByText('Kaydedildi: THYAO · Enerji')).toBeInTheDocument()
      expect(patchBody(fetchMock)).toEqual({ sector_override: 'energy' })
    })

    it('prefills the override and sends null for the provider value', async () => {
      const fetchMock = await pickThyao('energy')

      const sector = screen.getByLabelText('Sektör')
      expect(sector).toHaveTextContent('Enerji')
      await chooseOption(sector, 'Sağlayıcı değeri')
      fireEvent.click(screen.getByRole('button', { name: 'Sektörü kaydet' }))

      expect(await screen.findByText('Kaydedildi: THYAO · Sağlayıcı değeri')).toBeInTheDocument()
      expect(patchBody(fetchMock)).toEqual({ sector_override: null })
    })
  })
})
