import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PortfolioScope, Position, Summary } from '../api/summary'
import { setLocale, t } from '../i18n/strings'
import { formatMarketDate, formatShortMarketDate } from '../lib/date'
import { MISSING, plTone } from '../lib/format'
import { returnPercent, unrealized } from '../lib/positions'
import { setPrivacy } from '../lib/privacy'
import { ROW_40 } from './ui/table'
import { PositionsTable } from './PositionsTable'

// jsdom has no matchMedia: the table from md up, the phone list below. Nor
// showModal, for the position dialog.
let desktop = true
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

beforeEach(() => {
  // The copy-for-AI button reads the portfolio list for the name.
  vi.spyOn(globalThis, 'fetch').mockImplementation(
    async () => new Response(JSON.stringify([]), { status: 200 }),
  )
})

afterEach(() => {
  vi.restoreAllMocks()
  setPrivacy(false)
  setLocale('tr')
  desktop = true
})

function position(id: number, symbol: string, fields: Partial<Position> = {}): Position {
  return {
    instrument_id: id,
    symbol,
    name: `${symbol} Holding`,
    market: 'BIST',
    asset_class: null,
    sector: 'unknown',
    currency: 'TRY',
    quantity: '10',
    avg_cost: '50.00',
    cost_basis: '500.00',
    last_price: '60.00',
    has_price: true,
    price_source: 'bist',
    price_date: '2099-01-01',
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    market_value: '600.00',
    unrealized_pl: '100.00',
    realized_pl: '0.00',
    market_value_reporting: '600.00',
    unrealized_pl_reporting: '100.00',
    realized_pl_reporting: '0.00',
    total_pl_reporting: '100.00',
    closed: false,
    ...fields,
  }
}

function summary(positions: Position[], fields: Partial<Summary> = {}): Summary {
  return {
    currency: 'TRY',
    cash: '100.00',
    cash_by_currency: { TRY: '100.00' },
    cash_by_currency_reporting: { TRY: '100.00' },
    total_value: '1000.00',
    positions_value: '900.00',
    cost_basis: '800.00',
    unrealized_pl: '100.00',
    realized_pl: '0.00',
    fx_pl: null,
    missing_rates: [],
    positions,
    stats: { first_transaction_date: null, by_currency: {}, reporting: null },
    ...fields,
  }
}

// THYAO 600, GARAN 300 (its P/L the larger), with cash 100: weights 60% and 30%.
const twoPositions = () => [
  position(1, 'THYAO'),
  position(2, 'GARAN', {
    market_value: '300.00',
    market_value_reporting: '300.00',
    cost_basis: '100.00',
    unrealized_pl: '200.00',
    unrealized_pl_reporting: '200.00',
  }),
]

function renderTable(data: Summary, portfolioId: PortfolioScope = 1) {
  const handlers = { onTrade: vi.fn(), onEditPrice: vi.fn(), onRemovePrice: vi.fn() }
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <PositionsTable summary={data} portfolioId={portfolioId} {...handlers} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return handlers
}

const bodyRows = () => within(screen.getAllByRole('rowgroup')[1]).getAllByRole('row')
const rowOf = (symbol: string) => bodyRows().find((row) => within(row).queryByText(symbol))!
const symbols = () => bodyRows().map((row) => within(row).getAllByRole('cell')[0].textContent)
const header = (name: string) =>
  screen.getAllByRole('columnheader').find((th) => th.textContent?.startsWith(name))!

describe('PositionsTable', () => {
  it('shows the columns of the spec in order', () => {
    renderTable(summary(twoPositions()))
    expect(
      screen.getAllByRole('columnheader').map((th) => th.textContent?.replace(/[↑↓]/, '').trim()),
    ).toEqual([
      'Enstrüman',
      'Ağırlık',
      'Adet',
      'Ort. maliyet',
      'Fiyat',
      'Günlük',
      'Değer',
      'K/Z',
      'Eylemler',
    ])
    // The row menu column is named for screen readers.
    expect(screen.getByRole('columnheader', { name: 'Eylemler' })).toBeInTheDocument()
    // Its sr-only label is absolute: the scroller must be positioned to clip it.
    expect(screen.getByRole('table').parentElement).toHaveClass('relative', 'overflow-x-auto')
  })

  it('keeps a row on one 40px line without Al and Sat buttons', () => {
    renderTable(
      summary([
        position(9, 'AAPL', {
          name: 'Apple Inc.',
          currency: 'USD',
          market: 'US',
          last_price: '200.00',
          market_value: '2000.00',
          market_value_reporting: '600.00',
          day_change: '20.00',
          day_change_reporting: '6.00',
          prev_close: '198.00',
          prev_price_date: '2026-09-10',
          price_date: '2099-01-01',
        }),
      ]),
    )
    const row = rowOf('AAPL')
    expect(row.className).toContain(ROW_40)
    expect(within(row).queryByRole('button', { name: 'Al' })).toBeNull()
    expect(within(row).queryByRole('button', { name: 'Sat' })).toBeNull()
    // Symbol, name and the currency tag share the instrument cell's one line.
    const instrument = within(row).getAllByRole('cell')[0]
    const line = within(instrument).getByText('Apple Inc.').parentElement!
    expect(line).toContainElement(within(instrument).getByText('AAPL'))
    expect(line).toContainElement(within(instrument).getByText('USD'))
    expect(within(instrument).getByText('Apple Inc.')).toHaveAttribute('title', 'Apple Inc.')
    // The day change reads as a percent, its amount and dates in the tip.
    const day = within(row).getByRole('button', { name: '+%1' })
    expect(day).toHaveAccessibleDescription(
      `+$20,00 · Önceki kapanış ${formatShortMarketDate('2026-09-10')} → ${formatShortMarketDate('2099-01-01')}`,
    )
    // The P/L amount and its percent on one line.
    expect(within(row).getByText('+₺100,00 · +%20')).toBeInTheDocument()
  })

  it('weighs each position against the total including cash', () => {
    renderTable(
      summary([
        ...twoPositions(),
        // No rate: no weight, and out of the denominator.
        position(3, 'MSFT', {
          currency: 'USD',
          market_value_reporting: null,
          unrealized_pl_reporting: null,
        }),
      ]),
    )
    const weight = (symbol: string) => within(rowOf(symbol)).getAllByRole('cell')[1]
    expect(weight('THYAO')).toHaveTextContent('%60,0')
    expect(weight('GARAN')).toHaveTextContent('%30,0')
    expect(weight('MSFT')).toHaveTextContent('—')
  })

  it('sorts by Değer descending first and by the header clicked, with aria-sort', () => {
    renderTable(
      summary([
        position(3, 'AKBNK', {
          market_value_reporting: '450.00',
          unrealized_pl_reporting: '50.00',
        }),
        ...twoPositions(),
      ]),
    )
    expect(symbols()).toEqual([
      expect.stringContaining('THYAO'),
      expect.stringContaining('AKBNK'),
      expect.stringContaining('GARAN'),
    ])
    expect(header('Değer')).toHaveAttribute('aria-sort', 'descending')

    fireEvent.click(within(header('Değer')).getByRole('button'))
    expect(header('Değer')).toHaveAttribute('aria-sort', 'ascending')
    expect(symbols()[0]).toContain('GARAN')

    fireEvent.click(within(header('K/Z')).getByRole('button'))
    expect(header('K/Z')).toHaveAttribute('aria-sort', 'descending')
    expect(header('Değer')).toHaveAttribute('aria-sort', 'none')
    expect(symbols()).toEqual([
      expect.stringContaining('GARAN'),
      expect.stringContaining('THYAO'),
      expect.stringContaining('AKBNK'),
    ])

    // Enstrüman starts A to Z.
    fireEvent.click(within(header('Enstrüman')).getByRole('button'))
    expect(header('Enstrüman')).toHaveAttribute('aria-sort', 'ascending')
    expect(symbols()[0]).toContain('AKBNK')
  })

  it('sorts Günlük by the percent it shows, not by the amount', () => {
    const moved = (id: number, symbol: string, value: string, change: string) =>
      position(id, symbol, {
        market_value: value,
        market_value_reporting: value,
        day_change: change,
        day_change_reporting: change,
        prev_close: '1.00',
        prev_price_date: '2026-09-10',
      })
    renderTable(
      summary([
        // +5.000 on 1.000.000: +%0,5. +740,74 on 10.000: +%8.
        moved(1, 'BIG', '1005000.00', '5000.00'),
        moved(2, 'SMALL', '10000.00', '740.74'),
      ]),
    )
    fireEvent.click(within(header('Günlük')).getByRole('button'))
    expect(header('Günlük')).toHaveAttribute('aria-sort', 'descending')
    expect(symbols()).toEqual([expect.stringContaining('SMALL'), expect.stringContaining('BIG')])
  })

  it('holds Al, Sat and Fiyat gir in the row menu, and Elle fiyatı kaldır for a manual price', () => {
    const manual = position(2, 'GARAN', { price_source: 'manual', price_date: '2026-09-12' })
    const handlers = renderTable(summary([position(1, 'THYAO'), manual]))
    const open = (symbol: string) => {
      fireEvent.click(screen.getByRole('button', { name: `${symbol} işlemleri` }))
      return screen.getAllByRole('menuitem').map((item) => item.textContent)
    }
    const choose = (symbol: string, name: string) => {
      open(symbol)
      fireEvent.click(screen.getByRole('menuitem', { name }))
    }

    expect(open('THYAO')).toEqual(['Al', 'Sat', 'Fiyat gir'])
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    expect(open('GARAN')).toEqual(['Al', 'Sat', 'Fiyat gir', 'Elle fiyatı kaldır'])
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })

    choose('THYAO', 'Al')
    expect(handlers.onTrade).toHaveBeenLastCalledWith(
      'buy',
      expect.objectContaining({ symbol: 'THYAO' }),
    )
    choose('THYAO', 'Sat')
    expect(handlers.onTrade).toHaveBeenLastCalledWith(
      'sell',
      expect.objectContaining({ symbol: 'THYAO' }),
    )
    choose('GARAN', 'Fiyat gir')
    expect(handlers.onEditPrice).toHaveBeenCalledWith(manual, rowOf('GARAN'))
    choose('GARAN', 'Elle fiyatı kaldır')
    expect(handlers.onRemovePrice).toHaveBeenCalledWith(manual)
  })

  // The server refuses a buy, sell or manual price on a deposit, so its row
  // offers none; the row still opens the position page.
  it('leaves the row menu off a deposit', () => {
    renderTable(
      summary([
        position(1, 'THYAO'),
        position(2, 'TD-1', { market: 'DEPOSIT', name: 'Vadeli mevduat TRY 2026-07-01' }),
      ]),
    )

    expect(screen.getByRole('button', { name: 'THYAO işlemleri' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'TD-1 işlemleri' })).toBeNull()
    // The row names the deposit by currency and maturity, as plain text.
    expect(screen.queryByText('TD-1')).toBeNull()
    expect(within(rowOf('Vadeli mevduat TRY 2026-07-01')).queryByRole('link')).toBeNull()
  })

  it('puts a pill after a manual or stale price with the tooltip of today', () => {
    renderTable(
      summary([
        position(1, 'THYAO'),
        position(2, 'GARAN', { price_source: 'manual', price_date: '1990-01-01' }),
        position(3, 'TTE', { market: 'TEFAS', price_date: '2026-09-01' }),
      ]),
    )
    const manual = within(rowOf('GARAN')).getByRole('button', {
      description: t('prices.manualTitle', { date: formatMarketDate('1990-01-01') }),
    })
    expect(manual).toHaveTextContent('manuel')
    // After the price, in the same cell.
    expect(manual.closest('td')!.textContent).toMatch(/^₺60,00manuel/)
    const stale = within(rowOf('TTE')).getByRole('button', {
      description: t('prices.staleTitle', { date: formatMarketDate('2026-09-01') }),
    })
    expect(stale).toHaveTextContent('eski')
    expect(within(rowOf('THYAO')).queryByText(/manuel|eski/)).toBeNull()
  })

  it('switches to the closed positions with Açık | Kapalı', () => {
    renderTable(
      summary([
        position(1, 'THYAO'),
        position(9, 'QQQ', {
          quantity: '0',
          market_value: '0.00',
          market_value_reporting: '0.00',
          realized_pl: '74.39',
          realized_pl_reporting: '74.39',
          closed: true,
        }),
      ]),
    )
    const view = screen.getByRole('radiogroup', { name: 'Gösterilen pozisyonlar' })
    expect(within(view).getByRole('radio', { name: 'Açık' })).toHaveAttribute(
      'aria-checked',
      'true',
    )
    expect(screen.queryByText('QQQ')).toBeNull()

    fireEvent.click(within(view).getByRole('radio', { name: 'Kapalı' }))
    expect(screen.getByText('QQQ')).toBeInTheDocument()
    expect(screen.getByText('+₺74,39')).toBeInTheDocument()
    expect(screen.queryByText('THYAO')).toBeNull()
  })

  // The realized percent is over the cost of the shares sold; a
  // short's zero cost, or a backend without the field, has no percent.
  const closedRows = () =>
    summary([
      position(9, 'QQQ', {
        quantity: '0',
        realized_pl: '-50.00',
        realized_cost: '1000.00',
        closed: true,
      }),
      position(8, 'AAPL', {
        quantity: '0',
        realized_pl: '20.00',
        realized_cost: '0.00',
        closed: true,
      }),
      position(7, 'MSFT', { quantity: '0', realized_pl: '20.00', closed: true }),
    ])

  it('shows the realized percent of a closed position', () => {
    renderTable(closedRows())
    fireEvent.click(screen.getByRole('radio', { name: 'Kapalı' }))
    expect(
      screen.getAllByRole('columnheader').map((th) => th.textContent?.replace(/[↑↓]/, '')),
    ).toEqual(['Enstrüman', 'Gerçekleşmiş K/Z', 'K/Z %'])
    const percent = within(rowOf('QQQ')).getAllByRole('cell')[2]
    expect(percent).toHaveTextContent('-%5')
    expect(percent.className).toContain(plTone('-50.00'))
    expect(within(rowOf('AAPL')).getAllByRole('cell')[2]).toHaveTextContent(MISSING)
    expect(within(rowOf('MSFT')).getAllByRole('cell')[2]).toHaveTextContent(MISSING)
  })

  it('shows the realized percent next to the amount on a phone', () => {
    desktop = false
    renderTable(closedRows())
    fireEvent.click(screen.getByRole('radio', { name: 'Kapalı' }))
    const qqq = screen.getByText('QQQ').closest('li')!
    expect(qqq).toHaveTextContent('-₺50,00-%5')
    expect(screen.getAllByText(/%/)).toHaveLength(1)
  })

  it('sorts the closed view by realized P/L, reversed on a second click', () => {
    const closedOne = (id: number, symbol: string, pl: string) =>
      position(id, symbol, {
        quantity: '0',
        realized_pl: pl,
        realized_pl_reporting: pl,
        closed: true,
      })
    renderTable(
      summary([
        position(1, 'THYAO'),
        closedOne(9, 'AAPL', '20.00'),
        closedOne(8, 'MSFT', '-50.00'),
        closedOne(7, 'QQQ', '74.39'),
      ]),
    )
    const order = () => bodyRows().map((row) => within(row).getByRole('link').textContent)
    fireEvent.click(screen.getByRole('radio', { name: 'Kapalı' }))
    // Gerçekleşmiş K/Z descending first.
    expect(header('Gerçekleşmiş K/Z')).toHaveAttribute('aria-sort', 'descending')
    expect(order()).toEqual(['QQQ', 'AAPL', 'MSFT'])

    fireEvent.click(within(header('Enstrüman')).getByRole('button'))
    expect(order()).toEqual(['AAPL', 'MSFT', 'QQQ'])

    fireEvent.click(within(header('Gerçekleşmiş K/Z')).getByRole('button'))
    expect(header('Gerçekleşmiş K/Z')).toHaveAttribute('aria-sort', 'descending')
    expect(order()).toEqual(['QQQ', 'AAPL', 'MSFT'])

    fireEvent.click(within(header('Gerçekleşmiş K/Z')).getByRole('button'))
    expect(header('Gerçekleşmiş K/Z')).toHaveAttribute('aria-sort', 'ascending')
    expect(order()).toEqual(['MSFT', 'AAPL', 'QQQ'])

    // Açık keeps its own sort.
    fireEvent.click(screen.getByRole('radio', { name: 'Açık' }))
    expect(header('Değer')).toHaveAttribute('aria-sort', 'descending')
  })

  it('filters the rows by symbol or name with Turkish case rules', () => {
    renderTable(
      summary([
        ...twoPositions(),
        position(3, 'TI1', { name: 'İŞ PORTFÖY HİSSE SENEDİ FONU', market: 'TEFAS' }),
      ]),
    )
    const search = screen.getByRole('searchbox', { name: 'Pozisyon ara' })
    fireEvent.change(search, { target: { value: 'hisse' } })
    expect(symbols()).toEqual([expect.stringContaining('TI1')])
    // The server's total covers every position, not the one shown.
    expect(screen.queryByText(/^Toplam/)).toBeNull()

    fireEvent.change(search, { target: { value: 'xyz' } })
    expect(screen.getByText('Aramayla eşleşen pozisyon yok.')).toBeInTheDocument()
  })

  it('matches whichever way I is typed, in either locale (GLG-1143)', () => {
    const data = summary([
      position(1, 'BIMAS'),
      position(2, 'THYAO', { name: 'TURK HAVA YOLLARI' }),
      position(3, 'TI1', { name: 'İŞ PORTFÖY HİSSE SENEDİ FONU', market: 'TEFAS' }),
    ])
    renderTable(data)
    const search = screen.getByRole('searchbox', { name: 'Pozisyon ara' })
    fireEvent.change(search, { target: { value: 'bimas' } })
    expect(symbols()).toEqual([expect.stringContaining('BIMAS')])
    fireEvent.change(search, { target: { value: 'yollari' } })
    expect(symbols()).toEqual([expect.stringContaining('THYAO')])
    cleanup()

    setLocale('en')
    renderTable(data)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search positions' }), {
      target: { value: 'hisse' },
    })
    expect(symbols()).toEqual([expect.stringContaining('TI1')])
  })

  it('searches and sorts a deposit by its label, not its symbol or stored name', () => {
    setLocale('en')
    renderTable(
      summary([
        position(1, 'AKBNK'),
        position(2, 'TD-1', { market: 'DEPOSIT', name: 'Vadeli mevduat TRY 2026-07-01' }),
        position(3, 'TEA'),
      ]),
    )
    const search = screen.getByRole('searchbox', { name: 'Search positions' })
    fireEvent.change(search, { target: { value: 'term' } })
    expect(symbols()).toEqual([expect.stringContaining('Term deposit TRY 2026-07-01')])
    for (const value of ['TD-', 'vadeli']) {
      fireEvent.change(search, { target: { value } })
      expect(screen.getByText('No positions match the search.')).toBeInTheDocument()
    }

    fireEvent.change(search, { target: { value: '' } })
    fireEvent.click(within(header('Instrument')).getByRole('button'))
    // By symbol the deposit would sit between AKBNK and TEA.
    expect(symbols()).toEqual([
      expect.stringContaining('AKBNK'),
      expect.stringContaining('TEA'),
      expect.stringContaining('Term deposit'),
    ])
  })

  it('has no switch without closed positions', () => {
    renderTable(summary(twoPositions()))
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })

  it('renders the all scope read-only: no row menu, symbols as plain text', () => {
    renderTable(summary(twoPositions()), 'all')
    expect(screen.queryByRole('button', { name: /işlemleri$/ })).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getAllByRole('columnheader')).toHaveLength(8)
    expect(within(rowOf('THYAO')).getByText('THYAO')).toBeInTheDocument()
  })

  it('links a symbol to its position page', () => {
    renderTable(summary(twoPositions()))
    expect(screen.getByRole('link', { name: 'THYAO' })).toHaveAttribute(
      'href',
      '/portfolios/1/positions/1',
    )
  })

  it("totals Değer and K/Z from the summary's own figures", () => {
    // The rows sum to 900.00 and 300.00; the server's unrounded totals win.
    renderTable(
      summary(twoPositions(), {
        positions_value: '900.01',
        unrealized_pl: '300.01',
        cost_basis: '600.00',
      }),
    )
    const footer = within(screen.getAllByRole('rowgroup')[2])
    expect(footer.getByText('Toplam · 2 pozisyon, TRY olarak')).toBeInTheDocument()
    expect(footer.getByText('₺900,01')).toBeInTheDocument()
    expect(footer.getByText('+₺300,01 · +%50')).toBeInTheDocument()
  })

  it("keeps the total's percent over a negative cost, as the rows do", () => {
    // A net short portfolio: the percent is over the cost taken
    // absolute, as each row's is, so a gain still reads positive.
    renderTable(summary(twoPositions(), { unrealized_pl: '300.00', cost_basis: '-600.00' }))
    const footer = within(screen.getAllByRole('rowgroup')[2])
    expect(footer.getByText('+₺300,00 · +%50')).toBeInTheDocument()
  })

  it('says so when there are no open positions', () => {
    renderTable(
      summary([position(9, 'QQQ', { quantity: '0', closed: true })], {
        positions_value: '0.00',
        cost_basis: '0.00',
        unrealized_pl: '0.00',
      }),
    )
    expect(screen.getByText('Açık pozisyon yok.')).toBeInTheDocument()
    expect(screen.queryByText(/^Toplam/)).toBeNull()
  })

  it('masks every amount in privacy mode, not the weights', () => {
    setPrivacy(true)
    renderTable(summary(twoPositions()))
    const cells = within(rowOf('THYAO')).getAllByRole('cell')
    expect(cells[1]).toHaveTextContent('%60,0')
    expect(cells[3]).toHaveTextContent('•••')
    expect(cells[4]).toHaveTextContent('•••')
    expect(cells[6]).toHaveTextContent('•••')
    expect(cells[7]).toHaveTextContent('••• · +%20')
  })

  it('keeps the phone list without the price line, padded for the floating button', () => {
    desktop = false
    renderTable(
      summary([position(2, 'GARAN', { price_source: 'manual', price_date: '2026-09-12' })]),
    )
    expect(screen.queryByRole('table')).toBeNull()
    const item = within(screen.getByRole('listitem'))
    // Plain text: the row opens the position dialog, which links the page.
    expect(item.getByText('GARAN')).toBeInTheDocument()
    expect(item.queryByRole('link')).toBeNull()
    expect(item.queryByText('₺60,00')).toBeNull()
    expect(item.queryByText(/manuel/)).toBeNull()
    expect(screen.getByRole('list').parentElement).toHaveClass('pb-14')
  })
})

describe('PositionsTable position dialog', () => {
  const dialogTitle = () => within(screen.getByRole('dialog')).getByRole('heading', { level: 2 })

  it('opens from a row click, but not from the symbol link or the row menu', () => {
    renderTable(summary(twoPositions()))
    fireEvent.click(within(rowOf('THYAO')).getByRole('link', { name: 'THYAO' }))
    fireEvent.click(within(rowOf('THYAO')).getByRole('button', { name: /THYAO/ }))
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(within(rowOf('GARAN')).getByText('GARAN Holding'))
    expect(dialogTitle()).toHaveTextContent('GARAN')
  })

  it('opens from Enter on a row and flips through the rows as shown', () => {
    renderTable(summary(twoPositions()))
    fireEvent.keyDown(rowOf('THYAO'), { key: 'Enter' })
    expect(dialogTitle()).toHaveTextContent('THYAO')
    fireEvent.click(screen.getByRole('button', { name: t('position.dialog.next') }))
    expect(dialogTitle()).toHaveTextContent('GARAN')
    expect(screen.getByRole('button', { name: t('position.dialog.next') })).toBeDisabled()
  })

  it('opens from a phone row tap', () => {
    desktop = false
    renderTable(summary(twoPositions()))
    fireEvent.click(screen.getByText('THYAO Holding'))
    expect(dialogTitle()).toHaveTextContent('THYAO')
  })

  it('opens from a closed row and from a row of the all scope', () => {
    renderTable(
      summary([position(1, 'THYAO'), position(9, 'QQQ', { quantity: '0', closed: true })]),
    )
    fireEvent.click(screen.getByRole('radio', { name: 'Kapalı' }))
    fireEvent.click(screen.getByText('QQQ Holding'))
    expect(dialogTitle()).toHaveTextContent('QQQ')
    cleanup()
    renderTable(summary(twoPositions()), 'all')
    fireEvent.click(screen.getByText('GARAN Holding'))
    expect(dialogTitle()).toHaveTextContent('GARAN')
  })
})

describe('unrealized', () => {
  // A priced position whose currency has no FX rate has no reporting amount;
  // its percent must go too, or the position page shows "-" over a percent.
  it('gives no percent when the reporting amount is unknown', () => {
    expect(unrealized(position(1, 'AAPL', { unrealized_pl_reporting: null }))).toEqual({
      amount: null,
      percent: null,
    })
  })
})

describe('returnPercent', () => {
  // The one P/L-over-cost rule every screen uses: a negative cost
  // is taken absolute, a zero or missing cost gives no percent.
  it('divides by the absolute cost, and gives none without one', () => {
    expect(returnPercent('100.00', '400.00')).toBe('+%25')
    expect(returnPercent('100.00', '-400.00')).toBe('+%25')
    expect(returnPercent('-100.00', '-400.00')).toBe('-%25')
    expect(returnPercent('100.00', '0.00')).toBeNull()
    expect(returnPercent('100.00', undefined)).toBeNull()
    expect(returnPercent(null, '400.00')).toBeNull()
  })
})
