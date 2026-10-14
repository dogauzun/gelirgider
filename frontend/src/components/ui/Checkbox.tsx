import { Checkbox as Base } from '@base-ui/react/checkbox'
import { useId } from 'react'
import type { ReactNode } from 'react'
import { Icon } from './Icon'

// The app's checkbox, on Base UI (ADR 0023): a span with role="checkbox" in
// the field style, accent when checked, the global focus ring. With `label`
// the box sits in a <label> that is the whole 44 px tap target; the label
// names the box and `description` describes it. A box in a table row has no
// visible label and takes `aria-label` instead. The unchecked border is
// `faint`, the 3:1 a control's boundary needs (WCAG 1.4.11);
// `line-strong` drew it at 1.5:1.
//
// `pending` is for a save in flight, as on Button: Chromium blurs a focused
// control that becomes disabled, dropping a keyboard user to <body>,
// so the box stays enabled, is announced as unavailable and
// ignores a change meanwhile.
export function Checkbox({
  checked,
  onChange,
  indeterminate = false,
  disabled,
  label,
  description,
  pending,
  'aria-label': ariaLabel,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  indeterminate?: boolean
  disabled?: boolean
  label?: ReactNode
  description?: ReactNode
  pending?: boolean
  'aria-label'?: string
}) {
  const id = useId()
  const box = (
    <Base.Root
      checked={checked}
      indeterminate={indeterminate}
      disabled={disabled}
      onCheckedChange={(next) => {
        if (!pending) onChange(next)
      }}
      aria-label={ariaLabel}
      // The label's text, not the whole <label>, so the description is not
      // read twice. With an aria-label the empty list stops Base UI from
      // naming the box by the <label> around it, so the aria-label wins.
      aria-labelledby={ariaLabel ? '' : label ? `${id}-label` : undefined}
      aria-describedby={description ? `${id}-description` : undefined}
      // eslint-disable-next-line no-restricted-syntax -- the pending prop itself
      aria-disabled={disabled || pending}
      className="mt-0.5 flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-[4px] border border-faint bg-surface text-on-ink data-checked:border-accent data-checked:bg-accent data-disabled:cursor-default data-disabled:opacity-50 data-indeterminate:border-accent data-indeterminate:bg-accent aria-disabled:opacity-50"
    >
      <Base.Indicator className="flex">
        <Icon name={indeterminate ? 'minus' : 'check'} className="size-3.5 stroke-3" />
      </Base.Indicator>
    </Base.Root>
  )
  if (label === undefined) return box
  return (
    <label className="flex min-h-11 min-w-0 cursor-pointer items-center">
      <span className="flex min-w-0 items-start gap-3">
        {box}
        <span className="min-w-0">
          <span id={`${id}-label`} className="block text-[14px] font-medium text-ink">
            {label}
          </span>
          {description && (
            <span id={`${id}-description`} className="block text-[12px] text-muted">
              {description}
            </span>
          )}
        </span>
      </span>
    </label>
  )
}
