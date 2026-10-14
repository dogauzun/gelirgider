import { locales, setLocale, t, useLocale } from '../i18n/strings'
import { segmentedItem, segmentedTrack } from './ui/segmentedClass'

// Two segments of the UI v2 segmented control (ADR 0023), no dropdown.
// `type="button"` matters because the switch can sit inside a form.
export function LanguageSwitch({ className = '' }: { className?: string }) {
  const locale = useLocale()

  return (
    <div className={`${segmentedTrack} ${className}`}>
      {locales.map((code) => (
        <button
          key={code}
          type="button"
          aria-pressed={code === locale}
          onClick={() => setLocale(code)}
          className={`${segmentedItem(code === locale)} whitespace-nowrap`}
        >
          {t(`language.${code}`)}
        </button>
      ))}
    </div>
  )
}
