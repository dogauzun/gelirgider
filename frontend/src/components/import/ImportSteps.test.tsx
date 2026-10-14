import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { ImportSteps } from './ImportSteps'

const items = () => screen.getAllByRole('listitem')

it('marks step 1 as current and nothing as finished on the upload', () => {
  render(<ImportSteps step={1} />)
  expect(items().map((item) => item.textContent)).toEqual(['1Dosya seç', '2İncele', '3Bitti'])
  expect(items()[0]).toHaveAttribute('aria-current', 'step')
  expect(items()[1]).not.toHaveAttribute('aria-current')
  expect(screen.queryByText('(tamamlandı)')).not.toBeInTheDocument()
})

it('shows a check on the finished step and marks the review as current', () => {
  render(<ImportSteps step={2} />)
  expect(items()[0]).toHaveTextContent('Dosya seç (tamamlandı)')
  expect(items()[0]).not.toHaveTextContent('1')
  expect(items()[0].querySelector('svg')).not.toBeNull()
  expect(items()[1]).toHaveAttribute('aria-current', 'step')
  expect(items()[1]).toHaveTextContent('İncele')
})

it('names the statement under review in a batch', () => {
  render(<ImportSteps step={2} batch={{ index: 1, total: 3 }} />)
  expect(items()[1]).toHaveTextContent('İncele · Ekstre 2 / 3')
})

// GLG-1054: at 375 px the steps wrapped; short connectors and only the counter
// in a batch keep them on one line below `sm`, unchanged from `sm` up.
it('keeps the steps on one line below sm', () => {
  const { container } = render(<ImportSteps step={2} batch={{ index: 0, total: 3 }} />)
  const connectors = container.querySelectorAll('li > span.h-px')
  expect(connectors).toHaveLength(2)
  for (const c of connectors) {
    expect(c).toHaveClass('w-3', 'sm:w-8')
    expect(c).not.toHaveClass('w-8')
  }
  const short = items()[1].querySelector('.sm\\:hidden')
  expect(short).toHaveTextContent(/^İncele 1 \/ 3$/)
  const full = items()[1].querySelector('.hidden.sm\\:inline')
  expect(full).toHaveTextContent(/^İncele · Ekstre 1 \/ 3$/)
})

it('marks both earlier steps finished at the end', () => {
  render(<ImportSteps step={3} />)
  expect(screen.getAllByText('(tamamlandı)')).toHaveLength(2)
  expect(items()[2]).toHaveAttribute('aria-current', 'step')
})
