import { BROKERS, type useParseStatement } from '../../api/imports'
import { getLocale, t, type Locale } from '../../i18n/strings'
import { Notice } from '../ui/Notice'
import { Select } from '../ui/Select'
import { DropZone, type Refused } from './DropZone'
import { ParseError } from './ImportQueue'

const listFormats: Record<Locale, Intl.ListFormat> = {
  tr: new Intl.ListFormat('tr', { type: 'conjunction' }),
  en: new Intl.ListFormat('en', { type: 'conjunction' }),
}

// The brokers "Otomatik" tells apart: every PDF one (GLG-764).
function recognised(): string {
  return listFormats[getLocale()].format(
    BROKERS.filter((broker) => broker.key !== 'csv').map((broker) => broker.label),
  )
}

// Step 1 of the import page: the broker and the drop zone, with what the
// upfront parse says while it runs or once it failed.
export function UploadCard({
  broker,
  onBroker,
  onFiles,
  parse,
  portfolioId,
}: {
  broker: string
  onBroker: (broker: string) => void
  onFiles: (chosen: File[], dropped: Refused[]) => void
  parse: ReturnType<typeof useParseStatement>
  portfolioId: number
}) {
  return (
    <section aria-labelledby="import-upload" className="card p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="import-upload" tabIndex={-1} className="text-[14px] font-semibold text-ink">
          {t('import.uploadTitle')}
        </h2>
        <Select
          aria-label={t('import.broker')}
          aria-describedby="import-brokers"
          className="sm:w-56"
          value={broker}
          onChange={onBroker}
          options={[
            { value: 'auto', label: t('import.brokerAuto') },
            ...BROKERS.map((option) => ({ value: option.key, label: option.label })),
          ]}
        />
      </div>
      <div className="mt-3">
        <DropZone
          accept={broker === 'csv' ? '.csv,text/csv' : 'application/pdf'}
          multiple
          disabled={parse.isPending}
          onFiles={onFiles}
          hint={t(broker === 'csv' ? 'import.drop.csvHint' : 'import.drop.pdfHint')}
        />
      </div>
      {parse.isPending && (
        <p role="status" className="mt-2 text-[13px] text-muted">
          {t('import.parsing')}
        </p>
      )}
      {parse.isError && (
        <div className="mt-3">
          <Notice role="alert">
            <ParseError error={parse.error} portfolioId={portfolioId} />
          </Notice>
        </div>
      )}
      {/* Both Garanti PDFs are titled the same, so each says which
        one it takes (GLG-700). */}
      <p id="import-brokers" className="mt-3 text-[12px] text-muted">
        {broker === 'garanti'
          ? t('import.garanti.fileHint')
          : broker === 'garanti_yatirim'
            ? t('import.garantiYatirim.fileHint')
            : t('import.recognised', { brokers: recognised() })}
      </p>
      <details className="mt-2 text-[12px] text-muted">
        <summary className="cursor-pointer text-accent">{t('import.csvFormat')}</summary>
        <p className="mt-1">{t('import.csv.fileHint')}</p>
      </details>
    </section>
  )
}
