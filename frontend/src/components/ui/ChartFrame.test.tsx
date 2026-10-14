import { fireEvent, render, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { expect, it, vi } from 'vitest'
import { ChartFrame } from './ChartFrame'

type Query = ComponentProps<typeof ChartFrame>['query']

// GLG-1342: the failure with a Retry, the skeleton at the chart's height
// before the first data, then the chart, faded while the next range loads.
it('frames a history query in its four states', () => {
  const refetch = vi.fn()
  const query = (state: Partial<Query>): Query => ({
    data: undefined,
    isError: false,
    error: null,
    refetch,
    isFetching: false,
    isPlaceholderData: false,
    ...state,
  })
  const frame = (state: Partial<Query>) => (
    <ChartFrame query={query(state)} height={220}>
      <p>chart</p>
    </ChartFrame>
  )

  const { container, rerender } = render(frame({ isError: true, error: new Error('boom') }))
  expect(screen.getByRole('alert')).toHaveTextContent('Grafik yüklenemedi')
  fireEvent.click(screen.getByRole('button', { name: 'Tekrar dene' }))
  expect(refetch).toHaveBeenCalledOnce()
  expect(screen.queryByText('chart')).not.toBeInTheDocument()

  rerender(frame({}))
  expect(container.firstElementChild).toHaveStyle({ height: '220px' })
  expect(screen.queryByText('chart')).not.toBeInTheDocument()

  rerender(frame({ data: {}, isPlaceholderData: true }))
  expect(screen.getByText('chart').closest('[aria-busy]')).toHaveAttribute('aria-busy', 'true')

  rerender(frame({ data: {} }))
  expect(screen.getByText('chart').closest('[aria-busy]')).toHaveAttribute('aria-busy', 'false')
})
