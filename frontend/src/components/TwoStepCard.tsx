import { useMemo, useState, type ComponentProps, type FormEvent } from 'react'
import { encode } from 'uqr'
import { ApiError } from '../api'
import { useMe, useTotpConfirm, useTotpDisable, useTotpSetup } from '../api/auth'
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
  ...aria
}: { value: string; onChange: (code: string) => void } & Omit<
  ComponentProps<'input'>,
  'value' | 'onChange'
>) {
  return (
    <input
      {...aria}
      type="text"
      inputMode="numeric"
      autoComplete="one-time-code"
      required
      maxLength={6}
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

function EnableFlow() {
  const [asking, setAsking] = useState(false)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const setup = useTotpSetup(password)
  const confirm = useTotpConfirm(code, () => toast(t('account.totp.enabledToast')))

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

function DisableFlow() {
  const [asking, setAsking] = useState(false)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const disable = useTotpDisable(password, code, () => toast(t('account.totp.disabledToast')))

  if (!asking) {
    return (
      <Button variant="secondary" onClick={() => setAsking(true)} className="self-start">
        {t('account.totp.disable')}
      </Button>
    )
  }

  // The wrong password is the field above; any other rejection is the code.
  const wrongPassword =
    disable.error instanceof ApiError &&
    disable.error.body.error === 'current password is incorrect'
  const message = disable.isError ? disable.error.message : undefined

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        disable.mutate()
      }}
      onChange={() => disable.isError && disable.reset()}
      className="flex max-w-sm flex-col gap-4"
    >
      <Field label={t('account.currentPassword')} error={wrongPassword ? message : undefined}>
        <PasswordInput value={password} onChange={setPassword} />
      </Field>
      <Field label={t('account.totp.code')} error={wrongPassword ? undefined : message}>
        <CodeInput value={code} onChange={setCode} />
      </Field>
      <div className="flex gap-2">
        <Button type="submit" variant="danger" pending={disable.isPending}>
          {t('account.totp.disableSubmit')}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setAsking(false)
            setPassword('')
            setCode('')
            disable.reset()
          }}
        >
          {t('confirm.cancel')}
        </Button>
      </div>
    </form>
  )
}

// The flows are separate components so a finished one unmounts with its
// secret, password and code: `totp_enabled` on /api/me swaps them.
export function TwoStepCard() {
  const { data: me } = useMe()
  if (!me) return null

  return (
    <section className="card p-5">
      <h2 className="text-[14px] font-semibold text-ink">{t('account.totp')}</h2>
      <p className="mt-1 text-[13px] text-muted">{t('account.totp.help')}</p>
      <div className="mt-4 flex flex-col gap-4">
        <p className="text-[13px] font-medium text-ink-2">
          {me.totp_enabled ? t('account.totp.statusOn') : t('account.totp.statusOff')}
        </p>
        {me.totp_enabled ? <DisableFlow /> : <EnableFlow />}
      </div>
    </section>
  )
}
