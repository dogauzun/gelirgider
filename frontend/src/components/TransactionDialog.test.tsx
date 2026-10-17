import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { summaryQueryKey, type Position } from '../api/summary'
import type { Transaction } from '../api/transactions'
import { todayIstanbul } from '../lib/date'
import { chooseOption } from '../test/select'
import { TransactionDialog } from './TransactionDialog'
import {
  cashEffect,
  emptyForm,
  fromTransaction,
  sellExceedsHeld,
  toInput,
  validate,
} from './transactionForm'

// jsdom implements neither method on <dialog>; both only flip the open state.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

describe('TransactionDialog', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  // With onSubmit the caller keeps the row itself, so the dialog must
  // hand over the input and send nothing.
  it('hands the input to onSubmit and saves nothing', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    const onSubmit = vi.fn()
    const onClose = vi.fn()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={onClose} onSubmit={onSubmit} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
    await chooseOption(screen.getByLabelText('Para birimi'), 'Amerikan Doları (USD)')
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))

    expect(onSubmit).toHaveBeenCalledWith(
      {
        type: 'deposit',
        date: todayIstanbul(),
        amount: '500',
        currency: 'USD',
        fee: '0',
        note: '',
      },
      undefined,
    )
    expect(onClose).toHaveBeenCalled()
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  // Backdrop, Escape and Vazgeç close an untouched form at once, but
  // ask before throwing away a changed one.
  it('asks before closing a changed form', async () => {
    const onClose = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={onClose} onSubmit={vi.fn()} />
      </QueryClientProvider>,
    )
    const form = screen.getByRole('dialog', { name: 'İşlem ekle' })

    fireEvent.click(screen.getByRole('button', { name: 'Vazgeç' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByLabelText('Komisyon'), { target: { value: '5' } })
    fireEvent.click(form)
    const confirm = await screen.findByRole('dialog', {
      name: 'Kaydedilmemiş değişiklikler silinsin mi?',
    })
    fireEvent.click(within(confirm).getByRole('button', { name: 'Vazgeç' }))
    await waitFor(() => expect(confirm).not.toHaveAttribute('open'))
    expect(onClose).toHaveBeenCalledTimes(1)

    fireEvent(form, new Event('cancel'))
    fireEvent.click(await screen.findByRole('button', { name: 'Değişiklikleri sil' }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(2))
  })

  // The type control is a radio group, so one Tab stop and the
  // arrow keys move and select.
  it('moves the type with the arrow keys', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={vi.fn()} onSubmit={vi.fn()} />
      </QueryClientProvider>,
    )

    const buy = screen.getByRole('radio', { name: 'Alış' })
    expect(buy).toHaveAttribute('tabindex', '0')
    buy.focus()
    fireEvent.keyDown(buy, { key: 'ArrowRight' })

    const sell = screen.getByRole('radio', { name: 'Satış' })
    expect(sell).toHaveAttribute('aria-checked', 'true')
    expect(sell).toHaveFocus()
    expect(sell).toHaveAttribute('tabindex', '0')
    expect(buy).toHaveAttribute('tabindex', '-1')

    fireEvent.keyDown(sell, { key: 'ArrowUp' })
    fireEvent.keyDown(buy, { key: 'ArrowLeft' })
    expect(screen.getByRole('radio', { name: 'Ücret' })).toHaveAttribute('aria-checked', 'true')
  })

  // A fee row's amount is the fee, so it has no fee field, and one
  // typed under another type before switching must not be sent.
  it('hides the fee field for a fee row and sends fee 0', () => {
    const onSubmit = vi.fn()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={vi.fn()} onSubmit={onSubmit} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.change(screen.getByLabelText('Komisyon'), { target: { value: '1000' } })
    fireEvent.click(screen.getByRole('radio', { name: 'Ücret' }))
    expect(screen.queryByLabelText('Komisyon')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'fee', amount: '1', fee: '0' }),
      undefined,
    )
  })

  // The error used to stay until the next submit, so it sat under a
  // field the user had already fixed.
  it('clears the validation error when a field changes', async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={vi.fn()} onSubmit={vi.fn()} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))
    expect(screen.getByText('Enstrüman seçmelisin.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '10' } })
    expect(screen.queryByText('Enstrüman seçmelisin.')).not.toBeInTheDocument()

    // The currency Select fires no change event for the form to catch.
    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.click(screen.getByRole('button', { name: 'Ekle' }))
    expect(screen.getByText("Tutar 0'dan büyük olmalı.")).toBeInTheDocument()
    await chooseOption(screen.getByLabelText('Para birimi'), 'Euro (EUR)')
    expect(screen.queryByText("Tutar 0'dan büyük olmalı.")).not.toBeInTheDocument()
  })

  // The type switch used to clear the instrument, so a Buy -> Deposit
  // -> Buy round trip left the field empty and the save failed validation.
  it('keeps the instrument and the amount across a type round trip', () => {
    const onSubmit = vi.fn()
    const buy: Transaction = {
      id: 7,
      portfolio_id: 1,
      instrument_id: 42,
      instrument_symbol: 'AAPL',
      instrument_name: 'Apple Inc.',
      type: 'buy',
      date: '2026-09-01',
      quantity: '2',
      price: '230',
      amount: '460',
      fee: '0',
      currency: 'USD',
      note: null,
      created_at: '2026-09-01T00:00:00Z',
      import_id: null,
    }

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={buy} onClose={vi.fn()} onSubmit={onSubmit} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
    fireEvent.click(screen.getByRole('radio', { name: 'Alış' }))

    expect(screen.getByLabelText('Enstrüman')).toHaveValue('AAPL - Apple Inc.')
    expect(screen.getByLabelText('Adet')).toHaveValue('2')
    expect(screen.getByLabelText('Fiyat')).toHaveValue('230')

    // Back to the cash type: the amount typed there survived too.
    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    expect(screen.getByLabelText('Tutar')).toHaveValue('500')

    fireEvent.click(screen.getByRole('radio', { name: 'Alış' }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    // The payload carries only what a buy uses: the instrument's currency, not
    // the deposit's, and the buy's own amount rather than the deposit's, since
    // quantity and price are unchanged.
    expect(onSubmit).toHaveBeenCalledWith(
      {
        type: 'buy',
        date: '2026-09-01',
        instrument_id: 42,
        quantity: '2',
        price: '230',
        amount: '460',
        fee: '0',
        currency: 'USD',
        note: '',
      },
      'AAPL',
    )
  })

  // An imported trade stores the statement's rounded amount, which a
  // save deriving quantity × price used to replace.
  it('keeps a trade amount on save until quantity or price changes', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => new Response('{}', { status: 200 }))
    const imported: Transaction = {
      id: 9,
      portfolio_id: 1,
      instrument_id: 43,
      instrument_symbol: 'VTI',
      instrument_name: 'Vanguard Total Stock Market ETF',
      type: 'buy',
      date: '2026-09-01',
      quantity: '0.006218342',
      price: '331.82',
      amount: '2.06',
      fee: '0',
      currency: 'USD',
      note: null,
      created_at: '2026-09-01T00:00:00Z',
      import_id: 3,
    }
    const putBodies = () =>
      fetchSpy.mock.calls
        .filter(([path, init]) => path === '/api/transactions/9' && init?.method === 'PUT')
        .map(([, init]) => JSON.parse(init?.body as string))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={imported} onClose={vi.fn()} />
      </QueryClientProvider>,
    )

    fireEvent.change(screen.getByLabelText(/^Not/), { target: { value: 'checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    await waitFor(() => expect(putBodies()).toHaveLength(1))
    expect(putBodies()[0]).toMatchObject({ note: 'checked', amount: '2.06' })

    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '0,005' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    await waitFor(() => expect(putBodies()).toHaveLength(2))
    expect(putBodies()[1]).toMatchObject({ quantity: '0.005' })
    expect(putBodies()[1]).not.toHaveProperty('amount')

    // Retyped with trailing zeros, they equal the stored values again.
    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '0,006218342' } })
    fireEvent.change(screen.getByLabelText('Fiyat'), { target: { value: '331,820' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    await waitFor(() => expect(putBodies()).toHaveLength(3))
    expect(putBodies()[2]).toMatchObject({ amount: '2.06' })
  })

  // A sell over the quantity held was saved without a word. The
  // warning must not block the save, and an edited sell must count its own
  // quantity as held, since the summary already took it off.
  it('warns about a sell over the quantity held and still saves', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => new Response('{}', { status: 200 }))
    const sell: Transaction = {
      id: 10,
      portfolio_id: 1,
      instrument_id: 44,
      instrument_symbol: 'THYAO',
      instrument_name: 'Türk Hava Yolları',
      type: 'sell',
      date: '2026-09-02',
      quantity: '10',
      price: '120',
      amount: '1200',
      fee: '0',
      currency: 'TRY',
      note: null,
      created_at: '2026-09-02T00:00:00Z',
      import_id: null,
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    })
    // Bought 10, sold all 10 in the row being edited.
    queryClient.setQueryData(summaryQueryKey(1), {
      positions: [{ instrument_id: 44, quantity: '0' }],
    })
    const warning = 'Elindeki adetten fazla satıyorsun (10).'

    render(
      <QueryClientProvider client={queryClient}>
        <TransactionDialog portfolioId={1} editing={sell} onClose={vi.fn()} />
      </QueryClientProvider>,
    )

    expect(screen.queryByText(warning)).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Adet'), { target: { value: '15' } })
    expect(screen.getByText(warning)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/transactions/10',
        expect.objectContaining({ method: 'PUT' }),
      ),
    )
  })

  // Above 2^53 units Number(held) rounded the hint's quantity.
  it('shows a held quantity above 2^53 units exactly', () => {
    const form = { ...emptyForm('sell'), instrumentId: 44, quantity: '20000000' }
    const positions = [{ instrument_id: 44, quantity: '12345678.123456789' } as Position]
    expect(sellExceedsHeld(form, positions, null)).toBe('12.345.678,123456789')
  })

  // A browser's showModal() focuses the first focusable element in
  // tree order, the "Alış" radio, even when the dialog opens on another type.
  it('focuses the checked type radio on open', () => {
    vi.spyOn(HTMLDialogElement.prototype, 'showModal').mockImplementation(function (
      this: HTMLDialogElement,
    ) {
      this.open = true
      this.querySelector<HTMLElement>('button, input, select, textarea')?.focus()
    })

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog
          portfolioId={1}
          editing={null}
          initial={emptyForm('sell')}
          onClose={vi.fn()}
        />
      </QueryClientProvider>,
    )

    expect(screen.getByRole('radio', { name: 'Satış' })).toHaveFocus()
  })

  // Focus must be inside the open dialog after a rejected save. The
  // submit button is pending, not disabled, during the request, so Chromium
  // does not drop its focus to <body>.
  it('keeps focus in the dialog after a rejected save', async () => {
    let reject: (response: Response) => void = () => {}
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => new Promise<Response>((resolve) => (reject = resolve)),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={vi.fn()} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    // A saved row keeps its note; only the import preview drops it.
    expect(screen.getByLabelText('Not · isteğe bağlı')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Tutar'), { target: { value: '500' } })
    const submit = screen.getByRole('button', { name: 'Ekle' })
    submit.focus()
    fireEvent.click(submit)
    await waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(submit).toBeEnabled()
    expect(submit).toHaveFocus()

    reject(new Response('{"error":"boom"}', { status: 400 }))
    await screen.findByRole('alert')
    await waitFor(() => expect(submit).not.toHaveAttribute('aria-disabled'))
    expect(screen.getByRole('dialog')).toContainElement(document.activeElement as HTMLElement)
  })

  // A validation error sits on the field that caused it, described by
  // its message, and takes focus even when submitted from another input.
  it('shows a validation error on its field and focuses it', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} onClose={vi.fn()} onSubmit={vi.fn()} />
      </QueryClientProvider>,
    )

    fireEvent.click(screen.getByRole('radio', { name: 'Yatırma' }))
    const amount = screen.getByLabelText('Tutar')
    const date = screen.getByLabelText('Tarih')
    date.focus()
    fireEvent.keyDown(date, { key: 'Enter', ctrlKey: true })

    const message = screen.getByRole('alert')
    expect(message).toHaveTextContent("Tutar 0'dan büyük olmalı.")
    expect(amount).toHaveAttribute('aria-invalid', 'true')
    expect(amount).toHaveAttribute('aria-describedby', message.id)
    expect(date).not.toHaveAttribute('aria-invalid')
    expect(document.activeElement).toBe(amount)

    // Any edit clears it.
    fireEvent.change(amount, { target: { value: '5' } })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(amount).not.toHaveAttribute('aria-invalid')
  })
})

// Several trades in a row, by button or by ⌘↵.
describe('add and new', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  const sell = {
    ...emptyForm('sell', '2026-09-01'),
    instrumentId: 44,
    instrumentLabel: 'THYAO - Türk Hava Yolları',
    instrumentSymbol: 'THYAO',
    instrumentCurrency: 'TRY',
    quantity: '5',
    price: '120',
    fee: '3',
    note: 'first',
  }

  function renderAdd(onClose = vi.fn()) {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(async () => new Response('{}', { status: 200 }))
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} initial={sell} onClose={onClose} />
      </QueryClientProvider>,
    )
    return fetchSpy
  }

  async function expectSavedAndReset(fetchSpy: ReturnType<typeof renderAdd>, onClose: () => void) {
    await waitFor(() =>
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/portfolios/1/transactions',
        expect.objectContaining({ method: 'POST' }),
      ),
    )
    const instrument = screen.getByLabelText('Enstrüman')
    await waitFor(() => expect(instrument).toHaveValue(''))
    expect(screen.getByRole('radio', { name: 'Satış' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByLabelText('Tarih')).toHaveValue('2026-09-01')
    expect(screen.getByLabelText('Adet')).toHaveValue('')
    expect(screen.getByLabelText('Fiyat')).toHaveValue('')
    expect(screen.getByLabelText('Komisyon')).toHaveValue('0')
    expect(screen.getByLabelText(/^Not/)).toHaveValue('')
    expect(instrument).toHaveFocus()
    expect(onClose).not.toHaveBeenCalled()
  }

  it('shows Ekle ve yeni in add mode only', () => {
    renderAdd()
    expect(screen.getByRole('button', { name: 'Ekle ve yeni' })).toBeInTheDocument()
    cleanup()

    const editing: Transaction = {
      id: 10,
      portfolio_id: 1,
      instrument_id: 44,
      instrument_symbol: 'THYAO',
      instrument_name: 'Türk Hava Yolları',
      type: 'buy',
      date: '2026-09-02',
      quantity: '10',
      price: '120',
      amount: '1200',
      fee: '0',
      currency: 'TRY',
      note: null,
      created_at: '2026-09-02T00:00:00Z',
      import_id: null,
    }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={editing} onClose={vi.fn()} />
      </QueryClientProvider>,
    )
    expect(screen.queryByRole('button', { name: 'Ekle ve yeni' })).not.toBeInTheDocument()
  })

  it('saves, keeps type and date, clears the rest and focuses the first field', async () => {
    const onClose = vi.fn()
    const fetchSpy = renderAdd(onClose)
    fireEvent.click(screen.getByRole('button', { name: 'Ekle ve yeni' }))
    await expectSavedAndReset(fetchSpy, onClose)
  })

  it('does the same on ⌘↵', async () => {
    const onClose = vi.fn()
    const fetchSpy = renderAdd(onClose)
    fireEvent.keyDown(screen.getByLabelText(/^Not/), { key: 'Enter', metaKey: true })
    await expectSavedAndReset(fetchSpy, onClose)
  })

  // The buttons ignore clicks while a save runs; ⌘↵ must too, or a
  // second press on a slow connection books the row twice.
  it('ignores a second ⌘↵ while the save is running', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>(() => {}))
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} initial={sell} onClose={vi.fn()} />
      </QueryClientProvider>,
    )
    const note = screen.getByLabelText(/^Not/)
    fireEvent.keyDown(note, { key: 'Enter', metaKey: true })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Ekle' })).toHaveAttribute('aria-disabled', 'true'),
    )
    fireEvent.keyDown(note, { key: 'Enter', metaKey: true })
    await new Promise((resolve) => setTimeout(resolve, 0))
    // The sell also loads the summary for the oversell hint; count the saves.
    const posts = fetchSpy.mock.calls.filter(([, init]) => init?.method === 'POST')
    expect(posts).toHaveLength(1)
  })

  // isPending reaches the buttons a tick after the click, so a
  // double click's second click came before pending and booked the row twice.
  it.each(['Ekle', 'Ekle ve yeni'])('books the row once on a double click of %s', async (name) => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>(() => {}))
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog portfolioId={1} editing={null} initial={sell} onClose={vi.fn()} />
      </QueryClientProvider>,
    )
    const button = screen.getByRole('button', { name })
    fireEvent.click(button)
    fireEvent.click(button)
    await waitFor(() => expect(button).toHaveAttribute('aria-disabled', 'true'))
    const posts = fetchSpy.mock.calls.filter(([, init]) => init?.method === 'POST')
    expect(posts).toHaveLength(1)
  })

  it('shows the ⌘↵ hint only with a fine pointer', () => {
    const pointer = (fine: boolean) =>
      vi.stubGlobal('matchMedia', (query: string) => ({
        matches: query === '(pointer: fine)' && fine,
        media: query,
        addEventListener() {},
        removeEventListener() {},
      }))
    const hint = /ekle ve yeni işleme geç/

    pointer(false)
    renderAdd()
    expect(screen.queryByText(hint)).not.toBeInTheDocument()
    cleanup()

    pointer(true)
    renderAdd()
    expect(screen.getByText(hint)).toBeInTheDocument()
  })
})

describe('TransactionDialog numbers', () => {
  const dividend: Transaction = {
    id: 8,
    portfolio_id: 1,
    instrument_id: 42,
    instrument_symbol: 'THYAO',
    instrument_name: 'Türk Hava Yolları',
    type: 'dividend',
    date: '2026-09-01',
    quantity: null,
    price: null,
    amount: '150',
    fee: '22.5',
    currency: 'TRY',
    note: null,
    created_at: '2026-09-01T00:00:00Z',
    import_id: null,
  }

  function renderEditing(onSubmit = vi.fn()) {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TransactionDialog
          portfolioId={1}
          editing={dividend}
          onClose={vi.fn()}
          onSubmit={onSubmit}
        />
      </QueryClientProvider>,
    )
  }

  afterEach(cleanup)

  // "1e3" is not below zero, it is not a number at all.
  it('tells a value that is not a number from one that is not above 0', () => {
    renderEditing()
    const amount = screen.getByLabelText('Tutar')
    const save = screen.getByRole('button', { name: 'Kaydet' })

    fireEvent.change(amount, { target: { value: '1e3' } })
    fireEvent.click(save)
    expect(
      screen.getByText(
        'Tutar geçerli bir sayı olmalı (ondalık için nokta veya virgül kullanabilirsin).',
      ),
    ).toBeInTheDocument()

    for (const value of ['0', '-5', '']) {
      fireEvent.change(amount, { target: { value } })
      fireEvent.click(save)
      expect(screen.getByText("Tutar 0'dan büyük olmalı.")).toBeInTheDocument()
    }

    // The hint still wins over both.
    fireEvent.change(amount, { target: { value: '10.000' } })
    fireEvent.click(save)
    expect(screen.getByText('Binlik ayırıcı mı? 10000 veya 10.000,00 yaz.')).toBeInTheDocument()
  })

  // A positive value past the server's digit limit was reported as
  // "must be greater than 0".
  it('names the digit limit for a positive value with too many digits', () => {
    const form = { ...emptyForm('buy'), instrumentId: 1, quantity: '0,0000000001', price: '10' }
    expect(validate(form)).toEqual({
      field: 'quantity',
      message: 'Adet en fazla 15 tam ve 9 ondalık basamak olabilir.',
    })
  })

  // The edit form showed the API's "22.5" next to a page of "₺22,50".
  it('prefills API decimals with the locale separator and saves them unchanged', () => {
    const onSubmit = vi.fn()
    renderEditing(onSubmit)

    expect(screen.getByLabelText('Tutar')).toHaveValue('150')
    expect(screen.getByLabelText('Stopaj')).toHaveValue('22,5')

    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ amount: '150', fee: '22.5' }),
      'THYAO',
    )
  })
})

// Interest is a dividend with no instrument on the API, and one
// comes back as interest, whether saved or an import preview row (a Midas
// nema) with no symbol. A preview dividend whose symbol is still unknown stays
// a dividend waiting for its instrument.
describe('interest', () => {
  const nema: Transaction = {
    id: 5,
    portfolio_id: 1,
    instrument_id: null,
    instrument_symbol: null,
    instrument_name: null,
    type: 'dividend',
    date: '2025-12-31',
    quantity: null,
    price: null,
    amount: '47.93',
    fee: '7.19',
    currency: 'TRY',
    note: null,
    created_at: '',
    import_id: null,
  }

  it('round-trips through the form as a dividend with no instrument', () => {
    const form = fromTransaction(nema)
    expect(form.type).toBe('interest')
    expect(validate(form)).toBeNull()
    expect(toInput({ ...form, currency: 'USD' })).toEqual({
      type: 'dividend',
      date: '2025-12-31',
      amount: '47.93',
      fee: '7.19',
      currency: 'USD',
      note: '',
    })
    expect(fromTransaction({ ...nema, instrument_symbol: 'XYZ' }).type).toBe('dividend')
  })
})

describe('cashEffect', () => {
  // A fee above the proceeds showed "Added to cash +-$4.00".
  it('flips to cash out when the fee exceeds the incoming amount', () => {
    const sell = { ...emptyForm('sell'), quantity: '1', price: '1', fee: '5' }
    expect(cashEffect(sell)).toEqual({ out: true, amount: 4 })
    const dividend = { ...emptyForm('dividend'), amount: '10', fee: '12,5' }
    expect(cashEffect(dividend)).toEqual({ out: true, amount: 2.5 })
    expect(cashEffect({ ...sell, fee: '0,5' })).toEqual({ out: false, amount: 0.5 })
  })
})
