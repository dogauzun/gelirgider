import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router'
import { ApiError } from '../api'
import {
  isTotpChallenge,
  loginRedirectPath,
  useAuthConfig,
  useLogin,
  useLoginTotp,
} from '../api/auth'
import { AuthForm } from '../components/AuthForm'
import { SignedOutFrame, signedOutTitle } from '../components/SignedOutFrame'
import { Button } from '../components/ui/Button'
import { Field } from '../components/ui/Field'
import { inputClass } from '../components/ui/inputClass'
import { t } from '../i18n/strings'
import { useNavState, type NavState } from '../lib/navState'
import { useOneAtATime } from '../lib/useOneAtATime'
import { useTitle } from '../lib/useTitle'

// Not capped in length: a recovery code goes in the same field.
function CodeForm({
  isPending,
  error,
  onSubmit,
  onEdit,
}: {
  isPending: boolean
  error: string | null
  onSubmit: (code: string) => Promise<unknown>
  onEdit: () => void
}) {
  const [code, setCode] = useState('')
  const once = useOneAtATime()

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    once(() => onSubmit(code.trim()))
  }

  return (
    <SignedOutFrame>
      <form onSubmit={handleSubmit} className="w-full max-w-[400px] card p-7">
        <h1 className={signedOutTitle}>{t('auth.login.codeTitle')}</h1>
        <div className="mt-5">
          <Field
            label={t('auth.login.code')}
            hint={t('auth.login.codeHint')}
            error={error ?? undefined}
          >
            <input
              type="text"
              name="code"
              inputMode="numeric"
              autoComplete="one-time-code"
              required
              autoFocus
              value={code}
              onChange={(event) => {
                setCode(event.target.value)
                onEdit()
              }}
              className={inputClass({ size: 'lg' })}
            />
          </Field>
        </div>
        <Button type="submit" size="lg" pending={isPending} className="mt-6 w-full">
          {t('auth.login.codeSubmit')}
        </Button>
      </form>
    </SignedOutFrame>
  )
}

export function LoginPage() {
  useTitle(t('auth.login.title'))
  const navigate = useNavigate()
  const { state } = useNavState()
  // RegisterPage passes an i18n key here when it could not log the user in.
  const notice = state?.notice
  // The deep link ProtectedRoute sent the visitor away from. It rides along to
  // the register page and back so either way ends there.
  const from = state?.from
  const registrationClosed = useAuthConfig().data?.registration_open === false

  const done = () => navigate(loginRedirectPath(state), { replace: true })
  const mutation = useLogin(done)
  const totp = useLoginTotp(done)

  // An expired challenge sends the user back to the password form.
  const challenge = isTotpChallenge(mutation.data) ? mutation.data : null
  const expired = totp.error instanceof ApiError && totp.error.body.error === 'login expired'
  if (challenge && !expired) {
    return (
      <CodeForm
        isPending={totp.isPending}
        error={totp.error?.message ?? null}
        onSubmit={(code) => totp.mutateAsync({ token: challenge.token, code })}
        onEdit={() => totp.isError && totp.reset()}
      />
    )
  }
  const error = totp.error?.message ?? mutation.error?.message ?? null

  return (
    <AuthForm
      title={t('auth.login.title')}
      submitLabel={t('auth.login.submit')}
      isPending={mutation.isPending}
      error={error}
      notice={notice && !error ? t(notice) : null}
      onSubmit={(email, password) => {
        totp.reset()
        return mutation.mutateAsync({ email, password })
      }}
      onEdit={() => {
        if (!error) return
        mutation.reset()
        totp.reset()
      }}
      footer={
        <>
          {!registrationClosed && (
            <Link to="/register" state={{ from } satisfies NavState} className="text-accent">
              {t('auth.login.noAccount')}
            </Link>
          )}
          <span className="mt-2 block text-[12px] text-muted">
            {t('auth.login.forgotPassword')}
          </span>
        </>
      }
    />
  )
}
