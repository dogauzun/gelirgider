import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setPrivacy } from '../../lib/privacy'
import { mockChartSize } from '../../test/chartSize'
import { DrawdownChart } from './DrawdownChart'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // Reduced motion draws the final area at once; jsdom never ends the
  // animation.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)',
    addEventListener() {},
    removeEventListener() {},
  }))
})
afterEach(() => {
  restoreSize()
  vi.unstubAllGlobals()
  setPrivacy(false)
})

const series = [
  { date: '2025-03-01', value: 0 },
  { date: '2025-03-02', value: -0.2 },
  { date: '2025-03-03', value: -0.1 },
  { date: '2025-03-04', value: 0 },
]

const chart = () => screen.getByRole('application')
const tooltip = () => document.querySelector('[role="tooltip"]')?.textContent ?? ''
const yTicks = (container: HTMLElement) =>
  [...container.querySelectorAll('svg text[text-anchor="end"]')].map((el) =>
    (el.textContent ?? '').replace(/\s/g, ' '),
  )
// The area's line, as "x,y" pairs of its path.
const linePoints = (container: HTMLElement) =>
  [
    ...(container
      .querySelector('.series-drawdown .recharts-area-curve')!
      .getAttribute('d')!
      .matchAll(/[ML]([-\d.]+),([-\d.]+)/g) ?? []),
  ].map((m) => ({ x: Number(m[1]), y: Number(m[2]) }))

describe('DrawdownChart', () => {
  it('draws the series down from a zero line at the top with percent ticks', () => {
    const { container } = render(<DrawdownChart series={series} />)

    expect(yTicks(container)).toEqual(['-%20', '-%15', '-%10', '-%5', '%0'])
    const [a, b, c, d] = linePoints(container)
    expect(a.y).toBe(d.y)
    expect(b.y).toBeGreaterThan(c.y)
    expect(c.y).toBeGreaterThan(a.y)
    // Zero is the top of the plot: the first point sits on the zero line.
    const zero = container.querySelector('.recharts-reference-line line')!
    expect(Number(zero.getAttribute('y1'))).toBe(a.y)
  })

  it('names the chart by its range and depth, and the arrow keys move the tooltip', () => {
    render(<DrawdownChart series={series} />)

    expect(chart()).toHaveAccessibleName('01 Mar – 04 Mar zirveden düşüş, en büyük -%20')
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    fireEvent.keyDown(chart(), { key: 'ArrowRight' })
    expect(tooltip()).toBe('02 Mar-%20')
  })

  it('gives a series that never falls an axis below zero', () => {
    const { container } = render(
      <DrawdownChart
        series={[
          { date: '2025-03-01', value: 0 },
          { date: '2025-03-02', value: 0 },
        ]}
      />,
    )

    expect(yTicks(container).at(-1)).toBe('%0')
    expect(yTicks(container)[0]).toBe('-%1')
  })

  it('leaves percentages shown in privacy mode', () => {
    setPrivacy(true)
    const { container } = render(<DrawdownChart series={series} />)

    expect(yTicks(container)).toContain('-%20')
  })
})
