import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router'
import { afterEach, beforeAll, describe, expect, it, onTestFinished, vi } from 'vitest'
import { todayIstanbul } from '../lib/date'
import { setLocale } from '../i18n/strings'
import { formatMoney } from '../lib/format'
import { Toaster } from '../components/ui/Toast'
import { chooseOption } from '../test/select'
import { ImportPage } from './ImportPage'

// PastImports reads the browser's zone once, when it loads (GLG-1175), so the
// past-imports test's Istanbul has to be in place before the imports above
// run. The other tests keep the default zone.
vi.hoisted(() => vi.stubEnv('TZ', 'Europe/Istanbul'))
beforeAll(() => vi.unstubAllEnvs())

// jsdom has no matchMedia. The preview renders the table or the phone list by
// viewport (GLG-461), so tests get the table unless they set `desktop`.
let desktop = true

// jsdom implements neither method on <dialog>; both only flip the open state.
// Nor scrollIntoView, which a row the commit refuses calls (GLG-1065).
beforeAll(() => {
  Element.prototype.scrollIntoView = () => {}
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

// One preview with a row per status, which is what the page has to tell apart.
const preview = {
  broker: 'midas',
  sha256: 'abc123',
  period_start: '2025-12-01',
  period_end: '2025-12-31',
  currency: 'USD',
  warnings: [
    {
      code: 'overlap',
      message: 'period overlaps an earlier import (2025-12-01 – 2025-12-31)',
      import_id: 3,
      period_start: '2025-12-01',
      period_end: '2025-12-31',
    },
  ],
  rows: [
    {
      key: 'trade-0',
      status: 'ok',
      type: 'buy',
      date: '2025-12-26',
      symbol: 'VTI',
      instrument_id: 123,
      quantity: '2',
      price: '331.82',
      amount: '663.64',
      fee: '0',
      currency: 'USD',
    },
    {
      key: 'cash-0',
      status: 'ok',
      type: 'deposit',
      date: '2025-12-09',
      amount: '8237.46',
      fee: '0',
      currency: 'USD',
      description: 'Garanti BBVA',
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
    {
      key: 'skip-0',
      status: 'skipped',
      reason: 'fx conversion',
      description: '41,18 USD/TRY',
      raw: ['09/12/25 10:41:48', 'Döviz Satış', '41,18 USD/TRY kurundan 8.237,46 dolar satış'],
    },
    {
      key: 'err-0',
      status: 'error',
      section: 'YATIRIM İŞLEMLERİ',
      message: 'quantity: invalid number',
      date: '2025-12-30',
      raw: ['30/12/25', 'Alış', 'VTI', '?'],
    },
  ],
  holdings: [
    { symbol: 'VTI', quantity: '2', avg_cost: '331.82', currency: 'USD' },
    { symbol: 'QQQ', quantity: '1', avg_cost: '587.42', currency: 'USD' },
  ],
  // GLG-207: the statement's snapshot against the computed position -- one
  // match, one symbol the portfolio is short of, and one position the
  // statement never mentions.
  reconciliation: [
    { symbol: 'VTI', statement_quantity: '2', portfolio_quantity: '2', match: true },
    { symbol: 'QQQ', statement_quantity: '1', portfolio_quantity: '0', match: false },
    { symbol: 'AAPL', statement_quantity: null, portfolio_quantity: '3', match: false },
  ],
}

function portfoliosResponse() {
  return new Response(JSON.stringify([{ id: 1, name: 'Emeklilik' }]), { status: 200 })
}

// The instrument the user picks for the statement's unknown symbol.
const xyz = { id: 777, symbol: 'XYZ', market: 'US', name: 'XYZ Holding', currency: 'USD' }
// The fund the TEFAS lookup creates (GLG-733).
const gtl = {
  id: 888,
  symbol: 'GTL',
  market: 'TEFAS',
  name: 'GARANTİ PORTFÖY FON',
  currency: 'TRY',
}

// Mocks /api/portfolios (the sub-header), the instrument search behind the
// inline picker, and the parse endpoint; `parse` is what that POST answers
// with, given the uploaded file and the whole form.
function mockApi(
  parse: (file: File, form: FormData) => Response,
  onCommit?: (body: Record<string, unknown>) => Response,
  // The past-imports section at the bottom of the page lists these, and
  // records the id of an undo it is asked for (GLG-206).
  pastImports: Record<string, unknown>[] = [],
  onDelete?: (id: string) => void,
) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/api/portfolios' && method === 'GET') return portfoliosResponse()
    if (url.startsWith('/api/instruments?') && method === 'GET') {
      return new Response(JSON.stringify([xyz]), { status: 200 })
    }
    if (url === '/api/imports/funds' && method === 'POST') {
      const { code } = JSON.parse(init?.body as string) as { code: string }
      if (code !== 'GTL') return new Response('{"error":"fund not found"}', { status: 404 })
      return new Response(JSON.stringify(gtl), { status: 200 })
    }
    if (url === '/api/imports/parse' && method === 'POST') {
      const form = init?.body as FormData
      return parse(form.get('file') as File, form)
    }
    if (url === '/api/imports' && method === 'GET') {
      return new Response(JSON.stringify(pastImports), { status: 200 })
    }
    if (url === '/api/imports' && method === 'POST' && onCommit) {
      return onCommit(JSON.parse(init?.body as string) as Record<string, unknown>)
    }
    if (url.startsWith('/api/imports/') && method === 'DELETE') {
      onDelete?.(url.slice('/api/imports/'.length))
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${method} ${url}`)
  })
}

// Holds every request `when` matches until the returned release is called,
// which opens the gap a race needs (GLG-1063).
function hold(when: (url: string, method: string) => boolean) {
  const inner = vi.mocked(fetch).getMockImplementation()!
  let release!: () => void
  const held = new Promise<void>((resolve) => (release = resolve))
  vi.mocked(fetch).mockImplementation(async (input, init) => {
    if (when(String(input), init?.method ?? 'GET')) await held
    return inner(input, init)
  })
  return release
}

// Stands in for İşlemler, and shows what the import sent it: the query and
// the navigation state.
function TransactionsProbe() {
  const location = useLocation()
  return (
    <>
      <p>işlemler sayfası</p>
      <output data-testid="arrival">{`${location.search} ${JSON.stringify(location.state)}`}</output>
    </>
  )
}

// The page keeps Bitti per history entry (GLG-1072), and every MemoryRouter's
// first entry has the key "default": each test gets an entry of its own.
const importEntry = () => ({ pathname: '/portfolios/1/import', key: crypto.randomUUID() })

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[importEntry()]}>
        <Routes>
          <Route path="/portfolios/:id/import" element={<ImportPage />} />
          <Route path="/portfolios/:id/transactions" element={<TransactionsProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// The drop zone's file input; the zone itself opens it (GLG-965).
function fileInput() {
  return document.querySelector<HTMLInputElement>('input[type="file"]')!
}

// Step 1: pick the files, which starts the preview (GLG-965). The bytes never
// leave jsdom -- fetch is mocked -- so any content will do.
function upload(names = ['ekstre.pdf']) {
  const files = names.map((name) => new File(['%PDF-1.4'], name, { type: 'application/pdf' }))
  fireEvent.change(fileInput(), { target: { files } })
}

// One click of a run of clicks, as the browser counts them: the mousedown
// and the click carry the click's number in the run (GLG-1297).
function press(button: HTMLElement, detail: number) {
  fireEvent.mouseDown(button, { detail })
  fireEvent.click(button, { detail })
}

// İçe aktar, and "Yine de içe aktar" when rows that still need fixing would
// be left out (GLG-784).
function clickImport() {
  fireEvent.click(screen.getByRole('button', { name: 'İçe aktar' }))
  const anyway = screen.queryByRole('button', { name: 'Yine de içe aktar' })
  if (anyway) fireEvent.click(anyway)
}

// A pick requests the preview again, and İçe aktar waits for it (GLG-752).
async function importWhenReady() {
  const button = screen.getByRole('button', { name: 'İçe aktar' })
  await waitFor(() => expect(button).toBeEnabled())
  clickImport()
}

// The past imports and holdings panels share the page, so row queries are
// scoped to the preview table, the first of the two tables (GLG-793).
function table() {
  return within(screen.getAllByRole('table')[0])
}

// The row checkboxes, without the header's select-all (GLG-798).
function body() {
  return within(screen.getAllByRole('table')[0].querySelector('tbody')!)
}

// The boxes are ui/Checkbox, a span with role="checkbox" (GLG-963).
const isChecked = (box: HTMLElement) => box.getAttribute('aria-checked') === 'true'

// A preview with rows to fix opens on "Sorunlu" (GLG-966); a test about the
// other rows switches to "Tümü" first.
async function showAll() {
  fireEvent.click(await screen.findByRole('radio', { name: /^Tümü/ }))
}

// The raw line of a row opens on demand (GLG-966).
function openRawLines() {
  for (const button of screen.queryAllByRole('button', { name: 'Satırı göster' })) {
    fireEvent.click(button)
  }
}

describe('ImportPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    desktop = true
  })

  // GLG-965: the steps above, the upload and the history side by side from
  // lg with the upload first, and a chosen file goes straight to the preview.
  it('lays out step 1 and starts the preview as soon as a file is chosen', async () => {
    const parsed: string[] = []
    mockApi((file) => {
      parsed.push(file.name)
      return new Response(JSON.stringify(preview), { status: 200 })
    })
    renderPage()

    const current = () => screen.getAllByRole('listitem').find((item) => item.ariaCurrent)
    expect(current()).toHaveTextContent('Dosya seç')
    const upload = screen.getByRole('region', { name: 'Ekstre yükle' })
    const history = screen.getByRole('region', { name: 'Geçmiş içe aktarmalar' })
    expect(upload.parentElement).toBe(history.parentElement)
    expect(upload.parentElement).toHaveClass('grid', 'lg:grid-cols-[5fr_4fr]')
    expect(upload.parentElement!.firstElementChild).toBe(upload)
    expect(within(upload).getByText(/Midas, Garanti BBVA ve Garanti BBVA Yatırım/)).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Önizle' })).not.toBeInTheDocument()

    const zone = within(upload).getByRole('button', { name: /Dosyaları buraya bırak/ })
    const file = new File(['%PDF-1.4'], 'ocak.pdf', { type: 'application/pdf' })
    fireEvent.drop(zone, { dataTransfer: { files: [file], items: [], types: ['Files'] } })

    expect(await screen.findByRole('heading', { name: 'ocak.pdf' })).toBeInTheDocument()
    expect(parsed).toEqual(['ocak.pdf'])
    expect(current()).toHaveTextContent('İncele')
    expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('(tamamlandı)')
  })

  // Review Focus 1: a second drop while the first file is read is refused
  // with a message, never lost or stacked.
  it('refuses a second drop while a preview is being read', async () => {
    let release = () => {}
    const pending = new Promise<Response>((resolve) => {
      release = () => resolve(new Response(JSON.stringify(preview), { status: 200 }))
    })
    let calls = 0
    mockApi(() => {
      calls++
      return pending as unknown as Response
    })
    renderPage()

    upload()
    const zone = screen.getByRole('button', { name: /Dosyaları buraya bırak/ })
    await waitFor(() => expect(zone).toHaveAttribute('aria-disabled', 'true'))
    expect(screen.getByRole('status')).toHaveTextContent('Ekstre okunuyor...')
    const second = new File(['%PDF-1.4'], 'subat.pdf', { type: 'application/pdf' })
    fireEvent.drop(zone, { dataTransfer: { files: [second], items: [], types: ['Files'] } })
    expect(screen.getByRole('alert')).toHaveTextContent('Bir ekstre okunuyor')
    expect(calls).toBe(1)

    release()
    expect(await screen.findByRole('heading', { name: 'ekstre.pdf' })).toBeInTheDocument()
  })

  it('refuses a file of a type the chosen broker does not take', async () => {
    let calls = 0
    mockApi(() => {
      calls++
      return new Response(JSON.stringify(preview), { status: 200 })
    })
    renderPage()

    await chooseOption(screen.getByLabelText('Aracı kurum'), 'CSV')
    fireEvent.change(fileInput(), { target: { files: [new File(['%PDF'], 'a.pdf')] } })

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Seçilen aracı kurum bu dosya türünü almıyor: a.pdf',
    )
    expect(calls).toBe(0)
  })

  it('previews the statement with a checkbox state per row status', async () => {
    // The import the overlap warning names; a warning for an import no
    // longer listed is dropped (GLG-361).
    const overlapped = {
      id: 3,
      broker: 'midas',
      portfolio_id: 1,
      period_start: '2025-12-01',
      period_end: '2025-12-31',
      transaction_count: 4,
      created_at: '2026-01-02T09:30:00Z',
    }
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }), undefined, [overlapped])
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // GLG-966: one filter in place of the two notices, on Sorunlu on entry:
    // the unknown instrument and the unreadable row, each once.
    expect(screen.getByRole('radio', { name: 'Sorunlu 2' })).toHaveAttribute('aria-checked', 'true')
    expect(body().getAllByRole('row')).toHaveLength(2)
    expect(screen.queryByText(/elle düzeltilmeli/)).not.toBeInTheDocument()
    expect(screen.queryByText(/tam okunamadı/)).not.toBeInTheDocument()
    await showAll()
    expect(screen.getByRole('radio', { name: 'Tümü 5' })).toHaveAttribute('aria-checked', 'true')

    // ok rows are checked, the unknown instrument is present but blocked, and
    // the skipped and error rows have no checkbox at all. In date order
    // (GLG-966): 12.12, 20.12, 24.12, the error row's 30.12, the undated
    // skipped row last.
    const boxes = body().getAllByRole('checkbox')
    expect(boxes).toHaveLength(3)
    expect(boxes[0]).toBeChecked()
    expect(boxes[2]).toBeChecked()
    expect(boxes[1]).not.toBeChecked()
    expect(boxes[1]).toHaveAttribute('aria-disabled', 'true')
    const rows = body().getAllByRole('row')
    expect(rows[3]).toHaveTextContent('30.12.2025')
    expect(rows[4]).toHaveTextContent('Döviz işlemi, atlandı')

    // GLG-351: each box names its row -- date, symbol if any, and type.
    expect(
      table().getByRole('checkbox', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını seç',
      }),
    ).toBe(boxes[2])
    expect(
      table().getByRole('checkbox', { name: '09.12.2025 Para yatırma $8.237,46 satırını seç' }),
    ).toBe(boxes[0])
    // GLG-365: the edit buttons name their row the same way; the skipped row
    // has no date, symbol or type and keeps the plain name.
    expect(
      table().getByRole('button', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını düzenle',
      }),
    ).toBeInTheDocument()
    expect(table().getByRole('button', { name: 'Satırı düzenle' })).toBeInTheDocument()

    expect(table().getByText('Enstrüman bulunamadı: XYZ')).toBeInTheDocument()
    expect(table().getByText('Döviz işlemi, atlandı')).toBeInTheDocument()
    expect(table().getByText('quantity: invalid number')).toBeInTheDocument()
    openRawLines()
    expect(table().getByText('30/12/25 | Alış | VTI | ?')).toBeInTheDocument()

    // The overlap warning in Turkish with tr dates (GLG-328) and the period
    // rather than the import's id (GLG-361), the holdings panel and the two
    // counters. An overlap does not block the commit.
    expect(
      screen.getByText(
        'Bu dönem, daha önce içe aktarılan 01.12.2025 – 31.12.2025 dönemiyle çakışıyor.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled()
    expect(screen.getByText('Ekstreye göre pozisyonlar')).toBeInTheDocument()
    expect(screen.getByText('2 satır seçildi')).toBeInTheDocument()
    expect(screen.getByText('2 satır aktarılmayacak')).toBeInTheDocument()
  })

  // GLG-966: nothing to fix opens on Tümü; the compact table's nine columns,
  // quantity and price in one cell and the fee under Kesinti.
  it('opens on Tümü without rows to fix and lays the table out in nine columns', async () => {
    const rows = [{ ...preview.rows[0], fee: '1.5' }, preview.rows[1]]
    mockApi(() => new Response(JSON.stringify({ ...preview, rows }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(screen.getByRole('radio', { name: 'Tümü 2' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: 'Sorunlu 0' })).toHaveAttribute(
      'aria-checked',
      'false',
    )
    expect(
      table()
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual([
      '',
      'Tarih',
      'Tür',
      'Enstrüman',
      'Adet × fiyat',
      'Kesinti',
      'Tutar',
      'Durum',
      'Eylemler',
    ])
    const vti = within(body().getAllByRole('row')[1])
    expect(vti.getByText('2 × $331,82')).toBeInTheDocument()
    expect(vti.getByText('$1,50')).toBeInTheDocument()
  })

  // GLG-966: once the last row to fix is fixed, Sorunlu would be empty, so
  // every row shows; the phone list takes the same filter.
  it('shows every row once the last row to fix is fixed', async () => {
    const rows = [preview.rows[0], preview.rows[2]]
    mockApi(() => new Response(JSON.stringify({ ...preview, warnings: [], rows }), { status: 200 }))
    desktop = false
    renderPage()

    upload()
    await screen.findByRole('radio', { name: 'Sorunlu 1' })
    expect(screen.getAllByRole('checkbox', { name: /satırını seç$/ })).toHaveLength(1)

    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    expect(screen.getByRole('radio', { name: 'Tümü 2' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getAllByRole('checkbox', { name: /satırını seç$/ })).toHaveLength(2)
  })

  // GLG-415: a dividend row the server matched to a hand-entered dividend
  // warns with that dividend's date and starts unchecked; the user can still
  // check it.
  it('starts a likely duplicate dividend unchecked with a warning', async () => {
    const dividend = {
      key: 'div-0',
      status: 'ok',
      type: 'dividend',
      date: '2025-12-20',
      symbol: 'VTI',
      instrument_id: 123,
      amount: '2.40',
      fee: '0.48',
      currency: 'USD',
      duplicate_of: '2025-12-18',
    }
    mockApi(
      () =>
        new Response(JSON.stringify({ ...preview, warnings: [], rows: [dividend] }), {
          status: 200,
        }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const box = body().getByRole('checkbox')
    expect(box).not.toBeChecked()
    expect(box).not.toHaveAttribute('aria-disabled')
    expect(table().getByText('18.12.2025 tarihli temettünün tekrarı olabilir')).toBeInTheDocument()
    expect(screen.getByText('0 satır seçildi')).toBeInTheDocument()

    fireEvent.click(box)
    expect(box).toBeChecked()
  })

  // GLG-738: the opening rows the preview proposes (GLG-737) carry a pill and
  // sit under one line saying what they are; they start checked like any row.
  // Priced from two sources, the line names neither (GLG-1185).
  it('groups the proposed opening rows under one explanation', async () => {
    const opening = { status: 'ok', date: '2025-11-30', fee: '0', currency: 'TRY', opening: true }
    const rows = [
      { ...opening, key: 'open-cash-0', type: 'deposit', amount: '1500.00' },
      {
        ...opening,
        key: 'open-0',
        type: 'buy',
        symbol: 'THYAO',
        instrument_id: 5,
        quantity: '10',
        price: '100',
        amount: '1000',
      },
      {
        ...opening,
        key: 'open-1',
        type: 'buy',
        symbol: 'ASELS',
        instrument_id: 6,
        quantity: '5',
        price: '100',
        amount: '500',
        approximate_price: true,
      },
      { ...preview.rows[0] },
    ]
    mockApi(() => new Response(JSON.stringify({ ...preview, warnings: [], rows }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getAllByText('Açılış')).toHaveLength(3)
    const hint = table().getAllByText(
      'Bu ekstre, portföyde kaydı olmayan varlıklarla başlıyor. Açılış pozisyonları önerildi; fiyatlarını kontrol edip düzenleyebilirsin.',
    )
    expect(hint).toHaveLength(1)
    // Above the first opening row, across the table.
    expect(hint[0].closest('td')).toHaveAttribute('colspan', '9')
    expect(table().getAllByRole('row')[1]).toContainElement(hint[0])
    expect(table().getAllByText('Fiyat dönem sonu fiyatıdır, düzenleyebilirsin.')).toHaveLength(1)
    for (const box of body().getAllByRole('checkbox')) expect(box).toBeChecked()
  })

  // GLG-1185: opening buys priced with the statement's average cost (Midas)
  // say so, above the group and on each row, not the day-before price.
  it('names the average cost as the price of opening rows that use it', async () => {
    const opening = { status: 'ok', date: '2025-08-31', fee: '0', currency: 'USD', opening: true }
    const rows = [
      { ...opening, key: 'open-cash-USD', type: 'deposit', amount: '1500' },
      {
        ...opening,
        key: 'open-5',
        type: 'buy',
        symbol: 'QQQ',
        instrument_id: 5,
        quantity: '3',
        price: '500',
        amount: '1500',
        cost_price: true,
      },
      { ...preview.rows[0] },
    ]
    mockApi(() => new Response(JSON.stringify({ ...preview, warnings: [], rows }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(
      table().getByText(
        'Bu ekstre, portföyde kaydı olmayan varlıklarla başlıyor. Açılış pozisyonları, ekstredeki ortalama maliyetle önerildi.',
      ),
    ).toBeInTheDocument()
    expect(table().queryByText(/dönem başından bir gün önceki/)).not.toBeInTheDocument()
    expect(table().getByText('Fiyat ekstredeki ortalama maliyettir.')).toBeInTheDocument()
  })

  // GLG-1190: a closing sell for a position the statement omits carries its
  // pill and line and starts unchecked; checked, it closes that position's
  // reconciliation line, and its withdrawal follows it so the cash line
  // still matches.
  it('proposes closing rows unchecked under their own explanation', async () => {
    const closing = {
      key: 'close-9',
      status: 'ok',
      type: 'sell',
      date: '2025-11-30',
      symbol: 'AAPL',
      instrument_id: 9,
      quantity: '3',
      price: '200',
      amount: '600',
      fee: '0',
      currency: 'USD',
      closing: true,
    }
    const withdrawal = {
      key: 'close-cash-USD',
      status: 'ok',
      type: 'withdrawal',
      date: '2025-11-30',
      amount: '600',
      fee: '0',
      currency: 'USD',
      closing: true,
    }
    const reconciliation = preview.reconciliation.map((entry) =>
      entry.symbol === 'AAPL' ? { ...entry, instrument_id: 9 } : entry,
    )
    const cash = { currency: 'USD', statement_amount: '100', portfolio_amount: '100', match: true }
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            warnings: [],
            rows: [closing, withdrawal, preview.rows[0]],
            reconciliation,
            cash_reconciliation: cash,
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getAllByText('Kapanış')).toHaveLength(2)
    const hint = table().getByText(
      'Portföyde olup ekstrede olmayan pozisyonlar için kapanış satırları önerildi; önceki ayları içe aktarmadıysan bunları seçme.',
    )
    expect(table().getAllByRole('row')[1]).toContainElement(hint)
    const [box, cashBox, other] = body().getAllByRole('checkbox')
    expect(box).not.toBeChecked()
    expect(cashBox).toBeChecked()
    expect(other).toBeChecked()

    const panel = () => within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const cashLine = () => panel().getByText('Nakit').closest('tr')!
    expect(panel().getByText('AAPL')).toBeInTheDocument()
    expect(within(cashLine()).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    fireEvent.click(box)
    expect(panel().queryByText('AAPL')).not.toBeInTheDocument()
    expect(within(cashLine()).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
  })

  it('asks for a price on an opening buy that has none', async () => {
    const row = {
      key: 'open-0',
      status: 'error',
      type: 'buy',
      date: '2025-11-30',
      symbol: 'THYAO',
      instrument_id: 5,
      quantity: '10',
      fee: '0',
      currency: 'TRY',
      opening: true,
      code: 'opening_price',
      message: 'no price for the opening position',
    }
    mockApi(
      () =>
        new Response(JSON.stringify({ ...preview, warnings: [], rows: [row] }), { status: 200 }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(
      table().getByText('Açılış pozisyonu için fiyat bulunamadı, fiyatı gir'),
    ).toBeInTheDocument()
    expect(table().getByText('Açılış')).toBeInTheDocument()
  })

  // GLG-215: the Durum column is ~60px wide, so a statement line pushed into
  // it wrapped to a 40-line strip. It gets the table's full width instead,
  // under its row and on demand (GLG-966).
  it('puts the raw line of a skipped row on a row that spans the table', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await showAll()

    const text = '09/12/25 10:41:48 | Döviz Satış | 41,18 USD/TRY kurundan 8.237,46 dolar satış'
    expect(table().queryByText(text)).not.toBeInTheDocument()
    const skipped = table().getByText('Döviz işlemi, atlandı').closest('tr')!
    const show = within(skipped).getByRole('button', { name: 'Satırı göster' })
    expect(show).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(show)
    expect(within(skipped).getByRole('button', { name: 'Satırı gizle' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    const raw = table().getByText(text)
    expect(raw.closest('tr')).toBe(skipped.nextElementSibling)
    expect(raw.closest('td')).toHaveAttribute('colspan', '9')
    // GLG-761: `faint` fails WCAG AA contrast; the raw line and the skipped
    // row are readable text, so neither carries it.
    expect(raw).not.toHaveClass('text-faint')
    expect(raw.closest('tr')).not.toHaveClass('text-faint')
    // The status text stays where the column header promises it.
    expect(table().getByText('Döviz işlemi, atlandı').closest('td')).not.toHaveAttribute('colspan')
  })

  // GLG-798: one box over the committable rows checks or clears them all,
  // and shows a mix as indeterminate.
  it('selects all or none of the rows that can be imported', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await showAll()

    const all = table().getByRole('checkbox', { name: 'Tümünü seç' })
    const rows = () => body().getAllByRole('checkbox')
    expect(all).toBeChecked()

    fireEvent.click(all)
    expect(rows().map(isChecked)).toEqual([false, false, false])
    expect(screen.getByText('0 satır seçildi')).toBeInTheDocument()

    fireEvent.click(rows()[0])
    expect(all).not.toBeChecked()
    expect(all).toBePartiallyChecked()

    // The unknown instrument stays unchecked: it cannot be imported yet.
    fireEvent.click(all)
    expect(rows().map(isChecked)).toEqual([true, false, true])
    expect(all).not.toBePartiallyChecked()
  })

  // GLG-787: the rows to fix come first, now through the Sorunlu filter the
  // review opens on (GLG-966); the edit dialog repeats the statement line it
  // covers.
  it('points to the rows to fix and shows the statement line in the dialog', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // Only the rows to fix, the unknown XYZ first; its instrument search is
    // above the table (GLG-1193).
    const rows = body().getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('Enstrüman bulunamadı: XYZ')
    expect(screen.getByLabelText('XYZ için enstrüman seç')).toBeInTheDocument()

    fireEvent.click(table().getByRole('button', { name: /^30\.12\.2025 .*düzenle$/ }))
    const dialog = within(screen.getByRole('dialog'))
    expect(dialog.getByText('quantity: invalid number')).toBeInTheDocument()
    expect(dialog.getByText('30/12/25 | Alış | VTI | ?')).toBeInTheDocument()
  })

  // GLG-454: the phone row showed a bare dd/MM, so a row from an old
  // statement read as this year's. It names the month and keeps the year.
  it('shows the year of an old row in the phone list', async () => {
    const old = {
      ...preview,
      period_start: '1990-01-01',
      period_end: '1990-01-31',
      warnings: [],
      rows: [{ ...preview.rows[0], date: '1990-01-02' }],
    }
    mockApi(() => new Response(JSON.stringify(old), { status: 200 }))
    desktop = false
    renderPage()

    upload()
    expect(await screen.findByText(/^02 Oca 1990 · /)).toBeInTheDocument()
    // GLG-791: the detail line wraps instead of cutting into the price, and
    // the action row sticks to the bottom of the screen, a phone's too
    // (GLG-966). jsdom has no layout, so the classes stand in.
    expect(screen.getByText(/^02 Oca 1990 · /)).not.toHaveClass('truncate')
    expect(screen.getByTestId('preview-actions')).toHaveClass('sticky', 'bottom-0')
    // GLG-837: the bar's add button keeps its label on one line, so it does
    // not spill out of the fixed-height box.
    expect(
      within(screen.getByTestId('preview-actions')).getByRole('button', { name: 'Satır ekle' }),
    ).toHaveClass('whitespace-nowrap')
  })

  // GLG-461: a multi-year statement has hundreds of rows, and building the
  // hidden layout too doubled the DOM.
  it('builds only the preview layout that matches the viewport', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    const wide = renderPage()

    upload()
    const card = (await screen.findAllByRole('table'))[0].parentElement!
    expect(card.children).toHaveLength(1)
    expect(within(card).queryByRole('list')).not.toBeInTheDocument()
    wide.unmount()

    desktop = false
    renderPage()
    upload()
    await showAll()
    // The holdings check is the only table left (GLG-793).
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table').closest('section')).toHaveTextContent(
      'Ekstreye göre pozisyonlar',
    )
    expect(screen.getAllByRole('checkbox', { name: /satırını seç$/ })).toHaveLength(3)
  })

  it('keeps the edit column reachable when the table outgrows its card', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await showAll()

    // GLG-417: a wider table scrolls inside its card rather than clipping the
    // edit buttons. The description is cut in the instrument column, the
    // one that takes the room the others leave (w-full max-w-0), so it never
    // widens the table (GLG-966). jsdom has no layout, so the classes stand
    // in for the measured widths.
    expect(screen.getAllByRole('table')[0].parentElement).toHaveClass('overflow-x-auto')
    // GLG-689: positioned, so the absolute sr-only header labels are clipped by
    // the card instead of widening the page at 768 px.
    expect(screen.getAllByRole('table')[0].parentElement).toHaveClass('relative')
    const description = table().getByText('Garanti BBVA')
    expect(description).toHaveClass('truncate')
    expect(description.closest('td')).toHaveClass('w-full', 'max-w-0')
  })

  it('reconciles the statement snapshot against the portfolio and warns on a mismatch', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const lines = panel.getAllByRole('row').slice(1)
    expect(lines).toHaveLength(3)
    const cells = (line: HTMLElement) =>
      within(line)
        .getAllByRole('cell')
        .map((cell) => cell.textContent)

    // The matching line carries the check, both quantities, a zero difference
    // and the avg cost the statement printed.
    expect(within(lines[0]).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    expect(cells(lines[0]).slice(1)).toEqual(['2', '2', '0'])
    expect(lines[0]).toHaveTextContent('ort. maliyet')

    // GLG-793: the difference is statement minus portfolio, signed, and
    // stands out only when it is not zero.
    expect(within(lines[1]).getByRole('img', { name: 'Uyuşmuyor' })).toBeInTheDocument()
    expect(cells(lines[1]).slice(1)).toEqual(['1', '0', '+1'])
    expect(within(lines[1]).getByText('+1')).toHaveClass('text-warn')
    expect(within(lines[0]).getAllByRole('cell')[3]).not.toHaveClass('text-warn')

    // A position the statement does not mention has no statement quantity.
    expect(lines[2]).toHaveTextContent('AAPL')
    expect(cells(lines[2]).slice(1)).toEqual(['—', '3', '-3'])

    // Warned, not blocked.
    expect(
      screen.getByText('Ekstre ile portföy uyuşmuyor, önceki ayları içe aktarmamış olabilirsin.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled()
  })

  // GLG-755: Garanti statements print no average cost, so their holdings
  // carry "0" and the line is left out rather than showing a zero cost.
  it('leaves out the avg cost of a holding the statement printed none for', async () => {
    const noCost = {
      ...preview,
      holdings: [{ symbol: 'VTI', quantity: '2', avg_cost: '0', currency: 'TRY' }],
      reconciliation: [
        { symbol: 'VTI', statement_quantity: '2', portfolio_quantity: '2', match: true },
      ],
    }
    mockApi(() => new Response(JSON.stringify(noCost), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const lines = panel.getAllByRole('row').slice(1)
    expect(lines).toHaveLength(1)
    expect(lines[0]).toHaveTextContent('VTI')
    expect(lines[0]).not.toHaveTextContent('ort. maliyet')
  })

  // GLG-419: the statement's cash balance is the panel's last line, in its
  // currency, and a mismatch there warns like a position does.
  it('shows the cash line and warns when the cash balance does not reconcile', async () => {
    const cashOff = {
      ...preview,
      reconciliation: [
        { symbol: 'VTI', statement_quantity: '2', portfolio_quantity: '2', match: true },
      ],
      cash_reconciliation: {
        currency: 'USD',
        statement_amount: '1.64',
        portfolio_amount: '0.64',
        match: false,
      },
    }
    mockApi(() => new Response(JSON.stringify(cashOff), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const lines = panel.getAllByRole('row').slice(1)
    expect(lines).toHaveLength(2)
    expect(within(lines[1]).getByRole('img', { name: 'Uyuşmuyor' })).toBeInTheDocument()
    expect(lines[1]).toHaveTextContent('Nakit')
    expect(
      within(lines[1])
        .getAllByRole('cell')
        .slice(1)
        .map((cell) => cell.textContent),
    ).toEqual([
      formatMoney('1.64', 'USD'),
      formatMoney('0.64', 'USD'),
      `+${formatMoney('1', 'USD')}`,
    ])
    expect(
      screen.getByText('Ekstre ile portföy uyuşmuyor, önceki ayları içe aktarmamış olabilirsin.'),
    ).toBeInTheDocument()
  })

  // GLG-793: the cash line matches within a cent (GLG-419), so a rounding
  // difference is shown but not highlighted next to the check.
  it('highlights a difference only on a line that does not match', async () => {
    const rounding = {
      ...preview,
      reconciliation: [],
      cash_reconciliation: {
        currency: 'USD',
        statement_amount: '1000.004',
        portfolio_amount: '1000',
        match: true,
      },
    }
    mockApi(() => new Response(JSON.stringify(rounding), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const [line] = panel.getAllByRole('row').slice(1)
    expect(within(line).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    expect(within(line).getAllByRole('cell')[3]).not.toHaveClass('text-warn')
  })

  it('leaves out the mismatch warning when every position reconciles', async () => {
    const matching = {
      ...preview,
      reconciliation: [
        { symbol: 'VTI', statement_quantity: '2', portfolio_quantity: '2', match: true },
      ],
    }
    mockApi(() => new Response(JSON.stringify(matching), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(
      screen.queryByText('Ekstre ile portföy uyuşmuyor, önceki ayları içe aktarmamış olabilirsin.'),
    ).not.toBeInTheDocument()
  })

  // GLG-418: on a same_period statement the portfolio side already counts
  // the earlier import, so the mismatch does not point at missing months.
  it('drops the previous-months hint on a same_period statement', async () => {
    const again = {
      ...preview,
      warnings: [
        {
          code: 'same_period',
          message: 'period already imported (2025-12-01 – 2025-12-31), undo that import first',
          import_id: 6,
          period_start: '2025-12-01',
          period_end: '2025-12-31',
        },
      ],
    }
    const past = [
      {
        id: 6,
        broker: 'midas',
        portfolio_id: 1,
        period_start: '2025-12-01',
        period_end: '2025-12-31',
        transaction_count: 2,
        created_at: '2026-01-02T09:30:00Z',
      },
    ]
    mockApi(() => new Response(JSON.stringify(again), { status: 200 }), undefined, past)
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    expect(panel.getAllByRole('img', { name: 'Uyuşmuyor' })).toHaveLength(2)
    expect(
      screen.queryByText('Ekstre ile portföy uyuşmuyor, önceki ayları içe aktarmamış olabilirsin.'),
    ).not.toBeInTheDocument()
  })

  // GLG-1191: the panel follows the rows as İçe aktar would send them, not
  // the rows the preview started checked. Unchecking the VTI buy leaves VTI
  // and the cash short; pricing the QQQ opening buy, an error row until
  // then, makes QQQ match.
  it('recomputes the reconciliation from the checked and edited rows', async () => {
    const edited = {
      ...preview,
      warnings: [],
      rows: [
        {
          key: 'open-55',
          status: 'error',
          type: 'buy',
          date: '2025-11-30',
          symbol: 'QQQ',
          instrument_id: 55,
          quantity: '1',
          fee: '0',
          currency: 'USD',
          opening: true,
          code: 'opening_price',
          message: 'no price for the opening position',
        },
        preview.rows[0],
        preview.rows[1],
      ],
      reconciliation: [
        {
          symbol: 'VTI',
          instrument_id: 123,
          statement_quantity: '2',
          portfolio_quantity: '2',
          match: true,
        },
        {
          symbol: 'QQQ',
          instrument_id: 55,
          statement_quantity: '1',
          portfolio_quantity: '0',
          match: false,
        },
      ],
      cash_reconciliation: {
        currency: 'USD',
        statement_amount: '7573.82',
        portfolio_amount: '7573.82',
        match: true,
      },
    }
    mockApi(() => new Response(JSON.stringify(edited), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')
    await showAll()
    const panel = () => within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    const line = (name: string) =>
      panel()
        .getAllByRole('row')
        .find((row) => row.textContent?.includes(name))!
    const cells = (name: string) =>
      within(line(name))
        .getAllByRole('cell')
        .slice(1)
        .map((cell) => cell.textContent)
    const warning = 'Ekstre ile portföy uyuşmuyor, önceki ayları içe aktarmamış olabilirsin.'

    expect(within(line('VTI')).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    fireEvent.click(body().getAllByRole('checkbox')[1])
    expect(within(line('VTI')).getByRole('img', { name: 'Uyuşmuyor' })).toBeInTheDocument()
    expect(cells('VTI')).toEqual(['2', '0', '+2'])
    expect(within(line('Nakit')).getByRole('img', { name: 'Uyuşmuyor' })).toBeInTheDocument()
    fireEvent.click(body().getAllByRole('checkbox')[1])
    expect(within(line('VTI')).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    expect(screen.getByText(warning)).toBeInTheDocument()

    fireEvent.click(
      table().getByRole('button', {
        name: '30.11.2025 QQQ Alış 1 satırını düzenle',
      }),
    )
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '500' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() =>
      expect(within(line('QQQ')).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument(),
    )
    expect(cells('QQQ')).toEqual(['1', '1', '0'])
    // The opening deposit the page adds pays for the buy, so cash still
    // matches, and so does every line.
    expect(within(line('Nakit')).getByRole('img', { name: 'Eşleşiyor' })).toBeInTheDocument()
    expect(screen.queryByText(warning)).not.toBeInTheDocument()
  })

  it('commits only the checked rows and goes to the transactions page', async () => {
    let committed: Record<string, unknown> | null = null
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      (body) => {
        committed = body
        return new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 1,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    renderPage()
    // The app shell mounts the Toaster; the page alone does not.
    render(<Toaster />)

    upload()
    await showAll()

    // Drop the deposit; the buy is the only row left checked.
    fireEvent.click(body().getAllByRole('checkbox')[0])
    expect(screen.getByText('1 satır seçildi')).toBeInTheDocument()
    // The unknown symbol and the error row are not checked (GLG-784).
    expect(screen.getByText('2 satır aktarılmayacak')).toBeInTheDocument()

    // They are asked about before anything is sent.
    fireEvent.click(screen.getByRole('button', { name: 'İçe aktar' }))
    const ask = screen.getByRole('dialog', { name: '2 satır aktarılmayacak' })
    expect(committed).toBeNull()
    fireEvent.click(within(ask).getByRole('button', { name: 'Yine de içe aktar' }))
    await screen.findByText('işlemler sayfası')
    // İşlemler opens on the new import's rows, with its banner (GLG-936).
    expect(screen.getByTestId('arrival').textContent).toBe('?import=7 {"imported":7}')
    expect(
      await screen.findByText('1 işlem içe aktarıldı, 2 satır dışarıda kaldı'),
    ).toBeInTheDocument()

    expect(committed).toEqual({
      broker: 'midas',
      portfolio_id: 1,
      sha256: 'abc123',
      currency: 'USD',
      period_start: '2025-12-01',
      period_end: '2025-12-31',
      rows: [
        {
          type: 'buy',
          date: '2025-12-26',
          amount: '663.64',
          fee: '0',
          currency: 'USD',
          instrument_id: 123,
          quantity: '2',
          price: '331.82',
        },
      ],
    })
  })

  // GLG-772: Vazgeç on an untouched preview just closes it; once a row was
  // changed it asks first, and keeping the preview keeps the change.
  it('asks before Vazgeç drops changes made to the preview', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    upload()
    await showAll()
    fireEvent.click(body().getAllByRole('checkbox')[0])
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    const confirmDialog = await screen.findByRole('dialog', {
      name: 'İçe aktarmadan vazgeçilsin mi?',
    })
    expect(confirmDialog).toHaveTextContent(
      'Bu ekstrede yaptığın seçimler ve düzenlemeler kaybolacak.',
    )

    fireEvent.click(within(confirmDialog).getByRole('button', { name: 'Vazgeç' }))
    await waitFor(() => expect(confirmDialog).not.toHaveAttribute('open'))
    expect(screen.getByText('1 satır seçildi')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    fireEvent.click(await screen.findByRole('button', { name: 'İçe aktarmayı bırak' }))
    await waitFor(() => expect(screen.queryByRole('table')).not.toBeInTheDocument())
  })

  // GLG-360: a month with nothing in it is still recorded, so it shows under
  // past imports and a re-upload of the period is caught.
  it('commits an empty statement with no rows', async () => {
    const empty = { ...preview, warnings: [], rows: [], holdings: [], reconciliation: [] }
    let committed: Record<string, unknown> | null = null
    mockApi(
      () => new Response(JSON.stringify(empty), { status: 200 }),
      (body) => {
        committed = body
        return new Response(
          JSON.stringify({
            id: 9,
            transaction_count: 0,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    renderPage()

    upload()
    await screen.findByText('Ekstrede işlem bulunamadı.')

    const button = screen.getByRole('button', { name: 'İçe aktar' })
    expect(button).toBeEnabled()
    fireEvent.click(button)
    await screen.findByText('işlemler sayfası')

    expect(committed).toMatchObject({ sha256: 'abc123', rows: [] })
  })

  it('names the row and the field when the commit rejects a row', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () =>
        new Response(
          JSON.stringify({ error: 'rows[1].date: must be within 2025-12-01 – 2025-12-31' }),
          { status: 400 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // rows[1] is the deposit, the first row in date order (GLG-966).
    clickImport()
    expect(
      await screen.findByText(
        '1. satır: Tarih ekstre dönemi içinde olmalı (2025-12-01 – 2025-12-31).',
      ),
    ).toBeInTheDocument()
    // Still on the preview, so the rows can be fixed and sent again.
    expect(screen.getAllByRole('table')[0]).toBeInTheDocument()
  })

  // GLG-966: the review opened on Sorunlu, which never shows the checked ok
  // row the server refused, so the error switches to Tümü where the named
  // row sits at its number.
  it('shows every row when the commit rejects a row on Sorunlu', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () =>
        new Response(
          JSON.stringify({ error: 'rows[1].date: must be within 2025-12-01 – 2025-12-31' }),
          { status: 400 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')
    expect(screen.getByRole('radio', { name: 'Sorunlu 2' })).toHaveAttribute('aria-checked', 'true')

    clickImport()
    await screen.findByText(
      '1. satır: Tarih ekstre dönemi içinde olmalı (2025-12-01 – 2025-12-31).',
    )
    expect(screen.getByRole('radio', { name: 'Tümü 5' })).toHaveAttribute('aria-checked', 'true')
    expect(body().getAllByRole('row')[0]).toHaveTextContent('09.12.2025')
    expect(body().getAllByRole('row')[0]).toHaveTextContent('Para yatırma')
  })

  // GLG-1018: rows[i] counts the rows sent, not the rows shown. With the
  // first row unchecked, the server's rows[1] is the third row on screen.
  it('names the row on screen when an earlier row is left out of the commit', async () => {
    const third = { ...preview.rows[1], key: 'cash-1', date: '2026-01-15', amount: '300' }
    mockApi(
      () =>
        new Response(JSON.stringify({ ...preview, rows: [...preview.rows.slice(0, 2), third] }), {
          status: 200,
        }),
      () =>
        new Response(
          JSON.stringify({ error: 'rows[1].date: must be within 2025-12-01 – 2025-12-31' }),
          { status: 400 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')
    fireEvent.click(
      table().getByRole('checkbox', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını seç',
      }),
    )

    clickImport()
    expect(
      await screen.findByText(
        '3. satır: Tarih ekstre dönemi içinde olmalı (2025-12-01 – 2025-12-31).',
      ),
    ).toBeInTheDocument()
  })

  // GLG-1065: the commit error rendered below the table, out of view from a
  // long review. It is shown in the sticky bar, beside İçe aktar.
  it('shows a failed commit in the sticky bar', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')
    clickImport()

    const bar = within(screen.getByTestId('preview-actions'))
    expect(await bar.findByRole('alert')).toBeInTheDocument()
  })

  // GLG-1065: a refused row is marked by the error and scrolled into view,
  // in the table and in the phone list.
  it.each([true, false])(
    'marks and scrolls to the row a commit refuses (desktop %s)',
    async (wide) => {
      desktop = wide
      const scrollIntoView = vi.spyOn(Element.prototype, 'scrollIntoView')
      mockApi(
        () => new Response(JSON.stringify(preview), { status: 200 }),
        () =>
          new Response(
            JSON.stringify({ error: 'rows[1].date: must be within 2025-12-01 – 2025-12-31' }),
            { status: 400 },
          ),
      )
      renderPage()

      upload()
      await screen.findByRole('radio', { name: 'Tümü 5' })
      clickImport()

      const message = '1. satır: Tarih ekstre dönemi içinde olmalı (2025-12-01 – 2025-12-31).'
      const alert = await within(screen.getByTestId('preview-actions')).findByRole('alert')
      expect(alert).toHaveTextContent(message)
      // rows[1] is the deposit, the first row in date order (GLG-966).
      const marked = document.querySelector<HTMLElement>(
        '[aria-describedby="import-commit-error"]',
      )!
      expect(marked).toHaveTextContent('Para yatırma')
      expect(marked).toHaveAccessibleDescription(message)
      await waitFor(() => expect(scrollIntoView.mock.contexts.at(-1)).toBe(marked))

      // Once per error: the row remounted by the filter stays put.
      fireEvent.click(screen.getByRole('radio', { name: 'Sorunlu 2' }))
      fireEvent.click(screen.getByRole('radio', { name: 'Tümü 5' }))
      expect(document.querySelector('[aria-describedby="import-commit-error"]')).not.toBeNull()
      expect(scrollIntoView).toHaveBeenCalledTimes(1)
    },
  )

  // GLG-1065: only a failed commit is reset by a row action; resetting one in
  // flight dropped its onSuccess, so the import landed with no way on.
  it('still goes to the transactions page when a row is toggled during the commit', async () => {
    let release: (response: Response) => void = () => {}
    const held = new Promise<Response>((resolve) => (release = resolve))
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () => held as unknown as Response,
    )
    renderPage()

    upload()
    await showAll()
    clickImport()
    await screen.findByRole('button', { name: 'İçe aktarılıyor...' })
    fireEvent.click(body().getAllByRole('checkbox')[0])

    release(
      new Response(
        JSON.stringify({
          id: 7,
          transaction_count: 2,
          period_start: '2025-12-01',
          period_end: '2025-12-31',
        }),
        { status: 201 },
      ),
    )
    expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
  })

  // GLG-1299: outside a batch, Vazgeç right after İçe aktar reset the commit
  // in flight, so its import landed with no toast and no navigation.
  it('ignores Vazgeç while a single commit runs', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () =>
        new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 2,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        ),
    )
    renderPage()

    upload()
    await showAll()
    const release = hold((url, method) => url === '/api/imports' && method === 'POST')
    clickImport()
    await screen.findByRole('button', { name: 'İçe aktarılıyor...' })
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    release()

    expect(await screen.findByText('işlemler sayfası')).toBeInTheDocument()
  })

  // GLG-1065: a row action may fix the refused row, so the error and the
  // row's mark go with it rather than stay until the next İçe aktar.
  it('clears the commit error when the refused row is edited', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () =>
        new Response(
          JSON.stringify({ error: 'rows[1].date: must be within 2025-12-01 – 2025-12-31' }),
          { status: 400 },
        ),
    )
    renderPage()

    upload()
    await screen.findByRole('radio', { name: 'Tümü 5' })
    clickImport()
    await within(screen.getByTestId('preview-actions')).findByRole('alert')

    fireEvent.click(table().getByRole('button', { name: /^09\.12\.2025 .*satırını düzenle$/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() =>
      expect(within(screen.getByTestId('preview-actions')).queryByRole('alert')).toBeNull(),
    )
    expect(document.querySelector('[aria-describedby="import-commit-error"]')).toBeNull()
    expect(document.querySelector('.bg-loss-soft')).toBeNull()
  })

  it('shows the already-imported notice when the commit returns 409', async () => {
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      () => new Response(JSON.stringify({ error: 'already_imported' }), { status: 409 }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    clickImport()
    await screen.findByText('Bu ekstre zaten içe aktarılmış.')
    expect(screen.getByRole('link', { name: 'İşlemlere git' })).toHaveAttribute(
      'href',
      '/portfolios/1/transactions',
    )
  })

  it('shows the already-imported notice when the parse returns 409', async () => {
    mockApi(
      () =>
        new Response(JSON.stringify({ error: 'already_imported', import_id: 3 }), { status: 409 }),
    )
    renderPage()

    upload()
    await screen.findByText('Bu ekstre zaten içe aktarılmış.')

    expect(screen.getByRole('link', { name: 'İşlemlere git' })).toHaveAttribute(
      'href',
      '/portfolios/1/transactions',
    )
    // In the upload card, still on step 1, so the file can be swapped.
    expect(screen.getByRole('region', { name: 'Ekstre yükle' })).toContainElement(
      screen.getByRole('alert'),
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Dosyaları buraya bırak/ })).toHaveAttribute(
        'aria-disabled',
        'false',
      ),
    )
  })

  // GLG-1182: a statement is one account, so the file of another portfolio
  // names that portfolio and links to its transactions.
  it('names the other portfolio a parsed file is already imported into', async () => {
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            error: 'already_imported',
            import_id: 3,
            portfolio_id: 7,
            portfolio_name: 'Fonlar',
          }),
          { status: 409 },
        ),
    )
    renderPage()

    upload()
    await screen.findByText('Bu ekstre zaten Fonlar portföyüne aktarılmış.')
    expect(screen.getByRole('link', { name: 'İşlemlere git' })).toHaveAttribute(
      'href',
      '/portfolios/7/transactions',
    )
  })

  // GLG-599: CSV is picked like a broker and the field names what it reads.
  it('takes a CSV file and names its columns once CSV is picked', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    expect(fileInput()).toHaveAttribute('accept', 'application/pdf')
    await chooseOption(screen.getByLabelText('Aracı kurum'), 'CSV')

    expect(fileInput()).toHaveAttribute('accept', '.csv,text/csv')
    expect(screen.getByRole('button', { name: /Dosyaları buraya bırak/ })).toHaveTextContent(
      'CSV, birden fazla olabilir',
    )
    // The column list sits behind the "CSV biçimi" disclosure (GLG-965).
    const format = screen.getByText('CSV biçimi').closest('details')!
    expect(format).not.toHaveAttribute('open')
    expect(format).toHaveTextContent('date, type, symbol, quantity, price, amount, fee, currency')
  })

  // GLG-700: both Garanti PDFs are titled the same, so each option says which one it takes.
  it('names which Garanti statement each Garanti option takes', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    const broker = screen.getByLabelText('Aracı kurum')
    await chooseOption(broker, 'Garanti BBVA')
    expect(broker).toHaveAccessibleDescription(expect.stringContaining('fon bakiyeleri'))
    await chooseOption(broker, 'Garanti BBVA Yatırım')
    expect(fileInput()).toHaveAttribute('accept', 'application/pdf')
    expect(broker).toHaveAccessibleDescription(expect.stringContaining('hisse senedi alımları'))
  })

  // GLG-468: an action error is announced to screen readers.
  it('announces a failed parse', async () => {
    mockApi(() => new Response(JSON.stringify({ error: 'internal error' }), { status: 500 }))
    renderPage()

    upload()
    expect(await screen.findByRole('alert')).toHaveTextContent('Sunucu hatası.')
  })

  // GLG-361: a re-downloaded month has a new file hash, so it parses; the
  // preview names the period, points at the undo, and keeps İçe aktar
  // disabled until the earlier import is gone.
  it('blocks the commit of a statement already imported until it is undone', async () => {
    const past = [
      {
        id: 6,
        broker: 'midas',
        portfolio_id: 1,
        period_start: '2025-12-01',
        period_end: '2025-12-31',
        transaction_count: 2,
        created_at: '2026-01-02T09:30:00Z',
      },
    ]
    const again = {
      ...preview,
      warnings: [
        {
          code: 'same_period',
          message: 'period already imported (2025-12-01 – 2025-12-31), undo that import first',
          import_id: 6,
          period_start: '2025-12-01',
          period_end: '2025-12-31',
        },
      ],
    }
    // The undo empties the list the next GET answers with.
    mockApi(
      () => new Response(JSON.stringify(again), { status: 200 }),
      undefined,
      past,
      () => past.splice(0),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const notice =
      '01.12.2025 – 31.12.2025 dönemi bu portföye zaten içe aktarılmış. Yeniden aktarmak için önce aşağıdan eskisini geri al.'
    expect(screen.getByText(notice)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeDisabled()
    // A block is an error, not the advisory yellow (GLG-794).
    expect(screen.getByText(notice).closest('.border')).toHaveClass('bg-loss-soft')

    // Undone below, the block lifts without a new upload.
    fireEvent.click(screen.getByRole('button', { name: 'Geri al' }))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled())
    expect(screen.queryByText(notice)).not.toBeInTheDocument()
  })

  // GLG-1063: the rows of a statement imported before are flagged as likely
  // duplicates of that import's transactions. Undone below, they are not: the
  // preview is read again, so the rows come back checked and all of them are
  // committed. It is read again as soon as the import is deleted, not once
  // every query the undo refreshes is back: the list of imports, which lifts
  // the block, can be back first.
  it('reads the preview again after an undo so its duplicates are checked again', async () => {
    const past = [
      {
        id: 6,
        broker: 'csv',
        portfolio_id: 1,
        period_start: '2025-12-01',
        period_end: '2025-12-31',
        transaction_count: 2,
        created_at: '2026-01-02T09:30:00Z',
      },
    ]
    const ok = preview.rows.slice(0, 2)
    let parses = 0
    let committed: Record<string, unknown> | undefined
    mockApi(
      () => {
        parses++
        const imported = past.length > 0
        return new Response(
          JSON.stringify({
            ...preview,
            warnings: imported
              ? [
                  {
                    code: 'same_period',
                    message: 'period already imported',
                    import_id: 6,
                    period_start: '2025-12-01',
                    period_end: '2025-12-31',
                  },
                ]
              : [],
            rows: ok.map((row) => (imported ? { ...row, duplicate_of: row.date } : row)),
            reconciliation: [],
          }),
          { status: 200 },
        )
      },
      (body) => {
        committed = body
        return new Response(JSON.stringify({ id: 7, transaction_count: 2 }), { status: 201 })
      },
      past,
      () => {
        past.splice(0)
        deleted = true
      },
    )
    let deleted = false
    const release = hold((url) => deleted && url === '/api/portfolios')
    renderPage()

    upload()
    await screen.findAllByRole('table')
    expect(screen.getByText('0 satır seçildi')).toBeInTheDocument()
    expect(table().getByText('26.12.2025 tarihli işlemin tekrarı olabilir')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Geri al' }))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
    )
    expect(await screen.findByText('2 satır seçildi')).toBeInTheDocument()
    expect(parses).toBe(2)
    expect(table().queryByText(/tekrarı olabilir/)).not.toBeInTheDocument()
    expect(body().getAllByRole('checkbox').every(isChecked)).toBe(true)

    release()
    await importWhenReady()
    await screen.findByText('işlemler sayfası')
    expect(committed?.rows).toHaveLength(2)
  })

  // GLG-435: a period the commit refuses is named in the preview, and İçe
  // aktar stays disabled instead of failing only after the click.
  it('blocks the commit of a period the server refuses', async () => {
    const future = {
      ...preview,
      warnings: [
        {
          code: 'period',
          message: 'period_end: must not be in the future',
          import_id: 0,
          period_start: '2026-09-01',
          period_end: '2026-09-30',
        },
      ],
    }
    mockApi(() => new Response(JSON.stringify(future), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(
      screen.getByText('Ekstre dönemi henüz bitmemiş. Dönem bittikten sonra içe aktar.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeDisabled()
  })

  // GLG-577: several statements picked at once, reviewed one by one.

  const ocak = { period_start: '2025-01-01', period_end: '2025-01-31' }
  const subat = { period_start: '2025-02-01', period_end: '2025-02-28' }

  // A clean statement of the period in the currency, with nothing to review.
  function statement(period: typeof ocak, currency: string, extra = {}) {
    const body = {
      ...preview,
      ...period,
      currency,
      sha256: `${period.period_start}-${currency}`,
      warnings: [],
      rows: [],
      holdings: [],
      reconciliation: [],
      ...extra,
    }
    return new Response(JSON.stringify(body), { status: 200 })
  }

  // A statement row for the batch tests that compare two copies of a month.
  const deposit = {
    key: 'cash-0',
    status: 'ok',
    type: 'deposit',
    date: '2025-01-05',
    amount: '100',
    fee: '0',
    currency: 'TRY',
  }

  // Answers the parse per file name and counts the uploads of each file; a
  // commit is listed under past imports from then on, since the GET reads
  // the same array on every request.
  function mockBatch(
    answer: (name: string, past: Record<string, unknown>[], form: FormData) => Response,
  ) {
    const calls: Record<string, number> = {}
    const commits: Record<string, unknown>[] = []
    const past: Record<string, unknown>[] = []
    mockApi(
      (file, form) => {
        calls[file.name] = (calls[file.name] ?? 0) + 1
        return answer(file.name, past, form)
      },
      (body) => {
        commits.push(body)
        const created = {
          id: 10 + commits.length,
          transaction_count: 1,
          period_start: body.period_start,
          period_end: body.period_end,
        }
        past.unshift({
          ...created,
          broker: 'midas',
          portfolio_id: 1,
          currency: body.currency,
          created_at: '2026-01-02T09:30:00Z',
        })
        return new Response(JSON.stringify(created), { status: 201 })
      },
      past,
      // An undo takes the import off the list (GLG-1072).
      (id) =>
        past.splice(
          past.findIndex((item) => String(item.id) === id),
          1,
        ),
    )
    return { calls, commits }
  }

  // The past imports are a list too, so rows are read from the queue's.
  // The list folds while a statement is in review (GLG-790); unfold it first.
  function queue() {
    const show = screen.queryByRole('button', { name: 'Tüm dosyaları göster' })
    if (show) fireEvent.click(show)
    return within(screen.getByRole('list', { name: 'Seçilen ekstreler' }))
  }

  // Step 3, "Bitti" (GLG-967): the batch's end replaces the queue.
  function done() {
    return within(screen.getByRole('region', { name: /^(Bitti:|Hiçbir ekstre)/ }))
  }

  const step = () =>
    within(screen.getAllByRole('list')[0])
      .getAllByRole('listitem')
      .find((item) => item.ariaCurrent)

  it('parses every picked statement up front and queues them by period', async () => {
    const answers: Record<string, () => Response> = {
      'subat-usd.pdf': () => statement(subat, 'USD'),
      'ocak-usd.pdf': () => statement(ocak, 'USD'),
      'ocak-try.pdf': () => statement(ocak, 'TRY'),
      'bozuk.pdf': () =>
        new Response(
          JSON.stringify({ error: 'file: no period row found: not a Midas statement' }),
          { status: 400 },
        ),
      'eski.pdf': () =>
        new Response(JSON.stringify({ error: 'already_imported', import_id: 3 }), { status: 409 }),
    }
    mockBatch((name) => answers[name]())
    renderPage()

    upload(['subat-usd.pdf', 'bozuk.pdf', 'ocak-usd.pdf', 'eski.pdf', 'ocak-try.pdf'])
    expect(await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)).toBeInTheDocument()
    // GLG-790: the preview is headed by its file and broker, and the batch
    // list folds to its progress line while a statement is in review.
    expect(screen.getByRole('heading', { name: 'ocak-try.pdf' })).toBeInTheDocument()
    expect(screen.getByText('Midas · 01.01.2025 – 31.01.2025 · TRY')).toBeInTheDocument()
    expect(document.getElementById('import-queue-list')).not.toBeVisible()
    expect(screen.getByRole('button', { name: 'Tüm dosyaları göster' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    // GLG-967: the steps count the batch, the folded queue names the file
    // and, as Midas names its TRY and USD statements alike, its currency
    // (GLG-1194).
    expect(step()).toHaveTextContent('İncele · Ekstre 1 / 3')
    expect(screen.getByText('Ekstre 1 / 3').parentElement).toHaveTextContent(
      /^Ekstre 1 \/ 3 · ocak-try\.pdf · TRY · Tüm dosyaları göster$/,
    )

    // Earlier months first, TRY before USD, the unreadable ones last in the
    // order they were picked, each with its reason.
    const rows = queue().getAllByRole('listitem')
    expect(rows.map((row) => within(row).getByText(/\.pdf$/).textContent)).toEqual([
      'ocak-try.pdf',
      'ocak-usd.pdf',
      'subat-usd.pdf',
      'bozuk.pdf',
      'eski.pdf',
    ])
    expect(rows[0]).toHaveAttribute('aria-current', 'step')
    expect(rows[0]).toHaveTextContent('İnceleniyor')
    expect(rows[2]).toHaveTextContent('USD · 01.02.2025 – 28.02.2025')
    expect(rows[2]).toHaveTextContent('Sırada')
    expect(rows[3]).toHaveTextContent('Bu dosya bir Midas hesap ekstresi değil.')
    expect(rows[3]).toHaveTextContent('Aktarılmadı')
    expect(within(rows[3]).getByText('Aktarılmadı')).toHaveClass('text-loss')
    // GLG-765: a file imported earlier is skipped, not failed.
    expect(rows[4]).toHaveTextContent('Zaten içe aktarılmış')
    expect(rows[4]).toHaveTextContent('Atlandı')
    // Only the three statements that get a review are counted (GLG-786).
    expect(screen.getByText('Ekstre 1 / 3')).toHaveAttribute('role', 'status')
  })

  it('moves on to the next statement after a commit and stays on the page at the end', async () => {
    const { calls, commits } = mockBatch((name) =>
      name === 'ocak.pdf' ? statement(ocak, 'TRY') : statement(subat, 'TRY'),
    )
    renderPage()

    upload(['subat.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()

    // Parsed again at its turn, so its warnings see the month just imported.
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    expect(commits[0]).toMatchObject({ period_start: '2025-01-01', currency: 'TRY' })
    expect(calls['subat.pdf']).toBe(2)
    expect(screen.getByText('Ekstre 2 / 2')).toHaveAttribute('role', 'status')

    clickImport()
    // GLG-967: the batch ends at "Bitti" on the import page, one line per
    // statement with its rows and a link to İşlemler filtered to it.
    expect(await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')).toBeInTheDocument()
    expect(step()).toHaveTextContent('Bitti')
    const rows = done().getAllByRole('listitem')
    expect(rows.map((row) => within(row).getByText(/\.pdf$/).textContent)).toEqual([
      'ocak.pdf',
      'subat.pdf',
    ])
    rows.forEach((row, i) => {
      expect(row).toHaveTextContent('1 işlem')
      expect(within(row).getByRole('link', { name: 'İşlemlerde gör' })).toHaveAttribute(
        'href',
        `/portfolios/1/transactions?import=${11 + i}`,
      )
    })
    expect(commits[1]).toMatchObject({ period_start: '2025-02-01' })
    expect(screen.queryByRole('list', { name: 'Seçilen ekstreler' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Ekstre yükle' })).not.toBeInTheDocument()

    // "Başka ekstre yükle" returns to step 1.
    fireEvent.click(screen.getByRole('button', { name: 'Başka ekstre yükle' }))
    expect(screen.getByRole('region', { name: 'Ekstre yükle' })).toBeInTheDocument()
    expect(step()).toHaveTextContent('Dosya seç')
    expect(screen.queryByText(/^Bitti:/)).not.toBeInTheDocument()
  })

  // GLG-1072: an import undone below reads as undone on Bitti, without the
  // link to İşlemler, which would open on no rows.
  // GLG-1181: a fund renamed on TEFAS is named in an early statement only by
  // its old name, and a later statement of the batch prints that name next
  // to the code. The early one is parsed with the batch's names, so its row
  // shows the fund instead of an empty picker.
  it('resolves a fund an earlier statement names by its old name through the batch', async () => {
    const oldName = 'İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST'
    const fund = { symbol: `${oldName} FON`, instrument_id: 901 }
    const buy = {
      key: 'trade-0',
      type: 'buy',
      date: '2025-01-10',
      symbol: oldName,
      market: 'TEFAS',
      quantity: '4',
      price: '2.5',
      amount: '10',
      fee: '0',
      currency: 'TRY',
    }
    const sent: (string | null)[] = []
    mockBatch((name, _past, form) => {
      if (name === 'subat.pdf') {
        return statement(subat, 'TRY', { broker: 'garanti', names: [fund] })
      }
      sent.push(form.get('names') as string | null)
      const names = JSON.parse((form.get('names') as string | null) ?? '[]') as unknown[]
      const row = names.length
        ? {
            ...buy,
            status: 'ok',
            instrument_id: 901,
            resolved_by: 'name',
            instrument_symbol: 'IIE',
            instrument_name: 'BULLS PORTFÖY İKİNCİ SERBEST FON',
          }
        : { ...buy, status: 'unknown_instrument' }
      return statement(ocak, 'TRY', { broker: 'garanti', rows: [row] })
    })
    renderPage()

    upload(['subat.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    await showAll()
    const hint = await table().findByText('Başka bir ekstreden')
    const line = within(hint.parentElement as HTMLElement)
    expect(line.getByText('IIE')).toBeInTheDocument()
    expect(line.getByText('BULLS PORTFÖY İKİNCİ SERBEST FON')).toBeInTheDocument()
    expect(screen.queryByLabelText(`${oldName} için enstrüman seç`)).not.toBeInTheDocument()
    // The upfront parse knows no other statement yet; the one at its turn
    // sends the later statement's names.
    expect(sent).toEqual([null, JSON.stringify([fund])])
  })

  it('shows an import undone on Bitti as undone', async () => {
    mockBatch((name) => (name === 'ocak.pdf' ? statement(ocak, 'TRY') : statement(subat, 'TRY')))
    renderPage()

    upload(['subat.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    clickImport()
    await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')

    fireEvent.click(
      await waitFor(() => document.querySelector<HTMLElement>('[data-undo-import="11"]')!),
    )
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
    )

    await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')
    const [first, second] = done().getAllByRole('listitem')
    expect(first).toHaveTextContent('ocak.pdf')
    expect(within(first).getByText('Geri alındı')).toBeInTheDocument()
    expect(within(first).queryByRole('link')).not.toBeInTheDocument()
    expect(within(second).getByRole('link', { name: 'İşlemlerde gör' })).toBeInTheDocument()
  })

  // GLG-1072: a commit whose imports refetch fails is marked imported against
  // the list from before it; an import never listed is not an undone one.
  it('keeps a new import imported when the imports refetch fails', async () => {
    mockBatch((name) => (name === 'ocak.pdf' ? statement(ocak, 'TRY') : statement(subat, 'TRY')))
    const answer = vi.mocked(fetch).getMockImplementation()!
    let failList = false
    vi.mocked(fetch).mockImplementation(async (input, init) =>
      failList && String(input) === '/api/imports' && (init?.method ?? 'GET') === 'GET'
        ? new Response('{"error":"boom"}', { status: 500 })
        : answer(input, init),
    )
    renderPage()

    upload(['subat.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    failList = true
    clickImport()
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    clickImport()
    await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')

    expect(done().queryByText('Geri alındı')).not.toBeInTheDocument()
    expect(done().getAllByRole('link', { name: 'İşlemlerde gör' })).toHaveLength(2)
  })

  // GLG-1072: Back from a statement's "İşlemlerde gör" returns to Bitti with
  // every file's line, the failed one's reason included.
  it('returns to Bitti on Back from İşlemler', async () => {
    mockBatch((name) =>
      name === 'ocak.pdf'
        ? statement(ocak, 'TRY')
        : new Response(
            JSON.stringify({ error: 'file: no period row found: not a Midas statement' }),
            { status: 400 },
          ),
    )
    function Back() {
      const navigate = useNavigate()
      return <button onClick={() => navigate(-1)}>geri</button>
    }
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter initialEntries={[importEntry()]}>
          <Routes>
            <Route path="/portfolios/:id/import" element={<ImportPage />} />
            <Route path="/portfolios/:id/transactions" element={<Back />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    upload(['bozuk.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()
    await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')

    fireEvent.click(done().getByRole('link', { name: 'İşlemlerde gör' }))
    fireEvent.click(await screen.findByRole('button', { name: 'geri' }))

    expect(await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')).toBeInTheDocument()
    const rows = done().getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('ocak.pdf')
    expect(within(rows[0]).getByRole('link', { name: 'İşlemlerde gör' })).toBeInTheDocument()
    expect(rows[1]).toHaveTextContent('Bu dosya bir Midas hesap ekstresi değil.')
    expect(rows[1]).toHaveTextContent('Aktarılmadı')
  })

  // GLG-1072: an import undone on another visit to the page reads undone on
  // the Bitti that Back brings back.
  it('shows an import undone while away as undone on the Bitti Back restores', async () => {
    mockBatch((name) => (name === 'ocak.pdf' ? statement(ocak, 'TRY') : statement(subat, 'TRY')))
    function Nav() {
      const navigate = useNavigate()
      return (
        <>
          <button onClick={() => navigate(-1)}>geri</button>
          <button onClick={() => navigate('/portfolios/1/import')}>yeni içe aktarma</button>
        </>
      )
    }
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <MemoryRouter initialEntries={[importEntry()]}>
          <Nav />
          <Routes>
            <Route path="/portfolios/:id/import" element={<ImportPage />} />
            <Route path="/portfolios/:id/transactions" element={<p>işlemler sayfası</p>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    upload(['subat.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    clickImport()
    await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')

    fireEvent.click(done().getAllByRole('link', { name: 'İşlemlerde gör' })[0])
    await screen.findByText('işlemler sayfası')
    fireEvent.click(screen.getByRole('button', { name: 'yeni içe aktarma' }))
    fireEvent.click(
      await waitFor(() => document.querySelector<HTMLElement>('[data-undo-import="11"]')!),
    )
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
    )
    await waitFor(() =>
      expect(document.querySelector('[data-undo-import="11"]')).not.toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: 'geri' }))
    await screen.findByText('işlemler sayfası')
    fireEvent.click(screen.getByRole('button', { name: 'geri' }))

    expect(await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')).toBeInTheDocument()
    const [first] = done().getAllByRole('listitem')
    expect(first).toHaveTextContent('ocak.pdf')
    expect(within(first).getByText('Geri alındı')).toBeInTheDocument()
    expect(within(first).queryByRole('link')).not.toBeInTheDocument()
  })

  // GLG-764: one folder of Garanti fund and Garanti BBVA Yatırım statements.
  it('detects the broker of each statement under the automatic option and commits with it', async () => {
    const sent: string[] = []
    const { commits } = mockBatch((name, _past, form) => {
      sent.push(String(form.get('broker')))
      return name === 'fon.pdf'
        ? statement(ocak, 'TRY', { broker: 'garanti' })
        : statement(subat, 'TRY', { broker: 'garanti_yatirim' })
    })
    renderPage()

    expect(screen.getByLabelText('Aracı kurum')).toHaveTextContent('Otomatik (PDF)')
    upload(['yatirim.pdf', 'fon.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    const rows = queue().getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('fon.pdf')
    expect(rows[0]).toHaveTextContent('Garanti BBVA · TRY')
    expect(rows[1]).toHaveTextContent('yatirim.pdf')
    expect(rows[1]).toHaveTextContent('Garanti BBVA Yatırım · TRY')

    clickImport()
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    clickImport()
    expect(await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')).toBeInTheDocument()
    expect(new Set(sent)).toEqual(new Set(['auto']))
    expect(commits.map((body) => body.broker)).toEqual(['garanti', 'garanti_yatirim'])
  })

  it('blocks a second statement of a month imported earlier in the batch', async () => {
    // The server names the earlier import once it is committed. The two
    // copies read differently, so the user decides on the second (GLG-765).
    mockBatch((name, past) =>
      statement(ocak, 'TRY', {
        sha256: name,
        rows: [{ ...deposit, amount: name === 'ocak.pdf' ? '100' : '120' }],
        warnings: past.map((item) => ({
          code: 'same_period',
          message: 'period already imported (2025-01-01 – 2025-01-31), undo that import first',
          import_id: item.id,
          ...ocak,
        })),
      }),
    )
    renderPage()

    upload(['ocak.pdf', 'ocak-yeni.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()

    // Kept only because the past imports were refreshed before this parse.
    expect(
      await screen.findByText(
        '01.01.2025 – 31.01.2025 dönemi bu portföye zaten içe aktarılmış. Yeniden aktarmak için önce aşağıdan eskisini geri al.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeDisabled()
    // The mockup's order: Vazgeç, Bu ekstreyi atla, İçe aktar (GLG-1055).
    expect(
      within(screen.getByTestId('preview-actions'))
        .getAllByRole('button')
        .map((button) => button.textContent),
    ).toEqual(['Satır ekle', 'Vazgeç', 'Bu ekstreyi atla', 'İçe aktar'])

    fireEvent.click(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
    expect(await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')).toBeInTheDocument()
    const rows = done().getAllByRole('listitem')
    expect(within(rows[0]).getByRole('link', { name: 'İşlemlerde gör' })).toBeInTheDocument()
    expect(rows[1]).toHaveTextContent('Atlandı')
    // A file the user skipped says so, unlike a copy the batch skipped
    // itself (GLG-786).
    expect(rows[1]).toHaveTextContent('Sen atladın')
  })

  // GLG-1063: an undo started under one statement's preview and done once
  // the batch is on the next one does not read the first statement again in
  // place of the next.
  it('leaves the next statement alone when an undo lands after a commit', async () => {
    const mart = { period_start: '2025-03-01', period_end: '2025-03-31' }
    const periods: Record<string, typeof ocak> = {
      'ocak.pdf': ocak,
      'subat.pdf': subat,
      'mart.pdf': mart,
    }
    const { calls } = mockBatch((name) => statement(periods[name], 'TRY', { sha256: name }))
    const release = hold((_, method) => method === 'DELETE')
    renderPage()

    upload(['ocak.pdf', 'subat.pdf', 'mart.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)

    // Ocak is undone under Şubat's preview, and Şubat is committed before
    // the undo is back.
    fireEvent.click(await screen.findByRole('button', { name: 'Geri al' }))
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
    )
    clickImport()
    await screen.findByText(/ · 01\.03\.2025 – 31\.03\.2025 · TRY$/)

    release()
    // The undo is back once its buttons are no longer pending.
    await waitFor(() =>
      screen
        .getAllByRole('button', { name: 'Geri al' })
        .forEach((button) => expect(button).not.toHaveAttribute('aria-disabled')),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled())
    expect(calls['subat.pdf']).toBe(2)
    expect(screen.getByText(/ · 01\.03\.2025 – 31\.03\.2025 · TRY$/)).toBeInTheDocument()
  })

  // GLG-765: harmless repeats in a batch are skipped by themselves, each
  // with its reason.
  it('imports a file picked twice once and skips the copy', async () => {
    const { calls, commits } = mockBatch(() => statement(ocak, 'TRY'))
    renderPage()

    upload(['ocak.pdf', 'ocak (1).pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()

    expect(await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')).toBeInTheDocument()
    const rows = done().getAllByRole('listitem')
    expect(rows[1]).toHaveTextContent('ocak (1).pdf')
    expect(rows[1]).toHaveTextContent('ocak.pdf ile aynı dosya')
    expect(rows[1]).toHaveTextContent('Atlandı')
    expect(commits).toHaveLength(1)
    expect(calls['ocak (1).pdf']).toBe(1)
  })

  it('skips a statement the parse at its turn finds imported and moves on', async () => {
    const mart = { period_start: '2025-03-01', period_end: '2025-03-31' }
    // Imported elsewhere between the upfront parse and its turn.
    let subatParses = 0
    mockBatch((name) => {
      if (name === 'ocak.pdf') return statement(ocak, 'TRY')
      if (name === 'mart.pdf') return statement(mart, 'TRY')
      if (subatParses++ === 0) return statement(subat, 'TRY')
      return new Response(JSON.stringify({ error: 'already_imported', import_id: 3 }), {
        status: 409,
      })
    })
    renderPage()

    upload(['subat.pdf', 'mart.pdf', 'ocak.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    clickImport()

    // Şubat is skipped and Mart opens.
    await screen.findByText(/ · 01\.03\.2025 – 31\.03\.2025 · TRY$/)
    const rows = queue().getAllByRole('listitem')
    expect(rows[1]).toHaveTextContent('subat.pdf')
    expect(rows[1]).toHaveTextContent('Zaten içe aktarılmış')
    expect(rows[1]).toHaveTextContent('Atlandı')
    clickImport()
    expect(await screen.findByText('Bitti: 2 ekstre içe aktarıldı.')).toBeInTheDocument()
  })

  // GLG-1183: two different files of one statement are both kept, each
  // naming the other, for the user to pick one at its turn, even when their
  // rows read the same: a corrected re-issue may differ only in what the
  // preview does not show.
  it('keeps two different files of one period queued, each naming the other', async () => {
    const { commits } = mockBatch((name) =>
      statement(ocak, 'TRY', { sha256: name, rows: [deposit] }),
    )
    renderPage()

    upload(['ocak.pdf', 'ocak-duzeltme.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    // In sight with the queue folded too.
    expect(screen.getByText('Ekstre 1 / 2').parentElement).toHaveTextContent(
      'ocak.pdf · TRY · ocak-duzeltme.pdf ile aynı dönem',
    )
    const rows = queue().getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('ocak-duzeltme.pdf ile aynı dönem')
    expect(rows[1]).toHaveTextContent('ocak.pdf ile aynı dönem')
    expect(rows[1]).toHaveTextContent('Sırada')
    fireEvent.click(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
    await waitFor(() => expect(queue().getAllByRole('listitem')[1]).toHaveAttribute('aria-current'))
    clickImport()

    expect(await screen.findByText('Bitti: 1 ekstre içe aktarıldı.')).toBeInTheDocument()
    expect(commits).toHaveLength(1)
    expect(commits[0].sha256).toBe('ocak-duzeltme.pdf')
  })

  // GLG-772: skipping a statement drops the changes made to its preview, so
  // it asks first; an untouched one is skipped at once.
  it('asks before skipping a statement whose preview was changed', async () => {
    mockBatch((name) =>
      name === 'subat.pdf' ? statement(subat, 'TRY') : statement(ocak, 'TRY', { rows: [deposit] }),
    )
    renderPage()

    upload(['ocak.pdf', 'subat.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    const checkboxes = body().getAllByRole('checkbox')
    fireEvent.click(checkboxes[checkboxes.length - 1])
    fireEvent.click(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
    const confirmDialog = await screen.findByRole('dialog', { name: 'Bu ekstre atlansın mı?' })
    fireEvent.click(within(confirmDialog).getByRole('button', { name: 'Bu ekstreyi atla' }))

    expect(await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
    expect(await screen.findByText('Hiçbir ekstre içe aktarılmadı.')).toBeInTheDocument()
  })

  it('skips every statement left when Vazgeç ends the batch', async () => {
    const { commits } = mockBatch((name) =>
      name === 'subat.pdf'
        ? statement(subat, 'TRY')
        : statement(ocak, name === 'ocak-usd.pdf' ? 'USD' : 'TRY'),
    )
    renderPage()

    upload(['ocak.pdf', 'subat.pdf', 'ocak-usd.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    // The two files not reviewed yet would be skipped, so it asks (GLG-772).
    expect(await screen.findByText('Henüz incelenmemiş 2 ekstre atlanacak.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'İçe aktarmayı bırak' }))

    // Review Focus 3: nothing imported lists the reasons, no success line.
    expect(await screen.findByText('Hiçbir ekstre içe aktarılmadı.')).toBeInTheDocument()
    expect(screen.queryByText(/^Bitti:/)).not.toBeInTheDocument()
    const rows = done().getAllByRole('listitem')
    expect(rows).toHaveLength(3)
    for (const row of rows) expect(row).toHaveTextContent('Atlandı')
    // The one in review was skipped by the user, the rest unseen (GLG-786).
    expect(rows[0]).toHaveTextContent('Sen atladın')
    expect(rows[1]).toHaveTextContent('İncelenmeden atlandı')
    expect(rows[2]).toHaveTextContent('İncelenmeden atlandı')
    expect(commits).toHaveLength(0)
    expect(done().queryByRole('link')).not.toBeInTheDocument()
    // "Başka ekstre yükle" brings step 1 back, its file input empty.
    fireEvent.click(screen.getByRole('button', { name: 'Başka ekstre yükle' }))
    expect(screen.getByRole('region', { name: 'Ekstre yükle' })).toBeInTheDocument()
    expect(fileInput().value).toBe('')
  })

  // GLG-1292: isPending reaches the buttons a tick after the click, so a
  // double click on İçe aktar committed twice, and Atla right after it moved
  // on while the commit was still running.
  it('commits once on a double click and ignores Atla while the commit runs', async () => {
    const { commits } = mockBatch((name) =>
      name === 'subat.pdf' ? statement(subat, 'TRY') : statement(ocak, 'TRY'),
    )
    renderPage()

    upload(['ocak.pdf', 'subat.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    const release = hold((url, method) => url === '/api/imports' && method === 'POST')
    const commit = screen.getByRole('button', { name: 'İçe aktar' })
    fireEvent.click(commit)
    fireEvent.click(commit)
    fireEvent.click(screen.getByRole('button', { name: 'Bu ekstreyi atla' }))
    await waitFor(() => expect(commit).toHaveAttribute('aria-disabled', 'true'))
    release()

    expect(await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)).toBeInTheDocument()
    expect(commits).toHaveLength(1)
    expect(queue().getAllByRole('listitem')[0]).not.toHaveTextContent('Atlandı')
  })

  // GLG-1296: Vazgeç right after İçe aktar asked to end the batch while the
  // commit was still running.
  it('ignores Vazgeç while a batch commit runs', async () => {
    const { commits } = mockBatch((name) =>
      name === 'subat.pdf' ? statement(subat, 'TRY') : statement(ocak, 'TRY'),
    )
    renderPage()

    upload(['ocak.pdf', 'subat.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    const release = hold((url, method) => url === '/api/imports' && method === 'POST')
    const commit = screen.getByRole('button', { name: 'İçe aktar' })
    fireEvent.click(commit)
    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    await waitFor(() => expect(commit).toHaveAttribute('aria-disabled', 'true'))
    release()

    expect(await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)).toBeInTheDocument()
    expect(commits).toHaveLength(1)
    expect(screen.queryByText('Henüz incelenmemiş 1 ekstre atlanacak.')).not.toBeInTheDocument()
  })

  // GLG-1297: the next statement's Atla opens where the last one was, so
  // the second click of a double click, once its parse was back, skipped it
  // too. The browser counts that click as the second (detail 2).
  it('skips one statement on a double click on Atla', async () => {
    const mart = { period_start: '2025-03-01', period_end: '2025-03-31' }
    const periods: Record<string, typeof ocak> = {
      'ocak.pdf': ocak,
      'subat.pdf': subat,
      'mart.pdf': mart,
    }
    mockBatch((name) => statement(periods[name], 'TRY', { sha256: name }))
    renderPage()

    upload(['ocak.pdf', 'subat.pdf', 'mart.pdf'])
    await screen.findByText(/ · 01\.01\.2025 – 31\.01\.2025 · TRY$/)
    press(screen.getByRole('button', { name: 'Bu ekstreyi atla' }), 1)
    await screen.findByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)
    press(screen.getByRole('button', { name: 'Bu ekstreyi atla' }), 2)

    await waitFor(() => expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled())
    expect(screen.getByText(/ · 01\.02\.2025 – 28\.02\.2025 · TRY$/)).toBeInTheDocument()
    expect(queue().getAllByRole('listitem')[1]).toHaveAttribute('aria-current')
  })

  // GLG-210: the preview is where a statement gets finished by hand, so the
  // three repair routes each have to end up in the commit body.

  // Parse and commit in one go, keeping whatever the page sent.
  function mockPreviewAndCommit(parsed: object = preview) {
    const captured: { body: Record<string, unknown> | null } = { body: null }
    mockApi(
      () => new Response(JSON.stringify(parsed), { status: 200 }),
      (body) => {
        captured.body = body
        return new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 1,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    return captured
  }

  it('commits an unknown-instrument row once an instrument is picked', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    // Empty, so typing starts a fresh query instead of appending to the one
    // that found nothing (GLG-349), with the search's own hint rather than
    // the symbol, which read like a typed value (GLG-789).
    const picker = screen.getByLabelText('XYZ için enstrüman seç')
    expect(picker).toHaveValue('')
    expect(picker).toHaveAttribute('placeholder', 'Sembol veya isim ara')
    fireEvent.change(picker, { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    // The row is now ok: checked, no longer blocked, and marked as touched.
    const box = body().getAllByRole('checkbox')[1]
    expect(box).not.toHaveAttribute('aria-disabled')
    expect(box).toBeChecked()
    expect(table().getByText('elle')).toBeInTheDocument()

    await importWhenReady()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toContainEqual({
      type: 'buy',
      date: '2025-12-20',
      instrument_id: 777,
      quantity: '1',
      price: '10',
      amount: '10',
      fee: '0',
      currency: 'USD',
    })
    // GLG-736: the pick is sent under the preview's symbol, to be remembered.
    expect(captured.body?.aliases).toEqual([{ symbol: 'XYZ', instrument_id: 777 }])
  })

  // GLG-1297: a double click's first click on İçe aktar, pending while a
  // pick's parse runs, did nothing, so its second click commits.
  it('commits on the second click of a double click whose first hit a pending İçe aktar', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()
    const release = hold((url) => url === '/api/imports/parse')
    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
    const commit = screen.getByRole('button', { name: 'İçe aktar' })
    await waitFor(() => expect(commit).toHaveAttribute('aria-disabled', 'true'))
    press(commit, 1)
    release()
    await waitFor(() => expect(commit).not.toHaveAttribute('aria-disabled'))
    press(commit, 2)
    // Another row still needs fixing, so it asks first (GLG-784).
    fireEvent.click(await screen.findByRole('button', { name: 'Yine de içe aktar' }))

    await screen.findByText('işlemler sayfası')
    expect(captured.body?.aliases).toEqual([{ symbol: 'XYZ', instrument_id: 777 }])
  })

  // GLG-752: a pick requests the preview again with every pick so far, and
  // its opening rows and reconciliation replace the old ones; a row the user
  // unchecked stays unchecked.
  it('requests the preview again with the pick and keeps the check marks', async () => {
    const sent: (FormDataEntryValue | null)[] = []
    const opening = {
      key: 'open-0',
      status: 'ok',
      opening: true,
      type: 'buy',
      date: '2025-11-30',
      symbol: 'QQQ',
      instrument_id: 555,
      quantity: '1',
      price: '500',
      amount: '500',
      fee: '0',
      currency: 'USD',
    }
    mockApi((_file, form) => {
      const picks = form.get('picks')
      sent.push(picks)
      if (!picks) return new Response(JSON.stringify(preview), { status: 200 })
      const rows = preview.rows.map((row) =>
        row.key === 'trade-3'
          ? { ...row, status: 'ok', instrument_id: 777, resolved_by: 'alias' }
          : row,
      )
      return new Response(
        JSON.stringify({
          ...preview,
          rows: [
            { ...opening, key: 'open-cash-USD', type: 'deposit', amount: '500' },
            opening,
            ...rows,
          ],
          reconciliation: [
            { symbol: 'QQQ', statement_quantity: '1', portfolio_quantity: '1', match: true },
          ],
        }),
        { status: 200 },
      )
    })
    renderPage()

    upload()
    await showAll()
    const deposit = body().getAllByRole('checkbox')[0]
    fireEvent.click(deposit)
    expect(deposit).not.toBeChecked()
    // The unknown buy holds back the opening rows, and the page says so
    // (GLG-785).
    const pending = /Bilinmeyen sembollerin hepsini eşleştirdiğinde/
    expect(screen.getByText(pending)).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    expect(await table().findAllByText('Açılış')).toHaveLength(2)
    expect(screen.getByText('2 açılış satırı eklendi')).toHaveAttribute('role', 'status')
    expect(screen.queryByText(pending)).not.toBeInTheDocument()
    expect(sent).toEqual([null, JSON.stringify([{ symbol: 'XYZ', instrument_id: 777 }])])
    // In date order: the opening deposit and buy, the deposit, the picked XYZ
    // row and VTI.
    expect(body().getAllByRole('checkbox').map(isChecked)).toEqual([true, true, false, true, true])
    const panel = within(screen.getByText('Ekstreye göre pozisyonlar').closest('section')!)
    expect(panel.queryByRole('img', { name: 'Uyuşmuyor' })).not.toBeInTheDocument()
  })

  // GLG-1231: a pick aborts the parse an earlier pick still waits on, so
  // quick picks never hold the browser's few connections to the host, and
  // the aborted parse shows no error.
  it('aborts the parse of an earlier pick when a later pick starts one', async () => {
    const two = {
      ...preview,
      rows: [...preview.rows, { ...preview.rows[2], key: 'trade-4', symbol: 'ABC' }],
    }
    mockApi(() => new Response(JSON.stringify(two), { status: 200 }))
    const inner = vi.mocked(fetch).getMockImplementation()!
    const signals: (AbortSignal | null | undefined)[] = []
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const form = init?.body
      if (form instanceof FormData && form.get('picks')) {
        signals.push(init?.signal)
        // The first pick's parse hangs, as a slow TEFAS does, until aborted.
        if (signals.length === 1) {
          return new Promise<Response>((_, reject) =>
            init?.signal?.addEventListener('abort', () =>
              reject(new DOMException('aborted', 'AbortError')),
            ),
          )
        }
      }
      return inner(input, init)
    })
    renderPage()

    upload()
    await showAll()
    for (const symbol of ['XYZ', 'ABC']) {
      fireEvent.change(screen.getByLabelText(`${symbol} için enstrüman seç`), {
        target: { value: 'xyz' },
      })
      fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
      await waitFor(() => expect(signals).toHaveLength(symbol === 'XYZ' ? 1 : 2))
    }

    await waitFor(() => expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeEnabled())
    expect(signals[0]?.aborted).toBe(true)
    expect(signals[1]?.aborted).toBe(false)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  // GLG-820: an opening buy priced in the dialog keeps its price when a pick
  // requests the preview again, here one that adds an opening buy in front,
  // and the opening deposit pays for both.
  it('keeps an opening buy priced in the dialog across a pick', async () => {
    const opening = { date: '2025-11-30', fee: '0', currency: 'USD', opening: true }
    const vti = {
      ...opening,
      key: 'open-123',
      status: 'error',
      type: 'buy',
      symbol: 'VTI',
      instrument_id: 123,
      quantity: '3',
      code: 'opening_price',
      message: 'no price for the opening position',
    }
    const dividend = {
      key: 'div-0',
      status: 'unknown_instrument',
      type: 'dividend',
      date: '2025-12-15',
      symbol: 'XYZ',
      amount: '5',
      fee: '0',
      currency: 'USD',
    }
    const captured: { body: Record<string, unknown> | null } = { body: null }
    mockApi(
      (_file, form) => {
        const rows = form.get('picks')
          ? [
              { ...opening, key: 'open-cash-USD', status: 'ok', type: 'deposit', amount: '20' },
              {
                ...opening,
                key: 'open-777',
                status: 'ok',
                type: 'buy',
                symbol: 'XYZ',
                instrument_id: 777,
                quantity: '2',
                price: '10',
                amount: '20',
              },
              vti,
              { ...dividend, status: 'ok', instrument_id: 777, resolved_by: 'alias' },
            ]
          : [vti, dividend]
        return new Response(JSON.stringify({ ...preview, warnings: [], rows }), { status: 200 })
      },
      (body) => {
        captured.body = body
        return new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 4,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')
    fireEvent.click(table().getByRole('button', { name: '30.11.2025 VTI Alış 3 satırını düzenle' }))
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '100,005' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
    expect(await screen.findByText('1 açılış satırı eklendi')).toBeInTheDocument()

    await importWhenReady()
    await screen.findByText('işlemler sayfası')

    // 3 × 100.005 + 2 × 10 = 320.015, rounded up to the cent.
    expect(captured.body?.rows).toEqual([
      { type: 'deposit', date: '2025-11-30', amount: '320.02', fee: '0', currency: 'USD' },
      expect.objectContaining({ type: 'buy', instrument_id: 777, quantity: '2', price: '10' }),
      expect.objectContaining({ type: 'buy', instrument_id: 123, quantity: '3', price: '100.005' }),
      expect.objectContaining({ type: 'dividend', instrument_id: 777 }),
    ])
  })

  // GLG-820: the deposit the page added for an opening buy priced in the
  // dialog is in no preview; unchecked, it stays unchecked across a pick.
  it('keeps an unchecked opening deposit the page added across a pick', async () => {
    const vti = {
      key: 'open-123',
      status: 'error',
      type: 'buy',
      date: '2025-11-30',
      symbol: 'VTI',
      instrument_id: 123,
      quantity: '3',
      fee: '0',
      currency: 'USD',
      opening: true,
      code: 'opening_price',
      message: 'no price for the opening position',
    }
    const dividend = {
      key: 'div-0',
      status: 'unknown_instrument',
      type: 'dividend',
      date: '2025-12-15',
      symbol: 'XYZ',
      amount: '5',
      fee: '0',
      currency: 'USD',
    }
    const captured: { body: Record<string, unknown> | null } = { body: null }
    mockApi(
      (_file, form) => {
        const rows = form.get('picks')
          ? [vti, { ...dividend, status: 'ok', instrument_id: 777, resolved_by: 'alias' }]
          : [vti, dividend]
        return new Response(JSON.stringify({ ...preview, warnings: [], rows }), { status: 200 })
      },
      (body) => {
        captured.body = body
        return new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 2,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    renderPage()

    upload()
    await showAll()
    fireEvent.click(table().getByRole('button', { name: '30.11.2025 VTI Alış 3 satırını düzenle' }))
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    fireEvent.click(body().getAllByRole('checkbox')[0])
    expect(body().getAllByRole('checkbox')[0]).not.toBeChecked()

    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    await importWhenReady()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toEqual([
      expect.objectContaining({ type: 'buy', instrument_id: 123, quantity: '3', price: '100' }),
      expect.objectContaining({ type: 'dividend', instrument_id: 777 }),
    ])
  })

  // GLG-736: a row resolved by the user's earlier pick says so, names the
  // instrument it resolved to (GLG-750) and keeps the picker; a new pick
  // replaces it on every row of that symbol and is sent.
  it('marks a row resolved by an earlier pick and lets it be picked again', async () => {
    const name = 'GARANTI PORTFOY PARA PIYASASI'
    const remembered = (key: string, date: string) => ({
      key,
      status: 'ok',
      resolved_by: 'alias',
      instrument_symbol: 'ABC',
      instrument_name: 'ABC Fund',
      type: 'buy',
      date,
      symbol: name,
      instrument_id: 888,
      quantity: '10',
      price: '1.5',
      amount: '15',
      fee: '0',
      currency: 'USD',
    })
    const captured = mockPreviewAndCommit({
      ...preview,
      warnings: [],
      rows: [remembered('trade-0', '2025-12-20'), remembered('trade-1', '2025-12-21')],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getAllByText('Önceki seçimin')).toHaveLength(2)
    for (const hint of table().getAllByText('Önceki seçimin')) {
      const line = within(hint.parentElement as HTMLElement)
      expect(line.getByText('ABC')).toBeInTheDocument()
      expect(line.getByText('ABC Fund')).toBeInTheDocument()
    }
    expect(body().getAllByRole('checkbox').every(isChecked)).toBe(true)
    // A matched row shows no empty search box until Değiştir (GLG-789).
    expect(table().queryByLabelText(`${name} için enstrüman seç`)).not.toBeInTheDocument()
    fireEvent.click(table().getAllByRole('button', { name: `${name} eşleşmesini değiştir` })[0])
    fireEvent.change(table().getByLabelText(`${name} için enstrüman seç`), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    expect(table().queryByText('Önceki seçimin')).not.toBeInTheDocument()
    expect(table().queryByText('ABC Fund')).not.toBeInTheDocument()
    expect(table().queryByLabelText(`${name} için enstrüman seç`)).not.toBeInTheDocument()

    await importWhenReady()
    await screen.findByText('işlemler sayfası')
    expect(captured.body?.rows).toEqual([
      expect.objectContaining({ date: '2025-12-20', instrument_id: 777 }),
      expect.objectContaining({ date: '2025-12-21', instrument_id: 777 }),
    ])
    expect(captured.body?.aliases).toEqual([{ symbol: name, instrument_id: 777 }])
  })

  it('fixes every unknown row of the same symbol and currency with one pick', async () => {
    const unknown = (key: string, symbol: string, currency: string) => ({
      key,
      status: 'unknown_instrument',
      type: 'buy',
      date: '2025-12-20',
      symbol,
      quantity: '1',
      price: '10',
      amount: '10',
      fee: '0',
      currency,
    })
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            rows: [
              unknown('trade-0', 'XYZ', 'USD'),
              unknown('trade-1', 'XYZ', 'USD'),
              unknown('trade-2', 'ABC', 'USD'),
              unknown('trade-3', 'XYZ', 'USD'),
              unknown('trade-4', 'XYZ', 'TRY'),
            ],
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await showAll()

    const picker = screen.getAllByLabelText('XYZ için enstrüman seç')[0]
    fireEvent.change(picker, { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    // The three USD XYZ rows are ok and checked; ABC and the TRY XYZ still
    // wait for their own pick.
    const boxes = body().getAllByRole('checkbox')
    expect(boxes.map(isChecked)).toEqual([true, true, false, true, false])
    expect(boxes.map((box) => box.getAttribute('aria-disabled') === 'true')).toEqual([
      false,
      false,
      true,
      false,
      true,
    ])
    expect(screen.getAllByLabelText('XYZ için enstrüman seç')).toHaveLength(1)
    expect(screen.getByLabelText('ABC için enstrüman seç')).toBeInTheDocument()
  })

  // GLG-1193: a statement with many rows of a few unknown names showed one
  // picker per row. The names go once into a block above the table, each
  // with one picker and the number of rows it covers; a pick there resolves
  // every row of its name.
  it('lists each unknown name once above the table with one picker', async () => {
    const unknown = (key: string, symbol: string, date: string) => ({
      key,
      status: 'unknown_instrument',
      type: 'buy',
      date,
      symbol,
      quantity: '1',
      price: '10',
      amount: '10',
      fee: '0',
      currency: 'USD',
    })
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            rows: [
              unknown('trade-0', 'XYZ', '2025-12-02'),
              unknown('trade-1', 'ABC', '2025-12-03'),
              unknown('trade-2', 'XYZ', '2025-12-04'),
              unknown('trade-3', 'XYZ', '2025-12-05'),
              unknown('trade-4', 'ABC', '2025-12-06'),
            ],
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    const block = within(await screen.findByRole('region', { name: 'Eşleşmeyen isimler (2)' }))
    expect(screen.getAllByRole('combobox')).toHaveLength(2)
    expect(table().queryByRole('combobox')).not.toBeInTheDocument()
    expect(block.getByText('3 satır')).toBeInTheDocument()
    expect(block.getByText('2 satır')).toBeInTheDocument()
    // Each row still says what is wrong with it.
    expect(table().getAllByText('Enstrüman bulunamadı: XYZ')).toHaveLength(3)

    fireEvent.change(block.getByLabelText('XYZ için enstrüman seç'), { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    expect(screen.getByRole('region', { name: 'Eşleşmeyen isimler (1)' })).toBeInTheDocument()
    expect(screen.queryByLabelText('XYZ için enstrüman seç')).not.toBeInTheDocument()
    expect(screen.getByLabelText('ABC için enstrüman seç')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Sorunlu 2', checked: true })).toBeInTheDocument()
  })

  // GLG-678: a row the commit would refuse and of an unknown symbol says both,
  // and offers the picker like a plain unknown row. A pick gives it the
  // instrument but leaves it an error for the edit dialog.
  it('shows an unknown symbol on a row the commit would refuse', async () => {
    const row = {
      type: 'sell',
      date: '2025-12-20',
      symbol: 'XYZ',
      quantity: '5',
      price: '0',
      amount: '0',
      fee: '0',
      currency: 'USD',
    }
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            rows: [
              {
                ...row,
                key: 'trade-0',
                status: 'error',
                code: 'invalid',
                message:
                  'price: must be a positive number with at most 15 integer and 9 fraction digits',
                unknown_instrument: true,
              },
              { ...row, key: 'trade-1', status: 'unknown_instrument', price: '10', amount: '50' },
            ],
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await showAll()

    expect(table().getByText(/^Enstrüman bulunamadı: XYZ\. Fiyat 0'dan/)).toBeInTheDocument()
    // One name, one picker above the table for both rows (GLG-1193).
    expect(screen.getAllByLabelText('XYZ için enstrüman seç')).toHaveLength(1)
    expect(screen.getByText('2 satır')).toBeInTheDocument()

    const picker = screen.getByLabelText('XYZ için enstrüman seç')
    fireEvent.change(picker, { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))

    expect(screen.queryByLabelText('XYZ için enstrüman seç')).not.toBeInTheDocument()
    expect(table().queryByText(/Enstrüman bulunamadı/)).not.toBeInTheDocument()
    expect(table().getByText(/^Fiyat 0'dan/)).toBeInTheDocument()
    // Only the unknown row became ok; the error row still has no checkbox.
    expect(body().getAllByRole('checkbox')).toHaveLength(1)
  })

  // GLG-637: a CSV row with no currency cell and an unknown symbol previews as
  // TRY (GLG-622). The USD instrument picked for it books it in USD; a TRY
  // row would be refused by the commit, and with it the whole import.
  it('sends a picked instrument in its own currency', async () => {
    const captured = mockPreviewAndCommit({
      ...preview,
      broker: 'csv',
      currency: 'TRY',
      rows: [
        {
          key: 'trade-0',
          status: 'unknown_instrument',
          type: 'buy',
          date: '2025-12-20',
          symbol: 'XYZ',
          quantity: '1',
          price: '2',
          amount: '2',
          fee: '0',
          currency: 'TRY',
          currency_fallback: true,
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')

    fireEvent.change(screen.getByLabelText('XYZ için enstrüman seç'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
    expect(table().getAllByText(formatMoney('2', 'USD')).length).toBeGreaterThan(0)

    await importWhenReady()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toEqual([
      {
        type: 'buy',
        date: '2025-12-20',
        instrument_id: 777,
        quantity: '1',
        price: '2',
        amount: '2',
        fee: '0',
        currency: 'USD',
      },
    ])
  })

  // GLG-733: a fund row named by its name has no code the preview could ask
  // TEFAS about. Typing the fund's code offers a TEFAS lookup when the search
  // has no TEFAS match; the fund it returns is picked. An unknown code says so.
  it('looks a typed fund code up on TEFAS and picks the fund', async () => {
    const name = 'GARANTİ PORTFÖY TL FONU'
    const captured = mockPreviewAndCommit({
      ...preview,
      broker: 'garanti',
      currency: 'TRY',
      rows: [
        {
          key: 'trade-0',
          status: 'unknown_instrument',
          type: 'buy',
          date: '2025-12-20',
          symbol: name,
          market: 'TEFAS',
          quantity: '10',
          price: '1.5',
          amount: '15',
          fee: '0',
          currency: 'TRY',
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')
    const picker = screen.getByLabelText(`${name} için enstrüman seç`)

    fireEvent.change(picker, { target: { value: 'nop' } })
    fireEvent.click(await screen.findByRole('button', { name: "NOP fonunu TEFAS'ta ara" }))
    expect(await screen.findByRole('alert')).toHaveTextContent("TEFAS'ta NOP kodlu fon bulunamadı")

    // GLG-1292: a double click looks the fund up once, so NOP and GTL once each.
    fireEvent.change(picker, { target: { value: 'gtl' } })
    const lookup = await screen.findByRole('button', { name: "GTL fonunu TEFAS'ta ara" })
    fireEvent.click(lookup)
    fireEvent.click(lookup)
    expect(await table().findByText('GTL')).toBeInTheDocument()
    const lookups = vi.mocked(fetch).mock.calls.filter(([url]) => url === '/api/imports/funds')
    expect(lookups).toHaveLength(2)

    clickImport()
    await screen.findByText('işlemler sayfası')
    expect(captured.body?.rows).toEqual([
      expect.objectContaining({ instrument_id: 888, currency: 'TRY' }),
    ])
  })

  // GLG-646: the same TRY row fixed from the edit dialog instead of the inline
  // picker takes the picked instrument's currency too.
  it('sends an instrument picked in the dialog in its own currency', async () => {
    const captured = mockPreviewAndCommit({
      ...preview,
      broker: 'csv',
      currency: 'TRY',
      rows: [
        {
          key: 'trade-0',
          status: 'unknown_instrument',
          type: 'buy',
          date: '2025-12-20',
          symbol: 'XYZ',
          quantity: '1',
          price: '2',
          amount: '2',
          fee: '0',
          currency: 'TRY',
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')

    fireEvent.click(
      table().getByRole('button', {
        name: '20.12.2025 XYZ Alış 1 × ₺2,00 ₺2,00 satırını düzenle',
      }),
    )
    fireEvent.change(screen.getByLabelText('Enstrüman'), { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(table().getAllByText(formatMoney('2', 'USD')).length).toBeGreaterThan(0)

    clickImport()
    await screen.findByText('işlemler sayfası')
    // Nothing was picked inline, so nothing is remembered (GLG-736).
    expect(captured.body).not.toHaveProperty('aliases')

    expect(captured.body?.rows).toEqual([
      {
        type: 'buy',
        date: '2025-12-20',
        instrument_id: 777,
        quantity: '1',
        price: '2',
        amount: '2',
        fee: '0',
        currency: 'USD',
      },
    ])
  })

  // GLG-656: a row switched to another instrument in the dialog names it.
  it('shows the instrument picked in the dialog on the row', async () => {
    mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    fireEvent.click(
      table().getByRole('button', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını düzenle',
      }),
    )
    fireEvent.change(screen.getByLabelText('Enstrüman'), { target: { value: 'xyz' } })
    fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    expect(
      table().getByRole('button', {
        name: '26.12.2025 XYZ Alış 2 × $331,82 $663,64 satırını düzenle',
      }),
    ).toBeInTheDocument()
  })

  it('sends an error row with the values typed into the dialog', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // The error row's dialog opens empty but for the date the parser managed
    // to read.
    fireEvent.click(table().getByRole('button', { name: '30.12.2025 satırını düzenle' }))
    expect(screen.getByLabelText('Tarih')).toHaveValue('2025-12-30')

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toContainEqual({
      type: 'deposit',
      date: '2025-12-30',
      amount: '500',
      fee: '0',
      currency: 'USD',
    })
  })

  // GLG-817: an opening buy priced in the dialog is paid for by an opening
  // deposit of quantity × price, which the preview could not propose without
  // a price; without it the opening cost leaves cash negative.
  it('adds an opening deposit for an opening buy priced in the dialog', async () => {
    const captured = mockPreviewAndCommit({
      ...preview,
      warnings: [],
      rows: [
        {
          key: 'open-0',
          status: 'error',
          type: 'buy',
          date: '2025-11-30',
          symbol: 'VTI',
          instrument_id: 123,
          quantity: '3',
          fee: '0',
          currency: 'USD',
          opening: true,
          code: 'opening_price',
          message: 'no price for the opening position',
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')
    expect(body().queryAllByText('Yatırma')).toHaveLength(0)

    fireEvent.click(table().getByRole('button', { name: '30.11.2025 VTI Alış 3 satırını düzenle' }))
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '100,005' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    clickImport()
    await screen.findByText('işlemler sayfası')

    // 3 × 100.005 = 300.015, rounded up to the cent.
    expect(captured.body?.rows).toEqual([
      { type: 'deposit', date: '2025-11-30', amount: '300.02', fee: '0', currency: 'USD' },
      expect.objectContaining({ type: 'buy', quantity: '3', price: '100.005' }),
    ])
  })

  // GLG-817: the deposit follows a re-priced opening buy, and an unchecked one
  // leaves the sum.
  it('resizes the opening deposit to the opening buys priced and checked', async () => {
    const opening = { status: 'ok', date: '2025-11-30', fee: '0', currency: 'USD', opening: true }
    const captured = mockPreviewAndCommit({
      ...preview,
      warnings: [],
      rows: [
        { ...opening, key: 'open-cash-0', type: 'deposit', amount: '700' },
        {
          ...opening,
          key: 'open-0',
          type: 'buy',
          symbol: 'VTI',
          instrument_id: 123,
          quantity: '5',
          price: '100',
          amount: '500',
          approximate_price: true,
        },
        {
          ...opening,
          key: 'open-1',
          type: 'buy',
          symbol: 'QQQ',
          instrument_id: 124,
          quantity: '1',
          price: '200',
          amount: '200',
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')

    fireEvent.click(
      table().getByRole('button', {
        name: '30.11.2025 VTI Alış 5 × $100,00 $500,00 satırını düzenle',
      }),
    )
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '120' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(table().getByText(formatMoney('800', 'USD'))).toBeInTheDocument()

    fireEvent.click(body().getAllByRole('checkbox')[2])
    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toEqual([
      { type: 'deposit', date: '2025-11-30', amount: '600', fee: '0', currency: 'USD' },
      expect.objectContaining({ type: 'buy', quantity: '5', price: '120' }),
    ])
  })

  // GLG-817: a deposit the user unchecked stays unchecked while its buys are
  // unchecked and checked again.
  it('keeps an unchecked opening deposit unchecked', async () => {
    const opening = { status: 'ok', date: '2025-11-30', fee: '0', currency: 'USD', opening: true }
    const captured = mockPreviewAndCommit({
      ...preview,
      warnings: [],
      rows: [
        { ...opening, key: 'open-cash-0', type: 'deposit', amount: '500' },
        {
          ...opening,
          key: 'open-0',
          type: 'buy',
          symbol: 'VTI',
          instrument_id: 123,
          quantity: '5',
          price: '100',
          amount: '500',
        },
      ],
    })
    renderPage()

    upload()
    await screen.findAllByRole('table')

    const boxes = () => body().getAllByRole('checkbox')
    fireEvent.click(boxes()[0])
    fireEvent.click(boxes()[1])
    fireEvent.click(boxes()[1])
    expect(boxes()[0]).not.toBeChecked()
    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toEqual([
      expect.objectContaining({ type: 'buy', quantity: '5', price: '100' }),
    ])
  })

  // GLG-366: the statement's amount no longer matches a trade whose quantity
  // was changed by hand, so the server derives it instead.
  // GLG-981: the commit sets an imported row's note to the broker and the
  // period (ADR 0006), so a note typed in the preview would be thrown away.
  it('offers no note field in the preview edit and add dialogs', async () => {
    mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    fireEvent.click(
      table().getByRole('button', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını düzenle',
      }),
    )
    expect(screen.getByLabelText('Adet')).toBeInTheDocument()
    expect(screen.queryByLabelText('Not · isteğe bağlı')).not.toBeInTheDocument()
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Vazgeç' }))

    // "Satır ekle", apart from the page header's "İşlem ekle" (GLG-966).
    const addRow = screen.getByRole('button', { name: 'Satır ekle' })
    fireEvent.click(addRow)
    expect(screen.getByLabelText('Tarih')).toBeInTheDocument()
    expect(screen.queryByLabelText('Not · isteğe bağlı')).not.toBeInTheDocument()
  })

  it('leaves the amount of a trade edited in the dialog to the server', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    fireEvent.click(
      table().getByRole('button', {
        name: '26.12.2025 VTI Alış 2 × $331,82 $663,64 satırını düzenle',
      }),
    )
    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '3' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    // Saved again unchanged, the dialog hands back the table's estimate as the
    // amount (GLG-368); it still must not be sent.
    fireEvent.click(
      table().getByRole('button', {
        name: '26.12.2025 VTI Alış 3 × $331,82 $995,46 satırını düzenle',
      }),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toContainEqual({
      type: 'buy',
      date: '2025-12-26',
      instrument_id: 123,
      quantity: '3',
      price: '331.82',
      fee: '0',
      currency: 'USD',
    })
  })

  // GLG-538: a Midas nema imports as a dividend with no instrument. It reads
  // and reopens as Faiz/Nema, with the currency select an instrument would
  // otherwise replace, and is sent back without an instrument. An
  // unknown-instrument dividend is still a Temettü.
  it('labels, edits and sends a nema row as interest', async () => {
    const nema = {
      key: 'cash-1',
      status: 'ok',
      type: 'dividend',
      date: '2025-12-31',
      amount: '47.93',
      fee: '0',
      currency: 'TRY',
      description: 'Aylık Nema Geliri',
    }
    const unknownDividend = {
      key: 'div-0',
      status: 'unknown_instrument',
      type: 'dividend',
      date: '2025-12-15',
      symbol: 'XYZ',
      amount: '5',
      fee: '0',
      currency: 'USD',
    }
    const captured: { body: Record<string, unknown> | null } = { body: null }
    mockApi(
      () =>
        new Response(JSON.stringify({ ...preview, rows: [nema, unknownDividend] }), {
          status: 200,
        }),
      (body) => {
        captured.body = body
        return new Response(
          JSON.stringify({
            id: 7,
            transaction_count: 1,
            period_start: '2025-12-01',
            period_end: '2025-12-31',
          }),
          { status: 201 },
        )
      },
    )
    renderPage()

    upload()
    await showAll()
    expect(table().getByText('Temettü')).toBeInTheDocument()

    fireEvent.click(
      table().getByRole('button', { name: '31.12.2025 Faiz/Nema ₺47,93 satırını düzenle' }),
    )
    expect(screen.getByRole('radio', { name: 'Faiz/Nema' })).toBeChecked()
    expect(screen.queryByLabelText('Enstrüman')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Para birimi')).toHaveTextContent('Türk Lirası (TRY)')
    fireEvent.change(screen.getByLabelText('Stopaj'), { target: { value: '7,19' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    expect(table().getByText('Faiz/Nema')).toBeInTheDocument()
    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toEqual([
      { type: 'dividend', date: '2025-12-31', amount: '47.93', fee: '7.19', currency: 'TRY' },
    ])
  })

  // GLG-359: a cash row of a type the parser does not know still carries its
  // amount and currency in the last raw cell.
  it('opens an unknown cash type row as a deposit of its amount', async () => {
    const cashError = {
      key: 'err-1',
      status: 'error',
      section: 'HESAP İŞLEMLERİ',
      code: 'unknown_cash_type',
      value: 'Faiz',
      message: 'unknown cash type Faiz',
      date: '2025-12-20',
      raw: ['20/12/25', '20/12/25', 'Faiz', 'Aylık faiz', 'Gerçekleşti', '1.047,93 TRY'],
    }
    mockApi(() => new Response(JSON.stringify({ ...preview, rows: [cashError] }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    fireEvent.click(table().getByRole('button', { name: '20.12.2025 satırını düzenle' }))
    expect(screen.getByRole('radio', { name: 'Yatırma' })).toBeChecked()
    expect(screen.getByLabelText('Tutar')).toHaveValue('1047,93')
    expect(screen.getByLabelText('Para birimi')).toHaveTextContent('Türk Lirası (TRY)')
    expect(screen.getByLabelText('Tarih')).toHaveValue('2025-12-20')
  })

  // GLG-993: a first thousands group starting with 0 is no number, as in
  // parseDecimalInput (GLG-977), so it prefills nothing rather than 234.56.
  it('prefills nothing for an unknown cash amount whose first group starts with 0', async () => {
    const cashError = {
      key: 'err-1',
      status: 'error',
      section: 'HESAP İŞLEMLERİ',
      code: 'unknown_cash_type',
      value: 'Faiz',
      message: 'unknown cash type Faiz',
      date: '2025-12-20',
      raw: ['20/12/25', '20/12/25', 'Faiz', 'Aylık faiz', 'Gerçekleşti', '0.234,56 TRY'],
    }
    mockApi(() => new Response(JSON.stringify({ ...preview, rows: [cashError] }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    fireEvent.click(table().getByRole('button', { name: '20.12.2025 satırını düzenle' }))
    expect(screen.getByRole('radio', { name: 'Yatırma' })).not.toBeChecked()
    expect(screen.queryByDisplayValue('234,56')).not.toBeInTheDocument()
  })

  // GLG-497: the statement is Turkish format in every UI locale, so in the en
  // UI "1.000 TRY" is still a thousand, not 1.
  it('reads the unknown cash amount as Turkish format in the en UI', async () => {
    const cashError = {
      key: 'err-1',
      status: 'error',
      section: 'HESAP İŞLEMLERİ',
      code: 'unknown_cash_type',
      value: 'Faiz',
      message: 'unknown cash type Faiz',
      date: '2025-12-20',
      raw: ['20/12/25', '20/12/25', 'Faiz', 'Aylık faiz', 'Gerçekleşti', '1.000 TRY'],
    }
    mockApi(() => new Response(JSON.stringify({ ...preview, rows: [cashError] }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // Switched only now, so upload() and the row button keep their tr names;
    // the dialog opened next is rendered and prefilled in en.
    setLocale('en')
    try {
      fireEvent.click(table().getByRole('button', { name: '20.12.2025 satırını düzenle' }))
      expect(screen.getByRole('radio', { name: 'Deposit' })).toBeChecked()
      expect(screen.getByLabelText('Amount')).toHaveValue('1000')
    } finally {
      setLocale('tr')
    }
  })

  // GLG-621: a CSV row error names the column by its field key, which the
  // page shows as the column's name in the UI locale.
  it('names the CSV column of a row error in Turkish', async () => {
    const numberError = {
      key: 'err-1',
      status: 'error',
      section: 'CSV',
      code: 'bad_number',
      value: 'quantity',
      message: 'unreadable number',
      date: '2025-05-02',
      raw: ['2025-05-02', 'buy', 'SISE', 'abc'],
    }
    // GLG-639: an ambiguous thousands separator names its column too.
    const ambiguousError = {
      ...numberError,
      key: 'err-2',
      code: 'ambiguous_number',
      message: 'ambiguous thousands separator',
      raw: ['2025-05-02', 'buy', 'SISE', '1.000', '10'],
    }
    mockApi(
      () =>
        new Response(
          JSON.stringify({ ...preview, broker: 'csv', rows: [numberError, ambiguousError] }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getByText('Sayı okunamadı: Adet')).toBeInTheDocument()
    expect(table().getByText('Adet: Binlik ayırıcı mı? 1000 veya 1.000,00 yaz')).toBeInTheDocument()
    // GLG-796: its raw line breaks between cells rather than inside a
    // number.
    openRawLines()
    expect(table().getByText('2025-05-02 | buy | SISE | abc')).not.toHaveClass('break-all')
  })

  // GLG-829: a CSV row the commit would refuse was read; it is still a row
  // to fix, so Sorunlu counts it once with the unreadable one (GLG-966).
  it('counts a refused CSV row once among the rows to fix', async () => {
    const numberError = {
      key: 'err-1',
      status: 'error',
      section: 'CSV',
      code: 'bad_number',
      value: 'quantity',
      message: 'unreadable number',
      date: '2026-01-06',
      raw: ['2026-01-06', 'buy', 'THYAO', 'abc'],
    }
    const invalid = {
      key: 'trade-0',
      status: 'error',
      code: 'invalid',
      message: 'price: must be a positive number',
      type: 'sell',
      date: '2026-01-07',
      symbol: 'THYAO',
      quantity: '5',
      price: '0',
      amount: '0',
      fee: '0',
      currency: 'TRY',
    }
    mockApi(
      () =>
        new Response(JSON.stringify({ ...preview, broker: 'csv', rows: [numberError, invalid] }), {
          status: 200,
        }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(screen.getByRole('radio', { name: 'Sorunlu 2' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText(/okunamadı, elle kontrol et/)).not.toBeInTheDocument()
  })

  // GLG-660: an unknown CSV type cell is shown as written, even when it
  // reads like a column name.
  it('shows an unknown CSV type cell as written', async () => {
    const typeError = {
      key: 'err-1',
      status: 'error',
      section: 'CSV',
      code: 'unknown_type',
      value: 'price',
      message: 'unknown type',
      date: '2026-09-03',
      raw: ['2026-09-03', 'price', 'THYAO', '10', '300', '', '0', 'TRY'],
    }
    mockApi(() => new Response(JSON.stringify({ ...preview, rows: [typeError] }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getByText('Bilinmeyen işlem türü: price')).toBeInTheDocument()
  })

  // GLG-704: a Garanti ledger row the parser does not know is translated,
  // not shown as the server's English message.
  it('translates an unknown ledger row code', async () => {
    const rowError = {
      key: 'err-1',
      status: 'error',
      section: 'HESAP EKSTRESİ',
      code: 'unknown_row',
      value: 'VİRMAN',
      message: 'unknown ledger row',
      date: '2026-09-03',
      raw: ['03.09.2026', 'VİRMAN', '100,00'],
    }
    mockApi(() => new Response(JSON.stringify({ ...preview, rows: [rowError] }), { status: 200 }))
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getByText('Tanınmayan ekstre satırı: VİRMAN')).toBeInTheDocument()
  })

  // GLG-754: Garanti Yatırım share statements emit bad_row too, so it is not
  // called a fund statement row, and a transfer in without a cost says so.
  it('words a bad row neutrally and a transfer in without cost as such', async () => {
    const badRow = {
      key: 'err-1',
      status: 'error',
      section: 'ledger',
      code: 'bad_row',
      value: '20/03/2025',
      message: 'incomplete ledger row',
      date: '2025-03-20',
      raw: ['20/03/2025'],
    }
    const noCost = {
      key: 'err-2',
      status: 'error',
      section: 'trades',
      code: 'unknown_cost',
      value: 'HS-Virman Giriş',
      message: 'transfer in with unknown cost',
      date: '2023-08-26',
      raw: ['THYAO', '26/08/2023', '5.000', 'HS-Virman Giriş', '0.000'],
    }
    mockApi(
      () => new Response(JSON.stringify({ ...preview, rows: [badRow, noCost] }), { status: 200 }),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(table().getByText('Ekstre satırı okunamadı')).toBeInTheDocument()
    expect(table().queryByText(/Fon ekstresi/)).not.toBeInTheDocument()
    expect(table().getByText('HS-Virman Giriş: maliyet bilinmiyor, fiyatı gir')).toBeInTheDocument()
  })

  // GLG-647: a CSV has no period of its own, so with nothing checked there
  // is nothing to import, and an error-only one has no period to send.
  it('disables Import for a CSV preview with nothing checked', async () => {
    const errorRow = {
      key: 'err-1',
      status: 'error',
      section: 'CSV',
      code: 'bad_date',
      message: 'unreadable date',
      raw: ['yesterday', 'deposit', '1'],
    }
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            broker: 'csv',
            period_start: '',
            period_end: '',
            warnings: [],
            rows: [errorRow],
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    expect(screen.getByRole('button', { name: 'İçe aktar' })).toBeDisabled()
  })

  // GLG-649: with no period, a new row starts on today, not an empty date.
  it('starts an added row on today for a CSV preview without a period', async () => {
    const errorRow = {
      key: 'err-1',
      status: 'error',
      section: 'CSV',
      code: 'bad_date',
      message: 'unreadable date',
      raw: ['yesterday', 'deposit', '1'],
    }
    mockApi(
      () =>
        new Response(
          JSON.stringify({
            ...preview,
            broker: 'csv',
            period_start: '',
            period_end: '',
            warnings: [],
            rows: [errorRow],
          }),
          { status: 200 },
        ),
    )
    renderPage()

    upload()
    await screen.findAllByRole('table')

    // "Satır ekle", apart from the page header's "İşlem ekle" (GLG-966).
    const addRow = screen.getByRole('button', { name: 'Satır ekle' })
    fireEvent.click(addRow)
    expect(screen.getByLabelText('Tarih')).toHaveValue(todayIstanbul())
  })

  // GLG-966: a row added on Sorunlu is an ok row, which Sorunlu never shows;
  // the page switches to Tümü so it is seen, and not added twice.
  it('shows the added row when it is added on Sorunlu', async () => {
    mockPreviewAndCommit()
    renderPage()

    upload()
    await screen.findAllByRole('table')
    expect(screen.getByRole('radio', { name: 'Sorunlu 2' })).toHaveAttribute('aria-checked', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'Satır ekle' }))
    fireEvent.click(screen.getByRole('radio', { name: 'Çekme' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    expect(screen.getByRole('radio', { name: 'Tümü 6' })).toHaveAttribute('aria-checked', 'true')
    expect(
      table().getByRole('checkbox', { name: '31.12.2025 Para çekme $100,00 satırını seç' }),
    ).toBeInTheDocument()
  })

  it('appends an operation the statement never had and sends it', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    // "Satır ekle", apart from the page header's "İşlem ekle" (GLG-966).
    const addRow = screen.getByRole('button', { name: 'Satır ekle' })
    fireEvent.click(addRow)
    // An add dialog, not an edit one (GLG-1055).
    expect(
      within(screen.getByRole('dialog')).getByRole('heading', { name: 'İşlem ekle' }),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Tarih')).toHaveValue('2025-12-31')

    fireEvent.click(screen.getByRole('radio', { name: 'Çekme' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    // Header row, the five parsed rows and the new one at its date, after
    // the error row's 30.12 and before the undated skipped row (GLG-966).
    const rows = table().getAllByRole('row')
    expect(rows).toHaveLength(7)
    expect(rows[5]).toHaveTextContent('31.12.2025')

    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toContainEqual({
      type: 'withdrawal',
      date: '2025-12-31',
      amount: '100',
      fee: '0',
      currency: 'USD',
    })
  })

  // GLG-1055: the add dialog's "Ekle ve yeni" adds the row and stays open
  // for the next one, still in the statement's currency.
  it('keeps the add dialog open after Ekle ve yeni', async () => {
    const captured = mockPreviewAndCommit()
    renderPage()

    upload()
    await showAll()

    fireEvent.click(screen.getByRole('button', { name: 'Satır ekle' }))
    fireEvent.click(screen.getByRole('radio', { name: 'Çekme' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '100' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle ve yeni' }))

    expect(table().getAllByRole('row')).toHaveLength(7)
    expect(
      within(screen.getByRole('dialog')).getByRole('heading', { name: 'İşlem ekle' }),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Tutar')).toHaveValue('')
    expect(screen.getByLabelText('Para birimi')).toHaveTextContent('USD')

    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '50' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))
    clickImport()
    await screen.findByText('işlemler sayfası')

    expect(captured.body?.rows).toContainEqual({
      type: 'withdrawal',
      date: '2025-12-31',
      amount: '50',
      fee: '0',
      currency: 'USD',
    })
  })

  // GLG-206: the past imports section and its undo.

  const pastImports = [
    {
      id: 7,
      broker: 'midas',
      portfolio_id: 1,
      period_start: '2025-12-01',
      period_end: '2025-12-31',
      currency: 'USD',
      transaction_count: 12,
      // 2 January in Istanbul, 1 January in UTC.
      created_at: '2026-01-01T22:30:00Z',
    },
    // Another portfolio's import, which this page must not list.
    {
      id: 8,
      broker: 'midas',
      portfolio_id: 2,
      period_start: '2025-11-01',
      period_end: '2025-11-30',
      transaction_count: 3,
      created_at: '2025-12-02T09:30:00Z',
    },
  ]

  it('lists the past imports of this portfolio only', async () => {
    // A user in Istanbul, this year 2026 (only Date is faked, so findBy waits).
    vi.stubEnv('TZ', 'Europe/Istanbul')
    vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-10-06T12:00:00Z') })
    onTestFinished(() => {
      vi.useRealTimers()
      vi.unstubAllEnvs()
    })
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }), undefined, pastImports)
    renderPage()

    const row = (await screen.findByText('Midas · Ara 2025')).closest('li')!
    // The day it was imported where the user is, not the UTC one (GLG-965).
    expect(row).toHaveTextContent('12 işlem · USD · 02 Oca')
    expect(within(row).getByRole('link', { name: 'İşlemlerde gör' })).toHaveAttribute(
      'href',
      '/portfolios/1/transactions?import=7',
    )
    expect(screen.getAllByRole('button', { name: 'Geri al' })).toHaveLength(1)
    // GLG-800: quiet on the list, red only in the confirm.
    expect(screen.getByRole('button', { name: 'Geri al' })).toHaveClass('text-loss')
    expect(screen.getByRole('button', { name: 'Geri al' })).not.toHaveClass('bg-loss')
    expect(screen.queryByText(/Kas 2025/)).not.toBeInTheDocument()
  })

  it('deletes the import only after the confirm dialog is accepted', async () => {
    let deleted: string | null = null
    mockApi(
      () => new Response(JSON.stringify(preview), { status: 200 }),
      undefined,
      pastImports,
      (id) => {
        deleted = id
      },
    )
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: 'Geri al' }))
    expect(await screen.findByText('Bu içe aktarmadaki 12 işlem silinecek.')).toBeInTheDocument()
    expect(deleted).toBeNull()

    // The dialog's own button, not the row's.
    const dialog = within(screen.getByRole('dialog'))
    fireEvent.click(dialog.getByRole('button', { name: 'Geri al' }))

    await waitFor(() => expect(deleted).toBe('7'))
  })

  // GLG-985: the undone row, and the button the dialog hands focus back to,
  // leave the list, which dropped focus to <body>.
  it('moves focus to the next undo button, then to the heading, after an undo', async () => {
    let remaining = [
      pastImports[0],
      { ...pastImports[0], id: 9, period_start: '2025-10-01', period_end: '2025-10-31' },
    ]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/portfolios') return portfoliosResponse()
      if (url === '/api/imports' && method === 'GET') {
        return new Response(JSON.stringify(remaining), { status: 200 })
      }
      if (url.startsWith('/api/imports/') && method === 'DELETE') {
        const id = Number(url.slice('/api/imports/'.length))
        remaining = remaining.filter((item) => item.id !== id)
        return new Response(null, { status: 204 })
      }
      // The undo also refetches the portfolio's other queries.
      return new Response('{}', { status: 200 })
    })
    renderPage()

    async function undoFirst() {
      const button = (await screen.findAllByRole('button', { name: 'Geri al' }))[0]
      button.focus()
      fireEvent.click(button)
      fireEvent.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Geri al' }),
      )
      // What the browser does on dialog.close(), and jsdom does not.
      button.focus()
      await waitFor(() => expect(button).not.toBeInTheDocument())
    }

    await undoFirst()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Geri al' }))
    await undoFirst()
    expect(await screen.findByText('Bu portföyde henüz içe aktarma yok.')).toBeInTheDocument()
    expect(document.activeElement).toBe(
      screen.getByRole('heading', { name: 'Geçmiş içe aktarmalar' }),
    )
  })

  it('announces a failed past-imports load instead of the empty text', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input) === '/api/portfolios'
        ? portfoliosResponse()
        : new Response(JSON.stringify({ error: 'boom' }), { status: 500 }),
    )
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Geçmiş içe aktarmalar yüklenemedi')
    expect(screen.queryByText('Bu portföyde henüz içe aktarma yok.')).not.toBeInTheDocument()
  })

  it('shows no imports yet when the portfolio has none', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    expect(await screen.findByText('Bu portföyde henüz içe aktarma yok.')).toBeInTheDocument()
  })

  // GLG-876: below md the preview's action row is pinned to the bottom of the
  // screen, where a floating add button would sit over İçe aktar.
  it('keeps the add button in the header instead of floating', async () => {
    mockApi(() => new Response(JSON.stringify(preview), { status: 200 }))
    renderPage()

    const add = await screen.findByRole('button', { name: 'İşlem ekle' })
    expect(add.className).not.toMatch(/max-md:fixed/)
  })
})
