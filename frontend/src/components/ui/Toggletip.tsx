import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

// A `title` tooltip that touch and keyboard users can reach: the
// content becomes a button that shows `tip` in a bubble on tap, focus or mouse
// hover, and closes on blur, Escape, a tap elsewhere or the mouse leaving.
// The tip is the button's description, so a screen reader reads it with the
// content without opening the bubble. A tap opens rather than toggles: it
// also focuses the button, which already opened it. `align` picks the edge
// the bubble hangs from; it opens above, where a scrolling table has room.
export function Toggletip({
  tip,
  align = 'end',
  className = '',
  children,
}: {
  tip: string
  align?: 'start' | 'end'
  className?: string
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const root = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [open])

  return (
    <span ref={root} className={`relative inline-flex ${className}`}>
      <button
        type="button"
        aria-describedby={id}
        aria-expanded={open}
        onClick={() => setOpen(true)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') setOpen(false)
        }}
        onPointerEnter={(event) => event.pointerType === 'mouse' && setOpen(true)}
        onPointerLeave={(event) => event.pointerType === 'mouse' && setOpen(false)}
        className="cursor-help rounded text-inherit"
      >
        {children}
      </button>
      <span
        id={id}
        hidden={!open}
        className={`absolute bottom-full z-20 mb-1 w-max max-w-[16rem] rounded bg-surface px-2 py-1 text-left text-[12px] font-normal whitespace-normal text-ink shadow-pop ${
          align === 'start' ? 'left-0' : 'right-0'
        }`}
      >
        {tip}
      </span>
    </span>
  )
}
