import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatChartValue, type ChartPoint } from '../../lib/chart'
import { formatUnitPrice, MASK } from '../../lib/format'
import { setPrivacy } from '../../lib/privacy'
import { chartDrag, chartMove, chartPress, chartRelease, mockChartSize } from '../../test/chartSize'
import { LineChart } from './LineChart'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
})
afterEach(() => {
  restoreSize()
  setPrivacy(false)
  vi.restoreAllMocks()
})

const point = (date: string, total: string | null, cost: string | null = '900.00'): ChartPoint => ({
  date,
  total_value: total,
  cash: '0',
  cost_basis: cost,
})

const threeMonths = [
  point('2026-01-01', '1000.00'),
  point('2026-02-01', '1500.00'),
  point('2026-03-01', '2000.00'),
]

const chart = () => screen.getByRole('application')
const tooltip = () => document.querySelector('[role="tooltip"]')?.textContent ?? ''
const tooltipWrapper = (container: HTMLElement) =>
  container.querySelector<HTMLElement>('.recharts-tooltip-wrapper')!
const legend = (container: HTMLElement) =>
  container.querySelector('[data-chart-legend]')?.textContent ?? ''
// The drawn line of one series (not the value's area fill); the series' own
// class, set by LineChart.
const seriesPath = (container: HTMLElement, series: string) =>
  container.querySelector(`.series-${series} path[fill="none"]`)
// Every subpath starts with M: one per run of points with a value.
const segmentCount = (path: Element | null) => path?.getAttribute('d')?.match(/M/g)?.length ?? 0
const svgTexts = (container: HTMLElement) =>
  [...container.querySelectorAll('svg text')].map((el) => el.textContent ?? '')

describe('LineChart', () => {
  it('draws the value and cost series and names both in the legend', () => {
    const { container, rerender } = render(<LineChart points={threeMonths} />)

    expect(segmentCount(seriesPath(container, 'value'))).toBe(1)
    expect(segmentCount(seriesPath(container, 'cost'))).toBe(1)
    expect(legend(container)).toContain('Toplam değer')
    expect(legend(container)).toContain('Maliyet')

    rerender(<LineChart points={threeMonths} labels={{ value: 'Fiyat', cost: 'Ort. maliyet' }} />)
    expect(legend(container)).toContain('Fiyat')
    expect(legend(container)).toContain('Ort. maliyet')
  })

  it('draws no benchmark series or legend row without benchmark values', () => {
    const { container } = render(<LineChart benchmarkLabel="Dolar" points={threeMonths} />)

    expect(seriesPath(container, 'benchmark')).toBeNull()
    expect(legend(container)).not.toContain('Dolar')
  })

  it('draws the benchmark in separate segments at gaps, with a legend row', () => {
    const withBenchmark = (date: string, benchmark_value?: string) => ({
      ...point(date, '1000.00'),
      benchmark_value,
    })
    const { container } = render(
      <LineChart
        benchmarkLabel="Dolar"
        points={[
          withBenchmark('2026-01-01', '1000.00'),
          withBenchmark('2026-01-02', '1010.00'),
          withBenchmark('2026-01-03'),
          withBenchmark('2026-01-04', '1030.00'),
          withBenchmark('2026-01-05', '5000.00'),
        ]}
      />,
    )

    expect(segmentCount(seriesPath(container, 'benchmark'))).toBe(2)
    expect(legend(container)).toContain('Dolar')
    // 5000 is in the y domain, so the top tick reaches it.
    expect(svgTexts(container)).toContainEqual(expect.stringMatching(/^5\sB$/))
  })

  it('marks benchmark points from estimatedFrom on as estimated', () => {
    const withBenchmark = (date: string, total: string, benchmark_value: string) => ({
      ...point(date, total),
      benchmark_value,
    })
    render(
      <LineChart
        benchmarkLabel="TÜFE"
        estimatedFrom="2026-02-01"
        points={[
          withBenchmark('2026-01-01', '1000.00', '1000.00'),
          withBenchmark('2026-02-01', '1100.00', '1050.00'),
        ]}
      />,
    )

    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('TÜFE')
    expect(tooltip()).not.toContain('tahmini')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('1.050')
    expect(tooltip()).toContain('tahmini')
  })

  it('draws a marker only on dates that are in the series', () => {
    const { container } = render(
      <LineChart
        markers={[
          { date: '2026-02-01', kind: 'buy' },
          { date: '2025-12-01', kind: 'sell' },
        ]}
        points={threeMonths}
      />,
    )

    const shapes = container.querySelectorAll('[data-marker]')
    expect(shapes).toHaveLength(1)
    expect(shapes[0].getAttribute('data-marker')).toBe('buy')
    // Only the plotted kind is in the legend (GLG-807).
    const kinds = [...container.querySelectorAll('[data-legend-marker]')]
    expect(kinds.map((el) => el.textContent)).toEqual(['Alış'])

    // The marker is listed in the tooltip of its own date only.
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).not.toContain('Alış')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('Alış')
  })

  it('is named by its summary and moves the tooltip with the arrow keys', () => {
    render(<LineChart points={threeMonths} />)

    expect(chart()).toHaveAccessibleName(/Oca.*Mar.*1\.000 → 2\.000 \(\+1\.000\)/)
    expect(tooltip()).toBe('')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('01 Oca')
    expect(tooltip()).toContain('1.000')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('01 Şub')
    expect(tooltip()).toContain('1.500')
    expect(document.querySelector('[role="tooltip"]')).toHaveAttribute('aria-live', 'polite')
  })

  // A press would focus the layer under the pointer (tabindex -1), which
  // bubbles to Recharts' focus handler and pins the keyboard tooltip on the
  // first date (GLG-1034). Preventing the press's default keeps focus out; a
  // field focused elsewhere still blurs, as the rename field saves on blur.
  it('takes no focus from a mouse press, and still blurs a focused field', () => {
    const { container, rerender } = render(<LineChart points={threeMonths} />)
    const layer = () => container.querySelector('svg g')!
    expect(fireEvent.mouseDown(layer(), { button: 0 })).toBe(false)
    rerender(
      <>
        <input aria-label="name" />
        <LineChart points={threeMonths} onZoom={() => {}} />
      </>,
    )
    screen.getByLabelText('name').focus()
    expect(fireEvent.mouseDown(layer(), { button: 0 })).toBe(false)
    expect(document.activeElement).toBe(document.body)
  })

  it('places the keyboard tooltip off the active point: below it high up, above it low down', () => {
    // jsdom has no layout: the card measures as 150 x 60.
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(150)
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(60)
    const { container } = render(
      <LineChart
        points={[
          point('2026-01-01', '400.00', null),
          point('2026-01-02', '1200.00', null),
          point('2026-01-03', '2000.00', null),
        ]}
      />,
    )
    const card = () => {
      const m = tooltipWrapper(container).style.transform.match(
        /translate\(([-\d.]+)px, ([-\d.]+)px\)/,
      )!
      return { x: Number(m[1]), y: Number(m[2]) }
    }
    const dot = () => {
      const c = container.querySelector('.recharts-active-dot circle')!
      return { x: Number(c.getAttribute('cx')), y: Number(c.getAttribute('cy')) }
    }

    // 400, low in the plot: the card ends 12px above the point.
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(card().y + 60).toBeCloseTo(dot().y - 12)
    // 2000, at the top: the card starts 12px below the point.
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(card().y).toBeCloseTo(dot().y + 12)
  })

  it('shows the date on a day without any value', () => {
    const { container } = render(
      <LineChart
        points={[
          point('2026-01-01', '1000.00'),
          point('2026-01-02', null, null),
          point('2026-01-03', '1100.00'),
        ]}
      />,
    )

    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('02 Oca')
    expect(tooltip()).not.toContain('Toplam değer')
    expect(tooltipWrapper(container).style.visibility).toBe('visible')
  })

  it('shows the empty state when every value is zero', () => {
    const { container, getByText } = render(<LineChart points={[point('2026-01-01', '0', '0')]} />)

    expect(container.querySelector('svg')).not.toBeInTheDocument()
    expect(getByText('Grafik için yeterli veri yok.')).toBeInTheDocument()
  })

  it('labels a single point with one date', () => {
    const { container } = render(<LineChart points={[point('2026-01-01', '1000.00')]} />)

    expect(svgTexts(container).filter((s) => s.includes('Oca'))).toHaveLength(1)
  })

  it('labels one date per about 120 px of plot width', () => {
    // A 93-day range (Oca to Nis 2026); every date label carries a month.
    const days = Array.from({ length: 93 }, (_, i) =>
      point(new Date(Date.UTC(2026, 0, 1 + i)).toISOString().slice(0, 10), '1000.00'),
    )
    const dateLabels = (container: HTMLElement) =>
      svgTexts(container).filter((s) => /Oca|Şub|Mar|Nis/.test(s))
    // 968 px less the 56 px y axis and the 12 px right margin: a 900 px plot.
    restoreSize()
    restoreSize = mockChartSize(968, 240)
    const wide = render(<LineChart points={days} />)
    expect(dateLabels(wide.container)).toEqual([
      '01 Oca',
      '16 Oca',
      '01 Şub',
      '16 Şub',
      '03 Mar',
      '19 Mar',
      '03 Nis',
    ])
    wide.unmount()

    // A 200 px plot keeps three: the first, the middle and the last.
    restoreSize()
    restoreSize = mockChartSize(268, 240)
    const { container } = render(<LineChart points={days} />)
    expect(dateLabels(container)).toEqual(['01 Oca', '16 Şub', '03 Nis'])
  })

  it('reserves two legend lines below sm and one from sm', () => {
    const { container } = render(<LineChart points={threeMonths} />)

    expect(container.querySelector('[data-chart-legend]')).toHaveClass('min-h-9', 'sm:min-h-4')
  })

  it('adds the year to the date labels when the range spans calendar years', () => {
    const { container } = render(
      <LineChart points={[point('2025-12-30', '1000.00'), point('2026-01-02', '1000.00')]} />,
    )

    expect(svgTexts(container)).toContain('30 Ara 2025')
    expect(svgTexts(container)).toContain('02 Oca 2026')
  })

  it('prints the exact decimal string, not a rounded number', () => {
    // Above 2^53: Number() would print ...992.
    render(<LineChart points={[point('2026-01-01', '9007199254740993.00')]} />)

    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain(formatChartValue('9007199254740993.00'))
    expect(tooltip()).toContain('9.007.199.254.740.993')
  })

  it('subtracts the summary change exactly, and masks it in privacy mode', () => {
    // As floats 1.15 - 0.65 is 0.4999..., which would print 0, not +1.
    const points = [point('2026-01-01', '0.65'), point('2026-02-01', '1.15')]
    const { rerender } = render(<LineChart points={points} />)
    expect(chart()).toHaveAccessibleName(/1 → 1 \(\+1\)$/)

    setPrivacy(true)
    rerender(<LineChart points={points} />)
    expect(chart()).toHaveAccessibleName(new RegExp(`\\(${MASK}\\)$`))
  })

  it("formats the summary's first, last and change with formatValue", () => {
    // A fund priced in hundredths of a lira: whole numbers would read 0 → 0 (0).
    const points = [point('2026-01-01', '0.0312'), point('2026-02-01', '0.0321')]
    const price = (v: string) => formatUnitPrice(v, 'TRY', 6)
    const { rerender } = render(<LineChart points={points} formatValue={price} />)
    expect(chart()).toHaveAccessibleName(/₺0,0312 → ₺0,0321 \(\+₺0,0009\)$/)

    rerender(<LineChart points={[...points].reverse()} formatValue={price} />)
    expect(chart()).toHaveAccessibleName(/₺0,0321 → ₺0,0312 \(-₺0,0009\)$/)
  })

  it('masks every axis label and tooltip amount once privacy is turned on', () => {
    const { container, rerender } = render(<LineChart points={threeMonths} />)
    expect(svgTexts(container)).toContainEqual(expect.stringMatching(/^1\sB$/))

    setPrivacy(true)
    rerender(<LineChart points={threeMonths} />)
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })

    const yLabels = svgTexts(container).filter((s) => !/Oca|Şub|Mar/.test(s))
    expect(yLabels.length).toBeGreaterThan(1)
    expect(yLabels.every((s) => s === MASK)).toBe(true)
    expect(tooltip()).toContain(MASK)
    expect(tooltip()).not.toMatch(/\d{3}/)
  })

  it('draws nothing for a series that is all null, and no segment before the first value', () => {
    const { container } = render(
      <LineChart
        points={[
          point('2026-01-01', null, null),
          point('2026-01-02', '1000.00', null),
          point('2026-01-03', '1100.00', null),
        ]}
      />,
    )

    expect(seriesPath(container, 'cost')).toBeNull()
    expect(legend(container)).not.toContain('Maliyet')
    // The value line starts at its first value: one segment, right of the
    // first date, where the leading null is.
    const d = seriesPath(container, 'value')!.getAttribute('d')!
    expect(segmentCount(seriesPath(container, 'value'))).toBe(1)
    const firstDate = [...container.querySelectorAll('svg text')].find(
      (el) => el.textContent === '01 Oca',
    )
    expect(Number(d.match(/^M\s*([\d.]+)/)![1])).toBeGreaterThan(
      Number(firstDate!.getAttribute('x')),
    )
  })

  it('includes negative values on the y axis', () => {
    const { container } = render(
      <LineChart
        points={[point('2026-01-01', '-500.00', null), point('2026-01-02', '1500.00', null)]}
      />,
    )

    expect(svgTexts(container)).toContainEqual(expect.stringMatching(/^[-−]/))
  })

  it('gives two charts on one page different gradient ids', () => {
    const { container } = render(
      <>
        <LineChart points={threeMonths} />
        <LineChart points={threeMonths} />
      </>,
    )

    const ids = [...container.querySelectorAll('linearGradient')].map((el) => el.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
  })

  describe('legend toggle', () => {
    const withBenchmark = (date: string, total: string, benchmark_value?: string) => ({
      ...point(date, total, '100.00'),
      benchmark_value,
    })
    const benchmarked = [
      withBenchmark('2026-01-01', '1000.00', '1000.00'),
      withBenchmark('2026-02-01', '1500.00', '1100.00'),
      withBenchmark('2026-03-01', '2000.00', '1200.00'),
    ]
    const toggle = (name: string) => screen.getByRole('button', { name })
    const lowestTick = (container: HTMLElement) =>
      svgTexts(container).filter((s) => !/Oca|Şub|Mar|Nis/.test(s))[0]

    it('makes the cost and benchmark entries buttons, and nothing else', () => {
      render(
        <LineChart
          benchmarkLabel="Dolar"
          markers={[{ date: '2026-02-01', kind: 'buy' }]}
          points={benchmarked}
        />,
      )

      expect(toggle('Maliyet')).toHaveAttribute('aria-pressed', 'true')
      expect(toggle('Dolar')).toHaveAttribute('aria-pressed', 'true')
      expect(screen.getAllByRole('button')).toHaveLength(2)
    })

    it('hides the cost series from the plot and the tooltip, and shows it again', () => {
      const { container } = render(<LineChart points={benchmarked} />)

      fireEvent.click(toggle('Maliyet'))
      expect(toggle('Maliyet')).toHaveAttribute('aria-pressed', 'false')
      expect(seriesPath(container, 'cost')).toBeNull()
      expect(seriesPath(container, 'value')).not.toBeNull()
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      expect(tooltip()).toContain('Toplam değer')
      expect(tooltip()).not.toContain('Maliyet')

      fireEvent.click(toggle('Maliyet'))
      expect(toggle('Maliyet')).toHaveAttribute('aria-pressed', 'true')
      expect(seriesPath(container, 'cost')).not.toBeNull()
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      expect(tooltip()).toContain('Maliyet')
    })

    it('keeps a hidden entry muted and strikes it through', () => {
      render(<LineChart points={benchmarked} />)

      fireEvent.click(toggle('Maliyet'))
      expect(toggle('Maliyet')).toHaveClass('text-muted', 'line-through')
      expect(toggle('Maliyet')).not.toHaveClass('text-faint')
    })

    it('refits the y axis to the series left visible', () => {
      const { container } = render(<LineChart points={benchmarked} />)
      const before = lowestTick(container)

      fireEvent.click(toggle('Maliyet'))
      expect(lowestTick(container)).not.toBe(before)
    })

    it('keeps each chart its own hidden series', () => {
      const { container } = render(
        <>
          <LineChart points={benchmarked} />
          <LineChart points={benchmarked} />
        </>,
      )

      fireEvent.click(screen.getAllByRole('button', { name: 'Maliyet' })[0])
      expect(container.querySelectorAll('.series-cost path[fill="none"]')).toHaveLength(1)
    })

    it('keeps the state over a new range and shows a newly chosen benchmark', () => {
      const { container, rerender } = render(
        <LineChart benchmarkLabel="Dolar" points={benchmarked} />,
      )
      fireEvent.click(toggle('Maliyet'))
      fireEvent.click(toggle('Dolar'))
      expect(seriesPath(container, 'benchmark')).toBeNull()

      // Another range, same benchmark: both stay hidden.
      const later = [...benchmarked, withBenchmark('2026-04-01', '2500.00', '1300.00')]
      rerender(<LineChart benchmarkLabel="Dolar" points={later} />)
      expect(toggle('Maliyet')).toHaveAttribute('aria-pressed', 'false')
      expect(toggle('Dolar')).toHaveAttribute('aria-pressed', 'false')

      // Another benchmark is shown; cost stays hidden.
      rerender(<LineChart benchmarkLabel="Euro" points={later} />)
      expect(toggle('Euro')).toHaveAttribute('aria-pressed', 'true')
      expect(seriesPath(container, 'benchmark')).not.toBeNull()
      expect(toggle('Maliyet')).toHaveAttribute('aria-pressed', 'false')

      // The benchmark removed and chosen again: shown, nothing left over.
      fireEvent.click(toggle('Euro'))
      rerender(<LineChart points={threeMonths} />)
      expect(screen.queryByRole('button', { name: 'Euro' })).toBeNull()
      rerender(<LineChart benchmarkLabel="Euro" points={later} />)
      expect(toggle('Euro')).toHaveAttribute('aria-pressed', 'true')
    })
  })

  // At 600 px the plot runs from the y axis (56 px) to the right margin
  // (588 px): x 60 is the first of threeMonths, 320 the second, 580 the third,
  // and 20 is on the y axis, outside the plot.
  describe('drag to zoom', () => {
    const wrapper = (container: HTMLElement) => container.querySelector('.recharts-wrapper')!
    const span = (container: HTMLElement) => container.querySelector('.zoom-span')

    it('sets the span from the first to the last date, whichever way the drag goes', async () => {
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={threeMonths} onZoom={onZoom} />)

      await chartPress(wrapper(container), 60)
      await chartMove(wrapper(container), 580)
      // While dragging the span is shaded.
      expect(span(container)).not.toBeNull()
      chartRelease(wrapper(container), 580)
      expect(onZoom).toHaveBeenCalledTimes(1)
      expect(onZoom).toHaveBeenLastCalledWith('2026-01-01', '2026-03-01')
      expect(span(container)).toBeNull()

      await chartDrag(wrapper(container), 580, 320)
      expect(onZoom).toHaveBeenCalledTimes(2)
      expect(onZoom).toHaveBeenLastCalledWith('2026-02-01', '2026-03-01')
    })

    it('does nothing on a drag within one point', async () => {
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={threeMonths} onZoom={onZoom} />)

      await chartDrag(wrapper(container), 60, 70)
      expect(onZoom).not.toHaveBeenCalled()
      expect(span(container)).toBeNull()
    })

    // A year of days puts about 1.5 px between points, so a short drag still
    // crosses several of them (GLG-1032).
    it('zooms only on a drag of at least 8 px', async () => {
      const start = Date.UTC(2025, 0, 1)
      const year = Array.from({ length: 366 }, (_, i) =>
        point(new Date(start + i * 86400000).toISOString().slice(0, 10), String(1000 + i)),
      )
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={year} onZoom={onZoom} />)

      await chartDrag(wrapper(container), 300, 307)
      expect(onZoom).not.toHaveBeenCalled()
      expect(span(container)).toBeNull()

      await chartDrag(wrapper(container), 300, 308)
      expect(onZoom).toHaveBeenCalledTimes(1)
      const [from, to] = onZoom.mock.calls[0]
      expect(from < to).toBe(true)
    })

    it('does nothing and removes the span when released outside the plot', async () => {
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={threeMonths} onZoom={onZoom} />)

      // On the y axis, inside the chart.
      await chartPress(wrapper(container), 60)
      await chartMove(wrapper(container), 580)
      await chartMove(wrapper(container), 20)
      chartRelease(wrapper(container), 20)
      expect(onZoom).not.toHaveBeenCalled()
      expect(span(container)).toBeNull()

      // Outside the chart: the mouse leaves and the button comes up elsewhere,
      // and no listener is left on the window.
      const added = vi.spyOn(window, 'addEventListener')
      const removed = vi.spyOn(window, 'removeEventListener')
      await chartPress(wrapper(container), 60)
      await chartMove(wrapper(container), 580)
      expect(span(container)).not.toBeNull()
      fireEvent.mouseLeave(wrapper(container))
      fireEvent.mouseUp(document.body)
      expect(span(container)).toBeNull()
      // Back in, a move with no button down draws nothing.
      await chartMove(wrapper(container), 320)
      chartRelease(wrapper(container), 320)
      expect(onZoom).not.toHaveBeenCalled()
      expect(span(container)).toBeNull()
      const listeners = (spy: typeof added) =>
        spy.mock.calls.map(([type, listener]) => [type, listener])
      expect(listeners(added).length).toBeGreaterThan(0)
      expect(listeners(removed)).toEqual(expect.arrayContaining(listeners(added)))
    })

    it('cancels on Escape', async () => {
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={threeMonths} onZoom={onZoom} />)

      await chartPress(wrapper(container), 60)
      await chartMove(wrapper(container), 580)
      fireEvent.keyDown(window, { key: 'Escape' })
      expect(span(container)).toBeNull()
      chartRelease(wrapper(container), 580)
      expect(onZoom).not.toHaveBeenCalled()
    })

    it.each(['touch', 'pen'])('never zooms on a %s pointer', async (pointerType) => {
      const onZoom = vi.fn()
      const { container } = render(<LineChart points={threeMonths} onZoom={onZoom} />)

      await chartPress(wrapper(container), 60, pointerType)
      await chartMove(wrapper(container), 580)
      expect(span(container)).toBeNull()
      chartRelease(wrapper(container), 580)
      expect(onZoom).not.toHaveBeenCalled()
    })

    it('draws no span without onZoom', async () => {
      const { container } = render(<LineChart points={threeMonths} />)

      await chartPress(wrapper(container), 60)
      await chartMove(wrapper(container), 580)
      expect(span(container)).toBeNull()
      chartRelease(wrapper(container), 580)
    })
  })

  // ReturnCard's percent mode: twr and benchmark_twr in the value and
  // benchmark fields, as fractions, and no cost.
  describe('in percent', () => {
    const pct = (date: string, value: string, benchmark: string): ChartPoint => ({
      ...point(date, value, null),
      benchmark_value: benchmark,
    })
    const returns = [
      pct('2026-01-01', '0.000000', '0.000000'),
      pct('2026-02-01', '0.052000', '0.030000'),
      pct('2026-03-01', '-0.021000', '0.081000'),
    ]
    const yLabels = (container: HTMLElement) =>
      svgTexts(container).filter((s) => !/Oca|Şub|Mar/.test(s))

    it('labels the y axis with distinct percentages', () => {
      const { container } = render(
        <LineChart unit="percent" benchmarkLabel="Dolar" points={returns} />,
      )
      expect(yLabels(container)).toEqual(['-%5', '%0', '%5', '%10'])
    })

    it('draws a solid zero line and no area fill', () => {
      const { container, rerender } = render(<LineChart points={threeMonths} />)
      expect(container.querySelector('.zero-line')).toBeNull()
      expect(container.querySelector('.series-value path[fill^="url"]')).not.toBeNull()

      rerender(<LineChart unit="percent" benchmarkLabel="Dolar" points={returns} />)
      const zero = container.querySelector('.zero-line line')
      expect(zero).not.toBeNull()
      expect(zero).not.toHaveAttribute('stroke-dasharray')
      const paths = [...container.querySelectorAll('.series-value path')]
      expect(paths.length).toBeGreaterThan(0)
      expect(paths.every((el) => el.getAttribute('fill') === 'none')).toBe(true)
    })

    it('shows signed percentages and the difference in the tooltip and the summary', () => {
      render(<LineChart unit="percent" benchmarkLabel="Dolar" points={returns} />)

      expect(chart()).toHaveAccessibleName(/%0 → -%2,1 \(-%2,1\)$/)
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      // (1 + 0.052) / (1 + 0.03) - 1 = 2.1%
      expect(tooltip()).toContain('Toplam değer+%5,2')
      expect(tooltip()).toContain('Dolar+%3')
      expect(tooltip()).toContain('fark+%2,1')
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      // (1 - 0.021) / (1 + 0.081) - 1 = -9.4%
      expect(tooltip()).toContain('fark-%9,4')
    })

    it('leaves the difference out while the benchmark is hidden', () => {
      render(<LineChart unit="percent" benchmarkLabel="Dolar" points={returns} />)
      fireEvent.click(screen.getByRole('button', { name: 'Dolar' }))
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      expect(tooltip()).toContain('+%5,2')
      expect(tooltip()).not.toContain('fark')
    })

    it('does not mask percentages in privacy mode', () => {
      setPrivacy(true)
      const { container } = render(
        <LineChart unit="percent" benchmarkLabel="Dolar" points={returns} />,
      )
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })
      fireEvent.keyDown(chart(), { key: 'ArrowRight' })

      expect(yLabels(container)).toEqual(['-%5', '%0', '%5', '%10'])
      expect(tooltip()).toContain('+%5,2')
      expect(tooltip()).not.toContain(MASK)
      expect(chart()).toHaveAccessibleName(/-%2,1 \(-%2,1\)$/)
    })

    it('draws a flat 0% line rather than the empty state', () => {
      const flat = [point('2026-01-01', '0.000000', null), point('2026-02-01', '0.000000', null)]
      const { container } = render(<LineChart unit="percent" points={flat} />)
      expect(segmentCount(seriesPath(container, 'value'))).toBe(1)
    })

    it('puts the value hint on its legend entry', () => {
      const { container } = render(
        <LineChart
          unit="percent"
          points={returns}
          labels={{ value: 'Portföy (zaman ağırlıklı)', valueHint: 'Neden' }}
        />,
      )
      expect(
        within(container.querySelector('[data-chart-legend]')!).getByText(
          'Portföy (zaman ağırlıklı)',
        ),
      ).toHaveAttribute('title', 'Neden')
    })
  })

  it('renders a long daily range without a node per point', () => {
    const start = Date.UTC(2021, 0, 1)
    const points = Array.from({ length: 1800 }, (_, i) =>
      point(new Date(start + i * 86400000).toISOString().slice(0, 10), String(1000 + i)),
    )
    const { container } = render(<LineChart points={points} />)

    expect(segmentCount(seriesPath(container, 'value'))).toBe(1)
    expect(container.querySelectorAll('circle')).toHaveLength(0)
  })
})
