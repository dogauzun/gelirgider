import type { KeyboardEvent } from 'react'

const steps: Partial<Record<string, number>> = {
  ArrowRight: 1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowUp: -1,
}

// The onKeyDown of a WAI-ARIA radio group whose role="radio" children are
// `options` in order, and where only the checked one has tabIndex 0: the arrow
// keys move and select, wrapping at the ends (GLG-428, GLG-453).
export function radioGroupKeyDown<T>(options: readonly T[], value: T, select: (option: T) => void) {
  return (event: KeyboardEvent<HTMLElement>) => {
    const step = steps[event.key]
    if (!step) return
    event.preventDefault()
    const next = (options.indexOf(value) + step + options.length) % options.length
    select(options[next])
    event.currentTarget.querySelectorAll<HTMLElement>('[role="radio"]')[next].focus()
  }
}
