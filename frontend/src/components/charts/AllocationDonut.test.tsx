import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DONUT_SIZE, type AllocationRow } from '../../lib/allocation'
import { mockChartSize } from '../../test/chartSize'
import { AllocationDonut } from './AllocationDonut'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // jsdom never runs the slices' animation to its end: ask for reduced
  // motion, which Recharts honours by drawing the final slices at once.
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

const row = (key: string, value: number, share: number, color: string): AllocationRow => ({
  key,
  label: key,
  value,
  amount: String(value),
  share,
  color,
})

const rows = [
  row('THYAO', 300, 0.6, 'var(--color-cat-1)'),
  row('GARAN', 100, 0.2, 'var(--color-cat-2)'),
  row('cash', 100, 0.2, 'var(--color-cash)'),
]

function renderDonut(props: Partial<Parameters<typeof AllocationDonut>[0]> = {}) {
  return render(
    <AllocationDonut
      rows={rows}
      currency="TRY"
      totalLabel="₺500"
      active={null}
      onActive={() => {}}
      {...props}
    />,
  )
}

const sectors = (container: HTMLElement) => [
  ...container.querySelectorAll<SVGPathElement>('path.recharts-sector'),
]

describe('AllocationDonut', () => {
  it('draws one slice per row, hidden from assistive technology and never focused', () => {
    const { container } = renderDonut()

    expect(sectors(container)).toHaveLength(3)
    expect(container.firstElementChild).toHaveAttribute('aria-hidden', 'true')
    expect(container.querySelector('[tabindex]:not([tabindex="-1"])')).toBeNull()
    expect(screen.queryByRole('application')).toBeNull()
    expect(container).toHaveTextContent('Toplam₺500')
  })

  it('draws a single row as a full ring with no NaN in its path', () => {
    const { container } = renderDonut({ rows: [row('cash', 100, 1, 'var(--color-cash)')] })

    const [only] = sectors(container)
    expect(only.getAttribute('d')).toBeTruthy()
    expect(only.getAttribute('d')).not.toContain('NaN')
  })

  it('dims the other slices while a row is active', () => {
    const { container } = renderDonut({ active: 'GARAN' })

    expect(sectors(container).map((s) => s.getAttribute('fill-opacity'))).toEqual([
      '0.3',
      '1',
      '0.3',
    ])
  })

  it('reports the hovered slice and clears it on leave', () => {
    const onActive = vi.fn()
    const { container } = renderDonut({ onActive })

    fireEvent.mouseEnter(sectors(container)[1])
    expect(onActive).toHaveBeenLastCalledWith('GARAN')
    fireEvent.mouseLeave(sectors(container)[1])
    expect(onActive).toHaveBeenLastCalledWith(null)
  })

  // A press or tap focused the slice and drew a focus frame.
  it('takes no focus from a mouse press on a slice', () => {
    const { container } = renderDonut()

    for (const sector of sectors(container)) {
      expect(fireEvent.mouseDown(sector, { button: 0 })).toBe(false)
      expect(document.activeElement).toBe(document.body)
    }
  })

  it('keeps the tooltip inside the donut on every slice', () => {
    // Every element, the tooltip card too, now measures 120 x 40 px.
    restoreSize()
    restoreSize = mockChartSize(120, 40)
    const { container } = renderDonut()

    for (const sector of sectors(container)) {
      fireEvent.mouseEnter(sector)
      const wrapper = container.querySelector<HTMLElement>('.recharts-tooltip-wrapper')!
      const [, x, y] = wrapper.style.transform.match(/translate\(([-\d.]+)px, ([-\d.]+)px\)/)!
      expect(Number(x)).toBeGreaterThanOrEqual(0)
      expect(Number(x) + 120).toBeLessThanOrEqual(DONUT_SIZE)
      expect(Number(y)).toBeGreaterThanOrEqual(0)
      expect(Number(y) + 40).toBeLessThanOrEqual(DONUT_SIZE)
      fireEvent.mouseLeave(sector)
    }
  })
})
