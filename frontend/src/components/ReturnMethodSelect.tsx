import { RETURN_METHODS, type ReturnMethod } from '../api/history'
import { t } from '../i18n/strings'
import { FitSelect } from './ui/Select'

// The return method's select, shared by Özet, Performans and the position's
// return card.
export function ReturnMethodSelect({
  method,
  onChange,
}: {
  method: ReturnMethod
  onChange: (next: ReturnMethod) => void
}) {
  return (
    <FitSelect
      aria-label={t('chart.return.method')}
      value={method}
      onChange={onChange}
      options={RETURN_METHODS.map((m) => ({ value: m, label: t(`chart.return.${m}`) }))}
    />
  )
}
