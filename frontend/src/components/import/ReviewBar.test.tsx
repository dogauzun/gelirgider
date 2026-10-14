import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ReviewBar } from './ReviewBar'

it('holds the counts, the add-row button and the page buttons', () => {
  const onAddRow = vi.fn()
  render(
    <ReviewBar selected={38} leftOut={4} onAddRow={onAddRow}>
      <button type="button">İçe aktar</button>
    </ReviewBar>,
  )
  const bar = screen.getByTestId('preview-actions')

  expect(within(bar).getByText('38 satır seçildi')).toBeInTheDocument()
  expect(within(bar).getByText('4 satır aktarılmayacak')).toBeInTheDocument()
  expect(within(bar).getByRole('button', { name: 'İçe aktar' })).toBeInTheDocument()
  fireEvent.click(within(bar).getByRole('button', { name: 'Satır ekle' }))
  expect(onAddRow).toHaveBeenCalledOnce()
})

it('stays at the bottom of the screen at every width', () => {
  render(<ReviewBar selected={1} leftOut={0} onAddRow={() => {}} />)
  const bar = screen.getByTestId('preview-actions')

  // Sticky in the page column: in view while the rows scroll, and after the
  // last row once the table ends, so it never covers one.
  expect(bar).toHaveClass('sticky', 'bottom-0')
  expect(bar.className).not.toMatch(/max-md:sticky/)
  expect(screen.queryByText(/aktarılmayacak/)).not.toBeInTheDocument()
})

// On a phone the page buttons wrap to the bar's second line and
// stay on the right there, as they are on a wide screen. jsdom has no
// layout, so the classes stand in.
it('keeps the page buttons on the right when they wrap', () => {
  render(
    <ReviewBar selected={1} leftOut={0} onAddRow={() => {}}>
      <button type="button">İçe aktar</button>
    </ReviewBar>,
  )
  const group = screen.getByRole('button', { name: 'İçe aktar' }).parentElement!
  expect(group).toHaveClass('ml-auto', 'justify-end')
})
