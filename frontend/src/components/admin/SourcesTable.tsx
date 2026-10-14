import { useState } from 'react'
import { useProviders } from '../../api/providers'
import {
  isRunning,
  isStale,
  useBackfill,
  useRunSource,
  useSaveSource,
  useSources,
  type SourceStatus,
} from '../../api/sources'
import { getLocale, t, type Locale, type StringKey } from '../../i18n/strings'
import { formatMarketDate } from '../../lib/date'
import { formatNumber } from '../../lib/format'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { ActionsTh } from '../ui/ActionsTh'
import { Button } from '../ui/Button'
import { Checkbox } from '../ui/Checkbox'
import { inputClass } from '../ui/inputClass'
import { Notice } from '../ui/Notice'
import { Pill } from '../ui/Pill'
import { TABLE, TH } from '../ui/table'
import { toastQueued } from './toastQueued'

const descriptions: Record<string, StringKey> = {
  bist: 'admin.sources.desc.bist',
  'bist-splits': 'admin.sources.desc.bist-splits',
  tcmb: 'admin.sources.desc.tcmb',
  us: 'admin.sources.desc.us',
  crypto: 'admin.sources.desc.crypto',
  tefas: 'admin.sources.desc.tefas',
  tufe: 'admin.sources.desc.tufe',
  xu100: 'admin.sources.desc.xu100',
  gold: 'admin.sources.desc.gold',
}

const relativeFormats: Record<Locale, Intl.RelativeTimeFormat> = {
  tr: new Intl.RelativeTimeFormat('tr', { numeric: 'auto' }),
  en: new Intl.RelativeTimeFormat('en', { numeric: 'auto' }),
}

// "2 saat önce" / "2 hours ago" for a past instant.
function relative(iso: string): string {
  const seconds = (Date.parse(iso) - Date.now()) / 1000
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  const [unit, size] = units.find(([, s]) => Math.abs(seconds) >= s) ?? ['second', 1]
  return relativeFormats[getLocale()].format(Math.round(seconds / size), unit)
}

function absolute(iso: string): string {
  return formatMarketDate(iso, {
    timeZone: 'Europe/Istanbul',
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

// "4 Eki 14:05" in Istanbul time, with the year when it is not this year's.
// A stale pill shows this: 47 h and 49 h back both read "evvelsi gün", so
// the relative text could not say why only one is flagged.
function exact(iso: string): string {
  const year = (at: string) =>
    formatMarketDate(at, { timeZone: 'Europe/Istanbul', year: 'numeric' })
  return formatMarketDate(iso, {
    timeZone: 'Europe/Istanbul',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    ...(year(iso) === year(new Date().toISOString()) ? {} : { year: 'numeric' }),
  })
}

// A compact-table cell from md up (ui/table.ts's TD); below md the row is a
// grid and its cells are plain grid items.
const CELL = 'md:border-b md:border-paper md:px-2 md:whitespace-nowrap md:first:pl-4 md:last:pr-4'

// The sources as one compact table; below md each source is a list item: the
// source and its last success, then its time, then its actions. Fetches its own data and takes no props, so setup's step 2 can render
// it as Yönetim does.
export function SourcesTable() {
  const { data: sources, isError, error } = useSources()
  const { data: providers } = useProviders()
  // Only the US providers feed the us row; EVDS feeds the BIST 100 benchmark.
  const missingKeys = (providers ?? [])
    .filter((provider) => ['finnhub', 'tiingo'].includes(provider.name) && !provider.configured)
    .map((provider) => provider.name.charAt(0).toUpperCase() + provider.name.slice(1))

  if (isError) {
    return (
      <p role="alert" className="text-loss px-4 py-3 text-[13px]">
        {error.message}
      </p>
    )
  }
  if (!sources) return null

  const now = new Date()
  return (
    <table className={`${TABLE} max-md:block`}>
      <thead className="max-md:hidden">
        <tr>
          <th className={TH}>{t('admin.sources.source')}</th>
          <th className={TH}>{t('admin.sources.enabled')}</th>
          <th className={TH}>{t('admin.sources.at')}</th>
          <th className={TH}>{t('admin.sources.lastSuccess')}</th>
          <ActionsTh />
        </tr>
      </thead>
      <tbody className="max-md:block">
        {sources.map((source) => (
          <SourceRow
            key={source.name}
            source={source}
            stale={isStale(source, now)}
            missingKeys={source.name === 'us' ? missingKeys : []}
          />
        ))}
      </tbody>
    </table>
  )
}

function SourceRow({
  source,
  stale,
  missingKeys,
}: {
  source: SourceStatus
  stale: boolean
  missingKeys: string[]
}) {
  const [at, setAt] = useState(source.at)

  const save = useSaveSource(source.name)
  const run = useRunSource(source.name)
  const fill = useBackfill(source.name, toastQueued)
  // One per action: each sends its request once per click, and the
  // three still run side by side.
  const saveOnce = useOneAtATime()
  const runOnce = useOneAtATime()
  const fillOnce = useOneAtATime()
  const actionError = save.error ?? run.error ?? fill.error

  // One error line per row; any new action clears it.
  function resetErrors() {
    save.reset()
    run.reset()
    fill.reset()
  }

  const last = source.last_run
  const description = descriptions[source.name]
  const success = source.last_success_at
  const when = success ? (stale ? exact : relative)(success) : t('admin.sources.never')
  const notice = source.enabled && missingKeys.length > 0
  // TEFAS is opt-in: its endpoint is disallowed by the site's robots.txt.
  const tefasNote = source.name === 'tefas'
  // A failed run, an action error, the missing key note or the TEFAS note
  // sits in a row of its own under the source, which then carries the row's
  // bottom border.
  const extra = Boolean(last?.error || actionError || notice || tefasNote)
  const border = extra ? '' : 'md:border-b md:border-paper'

  return (
    <>
      <tr
        className={`max-md:grid max-md:grid-cols-[auto_minmax(0,1fr)_auto] max-md:items-center max-md:gap-x-3 max-md:gap-y-2 max-md:px-4 max-md:py-3 ${extra ? '' : 'max-md:border-b max-md:border-paper'} md:h-11`}
      >
        {/* From md the source column takes the width left over, and its
            description truncates rather than pushing the actions onto two
            lines. */}
        <td
          className={`${CELL} ${border} py-1.5 max-md:col-start-2 max-md:row-start-1 md:w-full md:max-w-0`}
        >
          <span className="flex items-center gap-2">
            <span className="text-ink font-medium">{source.name}</span>
            {isRunning(source) && <Pill tone="cash">{t('admin.sources.running')}</Pill>}
          </span>
          {description && (
            <span className="text-muted block truncate text-[11px]">{t(description)}</span>
          )}
        </td>
        <td className={`${CELL} ${border} max-md:col-start-1 max-md:row-start-1`}>
          {/* A 44 px tap target around the 16 px box; the negative margin
              keeps the row's layout as it was. */}
          <label className="-m-3.5 flex size-11 cursor-pointer items-center justify-center">
            <Checkbox
              aria-label={t('admin.sources.enabledLabel', { source: source.name })}
              checked={source.enabled}
              pending={save.isPending}
              onChange={(checked) => {
                saveOnce(() => {
                  resetErrors()
                  return save.mutateAsync({ enabled: checked, at: source.at })
                })
              }}
            />
          </label>
        </td>
        <td className={`${CELL} ${border} max-md:col-span-3 max-md:row-start-2`}>
          <span className="flex items-center gap-2">
            <span className="text-muted text-[12px] md:hidden">{t('admin.sources.at')}</span>
            {/* At least 112 px, and as wide as the native control's content:
                Chromium lays it out by the browser locale, not <html lang>,
                and a 12-hour one adds AM/PM, about 120 px. */}
            <span className="min-w-28">
              <input
                type="time"
                aria-label={t('admin.sources.atLabel', { source: source.name })}
                value={at}
                onChange={(event) => {
                  setAt(event.target.value)
                  resetErrors()
                }}
                onBlur={() => {
                  if (at !== source.at) save.mutate({ enabled: source.enabled, at })
                }}
                // h-10, not the h-9 it once appended: that one lost to inputClass's
                // h-10 in the stylesheet, so h-10 is what the row has always drawn.
                className={`${inputClass()} min-w-max`}
              />
            </span>
          </span>
        </td>
        <td
          data-testid="last-success"
          className={`${CELL} ${border} py-1.5 max-md:col-start-3 max-md:row-start-1 max-md:text-right`}
        >
          {/* Below md there is no header row to name the column. */}
          <span className="text-muted block text-[11px] md:hidden">
            {t('admin.sources.lastSuccess')}
          </span>
          <span title={source.last_success_at ? absolute(source.last_success_at) : undefined}>
            {stale ? <Pill tone="warn">{when}</Pill> : <span className="text-ink-2">{when}</span>}
          </span>
          {/* A failed run has no row count; its line sits under the row. */}
          {last?.finished_at && !last.error && (
            <span title={absolute(last.finished_at)} className="text-muted block text-[11px]">
              {t('admin.sources.lastRows', { count: formatNumber(last.rows) })}
            </span>
          )}
        </td>
        <td className={`${CELL} ${border} max-md:col-span-3 max-md:row-start-3 md:text-right`}>
          <span className="inline-flex gap-2 max-md:flex-wrap">
            <Button
              variant="secondary"
              aria-label={t('admin.sources.runNowLabel', { source: source.name })}
              pending={run.isPending}
              onClick={() => {
                runOnce(() => {
                  resetErrors()
                  return run.mutateAsync()
                })
              }}
            >
              {t('admin.sources.runNow')}
            </Button>
            {source.can_backfill && (
              <Button
                variant="secondary"
                aria-label={t('admin.sources.backfillOneLabel', { source: source.name })}
                pending={fill.isPending}
                onClick={() => {
                  fillOnce(() => {
                    resetErrors()
                    return fill.mutateAsync()
                  })
                }}
              >
                {t('admin.sources.backfillOne')}
              </Button>
            )}
          </span>
        </td>
      </tr>
      {extra && (
        <tr className="max-md:border-paper max-md:block max-md:border-b">
          <td
            colSpan={5}
            className="space-y-2 px-4 pb-3 whitespace-normal max-md:block md:border-b md:border-paper"
          >
            {last?.error && (
              <details className="text-loss text-[13px]">
                <summary
                  className="cursor-pointer"
                  aria-label={t('admin.sources.lastFailedLabel', { source: source.name })}
                >
                  {t('admin.sources.lastFailed')}
                </summary>
                <p className="text-ink-2 mt-1 text-[12px] break-words whitespace-normal">
                  {last.error}
                </p>
              </details>
            )}
            {actionError && (
              <p role="alert" className="text-loss text-[13px]">
                {actionError.message}
              </p>
            )}
            {tefasNote && <p className="text-muted text-[12px]">{t('admin.sources.tefasNote')}</p>}
            {notice && (
              <Notice>
                {t('admin.sources.usKeysMissing', { providers: missingKeys.join(', ') })}{' '}
                <a href="#admin-providers" className="font-medium underline">
                  {t('admin.sources.addKeys')}
                </a>
              </Notice>
            )}
          </td>
        </tr>
      )}
    </>
  )
}
