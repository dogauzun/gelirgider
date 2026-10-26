import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { EmptyCard } from './EmptyCard'

afterEach(cleanup)

it('renders the title and the actions without a body', () => {
  const { container } = render(
    <EmptyCard title="Nothing yet">
      <button type="button">Add</button>
    </EmptyCard>,
  )
  expect(screen.getByText('Nothing yet')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Add' })).toBeInTheDocument()
  expect(container.querySelectorAll('p')).toHaveLength(1)
})

it('renders the body under the title, and the icon and spacing class when given', () => {
  const { container } = render(
    <EmptyCard
      title="Nothing yet"
      body="Add the first one."
      icon={<svg data-testid="icon" />}
      className="mb-6"
    />,
  )
  expect(screen.getByText('Add the first one.')).toBeInTheDocument()
  expect(screen.getByTestId('icon')).toBeInTheDocument()
  expect(container.querySelector('section')).toHaveClass('mb-6', 'card')
})
