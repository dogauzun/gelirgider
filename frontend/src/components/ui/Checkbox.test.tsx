import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import { Checkbox } from './Checkbox'

function Controlled({ onChange }: { onChange: (checked: boolean) => void }) {
  const [checked, setChecked] = useState(false)
  return (
    <Checkbox
      aria-label="Tümünü seç"
      checked={checked}
      onChange={(next) => {
        setChecked(next)
        onChange(next)
      }}
    />
  )
}

it('is a checkbox that click and Space toggle', () => {
  const onChange = vi.fn()
  render(<Controlled onChange={onChange} />)
  const box = screen.getByRole('checkbox', { name: 'Tümünü seç' })
  expect(box).not.toBeChecked()

  fireEvent.click(box)
  expect(onChange).toHaveBeenLastCalledWith(true)
  expect(box).toBeChecked()

  box.focus()
  fireEvent.keyDown(box, { key: ' ' })
  fireEvent.keyUp(box, { key: ' ' })
  expect(onChange).toHaveBeenLastCalledWith(false)
  expect(box).not.toBeChecked()
})

// Review Focus 4: "select all" reads as mixed when some rows are selected.
it('reads as mixed when indeterminate', () => {
  render(<Checkbox aria-label="Tümünü seç" checked={false} indeterminate onChange={() => {}} />)
  expect(screen.getByRole('checkbox')).toHaveAttribute('aria-checked', 'mixed')
  expect(screen.getByRole('checkbox')).toBePartiallyChecked()
})

it('does not toggle when disabled', () => {
  const onChange = vi.fn()
  render(<Checkbox aria-label="Satırı seç" checked={false} disabled onChange={onChange} />)
  const box = screen.getByRole('checkbox')
  expect(box).toHaveAttribute('aria-disabled', 'true')
  fireEvent.click(box)
  fireEvent.keyUp(box, { key: ' ' })
  expect(onChange).not.toHaveBeenCalled()
})

// A pending box stays enabled and focused, as Button's pending does,
// and ignores a change while its save runs.
it('keeps a pending box enabled and focused, and ignores changes', () => {
  const onChange = vi.fn()
  render(<Checkbox aria-label="Satırı seç" checked={false} pending onChange={onChange} />)
  const box = screen.getByRole('checkbox')
  box.focus()
  fireEvent.click(box)
  expect(onChange).not.toHaveBeenCalled()
  expect(box).toHaveAttribute('aria-disabled', 'true')
  expect(box).not.toHaveAttribute('data-disabled')
  expect(box).toHaveFocus()
})

// The visible label names the box, the description describes it, and a
// click on the label's text toggles it.
it('is named by its label and toggled by a click on it', () => {
  const onChange = vi.fn()
  render(
    <Checkbox
      label="Herkese açık kayıt"
      description="Kapalıyken yalnızca yönetici kullanıcı ekler."
      checked={false}
      onChange={onChange}
    />,
  )
  const box = screen.getByRole('checkbox', { name: 'Herkese açık kayıt' })
  expect(box).toHaveAccessibleDescription('Kapalıyken yalnızca yönetici kullanıcı ekler.')
  fireEvent.click(screen.getByText('Herkese açık kayıt'))
  expect(onChange).toHaveBeenCalledWith(true)
  // The whole row is the 44 px tap target.
  expect(box.closest('label')).toHaveClass('min-h-11')
})

// An aria-label beats the visible label, as on a native checkbox.
it('takes its name from aria-label over the label', () => {
  render(<Checkbox label="tcmb" aria-label="tcmb açık" checked onChange={() => {}} />)
  expect(screen.getByRole('checkbox', { name: 'tcmb açık' })).toBeChecked()
})
