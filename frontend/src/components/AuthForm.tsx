import { useState, type FormEvent, type ReactNode } from 'react'
import { t } from '../i18n/strings'
import { useOneAtATime } from '../lib/useOneAtATime'
import { SignedOutFrame, signedOutTitle } from './SignedOutFrame'
import { PasswordField } from './PasswordField'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'

interface AuthFormProps {
  title: string
  submitLabel: string
  isPending: boolean
  error: string | null
  notice?: string | null
  passwordHint?: string
  // Register only: login must keep accepting addresses stored before the cap.
  emailMaxLength?: number
  // Register: the browser offers to generate and save a new password, and the
  // 8-character minimum is checked before submit. Login takes any length and
  // lets the server answer, so no untranslated browser tooltip shows there.
  newPassword?: boolean
  // The mutation's mutateAsync: a second submit while it runs is ignored.
  onSubmit: (email: string, password: string) => Promise<unknown>
  // Called on every edit, so the page can drop a server error about the
  // values it sent (GLG-1075).
  onEdit: () => void
  footer: ReactNode
}

// Shared shell for the login and register pages: same fields, same layout,
// different title/submit label/mutation.
export function AuthForm({
  title,
  submitLabel,
  isPending,
  error,
  notice = null,
  passwordHint,
  emailMaxLength,
  newPassword = false,
  onSubmit,
  onEdit,
  footer,
}: AuthFormProps) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const once = useOneAtATime()

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    once(() => onSubmit(email, password))
  }

  return (
    <SignedOutFrame>
      <form onSubmit={handleSubmit} className="w-full max-w-[400px] card p-7">
        <h1 className={signedOutTitle}>{title}</h1>
        <div className="mt-5">
          <Field label={t('auth.email')}>
            <input
              type="email"
              name="email"
              autoComplete="username"
              required
              maxLength={emailMaxLength}
              value={email}
              onChange={(event) => {
                setEmail(event.target.value)
                onEdit()
              }}
              className={inputClass({ size: 'lg' })}
            />
          </Field>
        </div>
        <PasswordField
          label={t('auth.password')}
          hint={passwordHint}
          value={password}
          onChange={(value) => {
            setPassword(value)
            onEdit()
          }}
          autoComplete={newPassword ? 'new-password' : 'current-password'}
          minLength={newPassword ? 8 : undefined}
          className="mt-4"
        />
        {notice && (
          // Neutral, not text-gain: it may be bad news like an expired
          // session (GLG-1075).
          <p role="status" className="mt-4 text-[13px] text-ink-2" data-testid="auth-notice">
            {notice}
          </p>
        )}
        {error && (
          <p role="alert" className="mt-4 text-[13px] text-loss" data-testid="auth-error">
            {error}
          </p>
        )}
        <Button type="submit" size="lg" pending={isPending} className="mt-6 w-full">
          {submitLabel}
        </Button>
        <p className="mt-4 text-[13px] text-ink-2">{footer}</p>
      </form>
    </SignedOutFrame>
  )
}
