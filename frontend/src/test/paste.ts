import { fireEvent } from '@testing-library/react'

// Pastes like a browser: a maxLength on the input cuts the value, which
// fireEvent.change alone never does.
export function paste(input: HTMLElement, value: string): void {
  const max = (input as HTMLInputElement).maxLength
  fireEvent.change(input, { target: { value: max >= 0 ? value.slice(0, max) : value } })
}
