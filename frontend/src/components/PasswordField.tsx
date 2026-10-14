import { useId, useState } from 'react'
import { t } from '../i18n/strings'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'
import { Icon } from './ui/Icon'

interface PasswordFieldProps {
  label: string
  hint?: string
  value: string
  onChange: (value: string) => void
  autoComplete: 'current-password' | 'new-password'
  minLength?: number
  className?: string
}

// The password input with its show/hide toggle, shared by the signed-out
// forms (login, register, setup step 1).
export function PasswordField({
  label,
  hint,
  value,
  onChange,
  autoComplete,
  minLength,
  className,
}: PasswordFieldProps) {
  const [revealed, setRevealed] = useState(false)
  const hintId = useId()

  return (
    <div className={className}>
      {/* The toggle sits over the input, so this Field stays message-free: a
          hint or an error under the input would move the input up. The hint
          goes below the wrapper instead, linked by hand. */}
      <div className="relative">
        <Field label={label}>
          <input
            type={revealed ? 'text' : 'password'}
            name="password"
            autoComplete={autoComplete}
            required
            minLength={minLength}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            aria-describedby={hint ? hintId : undefined}
            className={inputClass({ size: 'lg', trailingIcon: true })}
          />
        </Field>
        <Button
          variant="ghost"
          iconOnly
          aria-label={revealed ? t('auth.hidePassword') : t('auth.showPassword')}
          aria-pressed={revealed}
          onClick={() => setRevealed(!revealed)}
          className="absolute right-1 bottom-1 text-muted"
        >
          <Icon name={revealed ? 'eyeOff' : 'eye'} />
        </Button>
      </div>
      {hint && (
        <p id={hintId} className="mt-1 text-[12px] text-muted">
          {hint}
        </p>
      )}
    </div>
  )
}
