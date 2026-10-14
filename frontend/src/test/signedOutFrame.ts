import { within } from '@testing-library/react'
import { expect } from 'vitest'
import { signedOutTitle } from '../components/SignedOutFrame'

// The signed-out pages' shared frame around a page's h1: the brand above, the
// language switch below, and the one title size.
export function expectSignedOutFrame(heading: HTMLElement) {
  const frame = heading.closest('main')
  expect(frame).not.toBeNull()
  expect(within(frame!).getByText('gelirgider')).toBeInTheDocument()
  expect(within(frame!).getByRole('button', { name: 'English' })).toBeInTheDocument()
  expect(heading).toHaveClass(...signedOutTitle.split(' '))
}
