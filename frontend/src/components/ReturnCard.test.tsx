import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Benchmark, HistoryResponse, ReturnMethod } from '../api/history'
import type { RangeValue } from '../lib/ranges'
import { chartDrag, mockChartSize } from '../test/chartSize'
import { chooseOption } from '../test/select'
import { ReturnCard, type HistoryQuery } from './ReturnCard'

// Two points 1000 -> 1100 with no flow: rangeDelta is +100. twr runs 0 ->
// 20%, and benchmark_twr 0 -> 5% with benchmark values; without twr, the
// time-weighted return is undefined for the range (null on every point).
function history(
  benchmark?: {
    code: Benchmark
    mwr: string | null
    values?: boolean
  },
  { twr = true } = {},
): HistoryResponse {
  const values = benchmark?.values ?? benchmark?.mwr != null
  return {
    currency: 'TRY',
    points: [
      {
        date: '2026-07-01',
        total_value: '1000.00',
        cash: '1000.00',
        cost_basis: '0',
        invested: '900.00',
      },
      {
        date: '2026-10-01',
        total_value: '1100.00',
        cash: '1100.00',
        cost_basis: '0',
        invested: '900.00',
      },
    ].map((p, i) => ({
      ...p,
      ...(values && { benchmark_value: i === 0 ? '1000.00' : '1050.00' }),
      twr: twr ? ['0.000000', '0.200000'][i] : null,
      benchmark_twr: values && twr ? ['0.000000', '0.050000'][i] : null,
    })),
    missing_rates: [],
    missing_prices: [],
    net_flow: '0.00',
    returns: { simple: '0.100000', twr: twr ? '0.200000' : null, mwr: '0.300000' },
    benchmark: benchmark
      ? {
          code: benchmark.code,
          returns: { simple: benchmark.mwr, twr: benchmark.mwr, mwr: benchmark.mwr },
          estimated_from: null,
        }
      : null,
  }
}

const query = (data: HistoryResponse): HistoryQuery => ({
  data,
  isError: false,
  error: null,
  refetch: vi.fn(),
  isFetching: false,
  isPlaceholderData: false,
})

function renderCard({
  data = history(),
  benchmark = null,
  method = 'mwr',
  range = { key: '3m', from: '', to: '' },
}: {
  data?: HistoryResponse
  benchmark?: Benchmark | null
  method?: ReturnMethod
  range?: RangeValue
} = {}) {
  const handlers = { onRange: vi.fn(), onBenchmark: vi.fn(), onMethod: vi.fn() }
  render(
    <ReturnCard
      currency="TRY"
      query={query(data)}
      range={range}
      benchmark={benchmark}
      benchmarks={[
        { value: 'USD', label: 'Dolar' },
        { value: 'TUFE', label: 'Enflasyon (TÜFE)' },
      ]}
      method={method}
      {...handlers}
    />,
  )
  return handlers
}

// The pill or the no-data line: the element after the headline's amount.
const headline = () => screen.getByTestId('return-headline')

describe('ReturnCard', () => {
  afterEach(() => vi.restoreAllMocks())

  it("leads with the chosen method's return, the range delta and the range in words", () => {
    renderCard()
    expect(screen.getByRole('heading', { name: 'Getiri · son 3 ay' })).toBeInTheDocument()
    const line = within(headline())
    expect(line.getByText('+%30')).toHaveAttribute(
      'title',
      'İç verim oranı; para giriş çıkışlarının zamanlaması dahil',
    )
    expect(line.getByText('+%30')).toHaveClass('text-gain')
    expect(line.getByText('+₺100,00')).toBeInTheDocument()
  })

  it('follows the method', () => {
    renderCard({ method: 'simple' })
    expect(within(headline()).getByText('+%10')).toBeInTheDocument()
  })

  it('shows no pill without a benchmark', () => {
    renderCard()
    expect(headline()).not.toHaveTextContent('fark')
    expect(screen.queryByText(/bu aralık için veri yok/)).toBeNull()
  })

  it("pills the benchmark's return and the difference, coloured by its sign", () => {
    // (1 + 0.30) / (1 + 0.05) - 1 = 23.8%
    renderCard({ data: history({ code: 'USD', mwr: '0.050000' }), benchmark: 'USD' })
    const pill = within(headline()).getByText('Dolar +%5 · fark +%23,8')
    expect(pill).toHaveClass('text-gain')
  })

  it('pills a negative difference as a loss, and calls it real against TÜFE', () => {
    // (1 + 0.30) / (1 + 0.40) - 1 = -7.1%
    renderCard({ data: history({ code: 'TUFE', mwr: '0.400000' }), benchmark: 'TUFE' })
    const pill = within(headline()).getByText('Enflasyon (TÜFE) +%40 · reel -%7,1')
    expect(pill).toHaveClass('text-loss')
  })

  it('shows the real return after TÜFE with no benchmark selected, estimated when extrapolated', () => {
    // twr 20%, inflation 10%: 1.2 / 1.1 - 1 = 9.1%
    renderCard({ data: { ...history(), inflation: '0.100000' } })
    expect(screen.getByTestId('real-return')).toHaveTextContent('Reel getiri +%9,1')
    expect(screen.getByTestId('real-return')).not.toHaveTextContent('tahmini')
    cleanup()
    renderCard({
      data: { ...history(), inflation: '0.100000', inflation_estimated_from: '2026-09-01' },
    })
    expect(screen.getByTestId('real-return')).toHaveTextContent('Reel getiri +%9,1 (tahmini)')
  })

  it('leaves the real return line to the pill with TÜFE selected, and omits it without inflation', () => {
    renderCard({
      data: { ...history({ code: 'TUFE', mwr: '0.400000' }), inflation: '0.400000' },
      benchmark: 'TUFE',
    })
    expect(screen.queryByTestId('real-return')).toBeNull()
    cleanup()
    renderCard({ data: { ...history(), inflation: null } })
    expect(screen.queryByTestId('real-return')).toBeNull()
  })

  it('says so in place of the pill when the benchmark has no values in the range', () => {
    renderCard({ data: history({ code: 'USD', mwr: null }), benchmark: 'USD' })
    expect(within(headline()).getByText('Dolar · bu aralık için veri yok')).toBeInTheDocument()
    expect(headline()).not.toHaveTextContent('fark')
  })

  it('reaches the benchmark, method and range controls by name', async () => {
    const { onMethod, onBenchmark, onRange } = renderCard()
    await chooseOption(screen.getByLabelText('Getiri yöntemi'), 'Basit')
    expect(onMethod).toHaveBeenCalledWith('simple')
    await chooseOption(screen.getByLabelText('Karşılaştır'), 'Dolar')
    expect(onBenchmark).toHaveBeenCalledWith('USD')
    screen.getByRole('radio', { name: '1Y' }).click()
    expect(onRange).toHaveBeenCalledWith(expect.objectContaining({ key: '1y' }))
  })

  // In percent mode, the card's default.
  it('sets the range from a drag on the chart, and the clear button returns to the range before it', async () => {
    const restoreSize = mockChartSize()
    try {
      function Card() {
        const [range, setRange] = useState<RangeValue>({ key: '1y', from: '', to: '' })
        return (
          <ReturnCard
            currency="TRY"
            query={query(history())}
            range={range}
            onRange={setRange}
            benchmark={null}
            benchmarks={[]}
            onBenchmark={vi.fn()}
            method="mwr"
            onMethod={vi.fn()}
          />
        )
      }
      const { container } = render(<Card />)
      // The chart arrives with its lazy chunk. At 600 px, x 60 is the first
      // point and 580 the last (LineChart.test.tsx).
      const chart = await waitFor(() => {
        const el = container.querySelector('.recharts-wrapper')
        if (!el) throw new Error('no chart yet')
        return el
      })
      await chartDrag(chart, 580, 60)

      expect(screen.getByRole('radio', { name: 'Özel' })).toHaveAttribute('aria-checked', 'true')
      expect(screen.getByLabelText('Başlangıç')).toHaveValue('2026-07-01')
      expect(screen.getByLabelText('Bitiş')).toHaveValue('2026-10-01')
      fireEvent.click(screen.getByRole('button', { name: 'Özel aralığı temizle' }))
      expect(screen.getByRole('radio', { name: '1Y' })).toHaveAttribute('aria-checked', 'true')
    } finally {
      restoreSize()
    }
  })

  it('clears an Özel range it did not zoom to, as after a reload, to the default range', () => {
    const { onRange } = renderCard({
      range: { key: 'custom', from: '2026-07-01', to: '2026-10-01' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Özel aralığı temizle' }))
    expect(onRange).toHaveBeenLastCalledWith({ key: '3m', from: '', to: '' })
  })

  it('sizes each select to its longest label, so no label is cut', () => {
    renderCard()
    // 'Zaman ağırlıklı' (15) is the longest method label in Turkish.
    // jsdom keeps the declared width as written.
    expect(screen.getByLabelText('Getiri yöntemi').parentElement!.getAttribute('style')).toBe(
      'width: calc(15ch + 3rem);',
    )
    expect(screen.getByLabelText('Karşılaştır').parentElement!.getAttribute('style')).toBe(
      'width: calc(17ch + 3rem);',
    )
  })

  describe('percent mode', () => {
    let restoreSize: () => void
    beforeEach(() => {
      restoreSize = mockChartSize()
      localStorage.removeItem('chartMode')
    })
    afterEach(() => {
      restoreSize()
      localStorage.removeItem('chartMode')
    })
    // The legend and the chart arrive with the lazy charts chunk.
    const legend = () =>
      waitFor(() => {
        const el = document.querySelector<HTMLElement>('[data-chart-legend]')
        if (!el) throw new Error('no chart yet')
        return el
      })
    const tooltipAt = (index: number) => {
      for (let i = 0; i <= index; i++) {
        fireEvent.keyDown(screen.getByRole('application'), { key: 'ArrowRight' })
      }
      return document.querySelector('[role="tooltip"]')?.textContent ?? ''
    }

    it('opens in percent on twr and benchmark_twr, without cost, with the difference', async () => {
      renderCard({
        data: history({ code: 'USD', mwr: '0.050000' }),
        benchmark: 'USD',
        method: 'twr',
      })
      expect(screen.getByRole('radio', { name: '%' })).toHaveAttribute('aria-checked', 'true')
      const entries = await legend()
      expect(entries).toHaveTextContent('Portföy')
      expect(entries).not.toHaveTextContent('zaman ağırlıklı')
      expect(entries).not.toHaveTextContent('Maliyet')
      expect(entries).not.toHaveTextContent('Yatırılan')
      // (1 + 0.20) / (1 + 0.05) - 1 = 14.3%
      const tip = tooltipAt(1)
      expect(tip).toContain('Portföy+%20')
      expect(tip).toContain('Dolar+%5')
      expect(tip).toContain('fark+%14,3')
    })

    it('calls the difference real against TÜFE, as the pill does', async () => {
      renderCard({
        data: history({ code: 'TUFE', mwr: '0.050000' }),
        benchmark: 'TUFE',
        method: 'twr',
      })
      await legend()
      const tip = tooltipAt(1)
      expect(tip).toContain('reel+%14,3')
      expect(tip).not.toContain('fark')
    })

    it('switches to the value chart and keeps the choice', async () => {
      renderCard()
      fireEvent.click(screen.getByRole('radio', { name: '₺' }))
      expect(localStorage.getItem('chartMode')).toBe('money')
      expect(await legend()).toHaveTextContent('Toplam değer')
      expect(await legend()).toHaveTextContent('Maliyet')
      expect(await legend()).toHaveTextContent('Yatırılan')
      expect(tooltipAt(1)).toContain('1.100')
    })

    it('opens in the stored mode', async () => {
      localStorage.setItem('chartMode', 'money')
      renderCard()
      expect(screen.getByRole('radio', { name: '₺' })).toHaveAttribute('aria-checked', 'true')
      expect(await legend()).toHaveTextContent('Toplam değer')
    })

    it('labels the line time-weighted, with why, when the method is not', async () => {
      renderCard({ method: 'mwr' })
      expect(within(await legend()).getByText('Portföy (zaman ağırlıklı)')).toHaveAttribute(
        'title',
        expect.stringContaining('farklı'),
      )
    })

    it('shows the value chart, a disabled switch and why when twr is undefined', async () => {
      renderCard({ data: history(undefined, { twr: false }) })
      expect(screen.getByRole('radio', { name: '%' })).toBeDisabled()
      expect(screen.getByRole('radio', { name: '₺' })).toBeDisabled()
      expect(screen.getByRole('radio', { name: '₺' })).toHaveAttribute('aria-checked', 'true')
      expect(screen.getByText('Bu aralık için tanımsız')).toBeVisible()
      expect(await legend()).toHaveTextContent('Toplam değer')
    })

    it('keeps the benchmark legend toggle', async () => {
      renderCard({ data: history({ code: 'USD', mwr: '0.050000' }), benchmark: 'USD' })
      await legend()
      const toggle = screen.getByRole('button', { name: 'Dolar' })
      fireEvent.click(toggle)
      expect(toggle).toHaveAttribute('aria-pressed', 'false')
      expect(tooltipAt(1)).not.toContain('fark')
    })
  })
})
