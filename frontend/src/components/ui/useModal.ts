import { useEffect, useRef } from 'react'
import { focusIfLost } from '../../lib/useFocusLater'

// For a <dialog> mounted only while open: showModal() on mount, and on
// unmount focus goes back to what had it when the dialog opened. The browser
// restores focus only on close(); an open dialog that is simply removed drops
// focus to <body>.
//
// `fallback` is a selector for where focus goes when that opener is gone: a
// dialog opened by navigation state was opened from a button on the previous
// page, which is detached by now, or from nothing at all.
export function useModal(fallback?: string) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  // Read during the first render, before showModal() moves focus inside.
  const openerRef = useRef(document.activeElement)

  useEffect(() => {
    const dialog = dialogRef.current
    const opener = openerRef.current
    if (dialog && !dialog.open) dialog.showModal()
    return () => {
      // Only when focus was lost with the dialog: never take it back from
      // wherever the closing action already put it.
      const target =
        // eslint-disable-next-line no-restricted-syntax -- puts focus back on the opener itself
        opener instanceof HTMLElement && opener !== document.body && opener.isConnected
          ? opener
          : fallback
            ? document.querySelector<HTMLElement>(fallback)
            : null
      focusIfLost(target)
    }
  }, [fallback])

  return dialogRef
}
