import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SourcesSection } from './SourcesSection'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

const tcmb = {
  name: 'tcmb',
  enabled: true,
  at: '19:00',
  can_backfill: true,
  last_run: null,
  last_success_at: null,
}

function renderSection() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SourcesSection />
    </QueryClientProvider>,
  )
}

// The rows themselves are SourcesTable's, tested there.
describe('SourcesSection', () => {
  afterEach(() => vi.restoreAllMocks())

  it('puts the options in one row above the sources table and backfills all', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/admin/sources') return json([tcmb])
      if (url === '/api/admin/schedule') {
        return json({ catchup_enabled: true, backfill_enabled: true, backfill_from: '' })
      }
      if (url === '/api/admin/providers') return json([])
      if (url === '/api/admin/backfill' && init?.method === 'POST') return json({ queued: [] })
      return json({ error: 'unexpected' }, 500)
    })
    renderSection()

    const section = document.getElementById('admin-sources')!
    expect(within(section).getByRole('heading', { name: 'Kaynaklar' })).toHaveClass('text-[14px]')

    // The schedule checkboxes are named by their label, not "on".
    const catchup = await screen.findByRole('checkbox', { name: 'Açılışta eksikleri tamamla' })
    await vi.waitFor(() => expect(catchup).toBeChecked())
    expect(
      screen.getByRole('checkbox', { name: 'Açılışta geçmişi doldur' }),
    ).toHaveAccessibleDescription(
      'Sunucu açılırken açık kaynakların fiyat geçmişini başlangıç tarihinden bu yana doldurur.',
    )
    const options = catchup.closest('.grid')!
    expect(options).toHaveClass('md:grid-cols-3')
    expect(within(options as HTMLElement).getByLabelText('Geçmiş başlangıç tarihi')).toBeVisible()

    const table = await within(section).findByRole('table')
    expect(options.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(table).getByLabelText('tcmb açık')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tümünü doldur' }))
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) => url === '/api/admin/backfill' && init?.method === 'POST' && !init.body,
        ),
      ).toBe(true),
    )
  })

  // Chromium blurs a focused control the moment it is disabled, so a
  // keyboard user who toggled a checkbox lost focus to <body> while it saved.
  it('keeps a schedule checkbox enabled and focused while it saves', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    const puts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (init?.method === 'PUT') {
        puts.push(url)
        await held
        return new Response(null, { status: 204 })
      }
      if (url === '/api/admin/sources') return json([tcmb])
      if (url === '/api/admin/schedule') {
        return json({ catchup_enabled: true, backfill_enabled: true, backfill_from: '' })
      }
      if (url === '/api/admin/providers') return json([])
      return json({ error: 'unexpected' }, 500)
    })
    renderSection()

    const catchup = await screen.findByRole('checkbox', { name: 'Açılışta eksikleri tamamla' })
    await vi.waitFor(() => expect(catchup).not.toHaveAttribute('aria-disabled', 'true'))
    catchup.focus()
    fireEvent.click(catchup)
    await vi.waitFor(() => expect(puts).toHaveLength(1))
    await vi.waitFor(() => expect(catchup).toHaveAttribute('aria-disabled', 'true'))
    expect(catchup).toHaveFocus()
    // A second click while the save runs sends nothing.
    fireEvent.click(catchup)
    expect(puts).toEqual(['/api/admin/schedule'])

    release()
    await vi.waitFor(() => expect(catchup).not.toHaveAttribute('aria-disabled', 'true'))
    expect(catchup).toHaveFocus()
  })

  // isPending reaches the controls a tick after the click, so a
  // double click's second click came before pending and sent the request twice.
  it('sends one request per double click on backfill all and a schedule checkbox', async () => {
    const sent: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (init?.method) {
        sent.push(`${init.method} ${url}`)
        return new Promise<Response>(() => {})
      }
      if (url === '/api/admin/sources') return json([tcmb])
      if (url === '/api/admin/schedule') {
        return json({ catchup_enabled: true, backfill_enabled: true, backfill_from: '' })
      }
      if (url === '/api/admin/providers') return json([])
      return json({ error: 'unexpected' }, 500)
    })
    renderSection()

    const catchup = await screen.findByRole('checkbox', { name: 'Açılışta eksikleri tamamla' })
    await vi.waitFor(() => expect(catchup).not.toHaveAttribute('aria-disabled', 'true'))
    for (const control of [screen.getByRole('button', { name: 'Tümünü doldur' }), catchup]) {
      fireEvent.click(control)
      fireEvent.click(control)
      await vi.waitFor(() => expect(control).toHaveAttribute('aria-disabled', 'true'))
    }
    expect(sent).toEqual(['POST /api/admin/backfill', 'PUT /api/admin/schedule'])
  })
})
