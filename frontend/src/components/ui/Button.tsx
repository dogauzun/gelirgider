import { useEffect, useRef, type ComponentProps } from 'react'
import { focusIfLost } from '../../lib/useFocusLater'
import { buttonClass, type sizes, type variants } from './buttonClass'

type ButtonProps = ComponentProps<'button'> & {
  variant?: keyof typeof variants
  size?: keyof typeof sizes
  // A request this button started is running. The button stays enabled, so
  // it keeps focus and its place in the tab order, but is announced as
  // unavailable and ignores clicks (GLG-920).
  pending?: boolean
} & ({ iconOnly: true; 'aria-label': string } | { iconOnly?: false })

export function Button({
  variant = 'primary',
  size = 'md',
  iconOnly = false,
  type = 'button',
  className = '',
  disabled,
  pending = false,
  onClick,
  onFocus,
  ...props
}: ButtonProps) {
  // A focused button that turns disabled makes the browser
  // drop focus to <body>, and it does not come back when the button is enabled
  // again. So a failed request left focus outside the form or dialog. Put it
  // back here, unless something else took focus in the meantime (GLG-483).
  const lostFocus = useRef<HTMLButtonElement | null>(null)
  useEffect(() => {
    const button = lostFocus.current
    if (disabled || !button) return
    lostFocus.current = null
    focusIfLost(button)
  }, [disabled])

  return (
    <button
      type={type}
      disabled={disabled}
      // eslint-disable-next-line no-restricted-syntax -- the pending prop itself
      aria-disabled={pending || undefined}
      onClick={(event) => {
        // Also stops a submit button, and Enter in its form, from submitting.
        if (pending) event.preventDefault()
        else onClick?.(event)
      }}
      onFocus={(event) => {
        // A native listener: that blur fires while React commits the disabled
        // attribute, and React does not dispatch onBlur during a commit.
        const button = event.currentTarget
        const onBlur = () => {
          if (button.disabled) lostFocus.current = button
        }
        button.addEventListener('blur', onBlur, { once: true })
        onFocus?.(event)
      }}
      // A fixed height holds one line, so a label that wrapped at phone width
      // spilled past the border (GLG-837).
      className={buttonClass({ variant, size, iconOnly, className })}
      {...props}
    />
  )
}
