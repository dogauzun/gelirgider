import { fireEvent, screen, waitFor } from '@testing-library/react'
import { expect } from 'vitest'

// For every test that used to change a native <select>: opens the Select
// whose trigger this is and picks the option with that visible name, then
// waits for the list to close. Closed means hidden, not gone: from the second
// opening on, Base UI may keep the closed list mounted.
//
// Base UI commits a mouse click on an item only after a pointerdown on that
// item (a bare click is taken for a stray mouseup from the opening press),
// so the option gets the pointerdown a real click brings.
export async function chooseOption(trigger: HTMLElement, optionName: string): Promise<void> {
  fireEvent.click(trigger)
  await screen.findByRole('listbox')
  const option = await screen.findByRole('option', { name: optionName })
  fireEvent.pointerDown(option, { pointerType: 'mouse' })
  fireEvent.click(option)
  await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull())
}
