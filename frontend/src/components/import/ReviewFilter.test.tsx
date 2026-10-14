import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ReviewFilter } from './ReviewFilter'

it('shows both counts and marks the chosen one', () => {
  render(<ReviewFilter value="problems" total={42} problems={4} onChange={() => {}} />)

  expect(screen.getByRole('radiogroup', { name: 'Satırlar' })).toBeInTheDocument()
  expect(screen.getByRole('radio', { name: 'Tümü 42' })).toHaveAttribute('aria-checked', 'false')
  expect(screen.getByRole('radio', { name: 'Sorunlu 4' })).toHaveAttribute('aria-checked', 'true')
})

it('switches on a click and with the arrow keys', () => {
  const onChange = vi.fn()
  render(<ReviewFilter value="all" total={42} problems={4} onChange={onChange} />)

  fireEvent.click(screen.getByRole('radio', { name: 'Sorunlu 4' }))
  expect(onChange).toHaveBeenLastCalledWith('problems')

  fireEvent.keyDown(screen.getByRole('radio', { name: 'Tümü 42' }), { key: 'ArrowRight' })
  expect(onChange).toHaveBeenLastCalledWith('problems')
})
