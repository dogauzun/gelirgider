import type { ReactNode } from 'react'
import { t } from '../i18n/strings'
import { LanguageSwitch } from './LanguageSwitch'
import { Mark } from './Mark'

// The one title size of the signed-out pages' cards (GLG-970).
export const signedOutTitle = 'text-[22px] font-semibold text-ink'

// The frame of the pages without the app shell: login, register, both setup
// steps and a visitor's unknown URL. The brand above, the language switch
// below, the page's card in between.
export function SignedOutFrame({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 bg-paper px-4 py-10">
      <div className="text-center">
        <div className="flex items-center justify-center gap-2">
          <Mark />
          <span className="text-[15px] font-semibold text-ink">{t('app.name')}</span>
        </div>
        <p className="mt-2 text-[13px] text-muted">{t('auth.tagline')}</p>
      </div>
      {children}
      <LanguageSwitch />
    </main>
  )
}
