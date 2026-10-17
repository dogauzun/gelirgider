import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ExpectedDividend } from '../api/dividends'
import type { AttentionItem } from '../lib/attention'
import { setPrivacy } from '../lib/privacy'
import { AttentionCard } from './AttentionCard'

const dividend: ExpectedDividend = {
  instrument_id: 9,
  symbol: 'TUPRS',
  market: 'BIST',
  date: '2026-10-12',
  quantity: '100',
  per_share: '48',
  gross: '4800.00',
  stopaj: '720.00',
  net: '4080.00',
  currency: 'TRY',
}

// Out of display order on purpose: the card groups them.
const items: AttentionItem[] = [
  { kind: 'concentration', subject: 'cash', label: 'TRY', share: 0.466 },
  {
    kind: 'mover',
    instrument_id: 3,
    symbol: 'GARAN',
    amount: '5600',
    currency: 'TRY',
    ratio: 0.018,
  },
  { kind: 'dividend', dividend },
  { kind: 'missing-rate', currency: 'EUR', positions: 1 },
  { kind: 'missing-price', instrument_id: 4, symbol: 'XYZ' },
  { kind: 'stale-manual-prices', count: 3, days: 7, instrument_id: 5, symbol: 'ABC' },
]

function renderCard(list: AttentionItem[]) {
  const handlers = { onAddDividend: vi.fn(), onDismissDividend: vi.fn(), onEditPrice: vi.fn() }
  render(
    <MemoryRouter>
      <AttentionCard items={list} portfolioId={1} {...handlers} />
    </MemoryRouter>,
  )
  return handlers
}

describe('AttentionCard', () => {
  afterEach(() => setPrivacy(false))

  it('words the TRY cash share and no-hedge items under Yoğunlaşma', () => {
    renderCard([{ kind: 'try-cash-share', share: 0.466 }, { kind: 'no-hedge' }])
    expect(
      screen.getByText("Varlığın %46,6'i TL nakitte; enflasyon karşısında değer kaybeder"),
    ).toBeTruthy()
    expect(screen.getByText('USD, EUR, altın veya ABD hissesi yok')).toBeTruthy()
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Yoğunlaşma',
    ])
  })

  it('renders the groups in order with their headings', () => {
    renderCard(items)
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Veri',
      'Temettü onayı',
      'Bugün',
      'Yoğunlaşma',
    ])
    expect(screen.getByText(/EUR kur eksikliği nedeniyle toplam dışında · 1 pozisyon/)).toBeTruthy()
    expect(screen.getByText('3 elle fiyat 7 günden eski')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'GARAN' })).toHaveAttribute(
      'href',
      '/portfolios/1/positions/3',
    )
    expect(screen.getByText('Nakit')).toBeTruthy()
  })

  // The mover's link dropped the chart range, so the position
  // page opened at 3A.
  it("keeps the chart range on a mover's link", () => {
    render(
      <MemoryRouter initialEntries={['/portfolios/1?range=1y']}>
        <AttentionCard
          items={items}
          portfolioId={1}
          onAddDividend={vi.fn()}
          onDismissDividend={vi.fn()}
          onEditPrice={vi.fn()}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: 'GARAN' })).toHaveAttribute(
      'href',
      '/portfolios/1/positions/3?range=1y',
    )
  })

  it('leaves out an empty group', () => {
    renderCard(items.filter((item) => item.kind !== 'mover'))
    expect(screen.queryByRole('heading', { name: 'Bugün' })).toBeNull()
  })

  it('says all clear with no items and shows no count', () => {
    renderCard([])
    expect(screen.getByText('Her şey yolunda')).toBeTruthy()
    expect(screen.queryByRole('heading', { level: 3 })).toBeNull()
    expect(screen.queryByRole('img')).toBeNull()
  })

  it('counts only the Veri and Temettü items in the header', () => {
    renderCard(items)
    expect(screen.getByRole('img', { name: '4 öğe işlem bekliyor' })).toHaveTextContent('4')
  })

  it('calls the handlers with the dividend and the instrument', () => {
    const handlers = renderCard(items)
    fireEvent.click(screen.getByRole('button', { name: /TUPRS .* temettüsünü ekle/ }))
    expect(handlers.onAddDividend).toHaveBeenCalledWith(dividend)
    fireEvent.click(screen.getByRole('button', { name: /TUPRS .* temettüsünü yoksay/ }))
    expect(handlers.onDismissDividend).toHaveBeenCalledWith(dividend)
    fireEvent.click(screen.getByRole('button', { name: 'XYZ için fiyat gir' }))
    expect(handlers.onEditPrice).toHaveBeenCalledWith(4)
    fireEvent.click(screen.getByRole('button', { name: 'ABC fiyatını güncelle' }))
    expect(handlers.onEditPrice).toHaveBeenCalledWith(5)
  })

  // On Tüm portföyler the same symbol's actions in two portfolios
  // had identical names; each now names its portfolio. Without a portfolio
  // (a single portfolio's Özet) the names stay as they were, as the test
  // above shows.
  it("names each action's portfolio on Tüm portföyler", () => {
    const merged = (portfolio: string): AttentionItem[] => [
      { kind: 'dividend', dividend, portfolio },
      { kind: 'missing-price', instrument_id: 4, symbol: 'XYZ', portfolio },
      {
        kind: 'stale-manual-prices',
        count: 3,
        days: 7,
        instrument_id: 5,
        symbol: 'ABC',
        portfolio,
      },
    ]
    renderCard([...merged('Ana'), ...merged('Yan')])
    const names = screen.getAllByRole('button').map((b) => b.getAttribute('aria-label'))
    expect(new Set(names).size).toBe(names.length)
    for (const name of [
      'Ana: XYZ için fiyat gir',
      'Yan: XYZ için fiyat gir',
      'Ana: ABC fiyatını güncelle',
      'Yan: ABC fiyatını güncelle',
    ])
      expect(screen.getByRole('button', { name })).toBeTruthy()
    expect(screen.getByRole('button', { name: /^Ana: TUPRS .* temettüsünü ekle$/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /^Yan: TUPRS .* temettüsünü yoksay$/ })).toBeTruthy()
  })

  // Ekle and Yoksay remove the focused button once the expected
  // list refetches, and focus fell to <body>.
  describe('focus after Ekle or Yoksay', () => {
    // As SummaryPage drives the card: busy while the request and the
    // refetch run, then the list without the acted-on dividend.
    function act(initial: AttentionItem[], after: AttentionItem[], button: () => HTMLElement) {
      const handlers = { onAddDividend: vi.fn(), onDismissDividend: vi.fn(), onEditPrice: vi.fn() }
      const card = (list: AttentionItem[], busy: boolean) => (
        <MemoryRouter>
          <AttentionCard items={list} busy={busy} {...handlers} />
        </MemoryRouter>
      )
      const { rerender } = render(card(initial, false))
      const target = button()
      target.focus()
      fireEvent.click(target)
      rerender(card(initial, true))
      rerender(card(after, false))
    }

    // Tüm portföyler: the same dividend for two portfolios, told apart by
    // the portfolio, whose name may hold a quote.
    it("moves to the previous dividend's button after Ekle on the last one", () => {
      const first: AttentionItem = { kind: 'dividend', dividend, portfolio: 'Ana "BIST"' }
      const second: AttentionItem = { kind: 'dividend', dividend, portfolio: 'Yan' }
      act(
        [first, second],
        [first],
        () => screen.getAllByRole('button', { name: /TUPRS .* temettüsünü ekle/ })[1],
      )
      expect(document.activeElement).toBe(
        screen.getByRole('button', { name: /TUPRS .* temettüsünü ekle/ }),
      )
    })

    it('moves to the card heading after Yoksay on the only dividend', () => {
      act([{ kind: 'dividend', dividend }], [], () =>
        screen.getByRole('button', { name: /TUPRS .* temettüsünü yoksay/ }),
      )
      expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'Dikkat' }))
    })
  })

  // A long portfolio name truncated the whole line, the item's own
  // text with it. The name is its own element now, the one that shortens,
  // and carries its full text as a title.
  it('keeps a long portfolio name apart from the item text, with a title', () => {
    const name = 'Uzun vadeli büyüme portföyü (çocuklar için)'
    renderCard([{ kind: 'dividend', dividend, portfolio: name }])
    const portfolio = screen.getByTitle(name)
    expect(portfolio).toHaveTextContent(name)
    expect(portfolio).not.toHaveTextContent('TUPRS')
    expect(screen.getByText('TUPRS').closest(`[title="${name}"]`)).toBeNull()
  })

  it('masks amounts in privacy mode', () => {
    setPrivacy(true)
    renderCard(items)
    expect(screen.queryByText(/4\.080/)).toBeNull()
    expect(screen.queryByText(/5\.600/)).toBeNull()
    expect(screen.getAllByText(/•••/)).toHaveLength(2)
    // A share is not an amount and stays.
    expect(screen.getByText('%46,6')).toBeTruthy()
  })
})
