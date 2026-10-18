import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ExpectedDividend } from '../api/dividends'
import type { DuePlan } from '../api/plans'
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

const watched: AttentionItem & { kind: 'watch-target' } = {
  kind: 'watch-target',
  item: {
    id: 1,
    instrument_id: 7,
    symbol: 'THYAO',
    name: 'Türk Hava Yolları',
    market: 'BIST',
    currency: 'TRY',
    last_price: '312.50',
    price_date: '2026-10-16',
    price_source: 'bist',
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    target_price: '300.00',
    target_side: 'above',
    target_reached: true,
    note: '',
    created_at: '2026-10-01T00:00:00Z',
  },
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
  watched,
  { kind: 'missing-rate', currency: 'EUR', positions: 1 },
  { kind: 'missing-price', instrument_id: 4, symbol: 'XYZ' },
  { kind: 'stale-manual-prices', count: 3, days: 7, instrument_id: 5, symbol: 'ABC' },
]

function renderCard(list: AttentionItem[]) {
  const handlers = {
    onAddDividend: vi.fn(),
    onDismissDividend: vi.fn(),
    onCloseDeposit: vi.fn(),
    onBookPlan: vi.fn(),
    onSkipPlan: vi.fn(),
    onEditPrice: vi.fn(),
  }
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

  it('words an allocation drift item under Yoğunlaşma, signed', () => {
    renderCard([{ kind: 'allocation-drift', assetClass: 'Hisse senedi', drift: -6.04 }])
    expect(screen.getByText('Hisse senedi -6,0 p')).toBeTruthy()
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Yoğunlaşma',
    ])
  })

  it('lists a due deposit with its net, gross and stopaj under its own group', () => {
    const deposit = {
      id: 5,
      currency: 'TRY',
      principal: '20000.00',
      maturity_date: '2026-10-12',
      gross: '3829.18',
      stopaj: '574.38',
      net: '3254.80',
    }
    const handlers = renderCard([{ kind: 'due-deposit', deposit }])
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Vadesi gelen mevduat',
    ])
    expect(screen.getByText('₺3.254,80')).toBeTruthy()
    expect(screen.getByText(/brüt ₺3\.829,18 · stopaj ₺574,38/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /^Vadeyi kapat/ }))
    expect(handlers.onCloseDeposit).toHaveBeenCalledWith(deposit)
  })

  describe('watch targets', () => {
    it('lists a reached target under İzleme listesi with a link to the list', () => {
      renderCard([watched])
      expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
        'İzleme listesi',
      ])
      expect(screen.getByText('THYAO')).toHaveClass('font-semibold')
      expect(screen.getByText(/hedefinin üstünde: ₺312,50/)).toHaveTextContent(
        'THYAO ₺300,00 hedefinin üstünde: ₺312,50',
      )
      expect(screen.getByRole('link', { name: 'Listeye git' })).toHaveAttribute(
        'href',
        '/watchlist',
      )
      // It asks for no click, so the header shows no count.
      expect(screen.queryByRole('img')).toBeNull()
    })

    it('words a target on the below side', () => {
      renderCard([{ kind: 'watch-target', item: { ...watched.item, target_side: 'below' } }])
      expect(screen.getByText(/hedefinin altında: ₺312,50/)).toBeTruthy()
    })
  })

  describe('due plans', () => {
    const plan: DuePlan = {
      plan_id: 7,
      instrument_id: 3,
      symbol: 'GARAN',
      market: 'BIST',
      currency: 'TRY',
      amount: '1000.00',
      date: '2026-10-12',
      missed: 1,
      price: '123.45',
      price_date: '2026-10-12',
      quantity: '8',
      booked_amount: '987.60',
    }

    it('lists a due buy with its units and close, and Kaydet and Atla call back', () => {
      const handlers = renderCard([{ kind: 'due-plan', plan }])
      expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
        'Planlı alımlar',
      ])
      expect(screen.getByText(/8 adet × ₺123,45/)).toBeTruthy()
      fireEvent.click(screen.getByRole('button', { name: /^Kaydet: GARAN/ }))
      expect(handlers.onBookPlan).toHaveBeenCalledTimes(1)
      expect(handlers.onBookPlan).toHaveBeenCalledWith(plan)
      fireEvent.click(screen.getByRole('button', { name: /^Dönemi atla: GARAN/ }))
      expect(handlers.onSkipPlan).toHaveBeenCalledWith(plan)
    })

    it('shows fiyat yok and disables Kaydet for a buy with no close', () => {
      const noClose = {
        ...plan,
        price: null,
        price_date: null,
        quantity: null,
        booked_amount: null,
        missed: 3,
      }
      renderCard([{ kind: 'due-plan', plan: noClose }])
      expect(screen.getByText(/fiyat yok/)).toBeTruthy()
      expect(screen.getByText(/3 dönem bekliyor/)).toBeTruthy()
      expect(screen.getByRole('button', { name: /^Kaydet: GARAN/ })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^Dönemi atla: GARAN/ })).toBeEnabled()
    })

    it('says the amount buys no unit and disables Kaydet for a zero quantity', () => {
      const noUnit = { ...plan, price: '310', quantity: '0', booked_amount: '0.00' }
      renderCard([{ kind: 'due-plan', plan: noUnit }])
      expect(screen.getByText(/bir birim bile almıyor/)).toBeTruthy()
      expect(screen.queryByText(/0 adet/)).toBeNull()
      expect(screen.getByRole('button', { name: /^Kaydet: GARAN/ })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^Dönemi atla: GARAN/ })).toBeEnabled()
    })

    // Booking GARAN moves its next date past THYAO's, so the list re-sorts;
    // the focused button has to stay with GARAN's plan, not the row index.
    it('keeps focus on the booked plan when it stays due and re-sorts', () => {
      const garan: AttentionItem = { kind: 'due-plan', plan }
      const thyao: AttentionItem = {
        kind: 'due-plan',
        plan: { ...plan, plan_id: 8, symbol: 'THYAO', date: '2026-10-20' },
      }
      const garanNext: AttentionItem = {
        kind: 'due-plan',
        plan: { ...plan, date: '2026-11-12' },
      }
      const card = (list: AttentionItem[], busy: boolean) => (
        <MemoryRouter>
          <AttentionCard
            items={list}
            busy={busy}
            onAddDividend={vi.fn()}
            onDismissDividend={vi.fn()}
            onCloseDeposit={vi.fn()}
            onBookPlan={vi.fn()}
            onSkipPlan={vi.fn()}
            onEditPrice={vi.fn()}
          />
        </MemoryRouter>
      )
      const { rerender } = render(card([garan, thyao], false))
      const kaydet = screen.getByRole('button', { name: /^Kaydet: GARAN/ })
      kaydet.focus()
      fireEvent.click(kaydet)
      rerender(card([garan, thyao], true))
      rerender(card([thyao, garanNext], false))
      expect(document.activeElement).toBe(screen.getByRole('button', { name: /^Kaydet: GARAN/ }))
    })

    it('keeps Kaydet enabled for a cash deposit plan, which needs no close', () => {
      renderCard([
        {
          kind: 'due-plan',
          plan: {
            ...plan,
            instrument_id: null,
            symbol: null,
            market: null,
            quantity: null,
            price: null,
          },
        },
      ])
      expect(screen.getByText('Para girişi')).toBeTruthy()
      expect(screen.getByRole('button', { name: /^Kaydet: Para girişi/ })).toBeEnabled()
    })
  })

  it('renders the groups in order with their headings', () => {
    renderCard(items)
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Veri',
      'Temettü onayı',
      'İzleme listesi',
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
          onCloseDeposit={vi.fn()}
          onBookPlan={vi.fn()}
          onSkipPlan={vi.fn()}
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
      const handlers = {
        onAddDividend: vi.fn(),
        onDismissDividend: vi.fn(),
        onCloseDeposit: vi.fn(),
        onBookPlan: vi.fn(),
        onSkipPlan: vi.fn(),
        onEditPrice: vi.fn(),
      }
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
    expect(screen.queryByText(/312/)).toBeNull()
    expect(screen.getAllByText(/•••/)).toHaveLength(3)
    // A share is not an amount and stays.
    expect(screen.getByText('%46,6')).toBeTruthy()
  })
})
