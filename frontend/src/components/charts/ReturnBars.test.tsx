import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PeriodBucket } from '../../api/history'
import { mockChartSize } from '../../test/chartSize'
import { ReturnBars } from './ReturnBars'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // As in BarChart.test: reduced motion draws the final bars at once.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)',
    addEventListener() {},
    removeEventListener() {},
  }))
})
afterEach(() => {
  restoreSize()
  vi.unstubAllGlobals()
})

// A month bucket of the periods endpoint; benchmark undefined means none.
function month(period: string, twr: string | null, benchmark?: string | null): PeriodBucket {
  return {
    period,
    from: `${period}-01`,
    to: `${period}-28`,
    partial: false,
    returns: { simple: twr, twr, mwr: twr },
    benchmark:
      benchmark === undefined ? null : { simple: benchmark, twr: benchmark, mwr: benchmark },
  }
}

// 26 months, 2024-09 .. 2026-10, each 1%.
const months26 = Array.from({ length: 26 }, (_, i) => {
  const d = new Date(Date.UTC(2024, 8 + i, 1))
  return month(d.toISOString().slice(0, 7), '0.010000')
})

const chart = () => screen.getByRole('application')
const tooltip = () => document.querySelector('[role="tooltip"]')?.textContent ?? ''
// Each drawn bar's top and bottom; Recharts gives a negative bar a negative
// height from its far end.
const bars = (container: HTMLElement, series: string) =>
  [...container.querySelectorAll(`.series-${series} path`)].map((p) => {
    const y = Number(p.getAttribute('y'))
    const height = Number(p.getAttribute('height'))
    return {
      top: Math.min(y, y + height),
      bottom: Math.max(y, y + height),
      height: Math.abs(height),
    }
  })
const zeroY = (container: HTMLElement) =>
  Number(container.querySelector('.recharts-reference-line line')!.getAttribute('y1'))

describe('ReturnBars', () => {
  it('draws one group per month for the last 24 buckets', () => {
    const { container } = render(<ReturnBars buckets={months26} method="twr" />)

    expect(bars(container, 'portfolio')).toHaveLength(24)
    // The two oldest months are cut: the name starts at 2024-11.
    expect(chart()).toHaveAccessibleName(/^Kas 24 – Eki 26/)
  })

  it('draws a benchmark series only with a benchmark', () => {
    const buckets = [
      month('2026-08', '0.020000', '0.010000'),
      month('2026-09', '0.010000', '0.030000'),
    ]
    const { container, rerender } = render(<ReturnBars buckets={buckets} method="twr" />)
    expect(bars(container, 'benchmark')).toHaveLength(0)
    expect(screen.queryByText('Dolar')).not.toBeInTheDocument()

    rerender(<ReturnBars buckets={buckets} method="twr" benchmarkLabel="Dolar" />)
    expect(bars(container, 'benchmark')).toHaveLength(2)
    expect(bars(container, 'portfolio')).toHaveLength(2)
    expect(screen.getByText('Dolar')).toBeInTheDocument()
    expect(screen.getByText('Portföy')).toBeInTheDocument()
  })

  it('draws a negative return below the zero line and a positive one above it', () => {
    const { container } = render(
      <ReturnBars
        buckets={[month('2026-08', '-0.040000'), month('2026-09', '0.050000')]}
        method="twr"
      />,
    )

    const zero = zeroY(container)
    const [down, up] = bars(container, 'portfolio')
    expect(down.top).toBeCloseTo(zero, 0)
    expect(down.bottom).toBeGreaterThan(zero + 1)
    expect(up.bottom).toBeCloseTo(zero, 0)
    expect(up.height).toBeGreaterThan(1)
  })

  it('reads the chosen method', () => {
    const b = month('2026-09', '0.010000')
    b.returns.simple = '-0.020000'
    const { container } = render(
      <ReturnBars buckets={[b, month('2026-10', '0.010000')]} method="simple" />,
    )

    expect(bars(container, 'portfolio')[0].bottom).toBeGreaterThan(zeroY(container) + 1)
  })

  it('draws no bar for a null return and says so in the tooltip', () => {
    const { container } = render(
      <ReturnBars
        buckets={[month('2026-08', null, '0.010000'), month('2026-09', '0.012345', null)]}
        method="twr"
        benchmarkLabel="Dolar"
      />,
    )

    expect(bars(container, 'portfolio').filter((b) => b.height > 0)).toHaveLength(1)
    expect(bars(container, 'benchmark').filter((b) => b.height > 0)).toHaveLength(1)

    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toBe('Ağustos 2026PortföytanımsızDolar+%1')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toBe('Eylül 2026Portföy+%1,23Dolartanımsız')
  })

  it('is one focus stop named by its range and series', () => {
    const { container } = render(
      <ReturnBars
        buckets={[
          month('2026-08', '0.010000', '0.010000'),
          month('2026-09', '0.010000', '0.010000'),
        ]}
        method="twr"
        benchmarkLabel="Dolar"
      />,
    )

    expect([...container.querySelectorAll('[tabindex="0"]')]).toEqual([chart()])
    expect(chart()).toHaveAccessibleName('Ağu 26 – Eyl 26 aylık getiri: Portföy, Dolar')
  })

  it('labels the y axis in percent with a zero tick', () => {
    const { container } = render(
      <ReturnBars
        buckets={[month('2026-08', '-0.040000'), month('2026-09', '0.050000')]}
        method="twr"
      />,
    )

    const ticks = [...container.querySelectorAll('svg text[text-anchor="end"]')].map(
      (el) => el.textContent,
    )
    expect(ticks).toContain('%0')
    expect(ticks).toContain('-%4')
    expect(ticks).toContain('%4')
  })

  it('shows the empty state when no month has a return', () => {
    const { container } = render(<ReturnBars buckets={[month('2026-09', null)]} method="twr" />)

    expect(container.querySelector('svg')).not.toBeInTheDocument()
    expect(screen.getByText('Grafik için yeterli veri yok.')).toBeInTheDocument()
  })
})
