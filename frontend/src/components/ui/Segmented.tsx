import type { ReactNode } from 'react'
import { radioGroupKeyDown } from '../../lib/radioGroup'
import { segmentedItem, segmentedTrack } from './segmentedClass'

// The UI v2 segmented control (ADR 0023): a sunken well track holding
// role="radio" buttons, the checked one raised. Keyboard handling stays in
// lib/radioGroup.ts, the classes in segmentedClass.ts.

// A radiogroup of `options` on the segmented track: clicks and the arrow
// keys select, only the checked option is in the tab order.
export function Segmented<T>({
  options,
  value,
  onChange,
  label,
  optionKey = String,
  itemClassName = '',
  trackClassName = segmentedTrack,
  className = '',
  disabled,
  ...aria
}: {
  options: readonly T[]
  value: T
  onChange: (option: T) => void
  label: (option: T) => ReactNode
  'aria-label': string
  'aria-describedby'?: string
  optionKey?: (option: T) => string
  itemClassName?: string
  // Replaces the flex track, for a caller that lays the options out itself.
  trackClassName?: string
  className?: string
  disabled?: boolean
}) {
  return (
    <div
      // eslint-disable-next-line no-restricted-syntax -- the one radiogroup, see eslint.config.js
      role="radiogroup"
      className={`${trackClassName} ${className}`}
      onKeyDown={radioGroupKeyDown(options, value, onChange)}
      {...aria}
    >
      {options.map((option) => {
        const checked = option === value
        return (
          <button
            key={optionKey(option)}
            type="button"
            role="radio"
            aria-checked={checked}
            tabIndex={checked ? 0 : -1}
            disabled={disabled}
            onClick={() => onChange(option)}
            className={`${segmentedItem(checked)} ${itemClassName}`}
          >
            {label(option)}
          </button>
        )
      })}
    </div>
  )
}
