import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Allocation } from './Allocation'
import type { AllocationTarget } from '../api/allocationTargets'
import type { Position } from '../api/summary'
import { setPrivacy } from '../lib/privacy'
import { mockChartSize } from '../test/chartSize'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // jsdom has no showModal() on <dialog>; this only flips the open state.
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  mockTargets([])
  // Reduced motion: Recharts draws the final slices at once (see
  // AllocationDonut.test.tsx).
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)',
    addEventListener() {},
    removeEventListener() {},
  }))
})
afterEach(() => {
  vi.restoreAllMocks()
  restoreSize()
  vi.unstubAllGlobals()
  setPrivacy(false)
})

function mockTargets(targets: AllocationTarget[]) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = String(input)
    if (url === '/api/portfolios/1/allocation-targets') return Response.json({ targets })
    throw new Error(`unexpected request: ${url}`)
  })
}

function position(
  instrumentId: number,
  symbol: string,
  marketValue: string,
  marketValueReporting: string | null = marketValue,
  market = 'BIST',
  currency = 'TRY',
): Position {
  return {
    instrument_id: instrumentId,
    symbol,
    name: symbol,
    market,
    asset_class: null,
    sector: 'unknown',
    currency,
    quantity: '1',
    avg_cost: '0',
    cost_basis: '0',
    last_price: null,
    has_price: true,
    price_source: null,
    price_date: null,
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    day_change_base: null,
    market_value: marketValue,
    unrealized_pl: '0',
    realized_pl: '0',
    market_value_reporting: marketValueReporting,
    unrealized_pl_reporting: '0',
    realized_pl_reporting: '0',
    total_pl_reporting: '0',
    closed: false,
  }
}

type Props = Parameters<typeof Allocation>[0]

// Earlier tests in this file leave their DOM mounted, and symbols repeat
// across cases, so every assertion is scoped to the render's own container.
function renderAllocation(props: Partial<Props> = {}) {
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      <Allocation
        portfolioId={1}
        positions={[position(1, 'THYAO', '300.00'), position(2, 'GARAN', '100.00')]}
        cash="100.00"
        cashByCurrency={{ TRY: '100.00' }}
        cashByCurrencyReporting={{ TRY: '100.00' }}
        missingRates={[]}
        currency="TRY"
        {...props}
      />
    </QueryClientProvider>,
  )
  return { container, scoped: within(container) }
}

const legend = (scoped: { getAllByRole: (role: string) => HTMLElement[] }) =>
  scoped.getAllByRole('listitem').map((item) => item.textContent)

// The donut's slices once the charts chunk has loaded.
async function slices(container: HTMLElement) {
  await waitFor(() => expect(container.querySelector('path.recharts-sector')).not.toBeNull())
  return [...container.querySelectorAll('path.recharts-sector')]
}

describe('Allocation', () => {
  it('lists one row per position and one for cash, with amount and share', async () => {
    const { container, scoped } = renderAllocation()

    // Value descending, cash last even though it ties with GARAN.
    expect(legend(scoped)).toEqual(['THYAO₺300,00%60,0', 'GARAN₺100,00%20,0', 'Nakit₺100,00%20,0'])
    expect(scoped.getByText('3 kalem · nakit dahil')).toBeInTheDocument()
    expect(await slices(container)).toHaveLength(3)
  })

  it('claims cash in the header only when cash is in the donut', () => {
    // Zero or negative cash stays out of the donut, so the header
    // must not say "nakit dahil", in any view.
    for (const cash of ['-500.00', '0']) {
      const { container, scoped } = renderAllocation({
        positions: [position(1, 'THYAO', '1000.00')],
        cash,
        cashByCurrency: { TRY: cash },
        cashByCurrencyReporting: { TRY: cash },
      })
      expect(legend(scoped)).toEqual(['THYAO₺1.000,00%100,0'])
      expect(scoped.getByText('1 kalem')).toBeInTheDocument()
      for (const view of ['Pazar', 'Para birimi']) {
        fireEvent.click(scoped.getByRole('radio', { name: view }))
        expect(scoped.getByText('1 kalem')).toBeInTheDocument()
      }
      container.remove()
    }

    // The currency view folds cash into the currencies: positive USD cash
    // still counts as cash in the donut even when the total cash is negative.
    const { scoped } = renderAllocation({
      positions: [
        position(1, 'THYAO', '300.00'),
        position(2, 'AAPL', '10.00', '200.00', 'US', 'USD'),
      ],
      cash: '-400.00',
      cashByCurrency: { TRY: '-500.00', USD: '2.50' },
      cashByCurrencyReporting: { TRY: '-500.00', USD: '100.00' },
    })
    expect(scoped.getByText('2 kalem')).toBeInTheDocument()
    fireEvent.click(scoped.getByRole('radio', { name: 'Para birimi' }))
    expect(legend(scoped)).toEqual(['USD₺300,00%100,0'])
    expect(scoped.getByText('1 kalem · nakit dahil')).toBeInTheDocument()
  })

  it('puts the legend beside the donut on the card width, not the viewport', () => {
    const { scoped } = renderAllocation()
    // From lg the card is half the page, so a viewport breakpoint put the
    // legend beside the donut in a card too narrow for the names. jsdom has
    // no layout, so this checks the classes that make the switch.
    const row = scoped.getByRole('list').parentElement!
    expect(row.className).toContain('@md:flex-row')
    expect(row.className).not.toMatch(/(^|\s)md:flex-row/)
    expect(row.parentElement!.className).toContain('@container')
  })

  it('hides the donut from assistive technology and keeps it out of the tab order', async () => {
    const { container } = renderAllocation()
    await slices(container)

    const donut = container.querySelector('[data-allocation-donut]')
    expect(donut).toHaveAttribute('aria-hidden', 'true')
    expect(donut?.querySelector('[tabindex]:not([tabindex="-1"])')).toBeNull()
    expect(donut).toHaveTextContent('Toplam₺500')
  })

  it('highlights the pair: a legend row dims the other slices, a slice marks its row', async () => {
    const { container, scoped } = renderAllocation()
    await slices(container)
    const rows = scoped.getAllByRole('listitem')
    // Recharts redraws the slices through its store, a tick after the prop.
    const opacities = async (expected: string[]) =>
      waitFor(() =>
        expect(
          [...container.querySelectorAll('path.recharts-sector')].map((s) =>
            s.getAttribute('fill-opacity'),
          ),
        ).toEqual(expected),
      )

    fireEvent.mouseEnter(rows[1])
    await opacities(['0.3', '1', '0.3'])
    expect(rows[1]).toHaveClass('bg-well')
    fireEvent.mouseLeave(rows[1])
    await opacities(['1', '1', '1'])

    fireEvent.mouseEnter(container.querySelectorAll('path.recharts-sector')[0])
    expect(rows[0]).toHaveClass('bg-well')
    expect(rows[1]).not.toHaveClass('bg-well')
  })

  it('uses the reporting value and notes a position left out for a missing rate', () => {
    const { scoped } = renderAllocation({
      positions: [
        // Small in its own currency (USD) but large once converted.
        position(1, 'AAPL', '10.00', '900.00', 'US', 'USD'),
        position(2, 'THYAO', '500.00', '100.00'),
        position(3, 'MISSING', '1000.00', null),
      ],
      cash: '0.00',
      cashByCurrency: {},
      cashByCurrencyReporting: {},
    })

    expect(legend(scoped)).toEqual(['AAPL₺900,00%90,0', 'THYAO₺100,00%10,0'])
    expect(scoped.getByText('1 pozisyon kur eksikliği nedeniyle dahil değil')).toBeInTheDocument()
  })

  it('shows "Dağılım hesaplanamadı" and no donut when nothing has a reporting value', () => {
    const { container, scoped } = renderAllocation({
      positions: [position(1, 'MISSING', '1000.00', null)],
      cash: '0.00',
      cashByCurrency: {},
      cashByCurrencyReporting: {},
    })

    expect(scoped.getByText('Dağılım hesaplanamadı')).toBeInTheDocument()
    expect(scoped.getByText('1 pozisyon kur eksikliği nedeniyle dahil değil')).toBeInTheDocument()
    expect(container.querySelector('[data-allocation-donut], [data-chart-placeholder]')).toBeNull()
  })

  it('notes cash left out of the reporting total for a currency with no FX rate', () => {
    const { scoped } = renderAllocation({
      positions: [position(1, 'THYAO', '100.00')],
      cash: '0.00',
      cashByCurrency: { USD: '50.00' },
      cashByCurrencyReporting: {},
      missingRates: ['USD'],
    })

    expect(legend(scoped)).toEqual(['THYAO₺100,00%100,0'])
    expect(scoped.getByText('USD nakdi kur eksikliği nedeniyle dahil değil')).toBeInTheDocument()
  })

  it('switches between the symbol, market and currency views', () => {
    const { scoped } = renderAllocation({
      positions: [
        position(1, 'THYAO', '300.00'),
        position(2, 'GARAN', '100.00'),
        position(3, 'AAPL', '10.00', '200.00', 'US', 'USD'),
        position(4, 'BTC', '5.00', '100.00', 'CRYPTO', 'USD'),
      ],
      cash: '300.00',
      cashByCurrency: { TRY: '200.00', USD: '2.50' },
      cashByCurrencyReporting: { TRY: '200.00', USD: '100.00' },
    })

    fireEvent.click(scoped.getByRole('radio', { name: 'Pazar' }))
    expect(legend(scoped)).toEqual([
      'BIST₺400,00%40,0',
      'US₺200,00%20,0',
      'Kripto₺100,00%10,0',
      'Nakit₺300,00%30,0',
    ])

    fireEvent.click(scoped.getByRole('radio', { name: 'Para birimi' }))
    expect(legend(scoped)).toEqual(['TRY₺600,00%60,0', 'USD₺400,00%40,0'])
    expect(scoped.getByText('2 kalem · nakit dahil')).toBeInTheDocument()
  })

  it('folds rows past the fifth into "Diğer (n)" with its members in the title', () => {
    const { scoped } = renderAllocation({
      positions: ['A', 'B', 'C', 'D', 'E', 'F', 'G'].map((symbol, i) =>
        position(i + 1, symbol, String(70 - i * 10)),
      ),
      cash: '20.00',
      cashByCurrency: { TRY: '20.00' },
      cashByCurrencyReporting: { TRY: '20.00' },
    })
    const items = scoped.getAllByRole('listitem')

    expect(items[5]).toHaveTextContent('Diğer (2)₺30,00%10,0')
    expect(items[5]).toHaveAttribute('title', 'F, G')
    // The count still names every holding, not the folded legend.
    expect(scoped.getByText('8 kalem · nakit dahil')).toBeInTheDocument()
  })

  it('masks the amounts and the centre total in privacy mode, and keeps the shares', async () => {
    setPrivacy(true)
    const { container, scoped } = renderAllocation()
    await slices(container)

    expect(legend(scoped)[0]).toBe('THYAO•••%60,0')
    expect(container.querySelector('[data-allocation-donut]')).toHaveTextContent('Toplam•••')
  })

  it('shows the target and drift in the asset class view and opens the target dialog', async () => {
    mockTargets([
      { asset_class: 'equity', weight: '70' },
      { asset_class: 'cash', weight: '20' },
      { asset_class: 'deposit', weight: '10' },
    ])
    const { scoped } = renderAllocation()

    expect(scoped.queryByRole('button', { name: 'Hedef belirle' })).toBeNull()
    fireEvent.click(scoped.getByRole('radio', { name: 'Varlık sınıfı' }))

    await waitFor(() => expect(scoped.getAllByRole('listitem')).toHaveLength(3))
    const [equity, cash, deposit] = scoped.getAllByRole('listitem')
    expect(equity).toHaveTextContent('Hisse senedi₺400,00%80,0Hedef %70,0Sapma +10,0 p')
    expect(cash).toHaveTextContent('Nakit₺100,00%20,0Hedef %20,0Sapma 0,0 p')
    expect(deposit).toHaveTextContent('Mevduat₺0,00%0,0Hedef %10,0Sapma -10,0 p')
    expect(equity.querySelector('.text-gain')).not.toBeNull()
    expect(deposit.querySelector('.text-loss')).not.toBeNull()

    fireEvent.click(scoped.getByRole('button', { name: 'Hedef belirle' }))
    expect(scoped.getByText('Toplam: %100')).toBeInTheDocument()
    const save = scoped.getByRole('button', { name: 'Kaydet' })
    expect(save).toBeEnabled()
    fireEvent.change(scoped.getByLabelText('Hisse senedi'), { target: { value: '60' } })
    expect(scoped.getByText('Toplam: %90')).toBeInTheDocument()
    expect(save).toBeDisabled()
  })

  it('puts the target and drift on their own line so the class name keeps its width', async () => {
    mockTargets([{ asset_class: 'equity', weight: '100' }])
    const { scoped } = renderAllocation()
    const noTargets = scoped.getAllByRole('listitem')[0]
    expect(noTargets.className).not.toContain('flex-wrap')

    fireEvent.click(scoped.getByRole('radio', { name: 'Varlık sınıfı' }))
    await waitFor(() => expect(scoped.getAllByTitle('Hedef').length).toBeGreaterThan(0))
    const row = scoped.getAllByRole('listitem')[0]
    const label = scoped.getByText('Hisse senedi')
    // The label is in the row's first line; the target and drift are in a group that takes a full line.
    expect(row.className).toContain('flex-wrap')
    expect(label.className).not.toContain('truncate')
    expect(scoped.getAllByTitle('Hedef')[0].parentElement?.className).toContain('basis-full')
  })

  it('lists the planned buys for a typed contribution in the asset class view', async () => {
    mockTargets([
      { asset_class: 'equity', weight: '70' },
      { asset_class: 'cash', weight: '20' },
      { asset_class: 'deposit', weight: '10' },
    ])
    const { scoped } = renderAllocation()

    expect(scoped.queryByLabelText(/Katkı planla/)).toBeNull()
    fireEvent.click(scoped.getByRole('radio', { name: 'Varlık sınıfı' }))
    const input = await scoped.findByLabelText(/Katkı planla/)
    expect(scoped.getAllByRole('listitem')).toHaveLength(3)

    // Total 600: equity needs 20, cash 20, deposit 60; the gaps fill the amount.
    fireEvent.change(input, { target: { value: '100' } })
    const items = scoped.getAllByRole('listitem').slice(3)
    expect(items.map((item) => item.textContent)).toEqual([
      'Hisse senedi₺20,00',
      'Nakit₺20,00',
      'Mevduat₺60,00',
    ])
  })
})
