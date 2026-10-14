import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { expect, it } from 'vitest'
import { Segmented } from './Segmented'

const OPTIONS = ['a', 'b', 'c'] as const

function Harness() {
  const [value, setValue] = useState<(typeof OPTIONS)[number]>('a')
  return (
    <Segmented
      options={OPTIONS}
      value={value}
      onChange={setValue}
      label={(o) => o.toUpperCase()}
      aria-label="Seçenek"
    />
  )
}

it('moves selection and focus with ArrowRight, wrapping from the last to the first', () => {
  render(<Harness />)
  const group = screen.getByRole('radiogroup', { name: 'Seçenek' })
  screen.getByRole('radio', { name: 'A' }).focus()

  fireEvent.keyDown(group, { key: 'ArrowRight' })
  expect(screen.getByRole('radio', { name: 'B' })).toHaveAttribute('aria-checked', 'true')
  expect(screen.getByRole('radio', { name: 'B' })).toHaveFocus()
  expect(screen.getByRole('radio', { name: 'A' })).toHaveAttribute('tabindex', '-1')

  fireEvent.keyDown(group, { key: 'ArrowRight' })
  fireEvent.keyDown(group, { key: 'ArrowRight' })
  expect(screen.getByRole('radio', { name: 'A' })).toHaveAttribute('aria-checked', 'true')
  expect(screen.getByRole('radio', { name: 'A' })).toHaveFocus()
})
