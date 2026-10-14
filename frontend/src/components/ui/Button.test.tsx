import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { Button } from './Button'

// GLG-920: Chromium blurs a focused button the moment it is disabled, so a
// pending button left focus on <body> while its request ran. jsdom does not
// blur on disable, so the test checks the button stays enabled as well as
// focused, and that it ignores a click and does not submit its form.
it('keeps a pending button enabled and focused, and ignores clicks', () => {
  const onClick = vi.fn()
  const onSubmit = vi.fn((event: { preventDefault: () => void }) => event.preventDefault())
  const { rerender } = render(
    <form onSubmit={onSubmit}>
      <Button type="submit" onClick={onClick}>
        Kaydet
      </Button>
    </form>,
  )
  const button = screen.getByRole('button', { name: 'Kaydet' })
  button.focus()
  rerender(
    <form onSubmit={onSubmit}>
      <Button type="submit" pending onClick={onClick}>
        Kaydet
      </Button>
    </form>,
  )

  expect(button).toBeEnabled()
  expect(button).toHaveFocus()
  fireEvent.click(button)
  expect(onClick).not.toHaveBeenCalled()
  expect(onSubmit).not.toHaveBeenCalled()
  expect(button).toHaveAttribute('aria-disabled', 'true')
})
