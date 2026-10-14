import { Menu as Base } from '@base-ui/react/menu'
import { useRef, type ReactElement, type ReactNode } from 'react'
import { Link } from 'react-router'

// A dropdown menu on Base UI (ADR 0023): it owns the open state, focus,
// arrow keys, Escape and the outside click. The popup is portaled to <body>,
// so a click in it never reaches a card-wide link around the trigger; inside
// a modal <dialog> (the account menu in the drawer, GLG-876) it goes into
// that dialog instead, as in Select, or it would sit under it, inert.

// Also the item look of Select.
export const menuItemClass =
  'flex w-full cursor-default items-center gap-2 rounded-ctl px-2 py-1.5 text-[13px] outline-none data-highlighted:bg-paper data-disabled:opacity-50'

// `trigger` is the caller's own <Button aria-label=...>; Base UI renders it
// as the trigger, adding its props and ref to the button's own.
export function Menu({
  trigger,
  align = 'end',
  children,
}: {
  trigger: ReactElement
  align?: 'start' | 'end'
  children: ReactNode
}) {
  const triggerRef = useRef<HTMLButtonElement | null>(null)
  const dialogRef = useRef<HTMLDialogElement | null>(null)
  return (
    <Base.Root
      // Base UI focuses a pressed item after the item's own handler, and only
      // hands focus back to the trigger once the popup is gone. An item that
      // opens a confirm dialog would leave the dialog's opener on that removed
      // item, so its close() drops focus to <body>. Focus goes back as soon as
      // a press closes the menu instead, after Base UI's handlers; a rename
      // input that mounts with autoFocus still takes it from there (GLG-441).
      onOpenChange={(open, details) => {
        if (!open && details.reason === 'item-press') triggerRef.current?.focus()
      }}
    >
      <Base.Trigger
        ref={(el: HTMLButtonElement | null) => {
          triggerRef.current = el
          dialogRef.current = el?.closest('dialog') ?? null
        }}
        render={trigger}
      />
      <Base.Portal container={dialogRef}>
        {/* Above the sticky top bar (z-20) and the tab row under it. */}
        <Base.Positioner sideOffset={4} align={align} className="z-30">
          <Base.Popup className="min-w-44 rounded-card bg-surface p-1 shadow-pop outline-none">
            {children}
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  )
}

export function MenuItem({
  onSelect,
  tone = 'default',
  disabled,
  children,
}: {
  onSelect: () => void
  tone?: 'default' | 'danger'
  disabled?: boolean
  children: ReactNode
}) {
  return (
    <Base.Item
      onClick={onSelect}
      disabled={disabled}
      className={`${menuItemClass} ${tone === 'danger' ? 'text-loss' : 'text-ink'}`}
    >
      {children}
    </Base.Item>
  )
}

// A navigation item: Base UI's LinkItem rendering a react-router <Link>.
// closeOnClick: a client-side navigation keeps the page, and LinkItem
// leaves the menu open by default.
export function MenuLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Base.LinkItem closeOnClick render={<Link to={to} />} className={`${menuItemClass} text-ink`}>
      {children}
    </Base.LinkItem>
  )
}
