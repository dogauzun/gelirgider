import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Bar } from '../../lib/chart'
import { MASK } from '../../lib/format'
import { setPrivacy } from '../../lib/privacy'
import { mockChartSize } from '../../test/chartSize'
import { BarChart } from './BarChart'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // Bars grow from zero height, and jsdom never runs the animation to its
  // end: ask for reduced motion, which Recharts honours by drawing the final
  // bars at once.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)',
    addEventListener() {},
    removeEventListener() {},
  }))
})
afterEach(() => {
  restoreSize()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  setPrivacy(false)
})

const threeMonths: Bar[] = [
  { key: '2026-01', label: 'Oca', value: '250.00' },
  { key: '2026-02', label: 'Şub', value: '0' },
  { key: '2026-03', label: 'Mar', value: '1000.00' },
]

const chart = () => screen.getByRole('application')
const tooltip = () => document.querySelector('[role="tooltip"]')?.textContent ?? ''
// The drawn bars, in entry order; the class is BarChart's own.
const barHeights = (container: HTMLElement) =>
  [...container.querySelectorAll('.series-bars path')].map((p) => Number(p.getAttribute('height')))
// The y tick labels sit right-aligned left of the plot. Intl may put a
// no-break space before the unit, so whitespace is compared as a space.
const yTicks = (container: HTMLElement) =>
  [...container.querySelectorAll('svg text[text-anchor="end"]')].map((el) =>
    (el.textContent ?? '').replace(/\s/g, ' '),
  )

describe('BarChart', () => {
  it('draws one bar per entry and a zero as a 1 px hairline', () => {
    const { container } = render(<BarChart currency="TRY" bars={threeMonths} />)

    const heights = barHeights(container)
    expect(heights).toHaveLength(3)
    expect(heights[1]).toBe(1)
    expect(heights[2]).toBeGreaterThan(heights[0])
    expect(heights[0]).toBeGreaterThan(1)
  })

  // GLG-939: ₺812 next to ₺1,28 Mn scales to a fraction of a pixel, which
  // drew the same 1 px hairline as an empty month.
  it('draws a small non-zero month at least 3 px tall and a zero month at 1 px', () => {
    const { container } = render(
      <BarChart
        currency="TRY"
        bars={[
          { key: '2026-01', label: 'Oca', value: '812.00' },
          { key: '2026-02', label: 'Şub', value: '0' },
          { key: '2026-03', label: 'Mar', value: '1284650.40' },
        ]}
      />,
    )

    const heights = barHeights(container)
    expect(heights[0]).toBeGreaterThanOrEqual(3)
    expect(heights[1]).toBe(1)
  })

  // GLG-893: Intl's own compact currency output prints "450 B ₺" on ICU 77,
  // so the ticks must put the sign where formatMoney does, with Intl's
  // default compact rounding ("1,3 Mn", not "1,28 Mn").
  it('labels the y ticks as compact money with the sign before the digits', () => {
    const { container } = render(
      <BarChart
        currency="TRY"
        bars={[
          { key: '2026-01', label: 'Oca', value: '450000.00' },
          { key: '2026-02', label: 'Şub', value: '1284650.40' },
        ]}
      />,
    )

    expect(yTicks(container)).toEqual(['₺0', '₺1,3 Mn'])
  })

  it('is one focus stop named by its summary whose arrow keys move the tooltip', () => {
    const { container } = render(<BarChart currency="TRY" bars={threeMonths} />)

    expect([...container.querySelectorAll('[tabindex="0"]')]).toEqual([chart()])
    expect(chart()).toHaveAccessibleName('3 çubuk, en yüksek Mar: ₺1.000,00')
    expect(tooltip()).toBe('')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toBe('Oca₺250,00')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toBe('Şub₺0,00')
    expect(document.querySelector('[role="tooltip"]')).toHaveAttribute('aria-live', 'polite')
  })

  // As on LineChart (GLG-1034).
  it('takes no focus from a mouse press, and still blurs a focused field', () => {
    const { container } = render(
      <>
        <input aria-label="name" />
        <BarChart currency="TRY" bars={threeMonths} />
      </>,
    )
    const input = screen.getByLabelText('name')
    input.focus()
    expect(fireEvent.mouseDown(container.querySelector('svg g')!, { button: 0 })).toBe(false)
    expect(document.activeElement).toBe(document.body)
  })

  // GLG-924: the card was placed by the pointer, so it sat over the next
  // slot and the hovered bar, and anywhere at all for the keyboard.
  it('places the keyboard tooltip centred on the active bar, off its top, inside the plot', () => {
    // jsdom has no layout: the card measures as 150 x 60.
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(150)
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(60)
    const { container } = render(<BarChart currency="TRY" bars={threeMonths} />)
    const card = () => {
      const m = container
        .querySelector<HTMLElement>('.recharts-tooltip-wrapper')!
        .style.transform.match(/translate\(([-\d.]+)px, ([-\d.]+)px\)/)!
      return { x: Number(m[1]), y: Number(m[2]), width: 150, height: 60 }
    }
    const bar = (i: number) => {
      const p = container.querySelectorAll('.series-bars path')[i]
      const n = (a: string) => Number(p.getAttribute(a))
      return { x: n('x'), y: n('y'), width: n('width') }
    }
    // Each grid line carries the plot's box.
    const grid = container.querySelector('.recharts-cartesian-grid-horizontal line')!
    const g = (a: string) => Number(grid.getAttribute(a))
    const plot = {
      left: g('x'),
      right: g('x') + g('width'),
      top: g('y'),
      bottom: g('y') + g('height'),
    }
    const check = (i: number) => {
      const c = card()
      const b = bar(i)
      // Recharts rounds the bar's x to the pixel; the band's middle is not.
      expect(c.x + c.width / 2).toBeCloseTo(b.x + b.width / 2, 0)
      expect(c.y + c.height <= b.y || c.y >= b.y).toBe(true)
      expect(c.x).toBeGreaterThanOrEqual(plot.left)
      expect(c.x + c.width).toBeLessThanOrEqual(plot.right)
      expect(c.y).toBeGreaterThanOrEqual(plot.top)
      expect(c.y + c.height).toBeLessThanOrEqual(plot.bottom)
    }

    // Oca, a low bar: the card sits above its top.
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    check(0)
    expect(card().y + 60).toBeLessThanOrEqual(bar(0).y)
    // Mar, the tallest bar: the card sits below its top.
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    check(2)
    expect(card().y).toBeGreaterThanOrEqual(bar(2).y)
  })

  it('prints the exact decimal string, not a rounded number', () => {
    // Above 2^53: Number() would print ...992.
    render(
      <BarChart currency="TRY" bars={[{ key: 'a', label: 'Oca', value: '9007199254740993.00' }]} />,
    )

    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toContain('9.007.199.254.740.993,00')
  })

  it('shows the empty state when every value is zero', () => {
    const { container, getByText } = render(
      <BarChart currency="TRY" bars={[{ key: 'a', label: 'Oca', value: '0' }]} />,
    )

    expect(container.querySelector('svg')).not.toBeInTheDocument()
    expect(getByText('Grafik için yeterli veri yok.')).toBeInTheDocument()
  })

  it('masks the ticks, the name and the tooltip once privacy is turned on', () => {
    const { container, rerender } = render(<BarChart currency="TRY" bars={threeMonths} />)
    expect(yTicks(container)).toEqual(['₺0', '₺1 B'])

    setPrivacy(true)
    rerender(<BarChart currency="TRY" bars={threeMonths} />)
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })

    expect(yTicks(container)).toEqual([MASK, MASK])
    expect(chart()).toHaveAccessibleName(`3 çubuk, en yüksek Mar: ${MASK}`)
    expect(tooltip()).toBe(`Oca${MASK}`)
  })

  it('draws a negative value as zero', () => {
    const { container } = render(
      <BarChart
        currency="TRY"
        bars={[
          { key: 'a', label: 'Oca', value: '-500.00' },
          { key: 'b', label: 'Şub', value: '1000.00' },
        ]}
      />,
    )

    expect(barHeights(container)[0]).toBe(1)
    expect(yTicks(container)[0]).toBe('₺0')
  })
})
