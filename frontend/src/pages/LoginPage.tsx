import { Link, useNavigate } from 'react-router'
import { loginRedirectPath, useAuthConfig, useLogin } from '../api/auth'
import { AuthForm } from '../components/AuthForm'
import { t } from '../i18n/strings'
import { useNavState, type NavState } from '../lib/navState'
import { useTitle } from '../lib/useTitle'

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

  const mutation = useLogin(() => navigate(loginRedirectPath(state), { replace: true }))

  return (
    <AuthForm
      title={t('auth.login.title')}
      submitLabel={t('auth.login.submit')}
      isPending={mutation.isPending}
      error={mutation.error?.message ?? null}
      notice={notice && !mutation.error ? t(notice) : null}
      onSubmit={(email, password) => mutation.mutateAsync({ email, password })}
      onEdit={() => mutation.isError && mutation.reset()}
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
