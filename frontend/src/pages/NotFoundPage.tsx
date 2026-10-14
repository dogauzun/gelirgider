import { Link } from 'react-router'
import { NotFound } from '../components/NotFound'
import { SignedOutFrame, signedOutTitle } from '../components/SignedOutFrame'
import { buttonClass } from '../components/ui/buttonClass'
import { t } from '../i18n/strings'
import { useTitle } from '../lib/useTitle'

// A signed-in user's unknown URL, inside the app shell.
export function NotFoundPage() {
  useTitle(t('notFound.title'))
  return (
    <main id="main">
      <NotFound
        heading={t('notFound.message')}
        explanation={t('notFound.explanation')}
        to="/"
        linkLabel={t('portfolio.backToList')}
      />
    </main>
  )
}

// A visitor's unknown URL, in the frame of the other signed-out pages
// (GLG-970). Its title has no full stop like the frame's other titles, and
// its action is the login page, where / would only redirect (GLG-1058).
export function SignedOutNotFoundPage() {
  useTitle(t('notFound.title'))
  return (
    <SignedOutFrame>
      <div className="w-full max-w-[400px] card p-7">
        <h1 className={signedOutTitle}>{t('notFound.title')}</h1>
        <p className="mt-4 text-[13px] text-ink-2">{t('notFound.explanation')}</p>
        <Link to="/login" className={buttonClass({ size: 'lg', className: 'mt-6 w-full' })}>
          {t('auth.login.title')}
        </Link>
      </div>
    </SignedOutFrame>
  )
}
