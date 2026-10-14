import { useState, type FormEvent, type ReactNode } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { useSetupAdmin, useSetupStatus } from '../api/auth'
import { PasswordField } from '../components/PasswordField'
import { SignedOutFrame, signedOutTitle } from '../components/SignedOutFrame'
import { Button } from '../components/ui/Button'
import { Field } from '../components/ui/Field'
import { inputClass } from '../components/ui/inputClass'
import { t } from '../i18n/strings'
import { useTitle } from '../lib/useTitle'

// While no admin exists every route goes to /setup. A failed status request
// lets the routes render as before: the server still refuses setup-only
// actions on its own.
export function SetupGate({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  const { data, isLoading, failureCount } = useSetupStatus()

  if (isLoading) {
    return failureCount > 0 ? (
      <p role="alert" className="px-6 py-12 text-center text-sm text-muted">
        {t('health.error')}
      </p>
    ) : null
  }
  if (data?.needs_setup && pathname !== '/setup') {
    return <Navigate to="/setup" replace />
  }
  return children
}

// Step 1 of the setup wizard (ADR 0010): the token from the server log plus
// the first admin's email and password. Same frame as AuthForm.
export function SetupPage() {
  useTitle(t('setup.title'))
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [passwordAgain, setPasswordAgain] = useState('')
  // Set on submit, cleared on any edit of either password field: the native
  // minLength check can block the next submit, and a stale mismatch error
  // would sit next to it (as on the Account page).
  const [mismatch, setMismatch] = useState(false)
  const { data: status } = useSetupStatus()

  const mutation = useSetupAdmin({ token: token.trim(), email, password }, () =>
    navigate('/setup/configure', { replace: true }),
  )

  // Once an admin exists /setup goes home. Only while idle: our own success
  // refetches the status before navigating on, and must not be cut short.
  if (status && !status.needs_setup && mutation.isIdle) {
    return <Navigate to="/" replace />
  }

  // A server error is about the values sent: any edit drops it.
  // Only an error: a reset while pending would make the mutation idle and
  // let the redirect above cut our own success short.
  function clearError() {
    if (mutation.isError) mutation.reset()
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // No self-service reset exists, so never send a password typed only once.
    if (password !== passwordAgain) {
      setMismatch(true)
      mutation.reset()
      return
    }
    setMismatch(false)
    mutation.mutate()
  }

  return (
    <SignedOutFrame>
      <form onSubmit={handleSubmit} className="flex w-full max-w-[400px] flex-col gap-4 card p-7">
        <div>
          <p className="text-[13px] text-muted">{t('setup.step', { step: '1' })}</p>
          <h1 className={signedOutTitle}>{t('setup.title')}</h1>
        </div>
        <Field label={t('setup.token')} hint={t('setup.tokenHint')}>
          <input
            value={token}
            onChange={(event) => {
              setToken(event.target.value)
              clearError()
            }}
            autoComplete="off"
            className={inputClass({ size: 'lg' })}
          />
        </Field>
        <Field label={t('auth.email')} hint={t('setup.existingHint')}>
          <input
            type="email"
            name="email"
            autoComplete="username"
            required
            maxLength={254}
            value={email}
            onChange={(event) => {
              setEmail(event.target.value)
              clearError()
            }}
            className={inputClass({ size: 'lg' })}
          />
        </Field>
        {/* current-password, not new-password: an existing account is
            promoted only when this matches its stored password, so the
            browser must offer the saved one, not generate a new one. */}
        <PasswordField
          label={t('auth.password')}
          hint={t('auth.passwordHint')}
          autoComplete="current-password"
          minLength={8}
          value={password}
          onChange={(value) => {
            setPassword(value)
            setMismatch(false)
            clearError()
          }}
        />
        <Field
          label={t('setup.passwordAgain')}
          error={mismatch ? t('setup.passwordMismatch') : undefined}
        >
          <input
            type="password"
            name="password-again"
            autoComplete="current-password"
            required
            value={passwordAgain}
            onChange={(event) => {
              setPasswordAgain(event.target.value)
              setMismatch(false)
              clearError()
            }}
            className={inputClass({ size: 'lg' })}
          />
        </Field>
        {mutation.isError && (
          <p role="alert" className="text-[13px] text-loss" data-testid="auth-error">
            {mutation.error.message}
          </p>
        )}
        <Button type="submit" size="lg" pending={mutation.isPending} className="mt-2 w-full">
          {t('setup.submit')}
        </Button>
      </form>
    </SignedOutFrame>
  )
}
