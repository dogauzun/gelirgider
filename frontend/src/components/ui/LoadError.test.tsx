import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { LoadError } from './LoadError'

// Chromium blurs a focused button the moment it is disabled, so the
// Retry button is pending while the refetch runs; a second click is ignored.
it('keeps Retry focused and pending while the refetch runs', () => {
  const onRetry = vi.fn()
  const { rerender } = render(
    <LoadError title="Grafik yüklenemedi" error={null} onRetry={onRetry} />,
  )
  const retry = screen.getByRole('button', { name: 'Tekrar dene' })
  retry.focus()
  rerender(<LoadError title="Grafik yüklenemedi" error={null} onRetry={onRetry} retrying />)

  expect(retry).toHaveAttribute('aria-disabled', 'true')
  expect(retry).toBeEnabled()
  expect(retry).toHaveFocus()
  fireEvent.click(retry)
  expect(onRetry).not.toHaveBeenCalled()
})
