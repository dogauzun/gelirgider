import { useRef, useState } from 'react'
import {
  useProviders,
  useSaveProviderKey,
  useTestProviderKey,
  type ProviderStatus,
} from '../../api/providers'
import { t, type StringKey } from '../../i18n/strings'
import { Button } from '../ui/Button'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { inputClass } from '../ui/inputClass'
import { toast } from '../../lib/toast'
import { focusIfLost } from '../../lib/useFocusLater'
import { useOneAtATime } from '../../lib/useOneAtATime'

// Brand names stay as they are; what each provider feeds is translated.
const providerInfo: Record<string, { label: string; use: StringKey }> = {
  finnhub: { label: 'Finnhub', use: 'admin.providers.finnhubUse' },
  tiingo: { label: 'Tiingo', use: 'admin.providers.tiingoUse' },
  evds: { label: 'TCMB EVDS', use: 'admin.providers.evdsUse' },
}

// The `error` of a failed key test (GLG-308).
const testErrorKeys: Record<string, StringKey> = {
  'invalid key': 'admin.providers.testInvalidKey',
  'rate limited': 'admin.providers.testRateLimited',
  unreachable: 'admin.providers.testUnreachable',
}

// Takes no props, so the setup wizard's step 2 renders it as is (GLG-316).
export function ProvidersSection() {
  const { data: providers, isError, error } = useProviders()

  // The id is the target of the sources section's missing key note; the
  // scroll margin keeps the heading clear of the sticky header.
  return (
    <section id="admin-providers" className="card scroll-mt-20">
      <h2 className="text-ink border-line border-b px-4 py-3 text-[14px] font-semibold">
        {t('admin.section.providers')}
      </h2>
      {isError && (
        <p role="alert" className="text-loss px-4 py-3 text-[13px]">
          {error.message}
        </p>
      )}
      {providers && (
        <ul className="px-4">
          {providers.map((provider) => (
            <ProviderRow key={provider.name} provider={provider} />
          ))}
        </ul>
      )}
    </section>
  )
}

function ProviderRow({ provider }: { provider: ProviderStatus }) {
  const [typed, setTyped] = useState('')
  const [clearing, setClearing] = useState(false)
  const info = providerInfo[provider.name]
  const label = info?.label ?? provider.name

  const inputRef = useRef<HTMLInputElement>(null)
  const saveRef = useRef<HTMLButtonElement>(null)
  const clearRef = useRef<HTMLButtonElement>(null)

  const test = useTestProviderKey(provider.name, typed)
  // A save empties the input and so disables Kaydet; a clear disables
  // Temizle once the refetch reports no key. A focused button that turns
  // disabled drops focus to <body>, so it moves to the key input first,
  // unless the admin has moved on (GLG-1308).
  const save = useSaveProviderKey(provider.name, typed, () => {
    setTyped('')
    focusIfLost(inputRef.current, saveRef.current)
    toast(t('admin.providers.saved', { provider: label }))
  })
  const clear = useSaveProviderKey(provider.name, '', () => {
    focusIfLost(inputRef.current, clearRef.current)
    toast(t('admin.providers.cleared', { provider: label }))
  })
  // One per action, so a save and a test still run side by side (GLG-1292).
  const saveOnce = useOneAtATime()
  const testOnce = useOneAtATime()
  const actionError = save.error ?? clear.error ?? test.error

  // One result line per row; any new action or keystroke clears it.
  function resetResults() {
    test.reset()
    save.reset()
    clear.reset()
  }

  return (
    <li className="border-line border-b py-4 last:border-0">
      {/* Below md the status always sits under the description; wrapping
          only when the description is long laid the rows out unevenly. */}
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 max-md:flex-col">
        <div>
          <span className="text-ink block text-[14px] font-medium">{label}</span>
          {info && <span className="text-muted block text-[12px]">{t(info.use)}</span>}
        </div>
        {provider.error === 'undecryptable' ? (
          <span className="text-warn text-[13px]">{t('admin.providers.undecryptable')}</span>
        ) : (
          <span className={`text-[13px] ${provider.configured ? 'text-gain' : 'text-muted'}`}>
            {provider.configured
              ? `${t('admin.providers.configured')}${provider.last4 ? ` ••••${provider.last4}` : ''}`
              : t('admin.providers.notConfigured')}
          </span>
        )}
      </div>

      <form
        onSubmit={(event) => {
          event.preventDefault()
          saveOnce(() => {
            resetResults()
            return typed.trim() ? save.mutateAsync() : Promise.resolve()
          })
        }}
        className="mt-3 flex max-w-2xl flex-col gap-2 md:flex-row"
      >
        <input
          ref={inputRef}
          type="password"
          autoComplete="off"
          aria-label={t('admin.providers.key', { provider: label })}
          placeholder={t('admin.providers.keyPlaceholder')}
          value={typed}
          onChange={(event) => {
            setTyped(event.target.value)
            resetResults()
          }}
          className={`${inputClass()} md:flex-1`}
        />
        <div className="flex shrink-0 gap-2">
          <Button
            ref={saveRef}
            type="submit"
            aria-label={t('admin.providers.saveLabel', { provider: label })}
            disabled={!typed.trim()}
            pending={save.isPending}
          >
            {t('admin.providers.save')}
          </Button>
          <Button
            variant="secondary"
            aria-label={t('admin.providers.testLabel', { provider: label })}
            pending={test.isPending}
            onClick={() =>
              testOnce(() => {
                resetResults()
                return test.mutateAsync()
              })
            }
          >
            {t('admin.providers.test')}
          </Button>
          <Button
            ref={clearRef}
            variant="ghost"
            aria-label={t('admin.providers.clearLabel', { provider: label })}
            disabled={!provider.configured}
            pending={clear.isPending}
            onClick={() => {
              resetResults()
              setClearing(true)
            }}
          >
            {t('admin.providers.clear')}
          </Button>
        </div>
      </form>

      {test.data && (
        <p role="status" className={`mt-2 text-[13px] ${test.data.ok ? 'text-gain' : 'text-loss'}`}>
          {test.data.ok
            ? t('admin.providers.testOk')
            : t(testErrorKeys[test.data.error ?? ''] ?? 'error.unknown')}
        </p>
      )}
      {actionError && (
        <p role="alert" className="text-loss mt-2 text-[13px]">
          {actionError.message}
        </p>
      )}

      <ConfirmDialog
        open={clearing}
        danger
        title={t('admin.providers.clearTitle', { provider: label })}
        body={t('admin.providers.clearBody', { provider: label })}
        confirmLabel={t('admin.providers.clearConfirm', { provider: label })}
        onConfirm={() => {
          setClearing(false)
          clear.mutate()
        }}
        onCancel={() => setClearing(false)}
      />
    </li>
  )
}
