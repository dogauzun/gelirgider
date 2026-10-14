import { t } from '../../i18n/strings'
import { TH } from './table'

// The header over a row's action buttons: no visible title, a label for
// screen readers.
export function ActionsTh() {
  return (
    <th className={TH}>
      <span className="sr-only">{t('table.actions')}</span>
    </th>
  )
}
