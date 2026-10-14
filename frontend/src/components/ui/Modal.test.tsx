import { fireEvent, render, screen } from '@testing-library/react'
import { useRef } from 'react'
import { expect, test, vi } from 'vitest'
import { Modal } from './Modal'

function Nested({ onOuter, onInner }: { onOuter: () => void; onInner: () => void }) {
  const outer = useRef<HTMLDialogElement>(null)
  const inner = useRef<HTMLDialogElement>(null)
  return (
    <Modal ref={outer} aria-label="outer" onClose={onOuter} open>
      <Modal ref={inner} aria-label="inner" onClose={onInner} open />
    </Modal>
  )
}

// React hands a nested dialog's cancel up through every onCancel in the tree:
// Escape in the inner dialog must close that one only.
test('Escape in a nested dialog closes only the inner one', () => {
  const onOuter = vi.fn()
  const onInner = vi.fn()
  render(<Nested onOuter={onOuter} onInner={onInner} />)

  // Escape on a modal <dialog> fires `cancel`; jsdom does not map the key.
  fireEvent(
    screen.getByRole('dialog', { name: 'inner' }),
    new Event('cancel', { cancelable: true }),
  )
  expect(onInner).toHaveBeenCalledOnce()
  expect(onOuter).not.toHaveBeenCalled()

  fireEvent(
    screen.getByRole('dialog', { name: 'outer' }),
    new Event('cancel', { cancelable: true }),
  )
  expect(onOuter).toHaveBeenCalledOnce()
})
