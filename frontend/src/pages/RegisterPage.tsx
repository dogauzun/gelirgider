import { Link, useNavigate } from 'react-router'
import { loginRedirectPath, useAuthConfig, useRegister } from '../api/auth'
import { AuthForm } from '../components/AuthForm'
import { SignedOutFrame, signedOutTitle } from '../components/SignedOutFrame'
import { t } from '../i18n/strings'
import { useNavState, type NavState } from '../lib/navState'
import { useTitle } from '../lib/useTitle'

export function RegisterPage() {
  useTitle(t('auth.register.title'))
  const navigate = useNavigate()
  const { state } = useNavState()
  // Forwarded from LoginPage: the deep link to land on afterwards (GLG-400).
  const from = state?.from
  const registrationClosed = useAuthConfig().data?.registration_open === false

  // The account exists even when the login after it fails: send them to the
  // login page with a notice then. Logged in, the same target as GuestRoute,
  // which redirects as soon as me refetches.
  const mutation = useRegister((loggedIn) =>
    loggedIn
      ? navigate(loginRedirectPath(state), { replace: true })
      : navigate('/login', {
          replace: true,
          state: { notice: 'auth.register.done', from } satisfies NavState,
        }),
  )

  if (registrationClosed) {
    return (
      <SignedOutFrame>
        <div className="w-full max-w-[400px] card p-7">
          <h1 className={signedOutTitle}>{t('auth.register.title')}</h1>
          <p className="mt-4 text-[13px] text-ink-2">{t('auth.register.closed')}</p>
          <p className="mt-4 text-[13px]">
            <Link to="/login" state={{ from } satisfies NavState} className="text-accent">
              {t('auth.register.hasAccount')}
            </Link>
          </p>
        </div>
      </SignedOutFrame>
    )
  }

  return (
    <AuthForm
      title={t('auth.register.title')}
      submitLabel={t('auth.register.submit')}
      passwordHint={t('auth.passwordHint')}
      emailMaxLength={254}
      newPassword
      isPending={mutation.isPending}
      error={mutation.error?.message ?? null}
      onSubmit={(email, password) => mutation.mutateAsync({ email, password })}
      onEdit={() => mutation.isError && mutation.reset()}
      footer={
        <Link to="/login" state={{ from } satisfies NavState} className="text-accent">
          {t('auth.register.hasAccount')}
        </Link>
      }
    />
  )
}
