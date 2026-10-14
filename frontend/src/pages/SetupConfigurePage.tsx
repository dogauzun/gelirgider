import { useNavigate } from 'react-router'
import { useMe } from '../api/auth'
import { AppShell } from '../components/AppShell'
import { ProvidersSection } from '../components/admin/ProvidersSection'
import { RegistrationToggle } from '../components/admin/RegistrationToggle'
import { SourcesSection } from '../components/admin/SourcesSection'
import { SignedOutFrame, signedOutTitle } from '../components/SignedOutFrame'
import { Button } from '../components/ui/Button'
import { t } from '../i18n/strings'
import { bottomBar } from '../lib/bottomBar'
import { useTitle } from '../lib/useTitle'
import { NotFoundPage } from './NotFoundPage'

// Step 2 (ADR 0010), optional. The sections save every change themselves, so
// Finish and Skip both just go home.
export function SetupConfigurePage() {
  const navigate = useNavigate()
  const { data: me } = useMe()
  useTitle(me?.is_admin ? t('setup.title') : t('notFound.title'))

  // A non-admin gets the not-found page of any unknown URL, in the shell
  // with its link home.
  if (!me?.is_admin) {
    return (
      <AppShell>
        <NotFoundPage />
      </AppShell>
    )
  }

  const home = () => navigate('/', { replace: true })

  // The page is long, so Skip and Finish stay in view in a bar fixed to the
  // bottom; the page's bottom padding is the bar's height (h-16), so the bar
  // never covers the language switch at the end, and keyboard focus
  // never scrolls a control under it.
  return (
    <div className="pb-16">
      <SignedOutFrame>
        <div className="flex w-full max-w-4xl flex-col gap-4">
          <div>
            <p className="text-[13px] text-muted">{t('setup.step', { step: '2' })}</p>
            <h1 className={signedOutTitle}>{t('setup.title')}</h1>
            <p className="mt-1 text-[13px] text-ink-2">{t('setup.configure.intro')}</p>
          </div>
          <ProvidersSection />
          <SourcesSection />
          <section className="card p-5">
            <RegistrationToggle />
          </section>
        </div>
        <div
          ref={bottomBar}
          className="fixed inset-x-0 bottom-0 z-10 h-16 border-t border-line bg-surface px-4"
        >
          <div className="mx-auto flex h-full max-w-4xl items-center justify-end gap-2">
            <Button variant="ghost" onClick={home}>
              {t('setup.configure.skip')}
            </Button>
            <Button onClick={home}>{t('setup.configure.finish')}</Button>
          </div>
        </div>
      </SignedOutFrame>
    </div>
  )
}
