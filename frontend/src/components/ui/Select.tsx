import { Select as Base } from '@base-ui/react/select'
import { Fragment, useRef } from 'react'
import { inputClass } from './inputClass'
import { Icon } from './Icon'
import { menuItemClass } from './Menu'

export type SelectOption<T extends string> = {
  value: T
  label: string
  disabled?: boolean
  group?: string // options sharing a group list under its label, as an optgroup
}

// Drop-in for a controlled native select element, on Base UI (ADR 0023). `id`,
// `aria-invalid` and `aria-describedby` go to the trigger, so it works as the
// child of <Field>, whose <label for> names it.
export function Select<T extends string>({
  value,
  onChange,
  options,
  id,
  'aria-label': ariaLabel,
  'aria-invalid': ariaInvalid,
  'aria-describedby': ariaDescribedBy,
  disabled,
  prefix,
  className = '',
}: {
  value: T
  onChange: (value: T) => void
  options: readonly SelectOption<T>[]
  id?: string
  'aria-label'?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
  disabled?: boolean
  // Muted text before the value on the trigger, such as "Kaynak:", for a
  // select that has no visible label of its own.
  prefix?: string
  // Layout classes on the trigger, such as flex-1 or max-w-xs, as a plain
  // string: lint blocks one that clashes with inputClass.
  className?: string
}) {
  // A modal <dialog> is in the top layer: a popup portaled to <body> would
  // be under it and inert, so it goes into the trigger's dialog instead.
  // Read when the popup mounts; no dialog means Base UI's default, <body>.
  const dialogRef = useRef<HTMLDialogElement | null>(null)
  return (
    <Base.Root
      value={value}
      // Never null: the value is always one of the options, none clears it.
      // Base UI also reports picking the current item again; a native select
      // fires no change for that, and callers that save on change rely on it.
      onValueChange={(next) => next !== null && next !== value && onChange(next)}
      items={options}
      disabled={disabled}
    >
      <Base.Trigger
        ref={(el) => {
          dialogRef.current = el?.closest('dialog') ?? null
        }}
        id={id}
        aria-label={ariaLabel}
        aria-invalid={ariaInvalid}
        aria-describedby={ariaDescribedBy}
        className={`${inputClass()} flex items-center justify-between gap-2 text-left ${className}`}
      >
        <span className="flex min-w-0 gap-1">
          {prefix && <span className="shrink-0 text-muted">{prefix}</span>}
          <Base.Value className="truncate" />
        </span>
        <Base.Icon className="shrink-0 text-muted">
          <Icon name="chevronDown" />
        </Base.Icon>
      </Base.Trigger>
      <Base.Portal container={dialogRef}>
        {/* Above the sticky app header (z-20), as Menu. */}
        <Base.Positioner sideOffset={4} alignItemWithTrigger={false} className="z-30">
          <Base.Popup className="max-h-[var(--available-height)] min-w-[var(--anchor-width)] overflow-y-auto rounded-card bg-surface p-1 shadow-pop outline-none">
            <Base.List>
              {[...new Set(options.map((o) => o.group))].map((group) => {
                const items = options
                  .filter((o) => o.group === group)
                  .map((option) => (
                    <Base.Item
                      key={option.value}
                      value={option.value}
                      disabled={option.disabled}
                      className={`${menuItemClass} justify-between text-ink`}
                    >
                      <Base.ItemText>{option.label}</Base.ItemText>
                      <Base.ItemIndicator className="text-accent">
                        <Icon name="check" />
                      </Base.ItemIndicator>
                    </Base.Item>
                  ))
                return group === undefined ? (
                  <Fragment key="">{items}</Fragment>
                ) : (
                  <Base.Group key={group}>
                    <Base.GroupLabel className="px-2 pt-2 pb-1 text-[12px] font-medium text-muted">
                      {group}
                    </Base.GroupLabel>
                    {items}
                  </Base.Group>
                )
              })}
            </Base.List>
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  )
}

// A Select as wide as its longest label, so no label is cut in either
// language and a pick does not change its width. A `ch` is the width of a
// digit, wider than most letters; 3rem is the trigger's padding and chevron.
export function FitSelect<T extends string>(props: {
  'aria-label': string
  value: T
  onChange: (value: T) => void
  options: SelectOption<T>[]
}) {
  const longest = Math.max(...props.options.map((o) => o.label.length))
  return (
    <span
      className="inline-block min-w-0 text-[12px] max-sm:flex-1"
      style={{ width: `calc(${longest}ch + 3rem)` }}
    >
      <Select {...props} className="text-[12px]" />
    </span>
  )
}
