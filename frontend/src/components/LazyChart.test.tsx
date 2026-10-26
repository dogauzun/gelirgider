import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import type { ChartPoint } from '../lib/chart'
import { mockChartSize } from '../test/chartSize'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // Each test loads LazyChart and the charts chunk afresh.
  vi.resetModules()
})
afterEach(() => {
  restoreSize()
  vi.doUnmock('./charts')
  vi.restoreAllMocks()
})

const points: ChartPoint[] = [
  { date: '2026-01-01', total_value: '1000.00', cash: '0', cost_basis: '900.00' },
  { date: '2026-02-01', total_value: '2000.00', cash: '0', cost_basis: '900.00' },
]

// A line chart with a sibling, as on a page.
async function renderPage() {
  const { LineChart } = await import('./LazyChart')
  return render(
    <>
      <LineChart points={points} />
      <p>sibling</p>
    </>,
  )
}

describe('LazyChart', () => {
  it('shows a placeholder while the chunk loads, then the chart', async () => {
    const { container } = await renderPage()

    expect(container.querySelector('.animate-pulse')).toBeInTheDocument()
    expect(screen.queryByRole('application')).not.toBeInTheDocument()

    expect(await screen.findByRole('application', { name: /Toplam değer/ })).toBeInTheDocument()
    expect(container.querySelector('.animate-pulse')).not.toBeInTheDocument()
  })

  it("reserves the line chart's legend lines while the chunk loads", async () => {
    const { container } = await renderPage()

    expect(container.querySelector('[data-chart-placeholder] > :last-child')).toHaveClass(
      'min-h-9',
      'sm:min-h-4',
    )
    expect(await screen.findByRole('application')).toBeInTheDocument()
  })

  it('shows chart.error in place of the chart when the chunk fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.doMock('./charts', () => {
      throw new Error('chunk failed')
    })
    await renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(t('chart.error'))
    expect(screen.getByText('sibling')).toBeInTheDocument()
    expect(screen.queryByRole('main')).not.toBeInTheDocument()
  })

  it('shows chart.error in the locale active at render, not at import', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.doMock('./charts', () => {
      throw new Error('chunk failed')
    })
    const { LineChart } = await import('./LazyChart')
    // The same module instance LazyChart loaded, after vi.resetModules.
    const strings = await import('../i18n/strings')
    strings.setLocale('en')
    try {
      render(<LineChart points={points} />)
      expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the chart')
    } finally {
      strings.setLocale('tr')
    }
  })

  it("shows a ring of the donut's size while the chunk loads, then the donut", async () => {
    const { AllocationDonut } = await import('./LazyChart')
    const { container } = render(
      <AllocationDonut
        rows={[{ key: 'cash', label: 'Nakit', value: 1, amount: '1', share: 1, color: 'red' }]}
        currency="TRY"
        totalLabel="₺1"
        active={null}
        onActive={() => {}}
      />,
    )

    const ring = container.querySelector<HTMLElement>('[data-chart-placeholder]')
    expect(ring).toHaveClass('rounded-full', 'border-16')
    expect([ring?.style.width, ring?.style.height]).toEqual(['150px', '150px'])
    await waitFor(() => expect(container.querySelector('[data-allocation-donut]')).not.toBeNull())
    expect(container.querySelector('[data-chart-placeholder]')).toBeNull()
  })

  it('renders nothing in place of the donut when the chunk fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.doMock('./charts', () => {
      throw new Error('chunk failed')
    })
    const { AllocationDonut } = await import('./LazyChart')
    const { container } = render(
      <div data-testid="slot">
        <AllocationDonut
          rows={[]}
          currency="TRY"
          totalLabel="₺1"
          active={null}
          onActive={() => {}}
        />
      </div>,
    )

    await waitFor(() => expect(container.querySelector('[data-chart-placeholder]')).toBeNull())
    expect(screen.getByTestId('slot')).toBeEmptyDOMElement()
  })

  it('gives the bar chart its own height while the chunk loads', async () => {
    const { BarChart } = await import('./LazyChart')
    const { container } = render(
      <BarChart bars={[{ key: 'a', label: 'Oca', value: '10' }]} currency="TRY" height={160} />,
    )

    expect(container.querySelector<HTMLElement>('[data-chart-placeholder]')?.style.height).toBe(
      '160px',
    )
    expect(await screen.findByRole('application', { name: /Oca/ })).toBeInTheDocument()
  })

  it("reserves the return bars' height and legend line while the chunk loads", async () => {
    const { ReturnBars } = await import('./LazyChart')
    const month = (period: string) => ({
      period,
      from: `${period}-01`,
      to: `${period}-28`,
      partial: false,
      returns: { simple: '0.010000', twr: '0.010000', mwr: '0.010000' },
      benchmark: null,
    })
    const { container } = render(
      <ReturnBars buckets={[month('2026-08'), month('2026-09')]} method="twr" height={180} />,
    )

    const placeholder = container.querySelector('[data-chart-placeholder]')
    expect(placeholder?.querySelector<HTMLElement>('[style]')?.style.height).toBe('180px')
    expect(placeholder?.lastElementChild).toHaveClass('mt-2', 'min-h-4')
    expect(await screen.findByRole('application', { name: /aylık getiri/ })).toBeInTheDocument()
  })
})
