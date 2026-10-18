import { useMemo, useState, type ComponentProps, type FormEvent } from 'react'
import type { UseMutationResult } from '@tanstack/react-query'
import { encode } from 'uqr'
import { ApiError } from '../api'
import { useMe, useTotpConfirm, useTotpDisable, useTotpRegenerate, useTotpSetup } from '../api/auth'
import { t } from '../i18n/strings'
import { toast } from '../lib/toast'
import { Button } from './ui/Button'
import { Field } from './ui/Field'
import { inputClass } from './ui/inputClass'

// Drawn here from the module matrix, never fetched: the secret stays on the
// page. Fixed black on white, whatever the theme: scanners read dark on light.
function QrCode({ text }: { text: string }) {
  const { data, size } = useMemo(() => encode(text, { ecc: 'M', border: 4 }), [text])
  const path = data
    .flatMap((row, y) => row.map((dark, x) => (dark ? `M${x} ${y}h1v1h-1z` : '')))
    .join('')
  return (
    <svg
      role="img"
      aria-label={t('account.totp.qr')}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className="h-48 w-48 rounded-ctl"
    >
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  )
}

const selectAll = (event: { currentTarget: HTMLInputElement }) => event.currentTarget.select()

// Field puts its id and aria props on its child, so these pass them on.
function CodeInput({
  value,
  onChange,
  recovery = false,
  ...aria
}: { value: string; onChange: (code: string) => void; recovery?: boolean } & Omit<
  ComponentProps<'input'>,
  'value' | 'onChange'
>) {
  return (
    <input
      {...aria}
      type="text"
      // A recovery code is 11 letters and digits: no cap, no numeric pad.
      inputMode={recovery ? undefined : 'numeric'}
      autoComplete="one-time-code"
      required
      maxLength={recovery ? undefined : 6}
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className={inputClass()}
    />
  )
}

function PasswordInput({
  value,
  onChange,
  ...aria
}: { value: string; onChange: (password: string) => void } & Omit<
  ComponentProps<'input'>,
  'value' | 'onChange'
>) {
  return (
    <input
      {...aria}
      type="password"
      autoComplete="current-password"
      required
      autoFocus
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className={inputClass()}
    />
  )
}

function EnableFlow({ onCodes }: { onCodes: (codes: string[]) => void }) {
  const [asking, setAsking] = useState(false)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const setup = useTotpSetup(password)
  const confirm = useTotpConfirm(code, onCodes, () => toast(t('account.totp.enabledToast')))

  function askPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setup.mutate(undefined, { onSuccess: () => setPassword('') })
  }

  if (setup.data) {
    return (
      <form
        onSubmit={(event) => {
          event.preventDefault()
          confirm.mutate()
        }}
        onChange={() => confirm.isError && confirm.reset()}
        className="flex max-w-sm flex-col gap-4"
      >
        <p className="text-[13px] text-muted">{t('account.totp.scan')}</p>
        <QrCode text={setup.data.uri} />
        <Field label={t('account.totp.uri')}>
          <input
            readOnly
            value={setup.data.uri}
            onFocus={selectAll}
            className={inputClass({ readOnly: true })}
          />
        </Field>
        <Field label={t('account.totp.secret')}>
          <input
            readOnly
            value={setup.data.secret}
            onFocus={selectAll}
            className={inputClass({ readOnly: true })}
          />
        </Field>
        <Field
          label={t('account.totp.code')}
          error={confirm.isError ? confirm.error.message : undefined}
        >
          <CodeInput value={code} onChange={setCode} />
        </Field>
        <Button type="submit" pending={confirm.isPending} className="self-start">
          {t('account.totp.confirm')}
        </Button>
      </form>
    )
  }

  if (!asking) {
    return (
      <Button onClick={() => setAsking(true)} className="self-start">
        {t('account.totp.enable')}
      </Button>
    )
  }

  return (
    <form
      onSubmit={askPassword}
      onChange={() => setup.isError && setup.reset()}
      className="flex max-w-sm flex-col gap-4"
    >
      <Field
        label={t('account.currentPassword')}
        error={setup.isError ? setup.error.message : undefined}
      >
        <PasswordInput value={password} onChange={setPassword} />
      </Field>
      <div className="flex gap-2">
        <Button type="submit" pending={setup.isPending}>
          {t('account.totp.continue')}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setAsking(false)
            setPassword('')
            setup.reset()
          }}
        >
          {t('confirm.cancel')}
        </Button>
      </div>
    </form>
  )
}

// A password and a code ask before a change that needs them: turning two-step
// off, or replacing the recovery codes. `useAction` gets both and `onCodes`,
// which only the regenerate action uses.
function ReauthFlow({
  openLabel,
  submitLabel,
  submitVariant,
  useAction,
  onCodes,
}: {
  openLabel: string
  submitLabel: string
  submitVariant: 'danger' | 'primary'
  useAction: (
    password: string,
    code: string,
    onCodes: (codes: string[]) => void,
  ) => UseMutationResult<unknown, Error, void>
  onCodes: (codes: string[]) => void
}) {
  const [asking, setAsking] = useState(false)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const action = useAction(password, code, onCodes)

  if (!asking) {
    return (
      <Button variant="secondary" onClick={() => setAsking(true)} className="self-start">
        {openLabel}
      </Button>
    )
  }

  // The wrong password is the field above; any other rejection is the code.
  const wrongPassword =
    action.error instanceof ApiError && action.error.body.error === 'current password is incorrect'
  const message = action.isError ? action.error.message : undefined

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        action.mutate()
      }}
      onChange={() => action.isError && action.reset()}
      className="flex max-w-sm flex-col gap-4"
    >
      <Field label={t('account.currentPassword')} error={wrongPassword ? message : undefined}>
        <PasswordInput value={password} onChange={setPassword} />
      </Field>
      <Field label={t('account.totp.codeOrRecovery')} error={wrongPassword ? undefined : message}>
        <CodeInput recovery value={code} onChange={setCode} />
      </Field>
      <div className="flex gap-2">
        <Button type="submit" variant={submitVariant} pending={action.isPending}>
          {submitLabel}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setAsking(false)
            setPassword('')
            setCode('')
            action.reset()
          }}
        >
          {t('confirm.cancel')}
        </Button>
      </div>
    </form>
  )
}

const useDisableAction = (password: string, code: string) =>
  useTotpDisable(password, code, () => toast(t('account.totp.disabledToast')))

// The codes live in the card's state alone: no query, storage or URL.
function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  // Awaited inside try: `navigator.clipboard` is undefined outside a secure
  // context, which throws before any promise exists.
  async function copy() {
    try {
      await navigator.clipboard.writeText(codes.join('\n'))
      toast(t('account.totp.codes.copied'))
    } catch {
      toast(t('account.totp.codes.copyFailed'), { error: true })
    }
  }

  return (
    <div className="flex max-w-sm flex-col gap-4">
      <p className="text-[13px] font-medium text-warn">{t('account.totp.codes.warning')}</p>
      <p className="text-[13px] text-muted">{t('account.totp.codes.help')}</p>
      <ul
        aria-label={t('account.totp.codes')}
        className="grid grid-cols-2 gap-x-4 gap-y-1 text-[14px] font-semibold tracking-wide text-ink"
      >
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <div className="flex gap-2">
        <Button variant="secondary" onClick={() => void copy()}>
          {t('account.totp.codes.copy')}
        </Button>
        <Button onClick={onDone}>{t('account.totp.codes.done')}</Button>
      </div>
    </div>
  )
}

// The flows are separate components so a finished one unmounts with its
// secret, password and code: `totp_enabled` on /api/me swaps them. The
// recovery codes are held here, as the enrolment flow unmounts right after
// it receives them.
export function TwoStepCard() {
  const { data: me } = useMe()
  const [codes, setCodes] = useState<string[] | null>(null)
  if (!me) return null

  const flows = me.totp_enabled ? (
    <>
      <ReauthFlow
        openLabel={t('account.totp.regenerate')}
        submitLabel={t('account.totp.regenerateSubmit')}
        submitVariant="primary"
        useAction={useTotpRegenerate}
        onCodes={setCodes}
      />
      <ReauthFlow
        openLabel={t('account.totp.disable')}
        submitLabel={t('account.totp.disableSubmit')}
        submitVariant="danger"
        useAction={useDisableAction}
        onCodes={setCodes}
      />
    </>
  ) : (
    <EnableFlow onCodes={setCodes} />
  )

  return (
    <section className="card p-5">
      <h2 className="text-[14px] font-semibold text-ink">{t('account.totp')}</h2>
      <p className="mt-1 text-[13px] text-muted">{t('account.totp.help')}</p>
      <div className="mt-4 flex flex-col gap-4">
        <p className="text-[13px] font-medium text-ink-2">
          {me.totp_enabled ? t('account.totp.statusOn') : t('account.totp.statusOff')}
        </p>
        {codes ? <RecoveryCodes codes={codes} onDone={() => setCodes(null)} /> : flows}
      </div>
    </section>
  )
}
