import type { ComponentProps, RefObject } from 'react'

type ModalProps = Omit<
  ComponentProps<'dialog'>,
  'ref' | 'onCancel' | 'onClick' | 'onClose' | 'className'
> & {
  // From useModal for a dialog mounted only while open; ConfirmDialog, which
  // stays mounted, drives showModal() and close() itself.
  ref: RefObject<HTMLDialogElement | null>
  onClose: () => void
}

// The one native modal <dialog> shell. It fills the viewport and is
// transparent, so the caller's panel inside sets the look.
export function Modal({ ref, onClose, children, ...props }: ModalProps) {
  return (
    // eslint-disable-next-line no-restricted-syntax
    <dialog
      {...props}
      ref={ref}
      // Escape fires `cancel`; let the owner drive the open state instead of
      // the element closing itself behind React's back. React hands a nested
      // dialog's cancel up the tree too: that one closes only itself.
      onCancel={(event) => {
        if (event.target !== event.currentTarget) return
        event.preventDefault()
        onClose()
      }}
      // A click that lands on the dialog and not on the panel is a backdrop
      // click.
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
      className="backdrop:bg-scrim m-0 h-full max-h-none w-full max-w-none bg-transparent p-0"
    >
      {children}
    </dialog>
  )
}
