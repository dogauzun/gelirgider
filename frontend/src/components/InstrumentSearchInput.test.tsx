import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n/strings'
import { InstrumentSearchInput } from './InstrumentSearchInput'

describe('InstrumentSearchInput', () => {
  // jsdom does not implement scrollIntoView, which the arrow keys call.
  const scrollIntoView = vi.fn()
  beforeEach(() => {
    scrollIntoView.mockClear()
    Element.prototype.scrollIntoView = scrollIntoView
  })

  afterEach(() => {
    vi.restoreAllMocks()
    setLocale('tr')
  })

  // GLG-225: an empty result set used to render nothing at all, so the user
  // could not tell "no match" from "still loading".
  it('shows the empty state once a query with no matches settles', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('[]'))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="ZZZZQQ" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    expect(screen.queryByText('Sonuç yok')).not.toBeInTheDocument()

    expect(await screen.findByText('Sonuç yok', { selector: 'p' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Sonuç yok')
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/instruments?q=ZZZZQQ',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  // GLG-1305: the edit dialog holds a picked instrument's label, which the
  // search cannot match, so focusing the field said "Sonuç yok" under it.
  it('does not search for the label of a picked instrument', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('[]'))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput
          value="THYAO - TURK HAVA YOLLARI"
          selected
          onChange={() => {}}
          onSelect={() => {}}
        />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    // Past the 300 ms delay, with time for a search it started to settle.
    await act(() => new Promise((resolve) => setTimeout(resolve, 500)))

    expect(screen.queryAllByText('Sonuç yok')).toHaveLength(0)
    expect(screen.getByRole('status')).toHaveTextContent('')
    expect(screen.getByRole('combobox')).toHaveAttribute('aria-expanded', 'false')
    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  // GLG-812: nothing showed while the query was in flight, and the outcome
  // was not announced to screen readers.
  it('shows a loading row in flight, then announces the result count', async () => {
    let respond: (response: Response) => void = () => {}
    vi.spyOn(globalThis, 'fetch').mockReturnValue(
      new Promise((resolve) => {
        respond = resolve
      }),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    expect(await screen.findByText('Aranıyor...')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('')

    respond(
      new Response(
        JSON.stringify([
          { id: 1, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
          { id: 2, symbol: 'GARFA', market: 'BIST', name: 'Garanti Faktoring', currency: 'TRY' },
        ]),
      ),
    )
    await screen.findByRole('listbox')
    expect(screen.queryByText('Aranıyor...')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('2 sonuç')
  })

  // GLG-1192: a search cut at the API's 20 rows said "20 sonuç", as if that
  // were every match, so a fund past the cap looked missing.
  it('says the results are cut at the cap and to keep typing', async () => {
    const funds = Array.from({ length: 20 }, (_, i) => ({
      id: i + 1,
      symbol: `F${String(i).padStart(2, '0')}`,
      market: 'TEFAS',
      name: `İSTANBUL PORTFÖY FON ${i}`,
      currency: 'TRY',
    }))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(funds)))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="istanbul portföy" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    const hint = 'İlk 20 sonuç, daraltmak için yazmaya devam et'
    expect(await screen.findByText(hint, { selector: 'p' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(hint)
    expect(screen.getAllByRole('option')).toHaveLength(20)
  })

  // GLG-323: the result list was mouse-only; Tab dropped focus to <body>.
  it('selects the first result with ArrowDown then Enter', async () => {
    const thyao = {
      id: 7,
      symbol: 'THYAO',
      market: 'BIST',
      name: 'Türk Hava Yolları',
      currency: 'TRY',
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify([thyao])))
    const onSelect = vi.fn()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="THY" onChange={() => {}} onSelect={onSelect} />
      </QueryClientProvider>,
    )

    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    await screen.findByRole('option', { name: /THYAO/ })
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onSelect).toHaveBeenCalledWith(thyao)
  })

  // GLG-991: for 300 ms after the text changed, the list still held the
  // previous query's results, and ArrowDown then Enter picked one of them.
  it('lists and selects nothing while the text differs from the searched query', async () => {
    const thyao = { id: 7, symbol: 'THYAO', market: 'BIST', name: 'THY', currency: 'TRY' }
    const asels = { id: 8, symbol: 'ASELS', market: 'BIST', name: 'Aselsan', currency: 'TRY' }
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async (url) => new Response(JSON.stringify(String(url).includes('THY') ? [thyao] : [asels])),
    )
    const onSelect = vi.fn()
    const client = new QueryClient()
    const renderSearch = (value: string) => (
      <QueryClientProvider client={client}>
        <InstrumentSearchInput value={value} onChange={() => {}} onSelect={onSelect} />
      </QueryClientProvider>
    )

    const { rerender } = render(renderSearch('THY'))
    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    await screen.findByRole('option', { name: /THYAO/ })
    expect(screen.getByRole('status')).toHaveTextContent('1 sonuç')

    // Fake timers from here on, so the 300 ms delay never ends on its own.
    vi.useFakeTimers()
    try {
      rerender(renderSearch('ASE'))
      fireEvent.keyDown(input, { key: 'ArrowDown' })
      fireEvent.keyDown(input, { key: 'Enter' })
      expect(onSelect).not.toHaveBeenCalled()
      expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
      expect(screen.queryByText('Sonuç yok')).not.toBeInTheDocument()
      expect(screen.getByRole('status')).toHaveTextContent('')
      act(() => {
        vi.advanceTimersByTime(300)
      })
    } finally {
      vi.useRealTimers()
    }

    // After the delay the new query's results are listed and selectable.
    await screen.findByRole('option', { name: /ASELS/ })
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith(asels)
  })

  // GLG-425: Chromium makes a scrolling listbox a Tab stop, so Tab from a long
  // result list landed on it and focus fell to <body>. jsdom does not model
  // scroller focusability, so assert the listbox opts out of the Tab order.
  it('keeps the result list out of the Tab order', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify([
          { id: 1, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
        ]),
      ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    expect(await screen.findByRole('listbox')).toHaveAttribute('tabindex', '-1')
  })

  // GLG-433: ArrowDown past the visible area left the highlighted option
  // scrolled out of view. jsdom has no layout, so assert the highlighted
  // option asks to be scrolled into view.
  it('scrolls the highlighted option into view', async () => {
    const results = Array.from({ length: 15 }, (_, i) => ({
      id: i + 1,
      symbol: `GAR${i}`,
      market: 'BIST',
      name: `Garanti ${i}`,
      currency: 'TRY',
    }))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(results)))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    await screen.findByRole('option', { name: /GAR11/ })
    for (let i = 0; i < 12; i++) fireEvent.keyDown(input, { key: 'ArrowDown' })

    const highlighted = screen.getByRole('option', { name: /GAR11/ })
    expect(input).toHaveAttribute('aria-activedescendant', highlighted.id)
    expect(scrollIntoView.mock.contexts.at(-1)).toBe(highlighted)
    expect(scrollIntoView).toHaveBeenLastCalledWith(expect.objectContaining({ block: 'nearest' }))
  })

  // GLG-445: ArrowUp with nothing highlighted landed on the second-to-last
  // option instead of the last.
  it('highlights the last result with ArrowUp from no highlight', async () => {
    const results = Array.from({ length: 3 }, (_, i) => ({
      id: i + 1,
      symbol: `GAR${i}`,
      market: 'BIST',
      name: `Garanti ${i}`,
      currency: 'TRY',
    }))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(results)))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    const last = await screen.findByRole('option', { name: /GAR2/ })
    fireEvent.keyDown(input, { key: 'ArrowUp' })

    expect(input).toHaveAttribute('aria-activedescendant', last.id)
  })

  // GLG-440: the list was absolute, so the import preview's scrolling table
  // card cut it off under its last rows. It is portaled out of the input's
  // ancestors; Base UI places it, above the input when there is no room below.
  it('renders the list outside the input’s overflow ancestors', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify([
          { id: 1, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
        ]),
      ),
    )

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <div className="overflow-auto">
          <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
        </div>
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    expect(container).not.toContainElement(await screen.findByRole('listbox'))
  })

  // GLG-1285: Base UI writes a picked item into the input and clears it on
  // Escape with the list closed. The caller sets its own label on select (the
  // dialog's "SYMBOL - Name"), so only typing may reach onChange.
  it('passes only typed text to onChange, not a pick or an Escape', async () => {
    const thyao = { id: 7, symbol: 'THYAO', market: 'BIST', name: 'THY', currency: 'TRY' }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify([thyao])))
    const onChange = vi.fn()
    const onSelect = vi.fn()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="THY" onChange={onChange} onSelect={onSelect} />
      </QueryClientProvider>,
    )

    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    fireEvent.click(await screen.findByRole('option', { name: /THYAO/ }))
    expect(onSelect).toHaveBeenCalledWith(thyao)
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(onChange).not.toHaveBeenCalled()

    fireEvent.change(input, { target: { value: 'THYA' } })
    expect(onChange).toHaveBeenCalledWith('THYA')
  })

  // GLG-1322: Base UI's two hidden dismiss buttons, there for screen readers
  // while the list is open, were named "Dismiss" in English in the tr UI.
  it('names the list’s dismiss buttons in the UI language', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify([
          { id: 1, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
        ]),
      ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    fireEvent.focus(screen.getByRole('combobox'))
    await screen.findByRole('listbox')
    expect(screen.queryAllByRole('button', { name: 'Dismiss' })).toHaveLength(0)
    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThan(0)
    for (const button of buttons) expect(button).toHaveAccessibleName('Önerileri kapat')
  })

  // GLG-1285: Escape and a click outside close the open list.
  it('closes the list on Escape and on a click outside', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify([
          { id: 1, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
        ]),
      ),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <InstrumentSearchInput value="garan" onChange={() => {}} onSelect={() => {}} />
      </QueryClientProvider>,
    )

    const input = screen.getByRole('combobox')
    fireEvent.focus(input)
    await screen.findByRole('listbox')
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    fireEvent.focus(input)
    await screen.findByRole('listbox')
    fireEvent.pointerDown(document.body)
    fireEvent.mouseDown(document.body)
    fireEvent.click(document.body)
    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument())
  })

  // GLG-544: GOLD reads as a word per locale, BIST stays a code.
  it('labels the market per locale in the result badge', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(
          JSON.stringify([
            { id: 1, symbol: 'GRAM', market: 'GOLD', name: 'Gram Altın', currency: 'TRY' },
            { id: 2, symbol: 'GARAN', market: 'BIST', name: 'Garanti BBVA', currency: 'TRY' },
          ]),
        ),
    )
    const renderSearch = () =>
      render(
        <QueryClientProvider client={new QueryClient()}>
          <InstrumentSearchInput value="gra" onChange={() => {}} onSelect={() => {}} />
        </QueryClientProvider>,
      )

    const { unmount } = renderSearch()
    fireEvent.focus(screen.getByRole('combobox'))
    expect(await screen.findByText('Altın · TRY')).toBeInTheDocument()
    expect(screen.getByText('BIST · TRY')).toBeInTheDocument()
    unmount()

    setLocale('en')
    renderSearch()
    fireEvent.focus(screen.getByRole('combobox'))
    expect(await screen.findByText('Gold · TRY')).toBeInTheDocument()
  })
})
