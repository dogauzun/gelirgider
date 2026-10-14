import { useState, type FormEvent, type ReactNode } from 'react'
import { ApiError } from '../api'
import { useChangePassword, useMe, useUpdateReportingCurrency } from '../api/auth'
import { PageHeader } from '../components/PageHeader'
import { Button } from '../components/ui/Button'
import { Field } from '../components/ui/Field'
import { inputClass } from '../components/ui/inputClass'
import { Select } from '../components/ui/Select'
import { Segmented } from '../components/ui/Segmented'
import { segmentedItem, segmentedTrack } from '../components/ui/segmentedClass'
import { locales, setLocale, t, useLocale } from '../i18n/strings'
import { useTitle } from '../lib/useTitle'
import { setTheme, themes, useTheme } from '../theme'
import { toast } from '../lib/toast'

const reportingCurrencies = ['TRY', 'USD', 'EUR'] as const

function Card({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
  return (
    <section className="card p-5">
      <h2 className="text-[14px] font-semibold text-ink">{title}</h2>
      {help && <p className="mt-1 text-[13px] text-muted">{help}</p>}
      <div className="mt-4">{children}</div>
    </section>
  )
}

// One setting of the Tercihler card. Without a label the control brings its
// own (a Field); the label text matches Field's.
function Row({ label, help, children }: { label?: string; help: string; children: ReactNode }) {
  return (
    <div className="py-4 first:pt-0 last:pb-0">
      {label && <p className="text-[13px] font-medium text-ink-2">{label}</p>}
      <div className={label ? 'mt-1.5' : undefined}>{children}</div>
      <p className="mt-1 text-[12px] text-muted">{help}</p>
    </div>
  )
}

function ReportingRow() {
  const { data: me } = useMe()
  const mutation = useUpdateReportingCurrency(() => toast(t('account.reportingCurrency.saved')))

  if (!me) {
    return null
  }

  return (
    <Row help={t('account.reportingHelp')}>
      <Field
        label={t('account.reportingCurrency')}
        hint={mutation.isPending ? t('account.reportingCurrency.saving') : undefined}
        error={mutation.isError ? mutation.error.message : undefined}
      >
        <Select
          value={me.reporting_currency}
          // Not disabled while saving: Chromium blurs a focused control that
          // becomes disabled, dropping a keyboard user to <body> (GLG-905).
          // The "saving" hint says a save runs; a pick meanwhile is ignored.
          onChange={(currency) => {
            if (!mutation.isPending) mutation.mutate(currency)
          }}
          options={reportingCurrencies.map((currency) => ({
            value: currency,
            label: t(`currency.${currency}`),
          }))}
          className="max-w-xs"
        />
      </Field>
    </Row>
  )
}

function PasswordSection() {
  const { data: me } = useMe()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newPasswordAgain, setNewPasswordAgain] = useState('')
  // Set on submit, cleared on any edit of either new-password field: the
  // native minLength check can block the next submit, and a stale mismatch
  // error would sit next to it (GLG-384).
  const [mismatch, setMismatch] = useState(false)

  const mutation = useChangePassword(currentPassword, newPassword, () => {
    setCurrentPassword('')
    setNewPassword('')
    setNewPasswordAgain('')
    toast(t('account.success'))
  })

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // A typo in the new password locks the user out: there is no self-service
    // reset, only the operator running -set-password (GLG-126). So confirm it
    // here and never send a password the user typed only once.
    if (newPassword !== newPasswordAgain) {
      setMismatch(true)
      mutation.reset()
      return
    }
    setMismatch(false)
    mutation.mutate()
  }

  // The server's "password must ..." rejections are about the new password,
  // so they go under that field; anything else, "current password is
  // incorrect" above all, stays under the current one (GLG-477).
  const serverError = mutation.error instanceof ApiError ? mutation.error.body.error : undefined
  const newPasswordRejected =
    typeof serverError === 'string' && serverError.startsWith('password must ')
  const errorMessage = mutation.isError ? mutation.error.message : undefined

  return (
    <Card title={t('account.section.password')} help={t('account.passwordHelp')}>
      {/* A server error is about the values sent: any edit drops it, as on
          the signed-out forms (GLG-1075). Only an error: a reset while
          pending would drop the success that clears the fields. */}
      <form
        onSubmit={handleSubmit}
        onChange={() => mutation.isError && mutation.reset()}
        className="flex max-w-sm flex-col gap-4"
      >
        {/* Password managers tie the new password to this account (GLG-1078). */}
        <input
          type="email"
          name="username"
          autoComplete="username"
          value={me?.email ?? ''}
          readOnly
          hidden
        />
        <Field
          label={t('account.currentPassword')}
          error={newPasswordRejected ? undefined : errorMessage}
        >
          <input
            type="password"
            name="current-password"
            autoComplete="current-password"
            required
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
            className={inputClass()}
          />
        </Field>
        <Field
          label={t('account.newPassword')}
          hint={t('auth.passwordHint')}
          error={newPasswordRejected ? errorMessage : undefined}
        >
          <input
            type="password"
            name="new-password"
            autoComplete="new-password"
            required
            minLength={8}
            value={newPassword}
            onChange={(event) => {
              setNewPassword(event.target.value)
              setMismatch(false)
            }}
            className={inputClass()}
          />
        </Field>
        <Field
          label={t('account.newPasswordAgain')}
          error={mismatch ? t('account.passwordMismatch') : undefined}
        >
          <input
            type="password"
            name="new-password-again"
            autoComplete="new-password"
            required
            value={newPasswordAgain}
            onChange={(event) => {
              setNewPasswordAgain(event.target.value)
              setMismatch(false)
            }}
            className={inputClass()}
          />
        </Field>
        <Button type="submit" pending={mutation.isPending} className="self-start">
          {t('account.submit')}
        </Button>
      </form>
    </Card>
  )
}

function LanguageRow() {
  const locale = useLocale()

  // The only place to change the language once signed in (the header has no
  // switch since GLG-572). The auth and setup pages keep their own
  // LanguageSwitch on the same store, so neither needs to know about the other.
  return (
    <Row label={t('account.section.language')} help={t('account.languageHelp')}>
      <div
        role="group"
        aria-label={t('account.section.language')}
        className={`${segmentedTrack} w-fit`}
      >
        {locales.map((code) => (
          <button
            key={code}
            type="button"
            aria-pressed={code === locale}
            onClick={() => setLocale(code)}
            className={`${segmentedItem(code === locale)} max-md:min-h-11`}
          >
            {t(`language.${code}`)}
          </button>
        ))}
      </div>
    </Row>
  )
}

function AppearanceRow() {
  const theme = useTheme()

  // Applies on click: the tokens in index.css do the rest, so there is nothing
  // to save and nothing to confirm.
  return (
    <Row label={t('account.section.appearance')} help={t('account.appearanceHelp')}>
      <Segmented
        options={themes}
        value={theme}
        onChange={setTheme}
        label={(name) => t(`theme.${name}`)}
        aria-label={t('account.section.appearance')}
        className="w-fit"
        itemClassName="max-md:min-h-11"
      />
    </Row>
  )
}

export function AccountPage() {
  useTitle(t('account.heading'))
  // The portfolio pages' column (GLG-968): left-aligned beside the sidebar,
  // 24 px gutter at every width.
  return (
    <main id="main">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        <PageHeader title={t('account.heading')} />
        {/* Below lg the h1 is sr-only, so the wrapper's top padding is the gap. */}
        <div className="grid gap-4 lg:mt-6 lg:grid-cols-2 lg:items-start">
          <Card title={t('account.section.preferences')}>
            <div className="divide-y divide-line">
              <ReportingRow />
              <LanguageRow />
              <AppearanceRow />
            </div>
          </Card>
          <PasswordSection />
        </div>
      </div>
    </main>
  )
}
