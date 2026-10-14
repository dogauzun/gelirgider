import { t } from '../../i18n/strings'
import { toast } from '../../lib/toast'

// The toast after a backfill request: the sources queued, or none.
export function toastQueued(queued: string[]) {
  toast(
    queued.length
      ? t('admin.sources.backfillQueued', { sources: queued.join(', ') })
      : t('admin.sources.backfillNone'),
  )
}
