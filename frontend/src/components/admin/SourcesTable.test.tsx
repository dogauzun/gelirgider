import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SourcesTable } from './SourcesTable'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

const hoursAgo = (hours: number) => new Date(Date.now() - hours * 3600_000).toISOString()

const failure = 'sync bist-splits: fetch: Get "https://example.test/x": Forbidden'

const done = {
  started_at: '2026-09-24T10:00:00Z',
  finished_at: '2026-09-24T10:00:05Z',
  kind: 'manual',
  rows: 3,
  error: null,
}

const source = (name: string, extra: object = {}) => ({
  name,
  enabled: true,
  at: '19:00',
  can_backfill: true,
  last_run: done,
  last_success_at: hoursAgo(1),
  ...extra,
})

// Answers the admin reads; `other` handles the writes.
function mockFetch(
  sources: object[],
  other: (
    url: string,
    init?: RequestInit,
  ) => Response | undefined | Promise<Response | undefined> = () => undefined,
  providers: object[] = [],
) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/admin/sources' && !init?.method) return json(sources)
    if (url === '/api/admin/providers') return json(providers)
    return (await other(url, init)) ?? json({ error: 'unexpected' }, 500)
  })
}

function renderTable() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SourcesTable />
    </QueryClientProvider>,
  )
}

const rowOf = (name: string) => within(screen.getByLabelText(`${name} açık`).closest('tr')!)

describe('SourcesTable', () => {
  afterEach(() => vi.restoreAllMocks())

  it('renders one table row per source under the columns of the spec', async () => {
    mockFetch([source('bist'), source('tcmb'), source('tefas')])
    renderTable()

    const table = await screen.findByRole('table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['Kaynak', 'Açık', 'Saat (İstanbul)', 'Son başarılı', 'Eylemler'])
    const boxes = await within(table).findAllByRole('checkbox')
    expect(boxes.map((box) => box.getAttribute('aria-label'))).toEqual([
      'bist açık',
      'tcmb açık',
      'tefas açık',
    ])
    const rows = boxes.map((box) => box.closest('tr'))
    expect(new Set(rows).size).toBe(3)
    // Below md each row is a two-line list item.
    for (const row of rows) expect(row).toHaveClass('max-md:grid')
    expect(table).toHaveClass('max-md:block')
  })

  it('shows the last success of a stale source as a warning pill, never for one that is off', async () => {
    mockFetch([
      source('bist', { last_success_at: hoursAgo(72) }),
      source('tcmb', { last_success_at: null, last_run: null }),
      source('tefas', { enabled: false, last_success_at: hoursAgo(24 * 365) }),
      source('us', { last_success_at: hoursAgo(1) }),
    ])
    renderTable()

    const pill = (name: string) =>
      rowOf(name).getByTestId('last-success').querySelector('.bg-warn-soft')
    await screen.findByLabelText('bist açık')
    // The time is checked with a fixed clock below (GLG-1057).
    expect(pill('bist')).toHaveTextContent(/\d{2}:\d{2}$/)
    expect(pill('tcmb')).toHaveTextContent('hiç')
    expect(pill('tefas')).toBeNull()
    expect(pill('us')).toBeNull()
    expect(rowOf('us').getByText('1 saat önce')).toBeInTheDocument()
  })

  // GLG-1057: 49 h and 47 h back both read "evvelsi gün", so the stale pill
  // shows the Istanbul date and time instead; a fresh source keeps the
  // relative text.
  it("shows a stale source's last success as its Istanbul date and time", async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-06T12:05:00Z'))
    try {
      mockFetch([
        source('bist', { last_success_at: '2026-10-04T11:05:00Z' }),
        source('us', { last_success_at: '2026-10-04T13:05:00Z' }),
      ])
      renderTable()

      await screen.findByLabelText('bist açık')
      const cell = (name: string) => rowOf(name).getByTestId('last-success')
      expect(cell('bist').querySelector('.bg-warn-soft')).toHaveTextContent(/^4 Eki 14:05$/)
      expect(cell('us').querySelector('.bg-warn-soft')).toBeNull()
      expect(rowOf('us').getByText('evvelsi gün')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('calls the same endpoints as before from each row', async () => {
    // The list always answers tcmb as on, so the time save sends enabled: true.
    const fetchMock = mockFetch(
      [source('tcmb'), source('bist-splits', { can_backfill: false })],
      (url, init) => {
        if (url === '/api/admin/sources/tcmb' && init?.method === 'PUT') {
          return new Response(null, { status: 204 })
        }
        if (url === '/api/admin/sources/tcmb/run') return new Response(null, { status: 202 })
        if (url === '/api/admin/backfill') return json({ queued: ['tcmb'] })
      },
    )
    renderTable()
    const writes = () =>
      fetchMock.mock.calls
        .filter(([, init]) => init?.method)
        .map(([url, init]) => [String(url), init?.method, init?.body])

    const toggle = await screen.findByLabelText('tcmb açık')
    fireEvent.click(toggle)
    await vi.waitFor(() => expect(writes()).toHaveLength(1))

    const time = screen.getByLabelText('tcmb saati (İstanbul)')
    fireEvent.change(time, { target: { value: '20:15' } })
    fireEvent.blur(time)
    await vi.waitFor(() => expect(writes()).toHaveLength(2))

    fireEvent.click(screen.getByRole('button', { name: 'tcmb için şimdi çalıştır' }))
    await vi.waitFor(() => expect(writes()).toHaveLength(3))
    fireEvent.click(screen.getByRole('button', { name: 'tcmb için geçmişi doldur' }))
    await vi.waitFor(() => expect(writes()).toHaveLength(4))

    expect(writes()).toEqual([
      ['/api/admin/sources/tcmb', 'PUT', JSON.stringify({ enabled: false, at: '19:00' })],
      ['/api/admin/sources/tcmb', 'PUT', JSON.stringify({ enabled: true, at: '20:15' })],
      ['/api/admin/sources/tcmb/run', 'POST', undefined],
      ['/api/admin/backfill', 'POST', JSON.stringify({ source: 'tcmb' })],
    ])
    // bist-splits has no history walk, as before.
    expect(
      rowOf('bist-splits').queryByRole('button', { name: /geçmişi doldur/i }),
    ).not.toBeInTheDocument()
  })

  it('translates a 409 run, shows a running source and a failed run', async () => {
    mockFetch(
      [
        source('bist', { last_run: { ...done, finished_at: null } }),
        source('bist-splits', { can_backfill: false, last_run: { ...done, error: failure } }),
        source('tcmb'),
      ],
      (url) => {
        if (url === '/api/admin/sources/tcmb/run') return json({ error: 'already running' }, 409)
      },
    )
    renderTable()

    await screen.findByLabelText('tcmb açık')
    expect(rowOf('tcmb').getByText('Son çalışma: 3 satır')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'tcmb için şimdi çalıştır' }))
    expect(await screen.findByText('Bu kaynak zaten çalışıyor.')).toBeInTheDocument()

    expect(rowOf('bist').getByText('Çalışıyor')).toBeInTheDocument()

    // GLG-385: a failed run shows a translated line; the raw error sits behind it.
    const failed = screen.getByText('Son çalışma başarısız')
    expect(failed.tagName).toBe('SUMMARY')
    expect(failed.closest('details')).not.toHaveAttribute('open')
    expect(screen.getByText(failure).closest('details')).toBe(failed.closest('details'))
  })

  // GLG-1071: "Son çalışma: 0 satır" read like a clean empty run.
  it('shows no row count for a failed run', async () => {
    mockFetch([source('tcmb', { last_run: { ...done, rows: 0, error: failure } })])
    renderTable()

    await screen.findByLabelText('tcmb açık')
    expect(screen.getByText('Son çalışma başarısız')).toBeInTheDocument()
    expect(screen.queryByText(/Son çalışma: .* satır/)).not.toBeInTheDocument()
  })

  // GLG-1311: every failed source's disclosure was named only "Son çalışma
  // başarısız", so a screen reader could not tell them apart.
  it("names each failed run's disclosure after its source", async () => {
    mockFetch([
      source('bist', { last_run: { ...done, error: failure } }),
      source('tcmb', { last_run: { ...done, error: failure } }),
    ])
    renderTable()

    await screen.findByLabelText('tcmb açık')
    const [bist, tcmb] = screen.getAllByText('Son çalışma başarısız')
    expect(bist).toHaveAccessibleName('bist: son çalışma başarısız')
    expect(tcmb).toHaveAccessibleName('tcmb: son çalışma başarısız')
  })

  // GLG-1071: the enable box sits in a 44 px label, so a tap beside it counts.
  it('toggles a source from a click on the box’s cell label', async () => {
    const puts: string[] = []
    mockFetch([source('tcmb')], (url, init) => {
      if (init?.method === 'PUT') {
        puts.push(url)
        return new Response(null, { status: 204 })
      }
    })
    renderTable()

    const label = (await screen.findByLabelText('tcmb açık')).closest('label')!
    expect(label).toHaveClass('size-11')
    fireEvent.click(label)
    await vi.waitFor(() => expect(puts).toEqual(['/api/admin/sources/tcmb']))
  })

  // GLG-917: the toggle stays enabled and focused while it saves, and a
  // second click meanwhile sends nothing.
  it('keeps the toggle enabled and focused while it saves', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    const puts: string[] = []
    mockFetch([source('tcmb')], async (url, init) => {
      if (init?.method === 'PUT') {
        puts.push(url)
        await held
        return new Response(null, { status: 204 })
      }
    })
    renderTable()

    const toggle = await screen.findByLabelText('tcmb açık')
    toggle.focus()
    fireEvent.click(toggle)
    await vi.waitFor(() => expect(toggle).toHaveAttribute('aria-disabled', 'true'))
    expect(toggle).toBeEnabled()
    expect(toggle).toHaveFocus()
    fireEvent.click(toggle)
    expect(puts).toEqual(['/api/admin/sources/tcmb'])
    release()
    await vi.waitFor(() => expect(toggle).toHaveAttribute('aria-disabled', 'false'))
  })

  // GLG-1292: isPending reaches the controls a tick after the click, so a
  // double click's second click came before pending and sent the request twice.
  it('sends one request per double click on the toggle, run and backfill', async () => {
    const sent: string[] = []
    mockFetch([source('tcmb')], (url, init) => {
      sent.push(`${init?.method} ${url}`)
      return new Promise<Response>(() => {})
    })
    renderTable()

    await screen.findByLabelText('tcmb açık')
    const row = rowOf('tcmb')
    for (const control of [
      row.getByLabelText('tcmb açık'),
      row.getByRole('button', { name: 'tcmb için şimdi çalıştır' }),
      row.getByRole('button', { name: 'tcmb için geçmişi doldur' }),
    ]) {
      fireEvent.click(control)
      fireEvent.click(control)
      await vi.waitFor(() => expect(control).toHaveAttribute('aria-disabled', 'true'))
    }
    expect(sent).toEqual([
      'PUT /api/admin/sources/tcmb',
      'POST /api/admin/sources/tcmb/run',
      'POST /api/admin/backfill',
    ])
  })

  it('names only the US providers in the missing key note', async () => {
    mockFetch([source('us', { last_run: null, last_success_at: null })], undefined, [
      { name: 'finnhub', configured: false, last4: '' },
      { name: 'tiingo', configured: true, last4: 'abcd' },
      { name: 'evds', configured: false, last4: '' },
    ])
    renderTable()

    // GLG-517: an unconfigured EVDS key is not a missing US key.
    expect(
      await screen.findByText(/^ABD fiyatları için eksik API anahtarı: Finnhub\.$/),
    ).toBeInTheDocument()
  })
})
