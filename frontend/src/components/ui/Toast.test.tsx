import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { flushToasts, toast } from '../../lib/toast'
import { Toaster } from './Toast'

afterEach(async () => {
  await flushToasts()
  vi.useRealTimers()
})

// An undo toast stays about 8 s, holds while hovered, and runs its
// commit only when it goes without the undo being pressed.
test('keeps an undo toast while hovered and commits it on timeout', () => {
  vi.useFakeTimers()
  const commit = vi.fn()
  render(<Toaster />)
  // The live region is there before any toast arrives.
  const region = screen.getByRole('status')

  act(() => toast('Deleted', { undo: vi.fn(), commit }))
  act(() => {
    vi.advanceTimersByTime(5000)
  })
  expect(region).toHaveTextContent('Deleted')

  fireEvent.pointerEnter(screen.getByText('Deleted').parentElement!)
  act(() => {
    vi.advanceTimersByTime(10000)
  })
  expect(region).toHaveTextContent('Deleted')
  expect(commit).not.toHaveBeenCalled()

  fireEvent.pointerLeave(screen.getByText('Deleted').parentElement!)
  act(() => {
    vi.advanceTimersByTime(3100)
  })
  expect(region).not.toHaveTextContent('Deleted')
  expect(commit).toHaveBeenCalledOnce()
})

// From lg the toasts centre on the content column, right of the
// 232 px sidebar; below md they sit above the floating add button.
test('places the toasts in the content column, above the floating button', () => {
  render(<Toaster />)
  expect(screen.getByRole('status').parentElement).toHaveClass(
    'lg:left-[232px]',
    'max-md:bottom-24',
  )
})

test('drops the commit when undo is pressed', () => {
  const undo = vi.fn()
  const commit = vi.fn()
  render(<Toaster />)

  act(() => toast('Deleted', { undo, commit }))
  fireEvent.click(screen.getByRole('button', { name: 'Geri al' }))
  void flushToasts()

  expect(undo).toHaveBeenCalledOnce()
  expect(commit).not.toHaveBeenCalled()
})

// An error toast stays until closed, is announced assertively, and
// arriving does not push an undo toast out and commit it early.
test('keeps an error toast in the alert region until closed', () => {
  vi.useFakeTimers()
  const commit = vi.fn()
  render(<Toaster />)

  act(() => toast('Deleted', { undo: vi.fn(), commit }))
  act(() => toast('Could not delete', { error: true }))
  act(() => toast('Saved'))
  act(() => toast('Copied'))
  expect(commit).not.toHaveBeenCalled()
  act(() => {
    vi.advanceTimersByTime(5000)
  })
  expect(screen.getByRole('alert')).toHaveTextContent('Could not delete')
  expect(screen.getByRole('status')).not.toHaveTextContent('Could not delete')

  fireEvent.click(screen.getByRole('button', { name: 'Kapat' }))
  expect(screen.getByRole('alert')).toBeEmptyDOMElement()
})

// A commit that fails during logout's flush adds an error toast,
// which must not survive into the next session.
test('flushes an error toast added by a failing commit', async () => {
  render(<Toaster />)
  const commit = () =>
    Promise.reject(new Error('500')).catch(() => toast('Failed', { error: true }))

  act(() => toast('Deleted', { undo: vi.fn(), commit }))
  await act(() => flushToasts())

  expect(screen.getByRole('alert')).toBeEmptyDOMElement()
})
